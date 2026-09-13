// persistent_drops.go — hash-based tracking of dropped messages.
// Ported from context_trimmer.py persistent re-drop system.
//
// Problem: CC re-sends full message history every call. When Stage 2 drops
// messages, CC immediately re-sends them. Without persistent tracking,
// drops are undone on the very next call → right back above trigger → death spiral.
//
// Solution: Hash dropped messages. On each subsequent call, re-drop any
// message whose hash matches. Includes safety valves:
//   - TTL expiry (MaxAge) — hashes expire on creation clock, NEVER refreshed
//   - Max hash cap (MaxHashes) — prevents unbounded growth
//   - Redrop cap (RedropCap) — if too many matches, hashes are stale poison → purge
//   - Cycle detector (ConsecutiveLimit) — if re-drops fire N consecutive calls → purge
package trimmer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"
	"time"
)

const (
	pdMaxAge          = 6 * time.Hour // hashes expire on creation clock
	pdMaxHashes       = 100           // cap per conversation
	pdRedropCap       = 30            // if >30 matches, stale poison → purge
	pdConsecutiveLimit = 10           // N consecutive fires → cycle → purge
)

// PersistentDrops tracks message hashes that have been dropped by Stage 2.
type PersistentDrops struct {
	mu      sync.Mutex
	entries map[string]*dropEntry // convID → entry
}

type dropEntry struct {
	Hashes           map[string]bool `json:"hashes"`
	CreatedAt        time.Time       `json:"created_at"`
	ConsecutiveFires int             `json:"consecutive_fires"`
}

// NewPersistentDrops creates an empty tracker.
func NewPersistentDrops() *PersistentDrops {
	return &PersistentDrops{
		entries: make(map[string]*dropEntry),
	}
}

// MsgHash hashes a message for persistent tracking.
// Uses role + first 200 chars of text content (stable across API calls).
func MsgHash(msg map[string]interface{}) string {
	role, _ := msg["role"].(string)
	text := extractMsgText(msg, 200)
	h := sha256.Sum256([]byte(role + ":" + text))
	return hex.EncodeToString(h[:16])
}

// extractMsgText extracts up to maxLen chars of text from a message.
func extractMsgText(msg map[string]interface{}, maxLen int) string {
	content := msg["content"]
	switch c := content.(type) {
	case string:
		if len(c) > maxLen {
			return c[:maxLen]
		}
		return c
	case []interface{}:
		var buf []byte
		for _, blk := range c {
			bm, ok := blk.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := bm["text"].(string); ok {
				buf = append(buf, text...)
				if len(buf) >= maxLen {
					return string(buf[:maxLen])
				}
			}
			if text, ok := bm["content"].(string); ok {
				buf = append(buf, text...)
				if len(buf) >= maxLen {
					return string(buf[:maxLen])
				}
			}
		}
		return string(buf)
	}
	return ""
}

// RedropsFor returns indices of messages that should be re-dropped
// (they match previously dropped hashes). Never re-drops the last keepRecent messages.
// Returns nil if no re-drops needed. Also handles safety valves (expiry, cap, cycle).
func (pd *PersistentDrops) RedropsFor(convID string, msgs []interface{}, keepRecent int) []int {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	entry := pd.entries[convID]
	if entry == nil || len(entry.Hashes) == 0 {
		return nil
	}

	// TTL expiry check
	if time.Since(entry.CreatedAt) > pdMaxAge {
		log.Printf("[TRIM] Persistent drops EXPIRED: conv=%s age=%v > %v — purging %d hashes",
			convID[:12], time.Since(entry.CreatedAt), pdMaxAge, len(entry.Hashes))
		delete(pd.entries, convID)
		return nil
	}

	// Hash cap check
	if len(entry.Hashes) > pdMaxHashes {
		log.Printf("[TRIM] Persistent drops OVER CAP: conv=%s has %d > %d — trimming",
			convID[:12], len(entry.Hashes), pdMaxHashes)
		pd.trimHashes(entry)
	}

	// Find matching messages in the safe window
	safeEnd := len(msgs) - keepRecent
	if safeEnd < 0 {
		safeEnd = 0
	}

	var indices []int
	for i := 0; i < safeEnd; i++ {
		msg, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		if entry.Hashes[MsgHash(msg)] {
			indices = append(indices, i)
		}
	}

	if len(indices) == 0 {
		// No matches — reset consecutive fires counter
		entry.ConsecutiveFires = 0
		return nil
	}

	// Redrop cap: if too many matches, hashes are stale poison
	if len(indices) > pdRedropCap {
		log.Printf("[TRIM] Persistent re-drop CAPPED: %d matches > cap %d for conv=%s — purging all hashes",
			len(indices), pdRedropCap, convID[:12])
		delete(pd.entries, convID)
		return nil
	}

	// Cycle detector: consecutive fires without resolution
	entry.ConsecutiveFires++
	if entry.ConsecutiveFires >= pdConsecutiveLimit {
		log.Printf("[TRIM] Persistent re-drop CYCLE DETECTED: %d consecutive fires for conv=%s — purging %d hashes",
			entry.ConsecutiveFires, convID[:12], len(entry.Hashes))
		delete(pd.entries, convID)
		return nil
	}

	log.Printf("[TRIM] Persistent re-drop: %d messages match for conv=%s (fire #%d, %d hashes tracked)",
		len(indices), convID[:12], entry.ConsecutiveFires, len(entry.Hashes))
	return indices
}

// RecordDrops hashes and stores messages at the given indices.
func (pd *PersistentDrops) RecordDrops(convID string, msgs []interface{}, indices []int) {
	if len(indices) == 0 {
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	entry := pd.entries[convID]
	if entry == nil {
		entry = &dropEntry{
			Hashes:    make(map[string]bool),
			CreatedAt: time.Now(), // set ONCE, never refreshed
		}
		pd.entries[convID] = entry
	}

	for _, idx := range indices {
		if idx < len(msgs) {
			msg, ok := msgs[idx].(map[string]interface{})
			if !ok {
				continue
			}
			entry.Hashes[MsgHash(msg)] = true
		}
	}

	// Cap hashes
	if len(entry.Hashes) > pdMaxHashes {
		pd.trimHashes(entry)
	}

	log.Printf("[TRIM] Persistent drops recorded: conv=%s added %d hashes (total %d)",
		convID[:12], len(indices), len(entry.Hashes))
}

// Cleanup removes expired entries. Call periodically (e.g. every 10 minutes).
func (pd *PersistentDrops) Cleanup() int {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	purged := 0
	for id, entry := range pd.entries {
		if time.Since(entry.CreatedAt) > pdMaxAge {
			delete(pd.entries, id)
			purged++
		}
	}
	return purged
}

// Stats returns current state for dashboard/logging.
func (pd *PersistentDrops) Stats() map[string]interface{} {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	totalHashes := 0
	for _, entry := range pd.entries {
		totalHashes += len(entry.Hashes)
	}
	return map[string]interface{}{
		"conversations": len(pd.entries),
		"total_hashes":  totalHashes,
	}
}

// MarshalJSON for optional persistence.
func (pd *PersistentDrops) MarshalJSON() ([]byte, error) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	return json.Marshal(pd.entries)
}

// trimHashes keeps only pdMaxHashes entries (arbitrary eviction since map has no order).
func (pd *PersistentDrops) trimHashes(entry *dropEntry) {
	count := 0
	for k := range entry.Hashes {
		if count >= pdMaxHashes {
			delete(entry.Hashes, k)
		}
		count++
	}
}
