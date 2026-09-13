package subagent

import (
	"strings"
	"testing"
)

func TestClassifySmallSystemDisablesCachingAndUsesParentAffinity(t *testing.T) {
	bodyA := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "aaaa"},
			map[string]interface{}{"type": "text", "text": "bbbb"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}
	bodyB := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "cccc"},
			map[string]interface{}{"type": "text", "text": "dddd"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}

	a := Classify(bodyA)
	b := Classify(bodyB)

	if !a.IsSubagent || !a.BypassCanonical || !a.BypassMessageCache || !a.DisableUpstreamCaching || !a.UseParentAffinity {
		t.Fatalf("expected small-system subagent classification, got %+v", a)
	}
	if a.Type != TypeSmallSystem {
		t.Fatalf("expected small-system type, got %q", a.Type)
	}
	if a.IsolateSession || b.IsolateSession {
		t.Fatalf("small-system requests should bypass Glass cache, not create isolated cache lanes: %+v %+v", a, b)
	}
}

func TestClassifySmallSystemWithToolsIsolates(t *testing.T) {
	body := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "small agent prompt"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "bash", "description": "run commands"},
			map[string]interface{}{"name": "read", "description": "read files"},
		},
	}

	info := Classify(body)
	if !info.IsSubagent {
		t.Fatal("expected subagent classification")
	}
	if info.Type != TypeSmallSystem {
		t.Fatalf("expected small_system type, got %q", info.Type)
	}
	if !info.HasTools {
		t.Fatal("expected HasTools=true")
	}
	if !info.IsolateSession {
		t.Fatal("expected IsolateSession=true for tool-bearing small_system subagent")
	}
	if info.BypassMessageCache {
		t.Fatal("tool-bearing subagent should NOT bypass message cache")
	}
	if info.UseParentAffinity {
		t.Fatal("tool-bearing subagent should NOT use parent affinity")
	}
	if info.SessionSuffix == "" {
		t.Fatal("expected non-empty session suffix")
	}
}

func TestClassifyModelSubagent(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-sonnet-4-5",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("x", 6000)},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}

	info := Classify(body)
	if !info.IsSubagent || !info.BlockByModel {
		t.Fatalf("expected model-routed subagent, got %+v", info)
	}
	if info.Type != TypeSonnet {
		t.Fatalf("expected sonnet type, got %q", info.Type)
	}
	if info.IsolateSession {
		t.Fatalf("large-system model subagent should not be isolated by size alone: %+v", info)
	}
}

func TestWithCacheContextMarksOpusSubset(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-opus-4-1",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("x", 9000)},
		},
		"messages": make([]interface{}, 6),
	}

	info := WithCacheContext(Classify(body), 20)
	if !info.IsSubagent || !info.UseFrozenPrefix {
		t.Fatalf("expected opus subset classification, got %+v", info)
	}
	if info.Type != TypeOpusSubset {
		t.Fatalf("expected opus subset type, got %q", info.Type)
	}
}

func TestTelemetryOverlayMarksAgentToolRequests(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{
						"type": "tool_use",
						"name": "TaskCreate",
						"input": map[string]interface{}{
							"subject":       "Research cache issue",
							"subagent_type": "Explore",
						},
					},
				},
			},
		},
	}

	info := TelemetryOverlay(body, Classification{})
	if !info.IsSubagent {
		t.Fatalf("expected telemetry overlay to mark agent-tool request as subagent, got %+v", info)
	}
	if info.Type != TypeAgentTool {
		t.Fatalf("expected telemetry overlay type %q, got %q", TypeAgentTool, info.Type)
	}
}

func TestTelemetryOverlayPreservesExistingClassification(t *testing.T) {
	body := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role": "assistant",
				"content": []interface{}{
					map[string]interface{}{"type": "tool_use", "name": "TaskCreate"},
				},
			},
		},
	}

	original := Classification{IsSubagent: true, Type: TypeSmallSystem}
	info := TelemetryOverlay(body, original)
	if info.Type != TypeSmallSystem {
		t.Fatalf("expected telemetry overlay to preserve existing type, got %q", info.Type)
	}
}
