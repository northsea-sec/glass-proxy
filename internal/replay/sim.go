package replay

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"proxy.local/app/internal/forcemode"
	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/subagent"
)

type Policy string

const (
	PolicyCurrent             Policy = "current"
	PolicyCachedChild         Policy = "cached_child"
	PolicyHybridThreshold     Policy = "hybrid_threshold"
	PolicyParentSharedIfExact Policy = "parent_shared_if_exact"
)

type RunnerConfig struct {
	GlassConfig   glass.GlassConfig
	ForceMode     forcemode.Config
	SpoofUsageCap int
}

type Options struct {
	Policy           Policy
	HybridThreshold  int
	OnlySubagentType string
	OnlyAffinityKey  string
}

type RequestResult struct {
	CaptureID       string `json:"capture_id"`
	Timestamp       string `json:"timestamp"`
	RequestKey      string `json:"request_key"`
	AffinityKey     string `json:"affinity_key"`
	SubagentType    string `json:"subagent_type,omitempty"`
	PolicyPath      string `json:"policy_path"`
	TotalTokens     int    `json:"total_tokens"`
	ReadTokens      int    `json:"read_tokens"`
	CreateTokens    int    `json:"create_tokens"`
	UncachedTokens  int    `json:"uncached_tokens"`
	MatchedCaptured bool   `json:"matched_captured"`
	FinalHash       string `json:"final_hash"`
	CapturedHash    string `json:"captured_hash,omitempty"`
}

type Summary struct {
	Policy           Policy          `json:"policy"`
	FixtureCount     int             `json:"fixture_count"`
	EvaluatedCount   int             `json:"evaluated_count"`
	TotalTokens      int             `json:"total_tokens"`
	ReadTokens       int             `json:"read_tokens"`
	CreateTokens     int             `json:"create_tokens"`
	UncachedTokens   int             `json:"uncached_tokens"`
	ReplayMatches    int             `json:"replay_matches"`
	ReplayMismatches int             `json:"replay_mismatches"`
	SmallSystemCalls int             `json:"small_system_calls"`
	Requests         []RequestResult `json:"requests"`
}

type cacheSim struct {
	seen map[string]int
}

func newCacheSim() *cacheSim {
	return &cacheSim{seen: make(map[string]int)}
}

func (c *cacheSim) Observe(body map[string]interface{}, hdr HeaderSnapshot) (readTokens, createTokens int) {
	segments := buildSegments(body, hdr)
	if len(segments) == 0 {
		return 0, 0
	}
	longestCached := 0
	for _, segment := range segments {
		if _, ok := c.seen[segment.Hash]; ok && segment.Tokens > longestCached {
			longestCached = segment.Tokens
		}
	}
	last := segments[len(segments)-1]
	if _, ok := c.seen[last.Hash]; !ok {
		createTokens = last.Tokens - longestCached
		if createTokens < 0 {
			createTokens = 0
		}
		for _, segment := range segments {
			c.seen[segment.Hash] = segment.Tokens
		}
	}
	return longestCached, createTokens
}

type segment struct {
	Hash   string
	Tokens int
}

func Run(fixtures []Fixture, cfg RunnerConfig, opts Options) (Summary, error) {
	dir, err := os.MkdirTemp("", "glass-replay-*")
	if err != nil {
		return Summary{}, err
	}
	defer os.RemoveAll(dir)

	glassCfg := cfg.GlassConfig
	glassCfg.ShadowDir = dir
	engine := glass.NewEngine(glassCfg)
	cache := newCacheSim()
	parentPrefixes := make(map[string]string)

	sort.Slice(fixtures, func(i, j int) bool {
		return parseFixtureTime(fixtures[i].Timestamp).Before(parseFixtureTime(fixtures[j].Timestamp))
	})

	summary := Summary{
		Policy:       opts.Policy,
		FixtureCount: len(fixtures),
	}

	for _, fixture := range fixtures {
		if opts.OnlySubagentType != "" && fixture.Meta.Subagent.Type != opts.OnlySubagentType {
			continue
		}
		if opts.OnlyAffinityKey != "" && fixture.Meta.AffinityKey != opts.OnlyAffinityKey {
			continue
		}
		if len(fixture.BodyPreGlass) == 0 {
			continue
		}

		body, err := decodeBody(fixture.BodyPreGlass)
		if err != nil {
			return Summary{}, fmt.Errorf("decode pre-glass %s: %w", fixture.CaptureID, err)
		}

		meta, path := metaForPolicy(fixture, body, opts, parentPrefixes)
		result := engine.Process(body, meta)
		if result.ColdWarmerSessionKey != "" {
			// Offline replay has no streaming proxy to release the cold gate,
			// so emulate the live first-SSE release immediately.
			engine.ReleaseColdGate(result.ColdWarmerSessionKey)
		}
		cfg.ForceMode.Apply(body)
		if cfg.SpoofUsageCap > 0 && cfg.GlassConfig.CacheMode() != glass.CacheModeContextAPI {
			delete(body, "context_management")
		}

		finalBytes, err := json.Marshal(body)
		if err != nil {
			return Summary{}, fmt.Errorf("marshal final %s: %w", fixture.CaptureID, err)
		}
		finalHash := canonicalHash(finalBytes)
		capturedHash := canonicalHash(fixture.BodyFinal)
		matched := capturedHash != "" && finalHash == capturedHash

		totalTokens := estimateTokens(body)
		readTokens, createTokens := cache.Observe(body, fixture.Header)
		uncachedTokens := totalTokens - readTokens
		if uncachedTokens < 0 {
			uncachedTokens = 0
		}

		if fixture.Meta.Subagent.Type == subagent.TypeSmallSystem {
			summary.SmallSystemCalls++
		}
		if matched {
			summary.ReplayMatches++
		} else if capturedHash != "" {
			summary.ReplayMismatches++
		}
		summary.EvaluatedCount++
		summary.TotalTokens += totalTokens
		summary.ReadTokens += readTokens
		summary.CreateTokens += createTokens
		summary.UncachedTokens += uncachedTokens
		summary.Requests = append(summary.Requests, RequestResult{
			CaptureID:       fixture.CaptureID,
			Timestamp:       fixture.Timestamp,
			RequestKey:      meta.RequestKey,
			AffinityKey:     meta.AffinityKey,
			SubagentType:    result.Subagent.Type,
			PolicyPath:      path,
			TotalTokens:     totalTokens,
			ReadTokens:      readTokens,
			CreateTokens:    createTokens,
			UncachedTokens:  uncachedTokens,
			MatchedCaptured: matched,
			FinalHash:       finalHash,
			CapturedHash:    capturedHash,
		})

		if !result.Subagent.IsSubagent {
			parentPrefixes[meta.AffinityKey] = systemToolsFingerprint(body)
		}
	}

	return summary, nil
}

func metaForPolicy(f Fixture, body map[string]interface{}, opts Options, parentPrefixes map[string]string) (glass.RequestMeta, string) {
	meta := glass.RequestMeta{
		SessionKey:       defaultString(f.Meta.SessionKey, f.Meta.RequestKey),
		RequestKey:       defaultString(f.Meta.RequestKey, f.Meta.SessionKey),
		AffinityKey:      defaultString(f.Meta.AffinityKey, f.Meta.SessionKey),
		ClientPID:        f.Meta.ClientPID,
		AnthropicVersion: f.Header.AnthropicVersion,
		Betas:            append([]string(nil), f.Header.Betas...),
		Subagent:         f.Meta.Subagent,
	}
	if meta.SessionKey == "" {
		meta.SessionKey = f.Meta.RequestSessionKey
	}
	if meta.RequestKey == "" {
		meta.RequestKey = meta.SessionKey
	}
	if meta.AffinityKey == "" {
		meta.AffinityKey = meta.SessionKey
	}

	info := meta.Subagent
	baseKey := defaultString(f.Meta.RequestSessionKey, f.Meta.SessionKey, f.Meta.AffinityKey)
	if baseKey == "" {
		baseKey = meta.SessionKey
	}

	switch opts.Policy {
	case PolicyCachedChild:
		if info.Type == subagent.TypeSmallSystem {
			info.IsSubagent = true
			info.BypassCanonical = true
			info.BypassMessageCache = false
			info.DisableUpstreamCaching = false
			info.UseParentAffinity = false
			info.IsolateSession = true
			info.SessionSuffix = subagent.StableSessionSuffix(subagent.SystemBlocks(body))
			meta.Subagent = info
			meta.SessionKey = subagent.ApplySessionSuffix(baseKey, info)
			meta.RequestKey = meta.SessionKey
			meta.AffinityKey = meta.SessionKey
			return meta, string(PolicyCachedChild)
		}
	case PolicyHybridThreshold:
		if info.Type == subagent.TypeSmallSystem && estimateTokens(body) >= opts.HybridThreshold {
			info.IsSubagent = true
			info.BypassCanonical = true
			info.BypassMessageCache = false
			info.DisableUpstreamCaching = false
			info.UseParentAffinity = false
			info.IsolateSession = true
			info.SessionSuffix = subagent.StableSessionSuffix(subagent.SystemBlocks(body))
			meta.Subagent = info
			meta.SessionKey = subagent.ApplySessionSuffix(baseKey, info)
			meta.RequestKey = meta.SessionKey
			meta.AffinityKey = meta.SessionKey
			return meta, fmt.Sprintf("%s(>= %d)", PolicyHybridThreshold, opts.HybridThreshold)
		}
	case PolicyParentSharedIfExact:
		if info.Type == subagent.TypeSmallSystem {
			want := parentPrefixes[meta.AffinityKey]
			if want != "" && want == systemToolsFingerprint(body) {
				info.IsSubagent = true
				info.BypassMessageCache = false
				info.DisableUpstreamCaching = false
				info.UseParentAffinity = true
				info.UseFrozenPrefix = true
				info.IsolateSession = false
				meta.Subagent = info
				meta.SessionKey = meta.AffinityKey
				meta.RequestKey = defaultString(f.Meta.RequestKey, meta.AffinityKey)
				return meta, string(PolicyParentSharedIfExact)
			}
		}
	}

	return meta, string(PolicyCurrent)
}

func buildSegments(body map[string]interface{}, hdr HeaderSnapshot) []segment {
	var segments []segment
	seen := make(map[string]struct{})
	appendSegment := func(payload map[string]interface{}) {
		data, err := json.Marshal(payload)
		if err != nil {
			return
		}
		hash := shortHash(data)
		if _, ok := seen[hash]; ok {
			return
		}
		seen[hash] = struct{}{}
		segments = append(segments, segment{
			Hash:   hash,
			Tokens: estimateTokens(payload),
		})
	}

	tools, _ := body["tools"].([]interface{})
	system, _ := body["system"].([]interface{})
	messages, _ := body["messages"].([]interface{})

	for i, tool := range tools {
		if hasCacheControl(tool) {
			appendSegment(prefixPayload(body, hdr, tools[:i+1], nil, nil))
		}
	}
	for i, block := range system {
		if hasCacheControl(block) {
			appendSegment(prefixPayload(body, hdr, tools, system[:i+1], nil))
		}
	}
	for i, raw := range messages {
		msg, _ := raw.(map[string]interface{})
		if msg == nil {
			continue
		}
		if hasCacheControl(msg) {
			appendSegment(prefixPayload(body, hdr, tools, system, messages[:i+1]))
		}
		if content, ok := msg["content"].([]interface{}); ok {
			for j, block := range content {
				if hasCacheControl(block) {
					msgCopy := deepCopyMap(msg)
					msgCopy["content"] = deepCopySlice(content[:j+1])
					msgs := append(deepCopySlice(messages[:i]), msgCopy)
					appendSegment(prefixPayload(body, hdr, tools, system, msgs))
				}
			}
		}
	}

	return segments
}

func prefixPayload(body map[string]interface{}, hdr HeaderSnapshot, tools []interface{}, system []interface{}, messages []interface{}) map[string]interface{} {
	payload := map[string]interface{}{
		"model":             body["model"],
		"anthropic_version": hdr.AnthropicVersion,
	}
	if len(hdr.Betas) > 0 {
		betas := append([]string(nil), hdr.Betas...)
		sort.Strings(betas)
		payload["betas"] = betas
	}
	if len(tools) > 0 {
		payload["tools"] = deepCopySlice(tools)
	}
	if len(system) > 0 {
		payload["system"] = deepCopySlice(system)
	}
	if len(messages) > 0 {
		payload["messages"] = deepCopySlice(messages)
	}
	return payload
}

func decodeBody(raw json.RawMessage) (map[string]interface{}, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body, nil
}

func shortHash(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:8])
}

func canonicalHash(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return shortHash(raw)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return shortHash(raw)
	}
	return shortHash(data)
}

func systemToolsFingerprint(body map[string]interface{}) string {
	payload := map[string]interface{}{
		"model":  body["model"],
		"tools":  body["tools"],
		"system": body["system"],
	}
	data, _ := json.Marshal(payload)
	return shortHash(data)
}

func estimateTokens(v interface{}) int {
	data, _ := json.Marshal(v)
	return (len(data) + 3) / 4
}

func deepCopySlice(src []interface{}) []interface{} {
	if len(src) == 0 {
		return nil
	}
	out := make([]interface{}, 0, len(src))
	for _, item := range src {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, deepCopyMap(m))
			continue
		}
		if s, ok := item.([]interface{}); ok {
			out = append(out, deepCopySlice(s))
			continue
		}
		out = append(out, item)
	}
	return out
}

func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(src))
	for key, val := range src {
		switch typed := val.(type) {
		case map[string]interface{}:
			out[key] = deepCopyMap(typed)
		case []interface{}:
			out[key] = deepCopySlice(typed)
		default:
			out[key] = typed
		}
	}
	return out
}

func hasCacheControl(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	if !ok {
		return false
	}
	_, ok = m["cache_control"]
	return ok
}

func defaultString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
