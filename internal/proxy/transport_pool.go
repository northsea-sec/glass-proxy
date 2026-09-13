// transport_pool.go — per-session upstream transport isolation.
//
// Each conversation gets its own http.Transport (and therefore its own TCP
// connection pool to Anthropic). This prevents cross-session interference
// at the connection layer and allows Anthropic's load balancer to distribute
// sessions across different backend workers.
//
// Transports are keyed by affinity key (parent session key), so subagents
// share their parent's transport. Idle transports are closed after a TTL.
package proxy

import (
	"log"
	"net/http"
	"sync"
	"time"
)

// TransportFactory creates a new http.RoundTripper for a session.
// Called once per new session key; the returned transport is reused for
// all subsequent requests in that session.
type TransportFactory func() http.RoundTripper

// transportEntry tracks a per-session transport and its last-use time.
type transportEntry struct {
	transport http.RoundTripper
	lastUsed  time.Time
}

// transportPool maintains per-session http transports.
// Each session key maps to its own transport with its own connection pool,
// ensuring different sessions establish independent TCP connections upstream.
type transportPool struct {
	mu      sync.Mutex
	entries map[string]*transportEntry
	factory TransportFactory
	ttl     time.Duration // idle transports are closed after this duration
	stopCh  chan struct{}
}

// newTransportPool creates a pool that provisions transports via factory.
// Starts a background goroutine that evicts idle transports every 60s.
func newTransportPool(factory TransportFactory, ttl time.Duration) *transportPool {
	p := &transportPool{
		entries: make(map[string]*transportEntry),
		factory: factory,
		ttl:     ttl,
		stopCh:  make(chan struct{}),
	}
	go p.reapLoop()
	return p
}

// Get returns the transport for the given session key, creating one if needed.
// Thread-safe; may be called from multiple goroutines concurrently.
func (p *transportPool) Get(sessionKey string) http.RoundTripper {
	if sessionKey == "" {
		// No session context — fall back to a shared default.
		// This should be rare (only for requests that bypass Glass).
		sessionKey = "_default"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	e, ok := p.entries[sessionKey]
	if ok {
		e.lastUsed = time.Now()
		return e.transport
	}

	// Create a new transport for this session.
	t := p.factory()
	p.entries[sessionKey] = &transportEntry{
		transport: t,
		lastUsed:  time.Now(),
	}
	log.Printf("[TRANSPORT-POOL] Created transport for session %s (pool size: %d)",
		sessionKey[:min(12, len(sessionKey))], len(p.entries))
	return t
}

// Len returns the current number of active transports.
func (p *transportPool) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Stop shuts down the reaper and closes all idle connections.
func (p *transportPool) Stop() {
	close(p.stopCh)
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, e := range p.entries {
		closeTransport(e.transport)
		delete(p.entries, key)
	}
}

// reapLoop runs in the background, closing transports idle longer than TTL.
func (p *transportPool) reapLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.reap()
		}
	}
}

func (p *transportPool) reap() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, e := range p.entries {
		if now.Sub(e.lastUsed) > p.ttl {
			closeTransport(e.transport)
			delete(p.entries, key)
			log.Printf("[TRANSPORT-POOL] Reaped idle transport %s (pool size: %d)",
				key[:min(12, len(key))], len(p.entries))
		}
	}
}

// closeTransport closes idle connections on a transport if it supports it.
func closeTransport(t http.RoundTripper) {
	type closer interface {
		CloseIdleConnections()
	}
	if c, ok := t.(closer); ok {
		c.CloseIdleConnections()
	}
}
