package glass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Keepalive sends periodic minimal requests to Anthropic to keep the
// system+tools prefix cached server-side.  When a real session starts,
// the prefix hits cc_read immediately instead of paying full cc_new.
//
// The keepalive captures the system prompt and tools array from the
// FIRST real request that passes through the Glass pipeline, then
// replays them with a dummy single-message body every interval.
type Keepalive struct {
	mu       sync.Mutex
	system   []interface{} // captured canonical system fragments
	tools    []interface{} // captured tools array
	model    string        // captured model name
	captured bool

	interval  time.Duration
	apiKey    string
	upstream  string // e.g. "https://api.anthropic.com"
	transport http.RoundTripper
	cancel    context.CancelFunc
}

// KeepaliveConfig holds settings for the keepalive goroutine.
type KeepaliveConfig struct {
	IntervalSec int               // seconds between keepalive pings (0 = disabled)
	APIKey      string            // Anthropic API key
	Upstream    string            // upstream URL (e.g. "https://api.anthropic.com")
	Transport   http.RoundTripper // HTTP transport to use
}

// NewKeepalive creates and starts a keepalive.  Returns nil if disabled.
func NewKeepalive(cfg KeepaliveConfig) *Keepalive {
	if cfg.IntervalSec <= 0 || cfg.APIKey == "" {
		log.Printf("[KEEPALIVE] Disabled (interval=%d, apiKey=%v)", cfg.IntervalSec, cfg.APIKey != "")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	ka := &Keepalive{
		interval:  time.Duration(cfg.IntervalSec) * time.Second,
		apiKey:    cfg.APIKey,
		upstream:  cfg.Upstream,
		transport: cfg.Transport,
		cancel:    cancel,
	}

	go ka.loop(ctx)
	log.Printf("[KEEPALIVE] Started — interval=%ds upstream=%s", cfg.IntervalSec, cfg.Upstream)
	return ka
}

// CapturePrefix records the system+tools+model from the first real request.
// Called by the Glass pipeline after Step 0 (system prompt processing).
// Thread-safe; only the first call has effect.
func (ka *Keepalive) CapturePrefix(system, tools []interface{}, model string) {
	if ka == nil {
		return
	}
	ka.mu.Lock()
	defer ka.mu.Unlock()
	if ka.captured {
		return
	}

	// Deep copy via JSON round-trip to avoid sharing mutable references.
	ka.system = jsonDeepCopy(system)
	ka.tools = jsonDeepCopy(tools)
	ka.model = model
	ka.captured = true
	log.Printf("[KEEPALIVE] Prefix captured — system=%d frags, tools=%d defs, model=%s",
		len(system), len(tools), model)
}

// Stop cancels the keepalive goroutine.
func (ka *Keepalive) Stop() {
	if ka != nil && ka.cancel != nil {
		ka.cancel()
	}
}

// loop runs the periodic keepalive ping.
func (ka *Keepalive) loop(ctx context.Context) {
	// Wait for prefix capture before starting the timer.
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
			ka.mu.Lock()
			ready := ka.captured
			ka.mu.Unlock()
			if ready {
				goto start
			}
		}
	}

start:
	log.Printf("[KEEPALIVE] Prefix ready — starting periodic pings every %s", ka.interval)

	// Fire immediately on first capture, then every interval.
	ka.ping()

	ticker := time.NewTicker(ka.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("[KEEPALIVE] Stopped")
			return
		case <-ticker.C:
			ka.ping()
		}
	}
}

// ping sends a minimal request to warm Anthropic's cache.
func (ka *Keepalive) ping() {
	ka.mu.Lock()
	system := ka.system
	tools := ka.tools
	model := ka.model
	ka.mu.Unlock()

	if system == nil {
		return
	}

	// Place cache_control on last system fragment to request extended TTL.
	sysCopy := stripCacheControlFromItems(jsonDeepCopy(system))
	if len(sysCopy) > 0 {
		if last, ok := sysCopy[len(sysCopy)-1].(map[string]interface{}); ok {
			last["cache_control"] = extendedCacheControl()
		}
	}

	// Also place cache_control on tools if present.
	toolsCopy := stripCacheControlFromItems(jsonDeepCopy(tools))
	if len(toolsCopy) > 0 {
		if last, ok := toolsCopy[len(toolsCopy)-1].(map[string]interface{}); ok {
			last["cache_control"] = extendedCacheControl()
		}
	}

	body := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"system":     sysCopy,
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "ping",
			},
		},
	}
	if len(toolsCopy) > 0 {
		body["tools"] = toolsCopy
	}

	data, err := json.Marshal(body)
	if err != nil {
		log.Printf("[KEEPALIVE] Marshal error: %v", err)
		return
	}

	url := ka.upstream + "/v1/messages"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		log.Printf("[KEEPALIVE] Request error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ka.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", ExtendedCacheTTLBeta)

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: ka.transport,
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[KEEPALIVE] Ping failed: %v (%.0fms)", err, float64(elapsed.Milliseconds()))
		return
	}
	defer resp.Body.Close()

	// Read and parse response for cache metrics.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var result map[string]interface{}
	json.Unmarshal(respBody, &result)

	ccRead, ccNew := 0, 0
	if usage, ok := result["usage"].(map[string]interface{}); ok {
		if v, ok := usage["cache_read_input_tokens"].(float64); ok {
			ccRead = int(v)
		}
		if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
			ccNew = int(v)
		}
	}

	log.Printf("[KEEPALIVE] Ping %d — cc_read=%d cc_new=%d (%.0fms)",
		resp.StatusCode, ccRead, ccNew, float64(elapsed.Milliseconds()))
	if resp.StatusCode >= 400 {
		log.Printf("[KEEPALIVE] Response body: %s", bytes.TrimSpace(respBody))
	}

	if resp.StatusCode == 429 {
		log.Printf("[KEEPALIVE] Rate limited — will retry next interval")
	}
}

// jsonDeepCopy deep-copies a slice via JSON round-trip.
func jsonDeepCopy(src []interface{}) []interface{} {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var dst []interface{}
	if json.Unmarshal(data, &dst) != nil {
		return nil
	}
	return dst
}

// Captured returns a printable status line for debugging.
func (ka *Keepalive) Captured() string {
	if ka == nil {
		return "disabled"
	}
	ka.mu.Lock()
	defer ka.mu.Unlock()
	if !ka.captured {
		return "waiting"
	}
	return fmt.Sprintf("ready (sys=%d tools=%d model=%s)", len(ka.system), len(ka.tools), ka.model)
}
