package codex

import "testing"

func TestResolveResponsesUpstreamUsesChatGPTBackendForJWTBearer(t *testing.T) {
	got := resolveResponsesUpstream(
		"https://api.openai.com",
		"",
		"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJjb2RleCJ9.signature",
	)

	want := "https://chatgpt.com/backend-api/codex/responses"
	if got != want {
		t.Fatalf("resolveResponsesUpstream() = %q, want %q", got, want)
	}
}

func TestResolveResponsesUpstreamUsesOpenAIResponsesForAPIKeyBearer(t *testing.T) {
	got := resolveResponsesUpstream(
		"https://api.openai.com",
		"https://chatgpt.com/backend-api/codex",
		"Bearer sk-test",
	)

	want := "https://api.openai.com/v1/responses"
	if got != want {
		t.Fatalf("resolveResponsesUpstream() = %q, want %q", got, want)
	}
}

func TestShouldUseChatGPTUpstreamRejectsNonBearerAuth(t *testing.T) {
	if shouldUseChatGPTUpstream("Basic abc123") {
		t.Fatal("expected non-bearer auth to stay off the ChatGPT backend")
	}
}

func TestResolveCodexUpstreamBaseUsesV1ForPublicOpenAI(t *testing.T) {
	got := resolveCodexUpstreamBase(
		"https://api.openai.com",
		"https://chatgpt.com/backend-api/codex",
		"Bearer sk-test",
	)

	want := "https://api.openai.com/v1"
	if got != want {
		t.Fatalf("resolveCodexUpstreamBase() = %q, want %q", got, want)
	}
}
