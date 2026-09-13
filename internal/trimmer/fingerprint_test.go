package trimmer

import "testing"

func TestSessionFingerprintDistinguishesLongSystemVariantsWithinSamePID(t *testing.T) {
	common := make([]byte, 650)
	for i := range common {
		common[i] = 'a'
	}

	bodyA := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": string(common) + " subagent lane"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}
	bodyB := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": string(common) + " parent lane"},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "hello"},
		},
	}

	if gotA, gotB := SessionFingerprint(bodyA, 123), SessionFingerprint(bodyB, 123); gotA == gotB {
		t.Fatalf("expected long-system variants to get different session fingerprints, got %q", gotA)
	}
}

func TestSessionFingerprintIncludesToolSignature(t *testing.T) {
	bodyA := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "shared preamble"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "read", "description": "read files"},
		},
	}
	bodyB := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "shared preamble"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "read", "description": "read files"},
			map[string]interface{}{"name": "bash", "description": "run commands"},
		},
	}

	if gotA, gotB := SessionFingerprint(bodyA, 123), SessionFingerprint(bodyB, 123); gotA == gotB {
		t.Fatalf("expected differing tool sets to get different session fingerprints, got %q", gotA)
	}
}
