// Package glass implements Session Glass — eviction-based context management.
// Messages are either fully present or fully evicted. No truncation, no content
// mutation. The prefix is frozen and byte-identical between eviction events.
package glass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"proxy.local/app/internal/runtimepaths"
)

// GlassConfig holds Glass-specific settings.
type GlassConfig struct {
	EvictTriggerTokens   int    `json:"evict_trigger_tokens"` // 165000
	EvictTargetTokens    int    `json:"evict_target_tokens"`  // 135000
	AnchorKeepMsgs       int    `json:"anchor_keep_messages"` // 4
	RecentKeepMsgs       int    `json:"recent_keep_messages"` // 20
	ShadowDir            string `json:"shadow_dir"`           // lane-owned Glass shadow dir
	SpoofUsageCap        int    `json:"spoof_usage_cap_tokens"`
	StripSysReminders    bool   `json:"strip_system_reminders"`
	StripThinking        bool   `json:"strip_thinking_blocks"`
	KeepaliveIntervalSec int    `json:"keepalive_interval_sec"` // 240 = every 4 min; 0 = disabled

	QuarantineCreateThreshold int `json:"quarantine_create_threshold_tokens"`
	QuarantineReadThreshold   int `json:"quarantine_read_threshold_tokens"`
	QuarantineStreakThreshold int `json:"quarantine_streak_threshold"`
	QuarantineWindowSec       int `json:"quarantine_window_sec"`

	// Rolling summarization
	RollingSummarizeEnabled  bool   `json:"rolling_summarize_enabled"`
	RollingSummarizeInterval int    `json:"rolling_summarize_interval"` // tokens between chunks (e.g. 50000)
	RollingSummarizeModel    string `json:"rolling_summarize_model"`    // e.g. "claude-opus-4-6"
	RecoveryGateEnabled      bool   `json:"recovery_gate_enabled"`
	CompressionFlushTokens   int    `json:"compression_flush_tokens"`   // flush when total tokens exceed this (default 200000)
	CompressionBatchSize     int    `json:"compression_batch_size"`     // only advance watermark in batches of this many messages (default 16, 0=eager)
	CompressionTriggerTokens int    `json:"compression_trigger_tokens"` // don't compress until estimated tokens exceed this (default 80000, 0=always)

	// Claude lane config (Anthropic /v1/messages).
	ClaudeUpstream string `json:"claude_upstream"` // optional override for Claude/Anthropic lane upstream

	// Cache context management mode (Anthropic lane only).
	//   "full"        - Glass manages compression, anchor breakpoints, cache_control placement (default).
	//   "off"         - No cache optimization: no compression, no breakpoints, no cache_control on messages.
	//                   Anthropic's implicit prefix caching still operates on system/tools.
	//   "context_api" - Delegate tool-result clearing to Anthropic's Context Editing API
	//                   (server-side, beta context-management-2025-06-27). Glass stops placing
	//                   cache_control on messages and stops compressing. Anthropic handles clearing.
	ContextCacheMode        string `json:"context_cache_mode"`         // "full" (default), "off", "context_api"
	ContextAPITriggerTokens int    `json:"context_api_trigger_tokens"` // input_tokens threshold before clearing fires (default 100000)
	ContextAPIKeepToolUses  int    `json:"context_api_keep_tool_uses"` // recent tool_use pairs to preserve (default 3)
	ContextAPIClearAtLeast  int    `json:"context_api_clear_at_least"` // minimum tokens to clear per activation (default 20000)
	ContextAPIClearThinking bool   `json:"context_api_clear_thinking"` // also clear old thinking blocks (default true)

	// OpenAI lane config (for non-Anthropic upstreams like Pi CLI / Qwen).
	OpenAIUpstream           string `json:"openai_upstream"`             // e.g. "http://192.168.8.239:8080"
	OpenAIEvictTriggerTokens int    `json:"openai_evict_trigger_tokens"` // 55000 for 66K context
	OpenAIEvictTargetTokens  int    `json:"openai_evict_target_tokens"`  // 45000 for 66K context
	OpenAIToolAllowlist      []string `json:"openai_tool_allowlist"`     // empty = keep all tools; non-empty = strip schemas outside the list BEFORE the budget check

	// Codex lane config (for OpenAI Codex CLI via Responses API).
	CodexUpstream           string `json:"codex_upstream"`             // default: "https://api.openai.com"
	CodexShadowDir          string `json:"codex_shadow_dir"`           // optional override; default: "~/.codex/glass-proxy/shadow"
	CodexEvictTriggerTokens int    `json:"codex_evict_trigger_tokens"` // default: 200000 (stay under 272K standard tier)
	CodexEvictTargetTokens  int    `json:"codex_evict_target_tokens"`  // default: 150000
	CodexSummarizeEnabled   bool   `json:"codex_summarize_enabled"`
	CodexSummarizeInterval  int    `json:"codex_summarize_interval"` // default: 50000 tokens between chunks
	CodexSummarizeModel     string `json:"codex_summarize_model"`    // default: "gpt-5.4"

	// Gemini lane config (for Google Gemini CLI via generateContent API).
	GeminiUpstream             string `json:"gemini_upstream"`             // default: "https://generativelanguage.googleapis.com"
	GeminiEvictTriggerTokens   int    `json:"gemini_evict_trigger_tokens"` // default: 700000 (1M window)
	GeminiEvictTargetTokens    int    `json:"gemini_evict_target_tokens"`  // default: 500000
	GeminiSummarizeEnabled     bool   `json:"gemini_summarize_enabled"`
	GeminiSummarizeInterval    int    `json:"gemini_summarize_interval"` // default: 80000 tokens between chunks
	GeminiSummarizeModel       string `json:"gemini_summarize_model"`    // default: "gemini-2.5-pro"
	GeminiExplicitCacheEnabled bool   `json:"gemini_explicit_cache_enabled"`
	GeminiCacheTTLSec          int    `json:"gemini_cache_ttl_sec"` // default: 3600 (1 hour)
}

// Cache mode constants.
const (
	CacheModeOff        = "off"
	CacheModeFull       = "full"
	CacheModeContextAPI = "context_api"
)

// CacheMode returns the effective cache mode, defaulting to "full".
func (c GlassConfig) CacheMode() string {
	switch c.ContextCacheMode {
	case CacheModeOff, CacheModeContextAPI:
		return c.ContextCacheMode
	default:
		return CacheModeFull
	}
}

// ContextAPIConfig returns the resolved context_api parameters with defaults applied.
func (c GlassConfig) ContextAPIConfig() (triggerTokens, keepToolUses, clearAtLeast int, clearThinking bool) {
	triggerTokens = c.ContextAPITriggerTokens
	if triggerTokens <= 0 {
		triggerTokens = 100000
	}
	keepToolUses = c.ContextAPIKeepToolUses
	if keepToolUses <= 0 {
		keepToolUses = 3
	}
	clearAtLeast = c.ContextAPIClearAtLeast
	if clearAtLeast <= 0 {
		clearAtLeast = 20000
	}
	clearThinking = c.ContextAPIClearThinking || c.ContextAPITriggerTokens == 0 // default true when unconfigured
	return
}

// CompressionBatch returns the effective batch size for watermark advancement.
// Default 16 (matches breakpointAdvanceThreshold for synchronized breaks).
// 0 means eager (advance every request) — the legacy behavior.
func (c GlassConfig) CompressionBatch() int {
	if c.CompressionBatchSize > 0 {
		return c.CompressionBatchSize
	}
	if c.CompressionBatchSize < 0 {
		return 0 // explicit 0 = eager
	}
	return 16 // default: batch of 16 messages
}

// CompressionTrigger returns the token threshold before compression activates.
// Default 0 (always compress — tool results are stale after first read).
// Set to a positive value to delay compression until the conversation is large.
// -1 means never compress.
func (c GlassConfig) CompressionTrigger() int {
	if c.CompressionTriggerTokens != 0 {
		return c.CompressionTriggerTokens
	}
	return 0 // compress from the start — the model already read the content
}

// DefaultGlassConfig returns sane defaults.
func DefaultGlassConfig() GlassConfig {
	return GlassConfig{
		EvictTriggerTokens:        165000,
		EvictTargetTokens:         135000,
		AnchorKeepMsgs:            4,
		RecentKeepMsgs:            20,
		ShadowDir:                 runtimepaths.Current().GlassShadowDir,
		SpoofUsageCap:             140000,
		StripSysReminders:         true,
		StripThinking:             true,
		KeepaliveIntervalSec:      240,
		QuarantineCreateThreshold: 120000,
		QuarantineReadThreshold:   15000,
		QuarantineStreakThreshold: 2,
		QuarantineWindowSec:       300,
		CompressionFlushTokens:    200000,
	}
}

// EvictionThresholds holds the trigger/target pair for a specific lane.
type EvictionThresholds struct {
	Trigger int
	Target  int
}

// OpenAIThresholds returns eviction thresholds for OpenAI lane sessions.
// Falls back to defaults suitable for 66K context windows.
func (c GlassConfig) OpenAIThresholds() EvictionThresholds {
	trigger := c.OpenAIEvictTriggerTokens
	if trigger == 0 {
		trigger = 55000
	}
	target := c.OpenAIEvictTargetTokens
	if target == 0 {
		target = 45000
	}
	return EvictionThresholds{Trigger: trigger, Target: target}
}

// AnthropicThresholds returns eviction thresholds for Anthropic lane sessions.
func (c GlassConfig) AnthropicThresholds() EvictionThresholds {
	return EvictionThresholds{Trigger: c.EvictTriggerTokens, Target: c.EvictTargetTokens}
}

// CodexThresholds returns eviction thresholds for Codex lane sessions.
// Defaults are tuned to stay within GPT-5.4's standard pricing tier (272K).
func (c GlassConfig) CodexThresholds() EvictionThresholds {
	trigger := c.CodexEvictTriggerTokens
	if trigger == 0 {
		trigger = 200000
	}
	target := c.CodexEvictTargetTokens
	if target == 0 {
		target = 150000
	}
	return EvictionThresholds{Trigger: trigger, Target: target}
}

// GeminiThresholds returns eviction thresholds for Gemini lane sessions.
// Defaults are tuned for Gemini 2.5 Pro's 1M token context window.
func (c GlassConfig) GeminiThresholds() EvictionThresholds {
	trigger := c.GeminiEvictTriggerTokens
	if trigger == 0 {
		trigger = 700000
	}
	target := c.GeminiEvictTargetTokens
	if target == 0 {
		target = 500000
	}
	return EvictionThresholds{Trigger: trigger, Target: target}
}

// SessionState tracks per-conversation eviction state.
// Persisted to disk under the active Glass shadow dir.
type SessionState struct {
	ConvID                string              `json:"conv_id"`
	EvictedHashes         map[string]bool     `json:"evicted_hashes"` // monotonic — only grows
	EvictedCount          int                 `json:"evicted_count"`  // total messages evicted
	LastAPIInput          int                 `json:"last_api_input"` // last real input_tokens from API
	LastAPIInputAt        time.Time           `json:"last_api_input_at"`
	WindowStart           int                 `json:"window_start"`    // first retained message index (0-based)
	BatchCount            int                 `json:"batch_count"`     // number of eviction batches
	ReferenceBatch        int                 `json:"reference_batch"` // first eviction batch that rendered the reference pair
	ReferenceUser         string              `json:"reference_user"`  // frozen user reference message
	ReferenceAsst         string              `json:"reference_asst"`  // frozen assistant reference message
	CreatedAt             time.Time           `json:"created_at"`
	UpdatedAt             time.Time           `json:"updated_at"`
	TopicHints            []string            `json:"topic_hints"` // per-batch topic summaries
	TopicHintFloorBatch   int                 `json:"topic_hint_floor_batch,omitempty"`
	ChapterID             string              `json:"chapter_id,omitempty"`
	ChapterMDPath         string              `json:"chapter_md_path,omitempty"`
	ChapterJSONPath       string              `json:"chapter_json_path,omitempty"`
	ChapterStatus         string              `json:"chapter_status,omitempty"`
	ChapterCheckpointRefs []ChapterCheckpoint `json:"chapter_checkpoint_refs,omitempty"`
	ChapterUpdatedAt      time.Time           `json:"chapter_updated_at,omitempty"`
	ChapterSealReason     string              `json:"chapter_seal_reason,omitempty"`

	// Compression state
	CompressionWatermark int `json:"compression_watermark,omitempty"` // last watermark position

	// PreCompressionSnapshot holds deep copies of messages (keyed by index)
	// captured before CompressOldMessages runs. The eviction/chapter path
	// uses these originals instead of post-compression truncated content.
	// Transient — not persisted to disk.
	PreCompressionSnapshot map[int]interface{} `json:"-"`
}

// SessionStore manages per-conversation Glass state.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*SessionState
	baseDir  string
}

// NewSessionStore creates a store backed by the given directory.
func NewSessionStore(baseDir string) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*SessionState),
		baseDir:  baseDir,
	}
}

// Get returns session state for a conversation, loading from disk if needed.
func (s *SessionStore) Get(convID string) *SessionState {
	s.mu.RLock()
	if st, ok := s.sessions[convID]; ok {
		s.mu.RUnlock()
		return st
	}
	s.mu.RUnlock()

	// Try loading from disk
	st := s.loadFromDisk(convID)
	if st == nil {
		st = &SessionState{
			ConvID:        convID,
			EvictedHashes: make(map[string]bool),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
	}

	s.mu.Lock()
	// Double-check another goroutine didn't load while we were reading disk
	if existing, ok := s.sessions[convID]; ok {
		s.mu.Unlock()
		return existing
	}
	s.sessions[convID] = st
	s.mu.Unlock()
	return st
}

// Save persists session state to disk.
func (s *SessionStore) Save(st *SessionState) error {
	st.UpdatedAt = time.Now()
	dir := filepath.Join(s.baseDir, st.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create glass dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func (s *SessionStore) loadFromDisk(convID string) *SessionState {
	path := filepath.Join(s.baseDir, convID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st SessionState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	if st.EvictedHashes == nil {
		st.EvictedHashes = make(map[string]bool)
	}
	return &st
}

func (st *SessionState) ClearAPITokenBudget() {
	if st == nil {
		return
	}
	st.LastAPIInput = 0
	st.LastAPIInputAt = time.Time{}
}

func (st *SessionState) ResetForFreshCache() {
	if st == nil {
		return
	}
	convID := st.ConvID
	createdAt := st.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	*st = SessionState{
		ConvID:        convID,
		EvictedHashes: make(map[string]bool),
		CreatedAt:     createdAt,
		UpdatedAt:     time.Now(),
	}
}

// msgHash returns a stable hash for a message (used for eviction tracking).
// Uses the JSON-serialized message content. Deterministic for identical messages.
func msgHash(msg map[string]interface{}) string {
	data, err := json.Marshal(msg)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// estimateTokens gives a rough token count for a JSON value.
// Uses 4 chars per token as a conservative estimate.
func estimateTokens(v interface{}) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data) / 4
}

// estimateMessageTokens estimates tokens for a single message.
func estimateMessageTokens(msg map[string]interface{}) int {
	return estimateTokens(msg)
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}
