package forcemode

import "testing"

func TestApplyPreservesConfiguredThinkingBudgetAndRaisesMaxTokens(t *testing.T) {
	cfg := Config{
		ForceThinking:  true,
		ThinkingBudget: 31999,
	}

	body := map[string]interface{}{
		"max_tokens": 1024,
		"thinking": map[string]interface{}{
			"type": "adaptive",
		},
	}

	if !cfg.Apply(body) {
		t.Fatal("expected thinking force mode to modify request")
	}

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatal("expected thinking block to exist")
	}
	if got, _ := thinking["type"].(string); got != "enabled" {
		t.Fatalf("thinking.type = %q, want enabled", got)
	}
	if got, _ := requestInt(thinking["budget_tokens"]); got != 31999 {
		t.Fatalf("thinking.budget_tokens = %d, want 31999", got)
	}
	if got, _ := requestInt(body["max_tokens"]); got != 32000 {
		t.Fatalf("max_tokens = %d, want 32000", got)
	}
}

func TestApplyDoesNotLowerMaxTokensWhenAlreadyLargeEnough(t *testing.T) {
	cfg := Config{
		ForceThinking:  true,
		ThinkingBudget: 31999,
	}

	body := map[string]interface{}{
		"max_tokens": 64000,
	}

	if !cfg.Apply(body) {
		t.Fatal("expected thinking force mode to modify request")
	}
	if got, _ := requestInt(body["max_tokens"]); got != 64000 {
		t.Fatalf("max_tokens = %d, want 64000", got)
	}
}

func TestApplyFallbackBudgetAlsoRaisesMaxTokens(t *testing.T) {
	cfg := Config{
		ForceThinking: true,
	}

	body := map[string]interface{}{
		"max_tokens": 1024,
		"thinking": map[string]interface{}{
			"type": "adaptive",
		},
	}

	if !cfg.Apply(body) {
		t.Fatal("expected thinking force mode to modify request")
	}

	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatal("expected thinking block to exist")
	}
	if got, _ := requestInt(thinking["budget_tokens"]); got != 31999 {
		t.Fatalf("thinking.budget_tokens = %d, want 31999", got)
	}
	if got, _ := requestInt(body["max_tokens"]); got != 32000 {
		t.Fatalf("max_tokens = %d, want 32000", got)
	}
}
