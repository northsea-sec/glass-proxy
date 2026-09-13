package proxy

import (
	"testing"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/forcemode"
)

func TestMergeForceModeUsesConfigOverrides(t *testing.T) {
	baseInterleaved := true
	base := forcemode.Config{
		ForceThinking:    false,
		ThinkingBudget:   0,
		ForceInterleaved: &baseInterleaved,
		BlockNonOpus:     false,
	}

	forceThinking := true
	budget := 31999
	forceInterleaved := false
	blockNonOpus := true
	cfg := config.Config{
		ForceThinking:       &forceThinking,
		ForceThinkingBudget: &budget,
		ForceInterleaved:    &forceInterleaved,
		BlockNonOpus:        &blockNonOpus,
	}

	merged := mergeForceMode(base, cfg)
	if !merged.ForceThinking {
		t.Fatalf("ForceThinking not overridden from config")
	}
	if merged.ThinkingBudget != budget {
		t.Fatalf("ThinkingBudget = %d, want %d", merged.ThinkingBudget, budget)
	}
	if merged.ForceInterleaved == nil || *merged.ForceInterleaved {
		t.Fatalf("ForceInterleaved = %v, want false", merged.ForceInterleaved)
	}
	if !merged.BlockNonOpus {
		t.Fatalf("BlockNonOpus not overridden from config")
	}
}

func TestMergeForceModePreservesEnvDefaultsWhenConfigUnset(t *testing.T) {
	baseInterleaved := true
	base := forcemode.Config{
		ForceThinking:    true,
		ThinkingBudget:   31999,
		ForceInterleaved: &baseInterleaved,
		BlockNonOpus:     true,
	}

	merged := mergeForceMode(base, config.Config{})
	if !merged.ForceThinking {
		t.Fatalf("ForceThinking should preserve base value")
	}
	if merged.ThinkingBudget != 31999 {
		t.Fatalf("ThinkingBudget = %d, want 31999", merged.ThinkingBudget)
	}
	if merged.ForceInterleaved == nil || !*merged.ForceInterleaved {
		t.Fatalf("ForceInterleaved should preserve base true value")
	}
	if !merged.BlockNonOpus {
		t.Fatalf("BlockNonOpus should preserve base value")
	}
}

func TestMergeForceModeApplyForcesThinkingFromConfig(t *testing.T) {
	forceThinking := true
	budget := 31999
	cfg := config.Config{
		ForceThinking:       &forceThinking,
		ForceThinkingBudget: &budget,
	}

	body := map[string]interface{}{
		"thinking": map[string]interface{}{
			"type": "adaptive",
		},
	}

	merged := mergeForceMode(forcemode.Config{}, cfg)
	if !merged.Apply(body) {
		t.Fatalf("expected merged force mode to modify request body")
	}

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("thinking block missing after apply")
	}
	if got, _ := thinking["type"].(string); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", got)
	}
	switch v := thinking["budget_tokens"].(type) {
	case int:
		if v != 31999 {
			t.Fatalf("thinking.budget_tokens = %d, want 31999", v)
		}
	case float64:
		if int(v) != 31999 {
			t.Fatalf("thinking.budget_tokens = %v, want 31999", v)
		}
	default:
		t.Fatalf("unexpected budget token type %T", v)
	}
}
