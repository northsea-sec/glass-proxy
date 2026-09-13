package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoaderMapsLegacyThinkingBudgetAlias(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glass_config.json")
	data := []byte(`{
  "enabled": true,
  "force_thinking": true,
  "thinking_budget": 31999
}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path)
	cfg := loader.Get()

	if cfg.ForceThinkingBudget == nil {
		t.Fatal("ForceThinkingBudget should be populated from thinking_budget")
	}
	if got := *cfg.ForceThinkingBudget; got != 31999 {
		t.Fatalf("ForceThinkingBudget = %d, want 31999", got)
	}
}

func TestLoaderReadsSmallSystemRepeatCacheFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "glass_config.json")
	data := []byte(`{
  "small_system_repeat_cache_enabled": true,
  "small_system_repeat_cache_min_hits": 3,
  "small_system_repeat_cache_window_sec": 900
}`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	loader := NewLoader(path)
	cfg := loader.Get()

	if !cfg.SmallSystemRepeatCacheEnabled {
		t.Fatal("SmallSystemRepeatCacheEnabled should be true")
	}
	if cfg.SmallSystemRepeatCacheMinHits != 3 {
		t.Fatalf("SmallSystemRepeatCacheMinHits = %d, want 3", cfg.SmallSystemRepeatCacheMinHits)
	}
	if cfg.SmallSystemRepeatCacheWindowSec != 900 {
		t.Fatalf("SmallSystemRepeatCacheWindowSec = %d, want 900", cfg.SmallSystemRepeatCacheWindowSec)
	}
}

func TestValidateRuntimeSupportRejectsRetiredSmallSystemRepeatCache(t *testing.T) {
	cfg := Config{
		SmallSystemRepeatCacheEnabled:   true,
		SmallSystemRepeatCacheMinHits:   3,
		SmallSystemRepeatCacheWindowSec: 900,
	}

	err := cfg.ValidateRuntimeSupport()
	if err == nil {
		t.Fatal("expected retired small-system repeat-cache config to be rejected")
	}
	if got := err.Error(); got == "" || got == "small_system_repeat_cache_enabled" {
		t.Fatalf("unexpected validation error %q", got)
	}
}
