// router.go — transparent Host-based upstream routing.
// Maps incoming Host headers directly to upstream targets.
// The client connects to what they think is the real provider.
// DNS/routing delivers the request to this proxy.
package proxy

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// Route maps an incoming Host to an upstream target.
type Route struct {
	HostMatch      string   // exact Host header match, e.g. "api.anthropic.com"
	UpstreamURL    *url.URL // e.g. https://api.anthropic.com
	UpstreamDomain string   // e.g. "api.anthropic.com"
	IsAPI          bool     // true for API endpoints (JSON), false for web (HTML)
}

// Router resolves incoming requests to upstream routes based on Host header.
type Router struct {
	mu     sync.RWMutex
	routes map[string]*Route // host -> route
}

// NewRouter creates a transparent Host-based router.
func NewRouter() *Router {
	return &Router{
		routes: make(map[string]*Route),
	}
}

// AddRoute registers a host -> upstream mapping.
func (r *Router) AddRoute(host, upstreamURL string, isAPI bool) error {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return fmt.Errorf("invalid upstream URL %q: %w", upstreamURL, err)
	}

	route := &Route{
		HostMatch:      host,
		UpstreamURL:    u,
		UpstreamDomain: u.Hostname(),
		IsAPI:          isAPI,
	}

	r.mu.Lock()
	r.routes[host] = route
	r.mu.Unlock()
	return nil
}

// Resolve finds the route for an incoming Host header.
// Returns nil if no route matches.
func (r *Router) Resolve(host string) *Route {
	// Strip port
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}

	r.mu.RLock()
	route := r.routes[host]
	r.mu.RUnlock()
	return route
}

// SetupDefaultRoutes configures all provider routes.
// Each route matches the real provider Host header — transparent to the client.
func (r *Router) SetupDefaultRoutes() error {
	routes := []struct {
		host string
		url  string
		api  bool
	}{
		// Anthropic
		{"api.anthropic.com", "https://api.anthropic.com", true},
		{"claude.ai", "https://claude.ai", false},
		// OpenAI
		{"api.openai.com", "https://api.openai.com", true},
		{"chatgpt.com", "https://chatgpt.com", false},
		// Gemini
		{"generativelanguage.googleapis.com", "https://generativelanguage.googleapis.com", true},
		{"gemini.google.com", "https://gemini.google.com", false},
		// DashScope / Qwen
		{"dashscope.aliyuncs.com", "https://dashscope.aliyuncs.com", true},
		// OpenRouter
		{"openrouter.ai", "https://openrouter.ai", false},
	}
	for _, rt := range routes {
		if err := r.AddRoute(rt.host, rt.url, rt.api); err != nil {
			return err
		}
	}
	return nil
}
