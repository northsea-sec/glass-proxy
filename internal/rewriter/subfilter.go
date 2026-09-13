// Package rewriter provides response body rewriting (sub_filter equivalent).
// Replaces upstream domain references with proxy domain in text responses.
package rewriter

import (
	"bytes"
	"strings"
)

// SubFilter defines a find/replace pair for domain rewriting.
type SubFilter struct {
	Find    string // e.g. "claude.ai"
	Replace string // e.g. "api.anthropic.com"
}

// RewriteBody applies sub_filter replacements to response body.
// Only rewrites text content types (JSON, HTML, JS, CSS, SSE).
func RewriteBody(body []byte, contentType string, filters []SubFilter) []byte {
	if len(filters) == 0 || len(body) == 0 {
		return body
	}
	if !isTextContent(contentType) {
		return body
	}
	result := body
	for _, f := range filters {
		result = bytes.ReplaceAll(result, []byte(f.Find), []byte(f.Replace))
	}
	return result
}

// RewriteSSEEvent rewrites domain references within a single SSE event line.
func RewriteSSEEvent(line []byte, filters []SubFilter) []byte {
	if len(filters) == 0 || len(line) == 0 {
		return line
	}
	result := line
	for _, f := range filters {
		result = bytes.ReplaceAll(result, []byte(f.Find), []byte(f.Replace))
	}
	return result
}

// RewriteHeaders rewrites request headers for upstream:
//   Host:    proxy domain -> upstream domain
//   Origin:  proxy domain -> upstream domain
//   Referer: proxy domain -> upstream domain
func RewriteRequestHeader(key, value, proxyDomain, upstreamDomain string) string {
	switch strings.ToLower(key) {
	case "host":
		return upstreamDomain
	case "origin", "referer":
		return strings.Replace(value, proxyDomain, upstreamDomain, 1)
	default:
		return value
	}
}

// RewriteSetCookie rewrites Set-Cookie domain from upstream to proxy domain.
func RewriteSetCookie(value, upstreamDomain, proxyDomain string) string {
	return strings.Replace(value, upstreamDomain, proxyDomain, -1)
}

func isTextContent(ct string) bool {
	ct = strings.ToLower(ct)
	textTypes := []string{
		"text/",
		"application/json",
		"application/javascript",
		"application/x-javascript",
		"text/event-stream",
		"application/xml",
	}
	for _, t := range textTypes {
		if strings.Contains(ct, t) {
			return true
		}
	}
	return false
}
