package proxy

import (
	"testing"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/subagent"
)

func TestDeriveRequestKeySeparatesSmallSystemFromParentAffinity(t *testing.T) {
	t.Parallel()

	body := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "tiny child prompt"},
		},
	}
	info := subagent.Classify(body)
	if !info.BypassMessageCache {
		t.Fatalf("expected small-system bypass classification")
	}

	got := deriveRequestKey("parent-session", "parent-session", info, body)
	if got == "parent-session" {
		t.Fatalf("expected distinct request key, got parent key %q", got)
	}
	if got == "" {
		t.Fatal("expected non-empty request key")
	}
}

func TestShouldUpdateGlassTokensSkipsBypassMessageCacheRequests(t *testing.T) {
	t.Parallel()

	if shouldUpdateGlassTokens(glass.ProcessResult{Subagent: subagent.Classification{BypassMessageCache: true}}) {
		t.Fatal("expected uncached child requests to skip Glass token accounting")
	}
	if !shouldUpdateGlassTokens(glass.ProcessResult{Subagent: subagent.Classification{}}) {
		t.Fatal("expected normal Glass requests to update token accounting")
	}
}

func TestEnsureAnthropicBetaAddsExtendedCacheTTLAndNormalizes(t *testing.T) {
	t.Parallel()

	got := ensureAnthropicBeta([]string{"beta-two", glass.ExtendedCacheTTLBeta, "beta-one", "beta-two"}, glass.ExtendedCacheTTLBeta)
	want := []string{"beta-one", "beta-two", glass.ExtendedCacheTTLBeta}
	if len(got) != len(want) {
		t.Fatalf("betas len=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("betas[%d]=%q want=%q (%v)", i, got[i], want[i], got)
		}
	}
}
