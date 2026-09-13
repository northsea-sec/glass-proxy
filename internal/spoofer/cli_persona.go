// cli_persona.go — CLI-appropriate persona generation.
//
// When the client is a CLI tool (Claude Code, Python SDK, etc),
// the egress headers must match what that CLI tool actually sends.
// Injecting Chrome Sec-Ch-Ua or Sec-Fetch-* on CLI traffic is a
// detection signal, not camouflage.
//
// CLI persona:
//   - User-Agent: rotated version from real version pool (deterministic per tenant+day)
//   - anthropic-version: preserved from client
//   - Accept: application/json
//   - Accept-Encoding: gzip, deflate, br
//   - NO Sec-Ch-Ua, NO Sec-Fetch-*, NO Accept-Language (real CLIs don't send these)
package spoofer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"time"
)

// CLIPersona is a CLI-appropriate identity.
type CLIPersona struct {
	UserAgent string
	Flavor    CLIFlavor
	Date      string
}

// Real Claude Code version strings observed in the wild.
// Updated Mar 2026. These rotate naturally as Anthropic ships updates.
var claudeCodeVersions = []string{
	"claude-code/2.1.47",
	"claude-code/2.1.50",
	"claude-code/2.1.51",
	"claude-code/2.1.53",
	"claude-code/2.1.55",
	"claude-code/2.1.58",
	"claude-code/2.1.59",
	"claude-code/2.1.62",
	"claude-code/2.1.63",
	"claude-code/2.1.64",
	"claude-code/2.1.66",
	"claude-code/2.1.67",
	"claude-code/2.1.68",
	"claude-code/2.1.69",
	"claude-code/2.1.70",
	"claude-code/2.1.71",
}

var pythonSDKVersions = []string{
	"anthropic-python/0.34.2",
	"anthropic-python/0.36.0",
	"anthropic-python/0.37.1",
	"anthropic-python/0.38.0",
	"anthropic-python/0.39.0",
	"anthropic-python/0.40.0",
}

var typescriptSDKVersions = []string{
	"anthropic-typescript/0.30.1",
	"anthropic-typescript/0.31.0",
	"anthropic-typescript/0.32.0",
	"anthropic-typescript/0.32.1",
}

// GetCLI returns a CLI persona for a tenant on the current day.
func (pm *PersonaManager) GetCLI(tenantID string, flavor CLIFlavor) *CLIPersona {
	today := time.Now().UTC().Format("2006-01-02")

	mac := hmac.New(sha256.New, pm.key)
	mac.Write([]byte("cli|" + tenantID + "|" + today))
	hash := mac.Sum(nil)
	idx := binary.BigEndian.Uint32(hash[0:4])

	var ua string
	switch flavor {
	case FlavorClaudeCode:
		ua = claudeCodeVersions[int(idx)%len(claudeCodeVersions)]
	case FlavorPythonSDK:
		ua = pythonSDKVersions[int(idx)%len(pythonSDKVersions)]
	case FlavorTypeScriptSDK:
		ua = typescriptSDKVersions[int(idx)%len(typescriptSDKVersions)]
	case FlavorGoSDK:
		// Go SDK doesn't set a custom UA — it uses Go's default
		ua = fmt.Sprintf("Go-http-client/2.0")
	case FlavorCurl:
		// Preserve curl — it's a legitimate client Anthropic sees constantly
		ua = fmt.Sprintf("curl/8.%d.0", idx%6+4) // curl/8.4.0 through curl/8.9.0
	default:
		// Unknown CLI — default to claude-code (most common)
		ua = claudeCodeVersions[int(idx)%len(claudeCodeVersions)]
	}

	return &CLIPersona{
		UserAgent: ua,
		Flavor:    flavor,
		Date:      today,
	}
}

// ApplyCLI injects CLI-appropriate headers. Strips browser-only headers.
// This is the CLI counterpart to Persona.Apply() for browser mode.
func (cp *CLIPersona) Apply(h http.Header) {
	h.Set("User-Agent", cp.UserAgent)
	h.Set("Accept", "application/json")

	// Real CLIs do NOT send these — their presence on CLI traffic is anomalous
	h.Del("Sec-Ch-Ua")
	h.Del("Sec-Ch-Ua-Mobile")
	h.Del("Sec-Ch-Ua-Platform")
	h.Del("Sec-Fetch-Site")
	h.Del("Sec-Fetch-Mode")
	h.Del("Sec-Fetch-Dest")
	h.Del("Accept-Language")
}
