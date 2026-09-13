// Package spoofer rewrites usage fields in API responses to cap reported input tokens.
// Ported from mitm_itt_addon.py usage spoofing logic.
//
// When input_tokens exceeds spoof_cap (default 140K), the response usage is
// rewritten to show the capped value. This prevents Claude Code from triggering
// its built-in context compaction (which destroys the optimized trim boundaries).
package spoofer

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultSpoofCap is the default input_tokens cap.
	// Claude Code triggers compaction around 150K — we cap below that.
	DefaultSpoofCap = 140000

	// SpoofedUtilization is the utilization value we write into rate-limit headers.
	// Must match the filter in RecordQuotaSample to avoid self-poisoning.
	SpoofedUtilization = 0.05
)

// rateLimitSpoofHeaders are the Anthropic rate-limit headers we overwrite.
// These prevent Claude Code from self-throttling (fast-mode disable, "stop and wait" prompts).
var rateLimitSpoofHeaders = map[string]string{
	"Anthropic-Ratelimit-Unified-Status":                "allowed",
	"Anthropic-Ratelimit-Unified-5h-Status":             "allowed",
	"Anthropic-Ratelimit-Unified-5h-Utilization":        "0.05",
	"Anthropic-Ratelimit-Unified-7d-Status":             "allowed",
	"Anthropic-Ratelimit-Unified-7d-Utilization":        "0.05",
	"Anthropic-Ratelimit-Unified-Representative-Claim":  "five_hour",
}

// rateLimitRemoveHeaders are headers whose presence triggers degraded behavior in CC.
var rateLimitRemoveHeaders = []string{
	"Anthropic-Ratelimit-Unified-Fallback-Percentage",
	"Anthropic-Ratelimit-Unified-Overage-Disabled-Reason",
	"Anthropic-Ratelimit-Unified-7d-Surpassed-Threshold",
}

// QuotaSample is a recorded real rate-limit observation (before spoofing).
type QuotaSample struct {
	Timestamp float64 `json:"t"`
	H5        float64 `json:"h5"`
	D7        float64 `json:"d7"`
}

// Spoofer rewrites usage data in API responses and rate-limit headers.
type Spoofer struct {
	enabled  bool
	spoofCap int
	stats    Stats

	quotaMu      sync.Mutex
	quotaSamples []QuotaSample
}

// Stats tracks spoofing activity.
type Stats struct {
	TotalSpoofed int `json:"total_spoofed"`
	TotalPassed  int `json:"total_passed"`
}

// New creates a spoofer with the given cap.
func New(enabled bool, spoofCap int) *Spoofer {
	if spoofCap <= 0 {
		spoofCap = DefaultSpoofCap
	}
	return &Spoofer{
		enabled:  enabled,
		spoofCap: spoofCap,
	}
}

// SpoofResponseBody rewrites usage.input_tokens in a non-streaming API response body.
// Returns the modified body bytes and whether spoofing was applied.
func (s *Spoofer) SpoofResponseBody(body []byte) ([]byte, bool) {
	if !s.enabled {
		s.stats.TotalPassed++
		return body, false
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		return body, false
	}

	spoofed := s.spoofUsage(resp)
	if !spoofed {
		s.stats.TotalPassed++
		return body, false
	}

	modified, err := json.Marshal(resp)
	if err != nil {
		return body, false
	}

	s.stats.TotalSpoofed++
	return modified, true
}

// SpoofSSEEvent rewrites usage in a parsed SSE event (message_start or message_delta).
// Modifies the event map in-place. Returns true if spoofed.
func (s *Spoofer) SpoofSSEEvent(event map[string]interface{}) bool {
	if !s.enabled {
		return false
	}

	// message_start: usage is in event.message.usage
	if msg, ok := event["message"].(map[string]interface{}); ok {
		if usage, ok := msg["usage"].(map[string]interface{}); ok {
			return s.capInputTokens(usage)
		}
	}

	// message_delta / final: usage is in event.usage
	if usage, ok := event["usage"].(map[string]interface{}); ok {
		return s.capInputTokens(usage)
	}

	return false
}

func (s *Spoofer) spoofUsage(resp map[string]interface{}) bool {
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return false
	}
	return s.capInputTokens(usage)
}

func (s *Spoofer) capInputTokens(usage map[string]interface{}) bool {
	inputTokens, ok := usage["input_tokens"].(float64)
	if !ok || inputTokens <= float64(s.spoofCap) {
		return false
	}

	original := int(inputTokens)
	usage["input_tokens"] = float64(s.spoofCap)
	log.Printf("[SPOOF] Capped input_tokens: %d -> %d", original, s.spoofCap)
	return true
}

// GetStats returns spoofing statistics.
func (s *Spoofer) GetStats() Stats {
	return s.stats
}

// SpoofRateLimitHeaders rewrites Anthropic rate-limit headers in the response.
// Records the real values for internal monitoring BEFORE overwriting.
// Returns the real 5h and 7d utilization values (for quota tracking).
func (s *Spoofer) SpoofRateLimitHeaders(resp *http.Response) (real5h, real7d float64) {
	if !s.enabled || resp == nil {
		return 0, 0
	}

	// Extract real values BEFORE spoofing (for our monitoring)
	real5h = parseHeaderFloat(resp.Header.Get("Anthropic-Ratelimit-Unified-5h-Utilization"))
	real7d = parseHeaderFloat(resp.Header.Get("Anthropic-Ratelimit-Unified-7d-Utilization"))

	// Record real quota sample (filtered against self-poisoning)
	if real5h > 0 {
		s.RecordQuotaSample(real5h, real7d)
	}

	// Overwrite with spoofed values — CC sees 5h=5% 7d=5% status=allowed
	for hdr, val := range rateLimitSpoofHeaders {
		resp.Header.Set(hdr, val)
	}
	// Remove headers that trigger degraded behavior
	for _, hdr := range rateLimitRemoveHeaders {
		resp.Header.Del(hdr)
	}

	if real5h > 0 {
		log.Printf("[SPOOF] Rate-limit headers spoofed: real 5h=%.1f%% 7d=%.1f%% → CC sees 5%%/5%% allowed", real5h*100, real7d*100)
	}

	return real5h, real7d
}

// RecordQuotaSample appends a real rate-limit observation for burn rate calculation.
// Rejects values matching the exact spoofed values (0.05/0.05) to prevent self-poisoning:
// our spoof writes 0.05 → if we read our own spoofed output back, it would corrupt tracking.
func (s *Spoofer) RecordQuotaSample(rl5h, rl7d float64) {
	// Reject spoofed values — our spoof writes 0.05/0.05. Without this filter,
	// reading our own spoofed headers back poisons quota tracking.
	if rl5h <= 0 || (math.Abs(rl5h-SpoofedUtilization) < 0.001 && math.Abs(rl7d-SpoofedUtilization) < 0.001) {
		return
	}

	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()

	sample := QuotaSample{
		Timestamp: float64(time.Now().Unix()),
		H5:        math.Round(rl5h*1e6) / 1e6,
		D7:        math.Round(rl7d*1e6) / 1e6,
	}
	s.quotaSamples = append(s.quotaSamples, sample)

	// Keep only last 1000 samples
	if len(s.quotaSamples) > 1000 {
		s.quotaSamples = s.quotaSamples[len(s.quotaSamples)-1000:]
	}
}

// QuotaSamples returns a copy of recorded quota samples.
func (s *Spoofer) QuotaSamples() []QuotaSample {
	s.quotaMu.Lock()
	defer s.quotaMu.Unlock()
	out := make([]QuotaSample, len(s.quotaSamples))
	copy(out, s.quotaSamples)
	return out
}

func parseHeaderFloat(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
