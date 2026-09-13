// headers.go — Host/Origin/Referer/Set-Cookie/CSP header rewriting.
// Rewrites domain references in HTTP headers for the transparent proxy.
package rewriter

import (
	"strings"
)

// HeaderRewriter rewrites HTTP headers between proxy and upstream domains.
type HeaderRewriter struct {
	ProxyDomain    string // e.g. "api.anthropic.com"
	UpstreamDomain string // e.g. "claude.ai"
}

// RewriteRequestHeaders modifies outgoing request headers.
// Host -> upstream, Origin/Referer domain swap, remove proxy-identifying headers.
func (h *HeaderRewriter) RewriteRequestHeaders(headers map[string][]string) {
	// Host
	if _, ok := headers["Host"]; ok {
		headers["Host"] = []string{h.UpstreamDomain}
	}

	// Origin
	if vals, ok := headers["Origin"]; ok {
		for i, v := range vals {
			headers["Origin"][i] = strings.Replace(v, h.ProxyDomain, h.UpstreamDomain, 1)
		}
	}

	// Referer
	if vals, ok := headers["Referer"]; ok {
		for i, v := range vals {
			headers["Referer"][i] = strings.Replace(v, h.ProxyDomain, h.UpstreamDomain, 1)
		}
	}

	// Sec-Fetch-Site: if "same-origin" keep it (browser thinks it's same-origin to proxy)
	// Don't touch — forwarding as-is is correct for transparent proxy pattern
}

// RewriteResponseHeaders modifies incoming response headers.
// Set-Cookie domains, CSP, Location headers.
func (h *HeaderRewriter) RewriteResponseHeaders(headers map[string][]string) {
	// Set-Cookie: rewrite domain from upstream to proxy
	if cookies, ok := headers["Set-Cookie"]; ok {
		for i, cookie := range cookies {
			headers["Set-Cookie"][i] = h.rewriteSetCookie(cookie)
		}
	}

	// Content-Security-Policy: rewrite upstream domain refs to proxy
	if csp, ok := headers["Content-Security-Policy"]; ok {
		for i, v := range csp {
			headers["Content-Security-Policy"][i] = strings.ReplaceAll(v, h.UpstreamDomain, h.ProxyDomain)
		}
	}

	// Location (redirects): rewrite upstream to proxy
	if loc, ok := headers["Location"]; ok {
		for i, v := range loc {
			headers["Location"][i] = strings.Replace(v, h.UpstreamDomain, h.ProxyDomain, 1)
		}
	}

	// Access-Control-Allow-Origin
	if acao, ok := headers["Access-Control-Allow-Origin"]; ok {
		for i, v := range acao {
			headers["Access-Control-Allow-Origin"][i] = strings.Replace(v, h.UpstreamDomain, h.ProxyDomain, 1)
		}
	}
}

func (h *HeaderRewriter) rewriteSetCookie(cookie string) string {
	// Replace domain= value
	cookie = strings.ReplaceAll(cookie, "domain="+h.UpstreamDomain, "domain="+h.ProxyDomain)
	cookie = strings.ReplaceAll(cookie, "domain=."+h.UpstreamDomain, "domain=."+h.ProxyDomain)
	// Replace domain refs in path/value
	cookie = strings.ReplaceAll(cookie, h.UpstreamDomain, h.ProxyDomain)

	// RFC 6265bis: SameSite=None REQUIRES Secure flag.
	// Browsers silently reject SameSite=None cookies without Secure,
	// which breaks cross-origin auth flows through the proxy.
	lower := strings.ToLower(cookie)
	if strings.Contains(lower, "samesite=none") && !strings.Contains(lower, "secure") {
		cookie += "; Secure"
	}

	return cookie
}
