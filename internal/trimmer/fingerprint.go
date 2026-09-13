// fingerprint.go — conversation fingerprinting via system prompt hash.
// Extracted from trimmer.go per plan structure.
package trimmer

import (
	"fmt"

	"proxy.local/app/internal/promptscope"
)

// SessionFingerprint identifies a session using the base prompt fingerprint plus
// an optional client PID suffix.
func SessionFingerprint(body map[string]interface{}, pid int) string {
	base := promptscope.Signature(body, promptscope.DefaultSystemPrefixChars)
	if pid > 0 {
		return fmt.Sprintf("%s_%d", base, pid)
	}
	return base
}

// ConvFingerprint identifies a conversation by combining system prompt hash with
// client PID (injected as _glass_pid by the proxy). This distinguishes multiple
// CC sessions that share the same system prompt.
func ConvFingerprint(body map[string]interface{}) string {
	switch pid := body["_glass_pid"].(type) {
	case int:
		return SessionFingerprint(body, pid)
	case int64:
		return SessionFingerprint(body, int(pid))
	case float64:
		return SessionFingerprint(body, int(pid))
	case string:
		var parsed int
		fmt.Sscanf(pid, "%d", &parsed)
		return SessionFingerprint(body, parsed)
	default:
		return SessionFingerprint(body, 0)
	}
}
