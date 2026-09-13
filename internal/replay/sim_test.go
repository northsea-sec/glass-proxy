package replay

import (
	"encoding/json"
	"testing"

	"proxy.local/app/internal/subagent"
)

func TestCacheSimReusesDeepestPrefix(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "shared system", "cache_control": map[string]interface{}{"type": "ephemeral"}},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello", "cache_control": map[string]interface{}{"type": "ephemeral"}},
				},
			},
		},
	}

	sim := newCacheSim()
	read, create := sim.Observe(body, HeaderSnapshot{AnthropicVersion: "2023-06-01"})
	if read != 0 {
		t.Fatalf("first request should not read cache, got %d", read)
	}
	if create == 0 {
		t.Fatal("first request should create a cache segment")
	}

	read, create = sim.Observe(body, HeaderSnapshot{AnthropicVersion: "2023-06-01"})
	if create != 0 {
		t.Fatalf("second request should not create cache, got %d", create)
	}
	if read == 0 {
		t.Fatal("second request should read the deepest cached prefix")
	}
}

func TestCachedChildPolicyRewritesSmallSystemLane(t *testing.T) {
	body := map[string]interface{}{
		"model": "claude-opus-4-6",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "tiny child prompt"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hi"}}},
		},
	}
	preGlass, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal pre-glass: %v", err)
	}

	fixture := Fixture{
		Meta: MetaSnapshot{
			RequestSessionKey: "parentbase",
			SessionKey:        "parentbase",
			RequestKey:        "parentbase_sub",
			AffinityKey:       "parentbase",
			Subagent: subagent.Classification{
				IsSubagent:             true,
				Type:                   subagent.TypeSmallSystem,
				BypassCanonical:        true,
				UseParentAffinity:      true,
				BypassMessageCache:     true,
				DisableUpstreamCaching: true,
			},
		},
		BodyPreGlass: preGlass,
	}

	meta, path := metaForPolicy(fixture, body, Options{Policy: PolicyCachedChild}, nil)
	if path != string(PolicyCachedChild) {
		t.Fatalf("expected cached-child path, got %s", path)
	}
	if meta.SessionKey == "parentbase" {
		t.Fatal("cached child policy should isolate the session key")
	}
	if meta.AffinityKey != meta.SessionKey {
		t.Fatalf("cached child policy should pin affinity to child lane, got affinity=%s session=%s", meta.AffinityKey, meta.SessionKey)
	}
	if meta.Subagent.BypassMessageCache {
		t.Fatal("cached child policy should keep message cache enabled")
	}
	if meta.Subagent.DisableUpstreamCaching {
		t.Fatal("cached child policy should permit upstream caching")
	}
}
