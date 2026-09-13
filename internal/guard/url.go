package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	urlPattern = regexp.MustCompile(`https?://[^\s<>"'()]+`)

	hiddenHTMLPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bhidden\b`),
		regexp.MustCompile(`(?i)display\s*:\s*none`),
		regexp.MustCompile(`(?i)visibility\s*:\s*hidden`),
		regexp.MustCompile(`(?i)opacity\s*:\s*0(?:[; }]|$)`),
		regexp.MustCompile(`(?i)font-size\s*:\s*0(?:px|em|rem|%|)(?:[; }]|$)`),
		regexp.MustCompile(`(?i)(left|top)\s*:\s*-\d`),
		regexp.MustCompile(`(?i)clip-path\s*:`),
		regexp.MustCompile(`(?i)\baria-hidden\s*=\s*["']?true["']?`),
	}

	promptInjectionPatterns = map[string]*regexp.Regexp{
		"ignore previous instructions": regexp.MustCompile(`(?is)\b(ignore|disregard|forget|override)\b.{0,64}\b(previous|prior|earlier|above)\b.{0,64}\b(instruction|prompt|message)s?\b`),
		"system prompt targeting":      regexp.MustCompile(`(?i)\b(system prompt|developer message|hidden instruction|policy prompt)\b`),
		"secret exfiltration request":  regexp.MustCompile(`(?is)\b(exfiltrat(?:e|ion)|leak|steal|dump|export|send|upload)\b.{0,96}\b(secret|credential|token|cookie|session|api[ _-]?key|environment variable|env|ssh key|password)\b`),
		"filesystem secret targeting":  regexp.MustCompile(`(?is)\b(read|open|cat|print)\b.{0,96}\b(\.env|id_rsa|authorized_keys|/etc/passwd|/etc/shadow|\.ssh|token|secret|cookie)\b`),
		"silent execution instruction": regexp.MustCompile(`(?is)\b(do not tell|without asking|silently|quietly|secretly)\b`),
	}
)

func ExtractURLs(text string) []string {
	matches := urlPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	var out []string
	for _, match := range matches {
		cleaned := strings.TrimRight(match, ".,;:!?)\"]'")
		if cleaned == "" {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func (s *Server) checkURL(ctx context.Context, req URLCheckRequest) URLCheckResponse {
	cacheKey := hashKey("url", req.URL, strings.Join(req.AllowHosts, ","))
	if cached, ok := s.urlCache.get(cacheKey); ok {
		return cached
	}

	resp := URLCheckResponse{
		Allowed: false,
	}
	raw := strings.TrimSpace(req.URL)
	if raw == "" {
		resp.Reasons = []string{"missing URL"}
		s.urlCache.set(cacheKey, resp)
		return resp
	}

	if reasons := detectInvisibleText(raw); len(reasons) > 0 {
		resp.Reasons = append(resp.Reasons, reasons...)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		resp.Reasons = append(resp.Reasons, "invalid URL")
		resp.Reasons = uniqueStrings(resp.Reasons)
		s.urlCache.set(cacheKey, resp)
		return resp
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		resp.Reasons = append(resp.Reasons, "unsupported URL scheme")
	}
	if parsed.Host == "" {
		resp.Reasons = append(resp.Reasons, "missing URL host")
	}
	if parsed.User != nil {
		resp.Reasons = append(resp.Reasons, "URL userinfo is not allowed")
	}
	parsed.Fragment = ""
	parsed.Host = strings.ToLower(parsed.Host)
	host := strings.ToLower(parsed.Hostname())
	resp.Host = host
	resp.NormalizedURL = parsed.String()

	if host == "" {
		resp.Reasons = append(resp.Reasons, "missing normalized host")
		resp.Reasons = uniqueStrings(resp.Reasons)
		s.urlCache.set(cacheKey, resp)
		return resp
	}

	if hasNonASCII(host) {
		resp.Reasons = append(resp.Reasons, "non-ASCII hostname is blocked")
	}

	allowlisted := hostMatchesAllowlist(host, req.AllowHosts)
	if !allowlisted {
		if ip := net.ParseIP(host); ip != nil {
			if !isSafePublicIP(ip) {
				resp.Reasons = append(resp.Reasons, "host resolves to a private or local IP")
			}
		} else {
			resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			ips, lookupErr := net.DefaultResolver.LookupIP(resolveCtx, "ip", host)
			if lookupErr != nil {
				resp.Reasons = append(resp.Reasons, "DNS lookup failed")
			} else if len(ips) == 0 {
				resp.Reasons = append(resp.Reasons, "host has no DNS records")
			} else {
				for _, ip := range ips {
					if !isSafePublicIP(ip) {
						resp.Reasons = append(resp.Reasons, "host resolves to a private or local IP")
						break
					}
				}
			}
		}

		if listed, reason := s.lookupURLhaus(ctx, resp.NormalizedURL); listed {
			resp.Reasons = append(resp.Reasons, reason)
		}
		if listed, reason := s.lookupPhishTank(ctx, resp.NormalizedURL); listed {
			resp.Reasons = append(resp.Reasons, reason)
		}
		if s.cfg.DnstwistEnabled {
			if suspicious, reason := s.checkDnstwist(ctx, host); suspicious {
				resp.Reasons = append(resp.Reasons, reason)
			}
		}
	}

	resp.Reasons = uniqueStrings(resp.Reasons)
	resp.Allowed = len(resp.Reasons) == 0
	s.urlCache.set(cacheKey, resp)
	return resp
}

func (s *Server) checkContent(ctx context.Context, req ContentCheckRequest) ContentCheckResponse {
	cacheKey := hashKey("content", req.SourceURL, req.ContentType, req.Content)
	if cached, ok := s.contentCache.get(cacheKey); ok {
		return cached
	}

	resp := ContentCheckResponse{
		Allowed: true,
		URLs:    ExtractURLs(req.Content),
	}

	content := req.Content
	if strings.TrimSpace(content) == "" {
		s.contentCache.set(cacheKey, resp)
		return resp
	}

	invisibleReasons := detectInvisibleText(content)
	promptReasons := detectPromptInjection(content)
	hiddenReasons := detectHiddenHTML(content)

	var reasons []string
	reasons = append(reasons, invisibleReasons...)
	reasons = append(reasons, promptReasons...)
	if len(hiddenReasons) > 0 && (len(promptReasons) > 0 || containsSensitiveTerms(content)) {
		reasons = append(reasons, hiddenReasons...)
	}

	for _, extractedURL := range resp.URLs {
		urlResp := s.checkURL(ctx, URLCheckRequest{
			URL: extractedURL,
		})
		if !urlResp.Allowed {
			reasons = append(reasons, "embedded URL blocked: "+strings.Join(urlResp.Reasons, ", "))
		}
	}

	// JS-X-Ray: scan JavaScript content for obfuscation/exfiltration patterns
	if s.cfg.JSXRayEnabled && looksLikeJavaScript(content, req.ContentType) {
		if flagged, jsReasons := s.runJSXRay(ctx, content); flagged {
			reasons = append(reasons, jsReasons...)
		}
	}

	// LLM Guard: ML-based prompt injection detection
	if s.cfg.LLMGuardEnabled {
		if flagged, lgReasons := s.scanLLMGuard(ctx, content); flagged {
			reasons = append(reasons, lgReasons...)
		}
	}

	reasons = uniqueStrings(reasons)
	if len(reasons) > 0 {
		resp.Allowed = false
		resp.Reasons = reasons
		resp.SanitizedText = buildSanitizedContent(req.SourceURL, reasons)
	}

	s.contentCache.set(cacheKey, resp)
	return resp
}

func detectInvisibleText(text string) []string {
	var reasons []string
	for _, r := range text {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e', '\u2066', '\u2067', '\u2068', '\u2069':
			reasons = append(reasons, "invisible or bidi control characters present")
			return uniqueStrings(reasons)
		}
		if unicode.Is(unicode.Cf, r) {
			reasons = append(reasons, "unicode format control characters present")
			return uniqueStrings(reasons)
		}
	}
	return uniqueStrings(reasons)
}

func detectPromptInjection(content string) []string {
	var reasons []string
	for label, pattern := range promptInjectionPatterns {
		if pattern.MatchString(content) {
			reasons = append(reasons, label)
		}
	}
	return uniqueStrings(reasons)
}

func detectHiddenHTML(content string) []string {
	lower := strings.ToLower(content)
	if !strings.Contains(lower, "<") || !strings.Contains(lower, ">") {
		return nil
	}

	var reasons []string
	for _, pattern := range hiddenHTMLPatterns {
		if pattern.MatchString(content) {
			reasons = append(reasons, "hidden HTML or CSS content detected")
			break
		}
	}
	return uniqueStrings(reasons)
}

func containsSensitiveTerms(content string) bool {
	lower := strings.ToLower(content)
	for _, term := range []string{"api key", "token", "cookie", "secret", "credential", "password", ".env", ".ssh", "session"} {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func buildSanitizedContent(sourceURL string, reasons []string) string {
	prefix := "glass-guard blocked unsafe tool output"
	if sourceURL != "" {
		prefix += " from " + sourceURL
	}
	if len(reasons) == 0 {
		return prefix + "."
	}
	return prefix + ": " + strings.Join(reasons, "; ") + "."
}

func hostMatchesAllowlist(host string, allowHosts []string) bool {
	for _, allowed := range allowHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}

func isSafePublicIP(ip net.IP) bool {
	return !(ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified())
}

func (s *Server) lookupURLhaus(ctx context.Context, rawURL string) (bool, string) {
	if strings.TrimSpace(s.cfg.URLhausAuthKey) == "" {
		return false, ""
	}

	form := url.Values{}
	form.Set("url", rawURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://urlhaus-api.abuse.ch/v1/url/", strings.NewReader(form.Encode()))
	if err != nil {
		return false, ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Auth-Key", s.cfg.URLhausAuthKey)
	req.Header.Set("User-Agent", "glass-guardd/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()

	type urlhausResp struct {
		QueryStatus string `json:"query_status"`
		Threat      string `json:"threat"`
		URLStatus   string `json:"url_status"`
	}

	var payload urlhausResp
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) != nil {
		return false, ""
	}
	if payload.QueryStatus == "ok" {
		reason := "URLhaus flagged URL"
		if payload.Threat != "" {
			reason += " (" + payload.Threat + ")"
		}
		return true, reason
	}
	return false, ""
}

func (s *Server) lookupPhishTank(ctx context.Context, rawURL string) (bool, string) {
	form := url.Values{}
	form.Set("url", rawURL)
	form.Set("format", "json")
	if strings.TrimSpace(s.cfg.PhishTankAppKey) != "" {
		form.Set("app_key", s.cfg.PhishTankAppKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://checkurl.phishtank.com/checkurl/", strings.NewReader(form.Encode()))
	if err != nil {
		return false, ""
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "glass-guardd/1.0")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()

	type phishTankResp struct {
		Results struct {
			InDatabase bool   `json:"in_database"`
			Verified   string `json:"verified"`
			Valid      string `json:"valid"`
		} `json:"results"`
	}

	var payload phishTankResp
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload) != nil {
		return false, ""
	}
	if payload.Results.InDatabase && strings.EqualFold(payload.Results.Valid, "y") {
		if strings.EqualFold(payload.Results.Verified, "y") {
			return true, "PhishTank verified phishing URL"
		}
		return true, "PhishTank flagged phishing URL"
	}
	return false, ""
}

func hashKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func stringifyJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

func looksLikeJavaScript(content, contentType string) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "javascript") || strings.Contains(ct, "ecmascript") {
		return true
	}
	prefix := content
	if len(prefix) > 500 {
		prefix = prefix[:500]
	}
	lower := strings.ToLower(prefix)
	jsSignals := 0
	for _, sig := range []string{"require(", "import ", "module.exports", "const ", "function ", "=>", "eval(", "process.env"} {
		if strings.Contains(lower, sig) {
			jsSignals++
		}
	}
	return jsSignals >= 2
}
