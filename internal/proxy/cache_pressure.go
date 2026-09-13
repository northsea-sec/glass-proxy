package proxy

import (
	"sync"
	"time"
)

// cachePressureTracker tracks active prefix hashes and detects Anthropic-side
// cache evictions. It maintains a sliding window of recently-seen prefixes and
// per-conversation cache_read history to distinguish self-inflicted cache breaks
// (our bytes changed) from external evictions (our bytes were stable but
// Anthropic evicted us due to slot pressure from other prefixes).
//
// This is observability-only. It does not modify request processing.
type cachePressureTracker struct {
	mu sync.Mutex

	// activePrefixes maps prefixHash -> last-seen time.
	// A prefix is "active" if seen within the TTL window.
	activePrefixes map[string]time.Time

	// lastConvState tracks per-conversation cache state for eviction detection.
	// Key: conversation_id (last 5 chars or full).
	lastConvState map[string]*convCacheState

	ttl time.Duration // TTL window for active prefix counting (matches Anthropic's 5-min default)
}

type convCacheState struct {
	lastPrefixHash string // prefix hash from the last request for this conv
	lastCacheRead  int    // cache_read_tokens from the last response for this conv
	lastSeen       time.Time
}

// systemToolsFloor is the approximate cache_read value when only the shared
// system+tools prefix survives an eviction. Conversation-specific messages
// are gone. Values observed empirically: 29,271 - 29,293 (nataraja era),
// varies with tool count. We use a threshold: if cr drops below this AND
// the prefix hash didn't change, it's an eviction.
const evictionCRThreshold = 35000 // generous: anything below 35K with unchanged hash = eviction signal

func newCachePressureTracker() *cachePressureTracker {
	return &cachePressureTracker{
		activePrefixes: make(map[string]time.Time),
		lastConvState:  make(map[string]*convCacheState),
		ttl:            5 * time.Minute,
	}
}

// ObserveRequest records a prefix hash for active prefix counting and returns:
//   - activePrefixCount: number of distinct active prefixes in the TTL window
//   - evictionDetected: true if this conversation's cache was likely evicted
//     by Anthropic (prefix unchanged but cache_read dropped significantly)
//
// Called after glass processing but before DB recording.
// cacheReadTokens comes from the API response (0 on first call).
func (t *cachePressureTracker) ObserveRequest(
	convID string,
	prefixHash string,
	cacheReadTokens int,
) (activePrefixCount int, evictionDetected bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()

	// --- Active prefix tracking ---

	// Expire stale entries
	for h, ts := range t.activePrefixes {
		if now.Sub(ts) > t.ttl {
			delete(t.activePrefixes, h)
		}
	}

	// Record this prefix
	if prefixHash != "" {
		t.activePrefixes[prefixHash] = now
	}

	activePrefixCount = len(t.activePrefixes)

	// --- Eviction detection ---

	// Expire stale conversation state (10 min -- longer than TTL to catch
	// cases where a conversation goes idle and comes back)
	for cid, st := range t.lastConvState {
		if now.Sub(st.lastSeen) > 10*time.Minute {
			delete(t.lastConvState, cid)
		}
	}

	prev, hasPrev := t.lastConvState[convID]

	if hasPrev && prefixHash != "" && prev.lastPrefixHash == prefixHash {
		// Same prefix hash as last time for this conversation.
		// If cache_read dropped significantly, Anthropic evicted us.
		if prev.lastCacheRead > evictionCRThreshold && cacheReadTokens > 0 && cacheReadTokens < evictionCRThreshold {
			evictionDetected = true
		}
	}

	// Update state for next comparison
	t.lastConvState[convID] = &convCacheState{
		lastPrefixHash: prefixHash,
		lastCacheRead:  cacheReadTokens,
		lastSeen:       now,
	}

	return activePrefixCount, evictionDetected
}
