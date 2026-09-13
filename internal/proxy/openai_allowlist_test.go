package proxy

import (
	"encoding/json"
	"os"
	"testing"
)

// loadFixture reads a captured request body from testdata.
func loadFixture(t *testing.T, name string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return body
}

func marshalMessages(t *testing.T, msgs []interface{}) []byte {
	t.Helper()
	out, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("marshal messages: %v", err)
	}
	return out
}

// TestOpenAIAllowlistStripMakesOmpRequestFit16K is the golden contract for
// the GLM advisor lane: the captured omp request (~11.1K estimated tokens
// with 7 tool schemas; the original 400 was the upstream's transient 4352
// ctx, not the request size) must stay inside the 13K trigger budget and
// get substantially smaller after the tool allowlist strip.
func TestOpenAIAllowlistStripMakesOmpRequestFit16K(t *testing.T) {
	body := loadFixture(t, "omp_capture_13k.json")

	// Baseline: without stripping, the request fits the 13K budget.
	msgs, _ := body["messages"].([]interface{})
	toolTokens := estimateOpenAITokens(body["tools"])
	base, err := buildOpenAIRequestMessages(msgs, toolTokens, 13000, 10000)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Apply the allowlist (advisor role keeps read + grep only).
	stripped := stripOpenAIToolsToAllowlist(body, []string{"read", "grep"})
	if stripped != 5 {
		t.Fatalf("stripped = %d, want 5 (7 tools, allowlist 2)", stripped)
	}
	// Idempotent: a second pass strips nothing (stable bytes across turns).
	if again := stripOpenAIToolsToAllowlist(body, []string{"read", "grep"}); again != 0 {
		t.Fatalf("second strip = %d, want 0 (idempotency)", again)
	}

	// Now the trimmed request fits the 13K/10K budget.
	toolTokens = estimateOpenAITokens(body["tools"])
	res, err := buildOpenAIRequestMessages(msgs, toolTokens, 13000, 10000)
	if err != nil {
		t.Fatalf("after strip: %v", err)
	}
	if res.TotalTokens > 13000 {
		t.Fatalf("TotalTokens = %d, want <= 13000", res.TotalTokens)
	}
	if base.TotalTokens-res.TotalTokens < 3000 {
		t.Fatalf("strip saved %d tokens, want >= 3000 (schemas are the lever)",
			base.TotalTokens-res.TotalTokens)
	}

	// Determinism: rebuilding produces byte-identical messages (token-prefix
	// cache upstream sees the same prefix every turn).
	a := marshalMessages(t, res.Messages)
	res2, err := buildOpenAIRequestMessages(msgs, toolTokens, 13000, 10000)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	b := marshalMessages(t, res2.Messages)
	if string(a) != string(b) {
		t.Fatalf("rebuild not byte-identical")
	}
}

// TestOpenAIAllowlistEmptyKeepsAllTools: no allowlist configured = no-op.
func TestOpenAIAllowlistEmptyKeepsAllTools(t *testing.T) {
	body := loadFixture(t, "omp_capture_13k.json")
	tools, _ := body["tools"].([]interface{})
	before := len(tools)
	if stripped := stripOpenAIToolsToAllowlist(body, nil); stripped != 0 {
		t.Fatalf("nil allowlist stripped %d, want 0", stripped)
	}
	after, _ := body["tools"].([]interface{})
	if len(after) != before {
		t.Fatalf("tool count changed %d -> %d", before, len(after))
	}
}
