package subagent

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

const (
	// SmallSystemThreshold marks system prompts that should bypass the main-session
	// canonical cache path and get their own isolated session suffix.
	SmallSystemThreshold = 5000

	TypeNone        = ""
	TypeHaiku       = "haiku"
	TypeSonnet      = "sonnet"
	TypeSmallSystem = "small_system"
	TypeOpusSubset  = "opus_subset"
	TypeAgentTool   = "agent_tool"
)

// Classification captures all subagent-related decisions derived from a request.
// A single classification is threaded through proxy, Glass, serializer, and debug
// so those layers stop guessing independently.
type Classification struct {
	IsSubagent             bool
	Type                   string
	ModelType              string
	SystemChars            int
	MessageCount           int
	ToolCount              int
	HasTools               bool
	BlockByModel           bool
	BypassCanonical        bool
	IsolateSession         bool
	UseFrozenPrefix        bool
	UseParentAffinity      bool
	BypassMessageCache     bool
	DisableUpstreamCaching bool
	SessionSuffix          string
}

// ClassifyOpts carries optional out-of-band context that helps distinguish
// fresh main sessions from agent-tool subagents.
type ClassifyOpts struct {
	// HasEstablishedParent is true when the proxy has already seen a main
	// session (10+ messages) from the same client PID. If false, a low-message
	// request with a full system prompt is more likely a fresh session, not a
	// subagent.
	HasEstablishedParent bool
}

// Classify inspects a parsed Anthropic request body and returns the shared
// subagent classification for the request.
func Classify(body map[string]interface{}, opts ...ClassifyOpts) Classification {
	var opt ClassifyOpts
	if len(opts) > 0 {
		opt = opts[0]
	}
	_ = opt
	system := SystemBlocks(body)
	tc := ToolCount(body)
	info := Classification{
		SystemChars:  SystemChars(system),
		MessageCount: MessageCount(body),
		ModelType:    ModelType(ModelName(body)),
		ToolCount:    tc,
		HasTools:     tc > 0,
	}

	if info.ModelType != TypeNone {
		info.IsSubagent = true
		info.Type = info.ModelType
		info.BlockByModel = true
	}

	if info.SystemChars > 0 && info.SystemChars < SmallSystemThreshold {
		info.IsSubagent = true
		if info.Type == TypeNone {
			info.Type = TypeSmallSystem
		}
		if info.MessageCount >= 10 {
			// Large message history: promote to isolated cached session.
			// Gets its own glass cache but NO Anthropic cache entries —
			// subagent-origin conversations must never compete with parent.
			info.IsolateSession = true
			info.BypassCanonical = true
			info.UseParentAffinity = false
			info.SessionSuffix = StableSessionSuffix(SystemBlocks(body))
			info.DisableUpstreamCaching = true
		} else if info.HasTools {
			// Tool-bearing subagent (Agent tool) with < 10 messages.
			// FIX-2026-3-24: These are ephemeral (1-5 msgs, discarded).
			// Creating Anthropic cache entries evicts main session caches.
			// Cost-benefit (TEST 10): 7.6:1 against caching.
			// Production evidence: 508K cc tokens, 18 total misses in 30s.
			// PRIOR ART: BURN_RATE_ROOT_CAUSE 2026-03-14 mitigation #1.
			info.IsolateSession = true
			info.BypassCanonical = true
			info.UseParentAffinity = false
			info.SessionSuffix = StableSessionSuffix(SystemBlocks(body))
			info.DisableUpstreamCaching = true
		} else {
			// Small context, no tools (title gen, summary): bypass caching
			info.BypassCanonical = true
			info.UseParentAffinity = true
			info.BypassMessageCache = true
			info.DisableUpstreamCaching = true
		}
	} else if info.SystemChars >= SmallSystemThreshold && info.HasTools && info.MessageCount > 0 && info.MessageCount < 10 && !info.BlockByModel && opt.HasEstablishedParent {
		// Agent tool subagent with FULL system prompt (inherits parent's >10K system).
		// INSIGHT-2026-2-25 D-3: "General-purpose subagent inherits identical system
		// prompt. System prompt is NOT a viable discriminator."
		// INSIGHT-2026-2-25 D-5: "Message count is the only reliable in-band discriminator."
		//
		// The MITM proxy (Feb 25) rejected system-size detection (fixes v1, v2) and
		// used message count: scope = "main" if msgs >= 10 else "sub".
		//
		// Without this block, agent_tool subagents:
		// - Share the parent's Glass cache (same conv_id) → prefix collision
		// - Bypass serializer parent-mapping (IsSubagent=false) → interleaving
		// - Keep cache_control markers → competing Anthropic cache entries
		// Production evidence (2026-03-16): 47 rebuilds, 6.39M tokens, ~$24/day.
		//
		// FIX-2026-3-19: Require HasEstablishedParent so fresh main sessions
		// (which also have <10 msgs) are NOT misclassified as subagents.
		// Removed BypassCanonical — agent_tool subagents share the parent's
		// system prompt, so they hit the canonical cache (fast) and get the
		// same sysprompt pipeline modifications.
		info.IsSubagent = true
		if info.Type == TypeNone {
			info.Type = TypeAgentTool
		}
		info.IsolateSession = true
		info.DisableUpstreamCaching = true // agent_tool subagents are ephemeral (1-10 msgs).
		// Explicit cache_control forces Anthropic to create cache entries that evict
		// the parent session's long-lived cache. Stripping cache_control lets Anthropic's
		// automatic caching decide. Evidence: 2026-03-25 instrumentation showed 4+
		// competing prefixes from 2 conversations — each agent_tool creating its own entry.
		info.UseParentAffinity = false
		info.SessionSuffix = StableSessionSuffix(system)
	}

	return info
}

// WithCacheContext extends the shared classification with cache-aware signals
// that are only available inside the Glass pipeline.
func WithCacheContext(info Classification, cacheLen int) Classification {
	if info.IsolateSession {
		return info
	}
	if info.MessageCount > 0 && cacheLen > 10 && float64(info.MessageCount) < float64(cacheLen)*0.8 {
		info.IsSubagent = true
		if info.Type == TypeNone {
			info.Type = TypeOpusSubset
		}
		info.UseFrozenPrefix = true
		info.UseParentAffinity = true
	}
	return info
}

// TelemetryOverlay marks agent-tool orchestration requests as subagent traffic
// for observability without changing any runtime cache/serializer behavior.
func TelemetryOverlay(body map[string]interface{}, info Classification) Classification {
	if info.IsSubagent {
		return info
	}
	if !hasAgentToolActivity(body) {
		return info
	}
	info.IsSubagent = true
	if info.Type == TypeNone {
		info.Type = TypeAgentTool
	}
	return info
}

// ApplySessionSuffix returns the effective session key for isolated subagents.
func ApplySessionSuffix(base string, info Classification) string {
	if base == "" || !info.IsolateSession || info.SessionSuffix == "" {
		return base
	}
	return base + info.SessionSuffix
}

// ModelType classifies model-routed subagents.
func ModelType(model string) string {
	model = strings.ToLower(model)
	switch {
	case strings.Contains(model, "haiku"):
		return TypeHaiku
	case strings.Contains(model, "sonnet"):
		return TypeSonnet
	default:
		return TypeNone
	}
}

// ModelName returns the request model, if present.
func ModelName(body map[string]interface{}) string {
	model, _ := body["model"].(string)
	return model
}

// MessageCount returns the number of request messages.
func MessageCount(body map[string]interface{}) int {
	if msgs, ok := body["messages"].([]interface{}); ok {
		return len(msgs)
	}
	return 0
}

// ToolCount returns the number of tools in the request.
func ToolCount(body map[string]interface{}) int {
	if tools, ok := body["tools"].([]interface{}); ok {
		return len(tools)
	}
	return 0
}

// SystemBlocks returns the request system prompt as structured blocks.
func SystemBlocks(body map[string]interface{}) []interface{} {
	switch sys := body["system"].(type) {
	case []interface{}:
		return sys
	case string:
		if strings.TrimSpace(sys) == "" {
			return nil
		}
		return []interface{}{
			map[string]interface{}{"type": "text", "text": sys},
		}
	default:
		return nil
	}
}

// SystemChars returns the total character count across system blocks.
func SystemChars(system []interface{}) int {
	total := 0
	for _, frag := range system {
		if block, ok := frag.(map[string]interface{}); ok {
			if text, ok := block["text"].(string); ok {
				total += len(text)
			}
		}
	}
	return total
}

// StableSessionSuffix derives a collision-resistant suffix from the actual
// system prompt content, not just its length.
func StableSessionSuffix(system []interface{}) string {
	if len(system) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, frag := range system {
		if block, ok := frag.(map[string]interface{}); ok {
			if text, ok := block["text"].(string); ok {
				sb.WriteString(text)
			}
		}
	}
	if sb.Len() == 0 {
		return ""
	}
	h := sha256.Sum256([]byte(sb.String()))
	return fmt.Sprintf("_sub_%x", h[:4])
}

func hasAgentToolActivity(body map[string]interface{}) bool {
	msgs, _ := body["messages"].([]interface{})
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := msg["content"].([]interface{})
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if !ok {
				continue
			}
			if tp, _ := block["type"].(string); tp != "tool_use" {
				continue
			}
			name, _ := block["name"].(string)
			if name == "Agent" || name == "Task" || name == "TaskCreate" {
				return true
			}
			input, _ := block["input"].(map[string]interface{})
			if subType, _ := input["subagent_type"].(string); strings.TrimSpace(subType) != "" {
				return true
			}
		}
	}
	return false
}
