// Package config provides hot-reloadable JSON configuration for the transparent proxy.
// Mirrors trimmer_config.json schema from Python reference.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Config holds all trimmer/proxy settings. JSON field names match Python config keys.
type Config struct {
	Enabled                   bool     `json:"enabled"`
	StripMCPTools             bool     `json:"strip_mcp_tools"`
	MCPDisabled               []string `json:"mcp_disabled"`
	TrimMessages              bool     `json:"trim_messages"`
	TrimThresholdTokens       int      `json:"trim_threshold_tokens"`
	TrimKeepRecent            int      `json:"trim_keep_recent"`
	TrimBatchSize             int      `json:"trim_batch_size"`
	TrimMaxToolResultChars    int      `json:"trim_max_tool_result_chars"`
	TrimMaxAssistantChars     int      `json:"trim_max_assistant_chars"`
	StripSystemReminders      bool     `json:"strip_system_reminders"`
	StripThinkingBlocks       bool     `json:"strip_thinking_blocks"`
	StripOldThinking          bool     `json:"strip_old_thinking"`
	ThinkingReplaceText       string   `json:"thinking_replace_text"`
	SystemReminderReplaceText string   `json:"system_reminder_replace_text"`
	InjectWatermarkBreakpoint bool     `json:"inject_watermark_breakpoint"`
	DropKeepMinMessages       int      `json:"drop_keep_min_messages"`
	DropTriggerTokens         int      `json:"drop_trigger_tokens"`
	DropTargetTokens          int      `json:"drop_target_tokens"`
	BlockSubagents            bool     `json:"block_subagents"`
	SpoofSystemPrompt         bool     `json:"sysprompt_enabled"`
	SpoofUsageCap             int      `json:"spoof_usage_cap_tokens"`
	Stage2CooldownSec         int      `json:"stage2_cooldown_s"`       // default 30
	Stage2EmergencyTokens     int      `json:"stage2_emergency_tokens"` // default 190000
	ForceThinking             *bool    `json:"force_thinking"`
	ForceThinkingBudget       *int     `json:"force_thinking_budget"`
	LegacyThinkingBudget      *int     `json:"thinking_budget"`
	// Retained only so stale configs that still enable the retired experiment
	// fail hard instead of silently no-oping.
	SmallSystemRepeatCacheEnabled   bool   `json:"small_system_repeat_cache_enabled"`
	SmallSystemRepeatCacheMinHits   int    `json:"small_system_repeat_cache_min_hits"`
	SmallSystemRepeatCacheWindowSec int    `json:"small_system_repeat_cache_window_sec"`
	ForceInterleaved                *bool  `json:"force_interleaved"`
	BlockNonOpus                    *bool  `json:"block_non_opus"`
	SyspromptReplaceFile            string `json:"sysprompt_replace_file"`
	SyspromptPatchFile              string `json:"sysprompt_patch_file"`

	// Template Injection — auto-inject into CC CLI requests when enabled.
	// UI writes these via config_server → glass_config.json → proxy hot-reloads.
	TplInjectEnabled     bool    `json:"tpl_inject_enabled"`
	TplInjectTechnique   string  `json:"tpl_inject_technique"`
	TplInjectModelFamily string  `json:"tpl_inject_model_family"`
	TplInjectMode        string  `json:"tpl_inject_mode"`
	TplInjectIntensity   float64 `json:"tpl_inject_intensity"`

	// Steganography — hide instructions in text/images of CC CLI requests.
	// ASCII smuggling hides invisible chars in text content blocks.
	// LSB embeds instructions in image pixel data (91%+ ASR on multimodal).
	StegoEnabled       bool   `json:"stego_enabled"`
	StegoMethod        string `json:"stego_method"`
	StegoHiddenMessage string `json:"stego_hidden_message"`
	StegoLSBEnabled    bool   `json:"stego_lsb_enabled"`
	StegoLSBMessage    string `json:"stego_lsb_message"`

	// Bypass Pipeline — apply any registered bypass technique inline.
	// Operator selects technique+params in Attack Lab, written to glass_config.json.
	// Proxy calls config_server → bypass_framework to transform prompts.
	BypassEnabled                 bool              `json:"bypass_enabled"`
	BypassTechnique               string            `json:"bypass_technique"`
	BypassModel                   string            `json:"bypass_model"`
	BypassParams                  map[string]string `json:"bypass_params"`
	AutoRefusalRewriteEnabled     bool              `json:"auto_refusal_rewrite_enabled"`
	AutoRefusalRewriteStrategy    string            `json:"auto_refusal_rewrite_strategy"`
	AutoUsagePolicyRewriteEnabled bool              `json:"auto_usage_policy_rewrite_enabled"`

	// Redteam Sidecar — apply redteam_transforms pipeline via config_server on every request.
	// When enabled the proxy POSTs {system, messages} to /api/redteam/apply and uses the result.
	RedteamSidecarEnabled bool `json:"redteam_sidecar_enabled"`
	// Security Guard — local URL/content/package verification sidecar.
	SecurityGuardEnabled                    bool     `json:"security_guard_enabled"`
	SecurityGuardBind                       string   `json:"security_guard_bind"`
	SecurityGuardBaseURL                    string   `json:"security_guard_base_url"`
	SecurityGuardFailClosed                 bool     `json:"security_guard_fail_closed"`
	SecurityGuardPackageMinAgeHours         int      `json:"security_guard_package_min_age_hours"`
	SecurityGuardRequireVerifiedAttestation bool     `json:"security_guard_require_verified_attestation"`
	SecurityGuardAllowedHosts               []string `json:"security_guard_allowed_hosts"`
	SecurityGuardAllowedPackages            []string `json:"security_guard_allowed_packages"`

	// Extended guard scanners (GLASSDD)
	SecurityGuardOSVEnabled       bool     `json:"security_guard_osv_enabled"`
	SecurityGuardGuardDogEnabled  bool     `json:"security_guard_guarddog_enabled"`
	SecurityGuardDnstwistEnabled  bool     `json:"security_guard_dnstwist_enabled"`
	SecurityGuardJSXRayEnabled    bool     `json:"security_guard_jsxray_enabled"`
	SecurityGuardLLMGuardEnabled  bool     `json:"security_guard_llmguard_enabled"`
	SecurityGuardLLMGuardBind     string   `json:"security_guard_llmguard_bind"`
	SecurityGuardProtectedDomains []string `json:"security_guard_protected_domains"`
	SecurityGuardVenvPath         string   `json:"security_guard_venv_path"`

	// Transport-layer evasion modifiers (applied on all outbound upstream requests)
	TransportChunked        bool   `json:"transport_chunked"`         // Use chunked Transfer-Encoding
	TransportPadBytes       int    `json:"transport_pad_bytes"`       // Append N random bytes to body
	TransportMethodOverride string `json:"transport_method_override"` // Add X-HTTP-Method-Override header
	TransportContentType    string `json:"transport_content_type"`    // Override Content-Type header

	// Master kill switch for the Glass eviction/cache pipeline. When true, the
	// proxy skips glassEngine.Process() entirely on the Anthropic lane. Lightweight
	// strip transforms (system reminders, old thinking) still run so the
	// strip_system_reminders / strip_thinking_blocks flags retain their effect.
	// Use to let native Claude Code compaction handle context (e.g. at 1M tier).
	GlassPassthrough bool `json:"glass_passthrough"`
}

// Defaults matching trimmer_config.json
var DefaultConfig = Config{
	Enabled:                                 true,
	StripMCPTools:                           false,
	MCPDisabled:                             nil,
	TrimMessages:                            true,
	TrimThresholdTokens:                     140000,
	TrimKeepRecent:                          20,
	TrimBatchSize:                           80,
	TrimMaxToolResultChars:                  700,
	TrimMaxAssistantChars:                   500,
	StripSystemReminders:                    true,
	StripThinkingBlocks:                     true,
	StripOldThinking:                        true,
	InjectWatermarkBreakpoint:               true,
	DropKeepMinMessages:                     6,
	DropTriggerTokens:                       180000,
	DropTargetTokens:                        140000,
	SpoofSystemPrompt:                       false,
	SpoofUsageCap:                           0,
	AutoRefusalRewriteEnabled:               false,
	AutoRefusalRewriteStrategy:              "full_comply",
	AutoUsagePolicyRewriteEnabled:           false,
	SmallSystemRepeatCacheEnabled:           false,
	SmallSystemRepeatCacheMinHits:           2,
	SmallSystemRepeatCacheWindowSec:         1800,
	SecurityGuardEnabled:                    false,
	SecurityGuardBind:                       "127.0.0.1:18900",
	SecurityGuardBaseURL:                    "http://127.0.0.1:18900",
	SecurityGuardFailClosed:                 true,
	SecurityGuardPackageMinAgeHours:         168,
	SecurityGuardRequireVerifiedAttestation: true,
	SecurityGuardOSVEnabled:                 true,
	SecurityGuardGuardDogEnabled:            false,
	SecurityGuardDnstwistEnabled:            false,
	SecurityGuardJSXRayEnabled:              false,
	SecurityGuardLLMGuardEnabled:            false,
	SecurityGuardLLMGuardBind:               "127.0.0.1:18901",
	SecurityGuardProtectedDomains:           []string{"github.com", "npmjs.com", "pypi.org", "registry.npmjs.org", "api.github.com"},
	SecurityGuardVenvPath:                   "COMMANDER/.venv",
}

// Loader watches a JSON config file and hot-reloads on mtime change.
type Loader struct {
	path    string
	mu      sync.RWMutex
	current Config
	mtime   time.Time
}

// NewLoader creates a config loader. Loads immediately or falls back to defaults.
func NewLoader(path string) *Loader {
	l := &Loader{
		path:    path,
		current: DefaultConfig,
	}
	l.reload()
	return l
}

// Get returns a snapshot of the current config (thread-safe).
func (l *Loader) Get() Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.current
}

// Poll checks mtime and reloads if changed. Call from a goroutine or before each request.
func (l *Loader) Poll() {
	info, err := os.Stat(l.path)
	if err != nil {
		return
	}
	l.mu.RLock()
	same := info.ModTime().Equal(l.mtime)
	l.mu.RUnlock()
	if same {
		return
	}
	l.reload()
}

func (l *Loader) reload() {
	data, err := os.ReadFile(l.path)
	if err != nil {
		log.Printf("[CONFIG] Cannot read %s: %v (using current/defaults)", l.path, err)
		return
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[CONFIG] Invalid JSON in %s: %v (keeping current)", l.path, err)
		return
	}
	cfg.normalizeAliases()
	info, _ := os.Stat(l.path)
	l.mu.Lock()
	l.current = cfg
	if info != nil {
		l.mtime = info.ModTime()
	}
	l.mu.Unlock()
	log.Printf("[CONFIG] Reloaded %s", l.path)
}

func (c *Config) normalizeAliases() {
	if c.ForceThinkingBudget == nil && c.LegacyThinkingBudget != nil {
		budget := *c.LegacyThinkingBudget
		c.ForceThinkingBudget = &budget
	}
}

// ValidateRuntimeSupport rejects stale config that still enables retired
// runtime features. Defaults may still carry disabled legacy keys for
// compatibility, but enabling them must fail explicitly.
func (c Config) ValidateRuntimeSupport() error {
	if c.SmallSystemRepeatCacheEnabled {
		return fmt.Errorf(
			"small_system_repeat_cache_enabled is no longer supported by the proxy runtime; remove small_system_repeat_cache_enabled, small_system_repeat_cache_min_hits, and small_system_repeat_cache_window_sec from config",
		)
	}
	return nil
}

// StartPolling runs a background goroutine that checks config every interval.
func (l *Loader) StartPolling(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			l.Poll()
		}
	}()
}
