package guard

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"time"
)

type jsxRayResult struct {
	Warnings     []jsxRayWarning `json:"warnings"`
	Dependencies []string        `json:"dependencies"`
	Error        string          `json:"error,omitempty"`
}

type jsxRayWarning struct {
	Kind     string `json:"kind"`
	Value    string `json:"value"`
	Location string `json:"location"`
}

// Dangerous warning kinds that indicate potential malicious behavior
var jsxRayDangerousKinds = map[string]string{
	"unsafe-import":        "dynamic or obfuscated import",
	"unsafe-regex":         "potentially dangerous regex (ReDoS)",
	"unsafe-stmt":          "use of eval() or Function()",
	"encoded-literal":      "encoded/obfuscated string literal",
	"short-identifiers":    "heavily minified/obfuscated code",
	"suspicious-literal":   "suspicious string literal (e.g., hex-encoded)",
	"obfuscated-code":      "code appears intentionally obfuscated",
	"weak-crypto":          "use of weak crypto",
	"shady-link":           "suspicious URL found in source",
}

func jsxRayScannerScript() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "scripts", "jsxray-scan.js")
}

func (s *Server) runJSXRay(ctx context.Context, jsSource string) (bool, []string) {
	script := jsxRayScannerScript()
	out, err := runSubprocessWithStdin(ctx, 15*time.Second, []byte(jsSource), "node", script)
	if err != nil {
		log.Printf("[GUARD-JSXRAY] scan failed: %v", err)
		return false, nil
	}

	var result jsxRayResult
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		log.Printf("[GUARD-JSXRAY] JSON parse error: %v", jsonErr)
		return false, nil
	}
	if result.Error != "" {
		log.Printf("[GUARD-JSXRAY] analysis error: %s", result.Error)
		return false, nil
	}

	var reasons []string
	for _, w := range result.Warnings {
		if desc, dangerous := jsxRayDangerousKinds[w.Kind]; dangerous {
			detail := desc
			if w.Value != "" {
				detail = fmt.Sprintf("%s: %s", desc, truncate(w.Value, 80))
			}
			reasons = append(reasons, fmt.Sprintf("JS-X-Ray: %s at %s (%s)", w.Kind, w.Location, detail))
		}
	}

	return len(reasons) > 0, reasons
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
