// openrouter_handler.go — OpenRouter lane handler.
//
// Accepts requests on /openrouter/<openai-compat path> and forwards them to
// openrouter.ai. The client is responsible for the OpenRouter auth header
// (Authorization: Bearer sk-or-...); Glass-Proxy relays it verbatim.
//
// Upstream is configurable via OPENROUTER_UPSTREAM env var.
package proxy

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const (
	openRouterPathPrefix      = "/openrouter"
	defaultOpenRouterUpstream = "https://openrouter.ai/api"
)

func openRouterUpstream() *url.URL {
	raw := strings.TrimSpace(os.Getenv("OPENROUTER_UPSTREAM"))
	if raw == "" {
		raw = defaultOpenRouterUpstream
	}
	u, err := url.Parse(raw)
	if err != nil {
		log.Printf("[OPENROUTER] invalid OPENROUTER_UPSTREAM=%q: %v; falling back to %s", raw, err, defaultOpenRouterUpstream)
		u, _ = url.Parse(defaultOpenRouterUpstream)
	}
	return u
}

func isOpenRouterRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	return strings.HasPrefix(r.URL.Path, openRouterPathPrefix+"/")
}

func (p *Proxy) handleOpenRouterRequest(w http.ResponseWriter, r *http.Request) {
	upstream := openRouterUpstream()
	rp := httputil.NewSingleHostReverseProxy(upstream)

	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		trimmed := strings.TrimPrefix(req.URL.Path, openRouterPathPrefix)
		if trimmed == "" {
			trimmed = "/"
		}
		// If upstream base path is non-empty, join it with the trimmed path.
		if base := strings.TrimSpace(upstream.Path); base != "" && base != "/" {
			req.URL.Path = joinURLPath(base, trimmed)
			if req.URL.RawPath != "" {
				req.URL.RawPath = joinURLPath(base, trimmed)
			}
		} else {
			req.URL.Path = trimmed
			req.URL.RawPath = ""
		}
		req.Host = upstream.Host
		req.Header.Set("Host", upstream.Host)
		req.Header.Del("Accept-Encoding")
	}
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[OPENROUTER] upstream error: %v", err)
		http.Error(w, "openrouter upstream error: "+err.Error(), http.StatusBadGateway)
	}

	log.Printf("[OPENROUTER] %s %s -> %s", r.Method, r.URL.Path, upstream.String())
	rp.ServeHTTP(w, r)
}
