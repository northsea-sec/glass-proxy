// detect.go — auto-detect client type from inbound HTTP request.
//
// The transparent proxy must present the correct egress TLS fingerprint
// and header set for the type of client that is actually connecting.
// Chrome TLS on CLI traffic is a detection signal, not camouflage.
//
// Detection is based on headers the client sends on the INBOUND side.
// These are observed before any rewriting — they reflect the real client.
package spoofer

import (
	"net/http"
	"strings"
)

// ClientMode describes what kind of client is connecting.
type ClientMode int

const (
	// ModeAPICLI — CLI SDK client (Claude Code, anthropic-python, anthropic-go, curl).
	// Expect: x-api-key, anthropic-version, no cookies, no Sec-Ch-Ua.
	// Egress: Node.js/OpenSSL TLS fingerprint, CLI-appropriate headers only.
	ModeAPICLI ClientMode = iota

	// ModeWebBrowser — real browser session (claude.ai web chat, OAuth login).
	// Expect: cookies, Sec-Ch-Ua, Sec-Fetch-*, full browser UA.
	// Egress: Chrome TLS fingerprint, full browser persona headers.
	ModeWebBrowser

	// ModeAPIBrowserAuth — browser-initiated OAuth that will redirect back to CLI.
	// Detected by: browser UA + OAuth/authorize path + no session cookie.
	// Egress: Chrome TLS (it IS a real browser making this request).
	ModeAPIBrowserAuth
)

// String returns the z-d hint value for the egress-proxy.
func (m ClientMode) String() string {
	switch m {
	case ModeWebBrowser, ModeAPIBrowserAuth:
		return "chrome"
	default:
		return "cli"
	}
}

// CLIFlavor identifies the specific CLI SDK for User-Agent normalization.
type CLIFlavor int

const (
	FlavorClaudeCode CLIFlavor = iota
	FlavorPythonSDK
	FlavorTypeScriptSDK
	FlavorGoSDK
	FlavorCurl
	FlavorUnknownCLI
)

// DetectClientMode determines the client type from inbound request headers.
// Called once per request, before any header rewriting.
func DetectClientMode(r *http.Request) (ClientMode, CLIFlavor) {
	ua := r.Header.Get("User-Agent")
	uaLower := strings.ToLower(ua)
	hasSecChUa := r.Header.Get("Sec-Ch-Ua") != ""
	hasSecFetch := r.Header.Get("Sec-Fetch-Site") != ""
	hasAPIKey := r.Header.Get("x-api-key") != ""
	hasAnthropicVer := r.Header.Get("anthropic-version") != ""

	// Browser login/OAuth redirect: browser UA + auth path + no session cookie
	if isBrowserUA(uaLower) && isOAuthPath(r.URL.Path) && !hasSessionCookie(r) {
		return ModeAPIBrowserAuth, FlavorClaudeCode
	}

	// Full browser session: has session cookie OR (browser UA + Sec-Ch-Ua + Sec-Fetch-*)
	if hasSessionCookie(r) {
		return ModeWebBrowser, FlavorClaudeCode
	}
	if isBrowserUA(uaLower) && (hasSecChUa || hasSecFetch) && !hasAPIKey {
		return ModeWebBrowser, FlavorClaudeCode
	}

	// CLI mode — determine flavor from User-Agent
	flavor := detectCLIFlavor(uaLower, hasAnthropicVer)
	_ = hasAPIKey // used in browser check above
	return ModeAPICLI, flavor
}

func isBrowserUA(ua string) bool {
	return strings.Contains(ua, "mozilla/") && strings.Contains(ua, "applewebkit/")
}

func isOAuthPath(path string) bool {
	return strings.Contains(path, "/oauth") ||
		strings.Contains(path, "/authorize") ||
		strings.Contains(path, "/login") ||
		strings.Contains(path, "/callback")
}

func hasSessionCookie(r *http.Request) bool {
	for _, c := range r.Cookies() {
		name := strings.ToLower(c.Name)
		if strings.Contains(name, "session") ||
			strings.Contains(name, "sid") ||
			strings.Contains(name, "token") ||
			name == "sessionkey" ||
			name == "__cf_bm" {
			return true
		}
	}
	return false
}

func detectCLIFlavor(ua string, hasAnthropicVer bool) CLIFlavor {
	switch {
	case strings.Contains(ua, "claude-code"):
		return FlavorClaudeCode
	case strings.Contains(ua, "anthropic-python"):
		return FlavorPythonSDK
	case strings.Contains(ua, "anthropic-typescript"):
		return FlavorTypeScriptSDK
	case strings.Contains(ua, "anthropic-go"):
		return FlavorGoSDK
	case strings.Contains(ua, "curl/"):
		return FlavorCurl
	case hasAnthropicVer:
		// Has anthropic-version header but unknown UA — likely an SDK
		return FlavorUnknownCLI
	default:
		return FlavorUnknownCLI
	}
}
