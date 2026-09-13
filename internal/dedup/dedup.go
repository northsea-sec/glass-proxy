// Package dedup implements request deduplication and coalescing.
// Ported from mitm_itt_addon.py _dedup_seen / _coalesce_inflight logic.
//
// Blocks duplicate API requests within a time window. If the same request
// body hash arrives while a previous identical request is still in-flight,
// the duplicate is rejected with 400 to prevent double-billing.
package dedup

import (
	"crypto/sha256"
	"fmt"
	"log"
	"sync"
	"time"
)

// Entry tracks a seen request.
type Entry struct {
	Hash      string
	FirstSeen time.Time
	InFlight  bool
	Waiters   int
}

// Deduplicator tracks recent request hashes and blocks duplicates.
type Deduplicator struct {
	mu      sync.Mutex
	seen    map[string]*Entry
	window  time.Duration // how long to remember request hashes
	stats   Stats
}

// Stats tracks dedup activity.
type Stats struct {
	TotalSeen    int `json:"total_seen"`
	TotalBlocked int `json:"total_blocked"`
	TotalExpired int `json:"total_expired"`
}

// New creates a deduplicator with the given window duration.
func New(window time.Duration) *Deduplicator {
	if window <= 0 {
		window = 5 * time.Second
	}
	d := &Deduplicator{
		seen:   make(map[string]*Entry),
		window: window,
	}
	// Background cleanup
	go d.cleanup()
	return d
}

// Check returns true if the request should PROCEED, false if it's a duplicate.
// tenantID scopes the dedup — identical bodies from different tenants are NOT considered duplicates.
func (d *Deduplicator) Check(tenantID string, body []byte) (proceed bool, hash string) {
	h := sha256.Sum256(body)
	hash = fmt.Sprintf("%s:%x", tenantID, h[:16])

	d.mu.Lock()
	defer d.mu.Unlock()

	d.stats.TotalSeen++

	entry, exists := d.seen[hash]
	if !exists {
		// First time seeing this request
		d.seen[hash] = &Entry{
			Hash:      hash,
			FirstSeen: time.Now(),
			InFlight:  true,
			Waiters:   0,
		}
		return true, hash
	}

	// Seen before — check if within window
	if time.Since(entry.FirstSeen) > d.window {
		// Expired — treat as new
		entry.FirstSeen = time.Now()
		entry.InFlight = true
		entry.Waiters = 0
		return true, hash
	}

	// Duplicate within window
	entry.Waiters++
	d.stats.TotalBlocked++
	log.Printf("[DEDUP] Blocked duplicate request (hash=%s, delta=%dms, waiters=%d)",
		hash[:8], time.Since(entry.FirstSeen).Milliseconds(), entry.Waiters)
	return false, hash
}

// Complete marks a request as no longer in-flight.
func (d *Deduplicator) Complete(hash string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry, ok := d.seen[hash]; ok {
		entry.InFlight = false
	}
}

// GetStats returns dedup statistics.
func (d *Deduplicator) GetStats() Stats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stats
}

func (d *Deduplicator) cleanup() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		d.mu.Lock()
		now := time.Now()
		for hash, entry := range d.seen {
			if now.Sub(entry.FirstSeen) > d.window*2 && !entry.InFlight {
				delete(d.seen, hash)
				d.stats.TotalExpired++
			}
		}
		d.mu.Unlock()
	}
}
