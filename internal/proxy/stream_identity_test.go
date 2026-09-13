package proxy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/subagent"
	"proxy.local/app/internal/trimmer"
)

func TestEffectiveStreamingConvIDPrefersGlassResultOverPIDlessFallback(t *testing.T) {
	t.Parallel()

	body := map[string]interface{}{
		"system": "You are a helpful assistant",
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
		},
	}

	req := httptest.NewRequest("POST", "http://proxy.test/v1/messages", nil)
	req = withGlassResult(req, glass.ProcessResult{
		RequestKey:  "request-lane",
		SessionKey:  "session-lane",
		AffinityKey: "affinity-lane",
	})

	got := effectiveStreamingConvID(req.Context(), body)
	if got != "request-lane" {
		t.Fatalf("effective streaming conv id = %q, want request key", got)
	}

	pidless := trimmer.SessionFingerprint(body, 0)
	if got == pidless {
		t.Fatalf("effective streaming conv id fell back to pidless fingerprint %q", got)
	}
}

func TestEffectiveStreamingConvIDPrefersContextConvID(t *testing.T) {
	t.Parallel()

	body := map[string]interface{}{"system": "x"}
	req := httptest.NewRequest("POST", "http://proxy.test/v1/messages", nil)
	req = withGlassResult(req, glass.ProcessResult{RequestKey: "request-lane"})
	req = withConvID(req, "context-conv")

	if got := effectiveStreamingConvID(req.Context(), body); got != "context-conv" {
		t.Fatalf("effective streaming conv id = %q, want context conv id", got)
	}
}

func TestEffectiveStreamingSubagentPrefersGlassResultClassification(t *testing.T) {
	t.Parallel()

	body := map[string]interface{}{
		"system": strings.Repeat("This is the full Claude Code system prompt. ", 300),
		"tools": []interface{}{
			map[string]interface{}{"name": "read"},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "subagent request"},
				},
			},
		},
	}

	if info := subagent.Classify(body); info.IsSubagent {
		t.Fatalf("setup error: expected pidless classification to look like a main session, got %+v", info)
	}

	req := httptest.NewRequest("POST", "http://proxy.test/v1/messages", nil)
	req = withGlassResult(req, glass.ProcessResult{
		Subagent: subagent.Classification{
			IsSubagent:             true,
			Type:                   subagent.TypeAgentTool,
			IsolateSession:         true,
			DisableUpstreamCaching: true,
		},
	})

	got := effectiveStreamingSubagent(body, req.Context())
	if !got.IsSubagent || got.Type != subagent.TypeAgentTool {
		t.Fatalf("effective streaming subagent = %+v, want agent_tool classification from Glass", got)
	}
}
