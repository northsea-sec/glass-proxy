package glass

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PrefixWarmerConfig configures shared prefix warming.
type PrefixWarmerConfig struct {
	IntervalSec int
	Upstream    string
	Transport   http.RoundTripper
	ShadowDir   string
}

type prefixLoop struct {
	cancel context.CancelFunc
}

// PrefixWarmer keeps multiple shared prefixes warm across fresh CC sessions.
type PrefixWarmer struct {
	mu             sync.Mutex
	registry       *PrefixRegistry
	loops          map[string]*prefixLoop
	interval       time.Duration
	upstream       string
	transport      http.RoundTripper
	latestKey      string
	latestIsBearer bool
	wg             sync.WaitGroup
}

// NewPrefixWarmer creates a multi-prefix warmer manager. It can start without
// an API key; the first real request supplies the key in memory.
func NewPrefixWarmer(cfg PrefixWarmerConfig) *PrefixWarmer {
	if cfg.IntervalSec <= 0 {
		log.Printf("[PREFIX] Shared warmer disabled (interval=%d)", cfg.IntervalSec)
		return nil
	}

	pw := &PrefixWarmer{
		registry:  NewPrefixRegistry(cfg.ShadowDir),
		loops:     make(map[string]*prefixLoop),
		interval:  time.Duration(cfg.IntervalSec) * time.Second,
		upstream:  cfg.Upstream,
		transport: cfg.Transport,
	}
	log.Printf("[PREFIX] Shared warmer started — interval=%ds upstream=%s", cfg.IntervalSec, cfg.Upstream)
	return pw
}

// Observe records a shared prefix profile and ensures there is a background
// warming loop for it. Returns the derived PrefixKey.
func (pw *PrefixWarmer) Observe(meta RequestMeta, system, tools []interface{}, model string) string {
	if pw == nil {
		return ""
	}

	prefixKey := PrefixFingerprint(model, system, tools, meta.AnthropicVersion, meta.Betas)
	if prefixKey == "" {
		return ""
	}

	if meta.APIKey != "" {
		pw.mu.Lock()
		pw.latestKey = meta.APIKey
		pw.latestIsBearer = meta.AuthIsBearer
		pw.mu.Unlock()
	}

	profile := PrefixProfile{
		PrefixKey:        prefixKey,
		Model:            model,
		System:           stripBillingHeader(system),
		Tools:            tools,
		AnthropicVersion: meta.AnthropicVersion,
		Betas:            normalizeBetas(meta.Betas),
	}
	pw.registry.Observe(profile)
	pw.ensureLoop(prefixKey)
	return prefixKey
}

// Stop halts all prefix warming loops.
func (pw *PrefixWarmer) Stop() {
	if pw == nil {
		return
	}

	pw.mu.Lock()
	loops := make([]*prefixLoop, 0, len(pw.loops))
	for key, loop := range pw.loops {
		loops = append(loops, loop)
		delete(pw.loops, key)
	}
	pw.mu.Unlock()

	for _, loop := range loops {
		loop.cancel()
	}
	pw.wg.Wait()
}

func (pw *PrefixWarmer) ensureLoop(prefixKey string) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if _, ok := pw.loops[prefixKey]; ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	pw.loops[prefixKey] = &prefixLoop{cancel: cancel}
	pw.wg.Add(1)
	go pw.loop(ctx, prefixKey)
}

func (pw *PrefixWarmer) loop(ctx context.Context, prefixKey string) {
	defer pw.wg.Done()
	pw.ping(prefixKey)

	ticker := time.NewTicker(pw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pw.ping(prefixKey)
		}
	}
}

func (pw *PrefixWarmer) ping(prefixKey string) {
	profile, ok := pw.registry.Get(prefixKey)
	if !ok {
		return
	}

	pw.mu.Lock()
	apiKey := pw.latestKey
	isBearer := pw.latestIsBearer
	pw.mu.Unlock()
	if apiKey == "" {
		return
	}
	if len(profile.System) == 0 || profile.Model == "" {
		return
	}

	sysCopy := stripCacheControlFromItems(jsonDeepCopy(profile.System))
	if len(sysCopy) > 0 {
		if last, ok := sysCopy[len(sysCopy)-1].(map[string]interface{}); ok {
			last["cache_control"] = extendedCacheControl()
		}
	}
	toolsCopy := stripCacheControlFromItems(jsonDeepCopy(profile.Tools))
	if len(toolsCopy) > 0 {
		if last, ok := toolsCopy[len(toolsCopy)-1].(map[string]interface{}); ok {
			last["cache_control"] = extendedCacheControl()
		}
	}

	body := map[string]interface{}{
		"model":      profile.Model,
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
		log.Printf("[PREFIX] Marshal error %s: %v", shortPrefix(prefixKey), err)
		return
	}

	req, err := http.NewRequest("POST", pw.upstream+"/v1/messages", bytes.NewReader(data))
	if err != nil {
		log.Printf("[PREFIX] Request error %s: %v", shortPrefix(prefixKey), err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if isBearer {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}
	req.Header.Set("anthropic-version", profile.AnthropicVersion)
	betas := normalizeBetas(append([]string(nil), profile.Betas...))
	betas = normalizeBetas(append(betas, ExtendedCacheTTLBeta))
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: pw.transport,
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		log.Printf("[PREFIX] Ping failed %s: %v (%.0fms)", shortPrefix(prefixKey), err, float64(elapsed.Milliseconds()))
		return
	}
	defer resp.Body.Close()

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

	pw.registry.MarkWarm(prefixKey, resp.StatusCode, ccRead, ccNew)
	log.Printf("[PREFIX] Ping %s %d — cc_read=%d cc_new=%d (%.0fms)", shortPrefix(prefixKey), resp.StatusCode, ccRead, ccNew, float64(elapsed.Milliseconds()))
	if resp.StatusCode >= 400 {
		log.Printf("[PREFIX] Ping %s response body: %s", shortPrefix(prefixKey), strings.TrimSpace(string(respBody)))
	}
}

func stripCacheControlFromItems(items []interface{}) []interface{} {
	if len(items) == 0 {
		return items
	}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		delete(item, "cache_control")
		if content, ok := item["content"].([]interface{}); ok {
			for _, blockRaw := range content {
				block, ok := blockRaw.(map[string]interface{})
				if !ok {
					continue
				}
				delete(block, "cache_control")
			}
		}
	}
	return items
}
