// Package glass — LocalCache implements a per-session message store.
//
// Instead of reactively filtering CC's messages (strip-by-hash), the proxy
// OWNS the conversation state. Messages are stored on first encounter (ingestion)
// and served back verbatim on every subsequent call. This guarantees byte-identical
// serialization because the source data never changes.
//
// CC sends full history every call. The proxy:
//  1. Ignores messages it already has (positions 0..N-1)
//  2. Ingests new messages at the end (positions N+)
//  3. Strips system-reminders and cache_control on ingestion (once, permanently)
//  4. Serves the cached copy as the frozen prefix
//
// Cache breaks happen ONLY when:
//   - Eviction fires (removes messages from the head, one break per batch)
//   - The hot prefix itself changes (new messages, anchor movement, eviction)
//
// Between calls with no new messages, Build() produces byte-identical output.
//
// BREAKPOINT STRATEGY (stable anchor):
//   - A single breakpoint anchor index is stored in LocalCache metadata.
//   - The anchor only advances when the cache grows past a threshold
//     (breakpointAdvanceThreshold new messages since last anchor set).
//   - This means the breakpoint stays on the SAME message for many turns,
//     keeping the prefix byte-identical and maximizing Anthropic cache hits.
//
// PREFIX HASH LOGGING:
//   - After Build(), a SHA-256 hash of the serialized prefix is computed.
//   - If the hash differs from the previous call, a log entry is emitted
//     identifying which message index diverged.
package glass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"proxy.local/app/internal/trimmer"
)

const (
	// breakpointAdvanceThreshold: only move the breakpoint anchor after this
	// many new messages have been ingested since the last anchor was set.
	// With typical user+assistant pairs (2 msgs each), this means the anchor
	// moves roughly every 8 exchange rounds. Each advance causes one cache
	// break (cache_control position changes). The compression watermark
	// batch size should match or be a multiple of this to synchronize the
	// two break mechanisms into a single combined break per cycle.
	breakpointAdvanceThreshold = 16
	// postOverflowHotTailKeep caps how much live working context remains in the
	// Anthropic-visible lane after a session enters shadow-backed mode. The rest
	// stays exact in shadow/archive but no longer mutates the hot prefix.
	postOverflowHotTailKeep = 8
	// When the recent tail has no plain user turns, only backtrack a small amount
	// looking for one. Walking all the way back to the last plain user can retain
	// nearly the whole conversation in tool-result-heavy sessions.
	postOverflowTailStartBacktrack = 4
	requestSourceIndexKey          = "_glass_src_idx"
)

type requestView struct {
	Messages          []interface{}
	AnchorIdx         int
	PrevAnchorIdx     int
	WatermarkIdx      int // output-space index of the compression watermark (-1 if not set)
	ReferenceInsertAt int
	StrippedCount     int
	OrphansFixed      int
}

// LocalCache is a per-session message store.
// Messages are stored on first encounter and served back verbatim.
// Thread-safe: all methods acquire the mutex.
type LocalCache struct {
	mu       sync.Mutex
	messages []CachedMsg
	convID   string
	// loadedFromDisk marks a cache snapshot resurrected from persistence after a
	// process restart. Ref-bearing snapshots are not safe to resume live.
	loadedFromDisk bool

	// Stable breakpoint anchor: the message index where cache_control is placed.
	// Only advances when enough new messages accumulate past it.
	breakpointAnchor int // message index for the breakpoint (0 = not set)
	msgsAtLastAnchor int // len(messages) when anchor was last set/advanced
	// When the stable anchor advances, keep the previous anchor available as a
	// second deep cache breakpoint so Anthropic can still reuse the older prefix.
	prevBreakpointAnchor int // internal index, -1 = none

	// Frozen prefix for subagents: once set, subagents always get messages[0:freezePoint]
	// regardless of how many new messages the parent adds. This ensures stable caching.
	freezePoint       int  // index where to freeze for subagents (0 = not set)
	freezePointLocked bool // once locked, freezePoint never changes

	// Prefix hash tracking for cache-break diagnostics.
	lastPrefixHash      string   // SHA-256 hex of last measured stable prefix
	lastPrefixMsgHashes []string // per-message hashes for last measured stable prefix
}

// CachedMsg is a message stored in the local cache.
type CachedMsg struct {
	Msg          map[string]interface{} // canonical message (immutable after ingestion)
	Hash         string                 // content hash
	Role         string
	IsReference  bool // true if this is a reference placeholder
	IsCompressed bool // true if thinking/tool results were selectively stripped
	Tokens       int
	OrigTokens   int // token count before compression (0 if never compressed)
}

// NewLocalCache creates an empty cache for a conversation.
func NewLocalCache(convID string) *LocalCache {
	return &LocalCache{
		convID:               convID,
		prevBreakpointAnchor: -1,
	}
}

// Ingest examines CC's messages and adds new ones to the cache.
//
// CC sends full history every call. Messages already in the cache (by position)
// are ignored — the cache serves its own stored copy instead. New messages at
// the end are deep-copied, cleaned (system-reminders stripped, cache_control
// removed), and cached permanently.
//
// If CC sends fewer messages than the cache has, the cache keeps its version
// unchanged. CC may have temporarily dropped messages, but the cache is the
// source of truth.
//
// Returns the number of new messages added.
func (lc *LocalCache) Ingest(ccMsgs []interface{}) int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	// Use INTERNAL length (including reference placeholders) for position comparison.
	// CC sends the full history on every call. Positions 0..internalLen-1 in CC's
	// request correspond to messages already in our cache (some may be evicted).
	// New messages start at position internalLen.
	cacheLen := len(lc.messages)

	// CC sent fewer or equal messages — nothing new to ingest.
	// Keep our cached version (we are the source of truth).
	if len(ccMsgs) <= cacheLen {
		return 0
	}

	// First, ensure we don't overwrite any references with re-sent data.
	var blockedPositions []int
	for i := 0; i < cacheLen && i < len(ccMsgs); i++ {
		// If this position has a reference, verify CC isn't trying to overwrite it
		if lc.messages[i].IsReference {
			raw, ok := ccMsgs[i].(map[string]interface{})
			if ok && !isReferencePlaceholder(raw) {
				blockedPositions = append(blockedPositions, i)
			}
		}
	}
	if len(blockedPositions) > 0 {
		log.Printf("[LOCALCACHE] BLOCKED reingest attempts conv=%s positions=%s (%d evicted slots replayed)",
			lc.convID, formatPositionRanges(blockedPositions), len(blockedPositions))
	}

	// Now process new messages
	added := 0
	for i := cacheLen; i < len(ccMsgs); i++ {
		raw, ok := ccMsgs[i].(map[string]interface{})
		if !ok {
			continue
		}

		// Deep copy so CC's future mutations don't affect our cache
		copied := deepCopyMsg(raw)

		// Strip cache_control — proxy controls breakpoints, not CC
		delete(copied, "cache_control")
		stripCacheControlFromContent(copied)

		// Strip system-reminders on ingestion (permanent, one-time)
		cleanSysRemindersInMsg(copied)

		// Strip thinking blocks on ingestion so the cache stores clean messages.
		// This makes build output deterministic — CC modifies thinking blocks
		// between requests, which broke prefix cache when stripped at build time.
		stripThinkingFromMsg(copied)

		// Tool result truncation disabled — let the model see full content.
		// truncateOversizedToolResults(copied)

		role, _ := copied["role"].(string)

		lc.messages = append(lc.messages, CachedMsg{
			Msg:    copied,
			Hash:   msgHash(copied),
			Role:   role,
			Tokens: estimateMessageTokens(copied),
		})
		added++
	}

	if added > 0 {
		log.Printf("[LOCALCACHE] conv=%s ingested %d new messages (total cached: %d)",
			lc.convID, added, len(lc.messages))

		// Claude Code can emit a user tool_result followed by a separate user
		// prompt. Anthropic requires those to be a single user turn, so absorb
		// the trailing same-role message into the preceding retained message
		// while keeping the absorbed slot as a reference placeholder.
		if repaired := lc.repairBrokenToolBoundariesLocked(); repaired > 0 {
			log.Printf("[LOCALCACHE] conv=%s repaired %d invalid tool/role boundaries during ingest",
				lc.convID, repaired)
		}

		// Update breakpoint anchor if needed
		lc.maybeAdvanceBreakpoint()
	}

	return added
}

// maybeAdvanceBreakpoint checks if the breakpoint anchor should move.
// Called under lock.
func (lc *LocalCache) maybeAdvanceBreakpoint() {
	n := len(lc.messages)
	if n < 2 {
		return
	}

	// First time: set anchor at n-2 (second-to-last message)
	if lc.breakpointAnchor == 0 && n >= 2 {
		lc.breakpointAnchor = n - 2
		lc.msgsAtLastAnchor = n
		lc.prevBreakpointAnchor = -1
		log.Printf("[LOCALCACHE] conv=%s breakpoint anchor initialized at msg[%d] (total=%d)",
			lc.convID, lc.breakpointAnchor, n)
		return
	}

	// Only advance if enough new messages accumulated past the anchor
	newSinceAnchor := n - lc.msgsAtLastAnchor
	if newSinceAnchor >= breakpointAdvanceThreshold {
		// Advance anchor to n-2 (keep 1 message after anchor as mutable tail)
		oldAnchor := lc.breakpointAnchor
		lc.prevBreakpointAnchor = oldAnchor
		lc.breakpointAnchor = n - 2
		lc.msgsAtLastAnchor = n
		log.Printf("[LOCALCACHE] conv=%s breakpoint anchor advanced: msg[%d] -> msg[%d] (delta=%d msgs, previous preserved)",
			lc.convID, oldAnchor, lc.breakpointAnchor, newSinceAnchor)
	}
}

// BreakpointAnchor returns the stable breakpoint index in the BUILD() output slice.
// Since Build() skips IsReference messages, this translates the internal cache
// position to the output position. Returns -1 if not yet set.
func (lc *LocalCache) BreakpointAnchor() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.breakpointAnchorOutputLocked()
}

// RawBreakpointAnchor returns the internal cache-index of the breakpoint anchor.
// Unlike BreakpointAnchor(), this is NOT adjusted for reference-message skipping.
// Used by the compression watermark clamp to prevent compression inside the frozen prefix.
func (lc *LocalCache) RawBreakpointAnchor() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.breakpointAnchor
}

// RawPrevBreakpointAnchor returns the internal (pre-Build) previous anchor index, or -1.
func (lc *LocalCache) RawPrevBreakpointAnchor() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.prevBreakpointAnchor
}

// PreviousBreakpointAnchor returns the prior stable breakpoint index in the
// Build() output slice, or -1 when none is preserved.
func (lc *LocalCache) PreviousBreakpointAnchor() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.previousBreakpointAnchorOutputLocked()
}

func (lc *LocalCache) breakpointAnchorOutputLocked() int {
	if lc.breakpointAnchor == 0 && len(lc.messages) < 2 {
		return -1
	}
	// Count non-reference messages up to and including the anchor position.
	// This gives the index in the Build() output slice.
	outputIdx := 0
	for i := 0; i <= lc.breakpointAnchor && i < len(lc.messages); i++ {
		if !lc.messages[i].IsReference {
			outputIdx++
		}
	}
	if outputIdx == 0 {
		return -1
	}
	return outputIdx - 1 // 0-based index into Build() output
}

func (lc *LocalCache) previousBreakpointAnchorOutputLocked() int {
	if lc.prevBreakpointAnchor < 0 {
		return -1
	}
	outputIdx := 0
	for i := 0; i <= lc.prevBreakpointAnchor && i < len(lc.messages); i++ {
		if !lc.messages[i].IsReference {
			outputIdx++
		}
	}
	if outputIdx == 0 {
		return -1
	}
	return outputIdx - 1
}

// compressionWatermarkOutputLocked maps a compression watermark (internal cache
// index) to the output-space index, skipping evicted reference slots.
func (lc *LocalCache) compressionWatermarkOutputLocked(watermark int) int {
	if watermark <= 0 {
		return -1
	}
	outputIdx := 0
	for i := 0; i < watermark && i < len(lc.messages); i++ {
		if !lc.messages[i].IsReference {
			outputIdx++
		}
	}
	if outputIdx == 0 {
		return -1
	}
	return outputIdx - 1 // 0-based index into Build() output
}

// CompressionWatermarkOutput maps a compression watermark (internal cache index)
// to the output-space index used by placeBreakpoint. Thread-safe.
func (lc *LocalCache) CompressionWatermarkOutput(watermark int) int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.compressionWatermarkOutputLocked(watermark)
}

// Build returns deep copies of non-evicted cached messages as a []interface{}
// for the request body. IsReference (evicted placeholder) messages are SKIPPED —
// only the actual retained messages are sent to Anthropic. The reference pair
// injected by injectReferenceMessages() represents the evicted content.
//
// Each call produces fresh copies so downstream code (placeBreakpoint,
// stripThinkingBlocks, final-body validation fallback) can safely mutate them without
// affecting the canonical cache. Between calls with no new messages, the
// copies are byte-identical when serialized because the source data is unchanged.
//
// Also computes and logs a prefix hash for cache-break diagnostics.
func (lc *LocalCache) Build() []interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	result := lc.buildCopiesLocked(len(lc.messages))

	// Compute prefix hash for diagnostics
	lc.computePrefixHash(result, lc.breakpointAnchorOutputLocked())

	return result
}

// BuildForRequest returns the normalized request view used for the live
// Anthropic request. This is the canonical prefix surface that breakpoint
// placement and prefix hashing must reason about.
func (lc *LocalCache) BuildForRequest(stripThinking bool) requestView {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	raw := lc.buildCopiesLocked(len(lc.messages))
	view := lc.normalizeRequestViewLocked(
		raw,
		lc.breakpointAnchorOutputLocked(),
		lc.previousBreakpointAnchorOutputLocked(),
		stripThinking,
	)
	lc.computePrefixHash(view.Messages, view.AnchorIdx)
	return view
}

// BuildPinnedFrameForRequest returns the post-overflow request view for
// sessions already backed by shadow history. The live Anthropic-visible lane is
// rebuilt as:
//
//	anchors | (injected reference pair) | bounded hot tail
//
// Process() later pins the first outbound breakpoint to the injected reference
// assistant and uses the pinned tail anchor here as the second message
// breakpoint. Rebuilding the bounded tail on every request prevents the live
// lane from silently regrowing between overflow batches.
func (lc *LocalCache) BuildPinnedFrameForRequest(stripThinking bool, anchorKeep, recentKeep int, requireUserFinal bool) requestView {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	retained := lc.retainedIndicesLocked()
	if len(retained) == 0 {
		return requestView{AnchorIdx: -1, PrevAnchorIdx: -1, ReferenceInsertAt: -1}
	}
	anchorCount := lc.pinnedAnchorCountLocked(anchorKeep, requireUserFinal)
	if anchorCount > len(retained) {
		anchorCount = len(retained)
	}
	if anchorCount <= 0 {
		// Safety: adjustReferenceInsertAt can return 0 when no safe boundary
		// is found. Use 1 to prevent index-out-of-range in tail start logic.
		anchorCount = 1
	}
	tailStart := lc.pinnedTailStartPosLocked(retained, anchorCount, recentKeep)

	selected := make([]int, 0, anchorCount+len(retained)-tailStart)
	selected = append(selected, retained[:anchorCount]...)
	if tailStart < len(retained) {
		selected = append(selected, retained[tailStart:]...)
	}

	raw := lc.buildCopiesFromIndicesLocked(selected)
	view := lc.normalizeRequestViewLocked(
		raw,
		pinnedViewAnchorIndex(len(raw), anchorCount),
		-1,
		stripThinking,
	)
	if anchorCount > len(view.Messages) {
		anchorCount = len(view.Messages)
	}
	view.ReferenceInsertAt = anchorCount
	lc.computePrefixHash(view.Messages, view.AnchorIdx)
	return view
}

func pinnedViewAnchorIndex(messageCount, anchorCount int) int {
	if messageCount == 0 {
		return -1
	}
	if messageCount == 1 {
		return 0
	}

	// Place the breakpoint right after the anchor messages.
	// This stays STABLE as the tail grows, keeping the prefix cache alive.
	// Previously this was messageCount-2 which moved every request.
	anchorIdx := anchorCount - 1
	if anchorIdx < 0 {
		anchorIdx = 0
	}
	if anchorIdx >= messageCount {
		anchorIdx = messageCount - 1
	}
	return anchorIdx
}

// SetFreezePoint locks the freeze point for subagents at the current message count.
// Once locked, it never changes, ensuring subagents always get the same prefix.
// Returns the freeze point (new or existing).
func (lc *LocalCache) SetFreezePoint() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if lc.freezePointLocked {
		return lc.freezePoint
	}

	// Lock at current message count
	lc.freezePoint = len(lc.messages)
	lc.freezePointLocked = true

	log.Printf("[LOCALCACHE] conv=%s freeze point locked at %d messages", lc.convID, lc.freezePoint)
	return lc.freezePoint
}

// BuildFrozen returns the frozen prefix for subagents — messages[0:freezePoint],
// skipping IsReference placeholders, matching Build() semantics.
// If freeze point not set, returns empty (subagent shouldn't run yet).
func (lc *LocalCache) BuildFrozen() []interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if !lc.freezePointLocked || lc.freezePoint == 0 {
		log.Printf("[LOCALCACHE] conv=%s BuildFrozen called but freeze point not set", lc.convID)
		return []interface{}{}
	}

	limit := lc.freezePoint
	if limit > len(lc.messages) {
		limit = len(lc.messages)
	}

	result := lc.buildCopiesLocked(limit)

	log.Printf("[LOCALCACHE] conv=%s BuildFrozen returning %d messages (total cache has %d, freeze=%d)",
		lc.convID, len(result), len(lc.messages), lc.freezePoint)

	return result
}

// BuildFrozenForRequest returns the normalized frozen-prefix view used by
// subagents sharing the parent's cached prefix.
func (lc *LocalCache) BuildFrozenForRequest(stripThinking bool) requestView {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if !lc.freezePointLocked || lc.freezePoint == 0 {
		log.Printf("[LOCALCACHE] conv=%s BuildFrozenForRequest called but freeze point not set", lc.convID)
		return requestView{AnchorIdx: -1, PrevAnchorIdx: -1, ReferenceInsertAt: -1}
	}

	limit := lc.freezePoint
	if limit > len(lc.messages) {
		limit = len(lc.messages)
	}

	raw := lc.buildCopiesLocked(limit)
	rawAnchor := lc.breakpointAnchorOutputLocked()
	frozenLen := len(raw)
	if rawAnchor >= frozenLen {
		rawAnchor = frozenLen - 1
	}

	view := lc.normalizeRequestViewLocked(
		raw,
		rawAnchor,
		lc.previousBreakpointAnchorOutputLocked(),
		stripThinking,
	)
	lc.computePrefixHash(view.Messages, view.AnchorIdx)

	log.Printf("[LOCALCACHE] conv=%s BuildFrozenForRequest returning %d messages (total cache has %d, freeze=%d, anchor=%d)",
		lc.convID, len(view.Messages), len(lc.messages), lc.freezePoint, view.AnchorIdx)

	return view
}

// computePrefixHash computes SHA-256 of the serialized stable prefix and logs on
// change. Called under lock.
func (lc *LocalCache) computePrefixHash(msgs []interface{}, anchorIdx int) {
	if len(msgs) == 0 {
		return
	}

	limit := len(msgs)
	if anchorIdx >= 0 && anchorIdx+1 <= len(msgs) {
		limit = anchorIdx + 1
	}
	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}
	stablePrefix := msgs[:limit]

	data, err := json.Marshal(stablePrefix)
	if err != nil {
		return
	}
	h := sha256.Sum256(data)
	currentHash := fmt.Sprintf("%x", h[:16])
	currentMsgHashes := make([]string, 0, len(stablePrefix))
	for _, m := range stablePrefix {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			currentMsgHashes = append(currentMsgHashes, "")
			continue
		}
		currentMsgHashes = append(currentMsgHashes, msgHash(mMap))
	}

	if lc.lastPrefixHash != "" && currentHash != lc.lastPrefixHash {
		// Prefix changed — find which message in the measured stable prefix diverged.
		divergeIdx := lc.findDivergence(lc.lastPrefixMsgHashes, currentMsgHashes)
		log.Printf("[LOCALCACHE] ⚠ PREFIX CHANGED conv=%s hash=%s->%s diverge_at=msg[%d] measured=%d total=%d anchor=%d",
			lc.convID, lc.lastPrefixHash[:12], currentHash[:12], divergeIdx, len(stablePrefix), len(msgs), anchorIdx)
	} else if lc.lastPrefixHash == "" {
		log.Printf("[LOCALCACHE] conv=%s prefix hash initialized: %s (measured=%d total=%d)",
			lc.convID, currentHash[:12], len(stablePrefix), len(msgs))
	}

	lc.lastPrefixHash = currentHash
	lc.lastPrefixMsgHashes = currentMsgHashes
}

// findDivergence compares two measured prefix hash lists and returns the first
// differing message index, or the first index past the shorter slice.
func (lc *LocalCache) findDivergence(prevHashes, currentHashes []string) int {
	limit := len(prevHashes)
	if len(currentHashes) < limit {
		limit = len(currentHashes)
	}
	for i := 0; i < limit; i++ {
		if prevHashes[i] != currentHashes[i] {
			return i
		}
	}
	if len(prevHashes) != len(currentHashes) {
		return limit
	}
	return -1
}

// TotalTokens returns the estimated total tokens of retained (non-evicted) messages.
// IsReference placeholders are excluded — they are not sent to Anthropic.
func (lc *LocalCache) TotalTokens() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	total := 0
	for _, cm := range lc.messages {
		if !cm.IsReference {
			total += cm.Tokens
		}
	}
	return total
}

// Len returns the number of retained (non-evicted) messages.
// Matches what Build() actually returns.
func (lc *LocalCache) Len() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	n := 0
	for _, cm := range lc.messages {
		if !cm.IsReference {
			n++
		}
	}
	return n
}

// InternalLen returns the full internal message count (including reference placeholders).
// Used for position tracking and Ingest() comparisons.
func (lc *LocalCache) InternalLen() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return len(lc.messages)
}

// ReferenceCount returns the number of reference placeholders currently present
// in the cache snapshot. This includes both eviction placeholders and
// repair-only placeholders that preserve original message positions.
func (lc *LocalCache) ReferenceCount() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	n := 0
	for _, cm := range lc.messages {
		if cm.IsReference {
			n++
		}
	}
	return n
}

// EvictionReferenceCount returns the number of reference placeholders that
// represent shadow-evicted messages. Repair placeholders are structural markers
// and must not be treated as shadow-backed state.
func (lc *LocalCache) EvictionReferenceCount() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	n := 0
	for _, cm := range lc.messages {
		if cm.isEvictionReference() {
			n++
		}
	}
	return n
}

func (cm CachedMsg) isEvictionReference() bool {
	return cm.IsReference && !strings.HasPrefix(cm.Hash, "repair_")
}

// BakeEvictionBookmark replaces the first IsReference slot in the cache with a
// simple user message pointing to the chapter archive. Called after eviction
// and chapter writing so the path is already known.
//
// If a bookmark already exists (content starts with the bookmark prefix) and
// the path hasn't changed, this is a no-op. If the path changed, the existing
// bookmark is updated in place.
//
// The bookmark is a plain user message. BuildForRequest will include it in the
// output as the first visible message, giving the agent the chapter path
// without any dynamic injection at request-build time.
// buildChapterDirListing reads a chapter session directory and returns a
// human-readable listing of each chapter file with its message range.
func buildChapterDirListing(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "Directory listing unavailable.\n"
	}
	var b strings.Builder
	b.WriteString("Chapter files in this directory:\n")
	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "index.md" {
			continue
		}
		found = true
		// Try to read the first line (the title) for context
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			b.WriteString(fmt.Sprintf("  - %s\n", e.Name()))
			continue
		}
		firstLine := strings.SplitN(string(data), "\n", 2)[0]
		firstLine = strings.TrimPrefix(firstLine, "# ")
		b.WriteString(fmt.Sprintf("  - %s — %s\n", e.Name(), firstLine))
	}
	if !found {
		b.WriteString("  (no chapter files yet)\n")
	}
	return b.String()
}

func (lc *LocalCache) BakeEvictionBookmark(chapterPath string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	const bookmarkPrefix = "Evicted context archived to: "

	// Build a listing of chapter files in the directory so the agent
	// knows exactly which files exist and what message ranges they cover.
	listing := buildChapterDirListing(chapterPath)

	text := bookmarkPrefix + chapterPath + "\n\n" + listing +
		"\nMANDATORY: Read the chapter files in this directory to recover evicted context. " +
		"Each file covers one eviction batch. Read what you need, then report your understanding of: " +
		"(1) what was being worked on, (2) key decisions made, (3) current state."

	// Scan for an existing bookmark and update it if the path changed.
	for i := range lc.messages {
		if lc.messages[i].IsReference {
			continue
		}
		role, _ := lc.messages[i].Msg["role"].(string)
		if role != "user" {
			continue
		}
		if content, ok := lc.messages[i].Msg["content"].([]interface{}); ok {
			for _, blk := range content {
				if block, ok := blk.(map[string]interface{}); ok {
					if t, _ := block["type"].(string); t == "text" {
						if txt, _ := block["text"].(string); strings.HasPrefix(txt, bookmarkPrefix) {
							if txt == text {
								return // already up to date
							}
							// Path changed — update in place
							block["text"] = text
							lc.messages[i].Hash = fmt.Sprintf("bookmark_%s", shortHashPrefix(chapterPath))
							lc.lastPrefixHash = ""
							lc.lastPrefixMsgHashes = nil
							log.Printf("[LOCALCACHE] conv=%s updated eviction bookmark to %s", lc.convID, chapterPath)
							return
						}
					}
				}
			}
		}
	}

	// No bookmark found — replace the first retained user message with the
	// bookmark. This puts the bookmark at position 0 in the output view,
	// making it the active context-setting message (not buried behind stale
	// anchor messages that create fake continuity). Then convert all other
	// anchor messages (between the bookmark and the eviction zone) to
	// references so the view is: [bookmark] → [hot tail].
	msg := map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": text,
			},
		},
	}

	bookmarkPlaced := false
	for i := range lc.messages {
		if lc.messages[i].IsReference {
			continue
		}
		if !bookmarkPlaced {
			// Replace first retained message with bookmark
			lc.messages[i] = CachedMsg{
				Msg:    msg,
				Hash:   fmt.Sprintf("bookmark_%s", shortHashPrefix(chapterPath)),
				Role:   "user",
				Tokens: estimateMessageTokens(msg),
			}
			bookmarkPlaced = true
			log.Printf("[LOCALCACHE] conv=%s baked eviction bookmark at slot %d -> %s", lc.convID, i, chapterPath)

			// Convert remaining pre-eviction anchor messages to references.
			// These are the stale messages between the bookmark and the
			// eviction zone that create fake continuity if left in place.
			for j := i + 1; j < len(lc.messages); j++ {
				if lc.messages[j].IsReference {
					break // reached the eviction zone, stop
				}
				lc.messages[j] = CachedMsg{
					IsReference: true,
					Role:        lc.messages[j].Role,
					Tokens:      0,
				}
			}
			lc.lastPrefixHash = ""
			lc.lastPrefixMsgHashes = nil
			return
		}
	}
	log.Printf("[LOCALCACHE] conv=%s WARNING: no retained slot available for eviction bookmark", lc.convID)
}

// hasBookmarkLocked returns true if the first non-reference message in the
// cache is an eviction bookmark. Must be called with lc.mu held.
func (lc *LocalCache) hasBookmarkLocked() bool {
	const bookmarkPrefix = "Evicted context archived to: "
	for _, cached := range lc.messages {
		if cached.IsReference {
			continue
		}
		if cached.Role != "user" {
			return false
		}
		content, ok := cached.Msg["content"].([]interface{})
		if !ok {
			return false
		}
		for _, blk := range content {
			block, ok := blk.(map[string]interface{})
			if !ok {
				continue
			}
			if t, _ := block["type"].(string); t == "text" {
				txt, _ := block["text"].(string)
				return strings.HasPrefix(txt, bookmarkPrefix)
			}
		}
		return false
	}
	return false
}

func shortHashPrefix(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:4])
}

// Evict removes oldest messages from the middle of the cache until total message
// tokens are at or below targetMsgTokens. Preserves anchorKeep messages at the
// front (task definition) and recentKeep messages at the back (current context).
//
// IMPORTANT: Messages are REPLACED with reference placeholders, not deleted.
// This maintains array positions and prevents reingest cycles.
//
// Returns the evicted messages for shadow file writing. Returns nil if no
// eviction was needed or possible.

// front (task definition) and recentKeep messages at the back (current context).
//
// IMPORTANT: Messages are REPLACED with reference placeholders, not deleted.
// This maintains array positions and prevents reingest cycles.
//
// Returns the evicted messages for shadow file writing. Returns nil if no
// eviction was needed or possible.
func (lc *LocalCache) Evict(targetMsgTokens, anchorKeep, recentKeep int) []map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	totalTokens := 0
	for _, cm := range lc.messages {
		totalTokens += cm.Tokens
	}

	if totalTokens <= targetMsgTokens {
		return nil
	}

	anchorEnd := anchorKeep
	if anchorEnd > len(lc.messages) {
		anchorEnd = len(lc.messages)
	}
	recentStart := len(lc.messages) - recentKeep
	if recentStart < anchorEnd {
		recentStart = anchorEnd
	}

	// Eviction zone is [anchorEnd, recentStart).
	// When anchorKeep + recentKeep >= len(messages), the zone is empty.
	// Dynamically reduce the effective anchor to open a gap, minimum 2.
	for recentStart <= anchorEnd && anchorEnd > 2 {
		anchorEnd--
		recentStart = len(lc.messages) - recentKeep
		if recentStart < anchorEnd {
			recentStart = anchorEnd
		}
		log.Printf("[LOCALCACHE] conv=%s eviction zone empty, reducing effective anchorKeep to %d",
			lc.convID, anchorEnd)
	}
	if recentStart <= anchorEnd {
		log.Printf("[LOCALCACHE] Cannot evict: anchors(%d) + recent(%d) cover all %d messages",
			anchorEnd, recentKeep, len(lc.messages))
		return nil
	}

	// Replace messages in eviction zone with references until under target
	var evicted []map[string]interface{}
	tokensDropped := 0

	for i := anchorEnd; i < recentStart && (totalTokens-tokensDropped) > targetMsgTokens; i++ {
		// Skip if already a reference
		if lc.messages[i].IsReference {
			continue
		}

		// Treat an immediate assistant tool_use + user tool_result exchange as
		// atomic. Evict both together or leave both intact; never orphan one half.
		if nextIdx, ok := lc.pairedToolResultIndexLocked(i); ok {
			if nextIdx >= recentStart || lc.messages[nextIdx].IsReference {
				continue
			}
			if lc.breaksAlternationIfEvictedLocked(i, nextIdx) {
				continue
			}
			lc.evictMessageAtLocked(i, &evicted, &tokensDropped)
			lc.evictMessageAtLocked(nextIdx, &evicted, &tokensDropped)
			i = nextIdx
			continue
		}
		if lc.userToolResultDependsOnPreviousLocked(i) {
			continue
		}
		if lc.breaksAlternationIfEvictedLocked(i, i) {
			if nextIdx, ok := lc.nextAlternatingPairIndexLocked(i, recentStart); ok {
				if !lc.breaksAlternationIfEvictedLocked(i, nextIdx) {
					lc.evictMessageAtLocked(i, &evicted, &tokensDropped)
					lc.evictMessageAtLocked(nextIdx, &evicted, &tokensDropped)
					i = nextIdx
					continue
				}
			}
			continue
		}

		lc.evictMessageAtLocked(i, &evicted, &tokensDropped)
	}

	if len(evicted) == 0 {
		return nil
	}

	// NOTE: We do NOT rebuild the array - messages stay at same positions

	// Reset breakpoint anchor after eviction (prefix changed)
	lc.breakpointAnchor = 0
	lc.msgsAtLastAnchor = 0
	lc.prevBreakpointAnchor = -1
	lc.lastPrefixHash = "" // force re-hash
	lc.lastPrefixMsgHashes = nil
	lc.maybeAdvanceBreakpoint()

	log.Printf("[LOCALCACHE] conv=%s replaced %d messages with references (~%d tokens saved): %d -> %d tokens",
		lc.convID, len(evicted), tokensDropped, totalTokens, totalTokens-tokensDropped)

	return evicted
}

// PinFrame archives the mutable bridge segment between the fixed anchor region
// and the bounded recent working tail. This is the one-way post-overflow mode
// switch: once a session is shadow-backed, the live Anthropic lane should stop
// exposing whichever mid-history messages happen to remain right after the
// reference trench.
func (lc *LocalCache) PinFrame(anchorKeep, recentKeep int, requireUserFinal bool) []map[string]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	retained := lc.retainedIndicesLocked()
	if len(retained) == 0 {
		return nil
	}
	anchorCount := lc.pinnedAnchorCountLocked(anchorKeep, requireUserFinal)
	if anchorCount > len(retained) {
		anchorCount = len(retained)
	}
	if anchorCount <= 0 {
		anchorCount = 1
	}
	tailStart := lc.pinnedTailStartPosLocked(retained, anchorCount, recentKeep)
	if tailStart <= anchorCount || tailStart > len(retained) {
		return nil
	}

	var evicted []map[string]interface{}
	tokensDropped := 0
	for pos := anchorCount; pos < tailStart; pos++ {
		lc.evictMessageAtLocked(retained[pos], &evicted, &tokensDropped)
	}
	if len(evicted) == 0 {
		return nil
	}
	if tokensDropped < 0 {
		tokensDropped = 0
	}

	lc.breakpointAnchor = 0
	lc.msgsAtLastAnchor = 0
	lc.prevBreakpointAnchor = -1
	lc.lastPrefixHash = ""
	lc.lastPrefixMsgHashes = nil
	lc.maybeAdvanceBreakpoint()

	log.Printf("[LOCALCACHE] conv=%s pinned-frame archived %d bridge messages (~%d tokens saved)",
		lc.convID, len(evicted), tokensDropped)

	return evicted
}

func (lc *LocalCache) nextAlternatingPairIndexLocked(idx, recentStart int) (int, bool) {
	if idx < 0 || idx+1 >= len(lc.messages) || idx+1 >= recentStart {
		return 0, false
	}
	cur := lc.messages[idx]
	next := lc.messages[idx+1]
	if cur.IsReference || next.IsReference {
		return 0, false
	}
	if cur.Role == "" || next.Role == "" || cur.Role == next.Role {
		return 0, false
	}
	if lc.userToolResultDependsOnPreviousLocked(idx + 1) {
		return 0, false
	}
	return idx + 1, true
}

func (lc *LocalCache) evictMessageAtLocked(idx int, evicted *[]map[string]interface{}, tokensDropped *int) {
	if idx < 0 || idx >= len(lc.messages) {
		return
	}
	if lc.messages[idx].IsReference {
		return
	}

	*evicted = append(*evicted, lc.messages[idx].Msg)
	*tokensDropped += lc.messages[idx].Tokens

	originalRole := lc.messages[idx].Role
	lc.messages[idx] = CachedMsg{
		Msg: map[string]interface{}{
			"role":    originalRole,
			"content": fmt.Sprintf("[Message %d moved to shadow file - %d tokens]", idx, lc.messages[idx].Tokens),
		},
		Hash:        fmt.Sprintf("ref_%d_%s", idx, lc.messages[idx].Hash[:8]),
		Role:        originalRole,
		IsReference: true,
		Tokens:      50,
	}
	*tokensDropped -= 50
}

func (lc *LocalCache) markReferenceLocked(idx int, reason string) {
	if idx < 0 || idx >= len(lc.messages) || lc.messages[idx].IsReference {
		return
	}
	originalRole := lc.messages[idx].Role
	lc.messages[idx] = CachedMsg{
		Msg: map[string]interface{}{
			"role":    originalRole,
			"content": fmt.Sprintf("[Message %d hidden by Glass repair: %s]", idx, reason),
		},
		Hash:        fmt.Sprintf("repair_%d_%s", idx, shortHashPrefix(lc.messages[idx].Hash)),
		Role:        originalRole,
		IsReference: true,
		Tokens:      50,
	}
}

func (lc *LocalCache) pairedToolResultIndexLocked(idx int) (int, bool) {
	if idx < 0 || idx+1 >= len(lc.messages) {
		return 0, false
	}
	msg := lc.messages[idx]
	next := lc.messages[idx+1]
	if msg.IsReference || next.IsReference || msg.Role != "assistant" || next.Role != "user" {
		return 0, false
	}
	toolUses := collectToolUseSet(msg.Msg["content"])
	if len(toolUses) == 0 {
		return 0, false
	}
	results := collectToolResultIDs(next.Msg["content"])
	if len(results) == 0 {
		return 0, false
	}
	for id := range toolUses {
		if !results[id] {
			return 0, false
		}
	}
	return idx + 1, true
}

func (lc *LocalCache) userToolResultDependsOnPreviousLocked(idx int) bool {
	if idx <= 0 || idx >= len(lc.messages) {
		return false
	}
	msg := lc.messages[idx]
	prev := lc.messages[idx-1]
	if msg.IsReference || prev.IsReference || msg.Role != "user" || prev.Role != "assistant" {
		return false
	}
	results := collectToolResultIDs(msg.Msg["content"])
	if len(results) == 0 {
		return false
	}
	toolUses := collectToolUseSet(prev.Msg["content"])
	if len(toolUses) == 0 {
		return false
	}
	for id := range results {
		if toolUses[id] {
			return true
		}
	}
	return false
}

func (lc *LocalCache) repairBrokenToolBoundariesLocked() int {
	repaired := 0
	for i := 0; i < len(lc.messages); i++ {
		msg := lc.messages[i]
		if msg.IsReference {
			continue
		}
		switch msg.Role {
		case "assistant":
			toolUses := collectToolUseSet(msg.Msg["content"])
			if len(toolUses) == 0 {
				continue
			}
			if i+1 >= len(lc.messages) {
				lc.markReferenceLocked(i, "dangling tool_use at tail")
				repaired++
				continue
			}
			next := lc.messages[i+1]
			if next.IsReference {
				lc.markReferenceLocked(i, "paired tool_result previously evicted")
				repaired++
				continue
			}
			if next.Role != "user" {
				lc.markReferenceLocked(i, "assistant tool_use not followed by user")
				repaired++
				continue
			}
			results := collectToolResultIDs(next.Msg["content"])
			matchedAll := len(results) > 0
			for id := range toolUses {
				if !results[id] {
					matchedAll = false
					break
				}
			}
			if !matchedAll {
				lc.markReferenceLocked(i, "assistant tool_use missing immediate tool_result")
				repaired++
			}
		case "user":
			results := collectToolResultIDs(msg.Msg["content"])
			if len(results) == 0 {
				continue
			}
			if i == 0 {
				lc.markReferenceLocked(i, "orphan tool_result at head")
				repaired++
				continue
			}
			prev := lc.messages[i-1]
			if prev.IsReference {
				lc.markReferenceLocked(i, "paired tool_use previously evicted")
				repaired++
				continue
			}
			if prev.Role != "assistant" {
				lc.markReferenceLocked(i, "user tool_result not preceded by assistant")
				repaired++
				continue
			}
			toolUses := collectToolUseSet(prev.Msg["content"])
			matchedAny := false
			for id := range results {
				if toolUses[id] {
					matchedAny = true
					break
				}
			}
			if !matchedAny {
				lc.markReferenceLocked(i, "user tool_result missing matching tool_use")
				repaired++
			}
		}
	}
	if repaired > 0 {
		lc.lastPrefixHash = ""
		lc.lastPrefixMsgHashes = nil
	}
	// After marking orphans, merging consecutive same-role messages that
	// the marking may have created is essential to avoid Anthropic 400s.
	merged := lc.mergeConsecutiveSameRoleLocked()
	if merged > 0 {
		lc.lastPrefixHash = ""
		lc.lastPrefixMsgHashes = nil
	}
	return repaired + merged
}

// mergeConsecutiveSameRoleLocked scans retained messages for consecutive
// same-role adjacencies (which repairBrokenToolBoundariesLocked can create
// by marking an interleaved message as reference) and merges the second
// message's content into the first, then marks the second as reference.
func (lc *LocalCache) mergeConsecutiveSameRoleLocked() int {
	merged := 0
	prevRetainedIdx := -1
	for i := 0; i < len(lc.messages); i++ {
		if lc.messages[i].IsReference {
			continue
		}
		if prevRetainedIdx >= 0 && lc.messages[prevRetainedIdx].Role == lc.messages[i].Role {
			// Consecutive same-role: merge content of i into prevRetainedIdx
			prevContent := msgContentSlice(lc.messages[prevRetainedIdx].Msg)
			currContent := msgContentSlice(lc.messages[i].Msg)
			combined := append(prevContent, currContent...)
			lc.messages[prevRetainedIdx].Msg["content"] = toInterfaceSlice(combined)
			// Absorb tokens
			lc.messages[prevRetainedIdx].Tokens += lc.messages[i].Tokens
			lc.markReferenceLocked(i, fmt.Sprintf("merged into msg[%d] to fix consecutive %s", prevRetainedIdx, lc.messages[prevRetainedIdx].Role))
			merged++
			log.Printf("[LOCALCACHE] Merged consecutive %s msg[%d] into msg[%d]",
				lc.messages[prevRetainedIdx].Role, i, prevRetainedIdx)
			// Don't advance prevRetainedIdx — the merged message at prevRetainedIdx
			// may still collide with the next retained message.
			continue
		}
		prevRetainedIdx = i
	}
	return merged
}

// msgContentSlice extracts the content field as a slice of maps.
func msgContentSlice(msg map[string]interface{}) []map[string]interface{} {
	raw, ok := msg["content"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	case string:
		return []map[string]interface{}{{"type": "text", "text": v}}
	}
	return nil
}

// toInterfaceSlice converts []map[string]interface{} back to []interface{}.
func toInterfaceSlice(blocks []map[string]interface{}) []interface{} {
	result := make([]interface{}, len(blocks))
	for i, b := range blocks {
		result[i] = b
	}
	return result
}

func messageHasToolUse(msg map[string]interface{}) bool {
	content, _ := msg["content"].([]interface{})
	return len(collectToolUseIDs(content)) > 0
}

func messageHasToolResult(msg map[string]interface{}) bool {
	content, _ := msg["content"].([]interface{})
	return len(collectOrderedToolResultIDs(content)) > 0
}

func mergeSameRoleMessages(prev, curr map[string]interface{}) map[string]interface{} {
	merged := deepCopyMsg(prev)
	combined := append(msgContentSlice(merged), msgContentSlice(curr)...)
	merged["content"] = toInterfaceSlice(combined)
	return merged
}

// fixConsecutiveRoles repairs same-role adjacencies created by non-contiguous
// pinned-frame selection. It prefers preserving immediate tool_use/tool_result
// boundaries over keeping the older adjacent message.
func fixConsecutiveRoles(msgs []interface{}) ([]interface{}, int) {
	if len(msgs) < 2 {
		return msgs, 0
	}
	result := make([]interface{}, 0, len(msgs))
	result = append(result, msgs[0])
	dropped := 0
	for i := 1; i < len(msgs); i++ {
		prev, okP := result[len(result)-1].(map[string]interface{})
		curr, okC := msgs[i].(map[string]interface{})
		if !okP || !okC {
			result = append(result, msgs[i])
			continue
		}
		prevRole, _ := prev["role"].(string)
		currRole, _ := curr["role"].(string)
		if prevRole != "" && prevRole == currRole {
			switch currRole {
			case "assistant":
				// If either assistant carries a tool_use, keep the later one so any
				// following user tool_result still has a matching immediate parent.
				if messageHasToolUse(prev) || messageHasToolUse(curr) {
					result[len(result)-1] = curr
					dropped++
					log.Printf("[LOCALCACHE] fixConsecutiveRoles: dropped earlier assistant at output position %d to preserve tool boundary", i-1)
					continue
				}
				result[len(result)-1] = mergeSameRoleMessages(prev, curr)
				dropped++
				log.Printf("[LOCALCACHE] fixConsecutiveRoles: merged consecutive assistant at output position %d", i)
				continue
			case "user":
				prevHasResult := messageHasToolResult(prev)
				currHasResult := messageHasToolResult(curr)
				switch {
				case prevHasResult && currHasResult:
					result[len(result)-1] = mergeSameRoleMessages(prev, curr)
					dropped++
					log.Printf("[LOCALCACHE] fixConsecutiveRoles: merged consecutive user tool_results at output position %d", i)
					continue
				case prevHasResult && !currHasResult:
					result[len(result)-1] = mergeSameRoleMessages(prev, curr)
					dropped++
					log.Printf("[LOCALCACHE] fixConsecutiveRoles: merged trailing user text into tool_result at output position %d", i)
					continue
				case !prevHasResult && currHasResult:
					result[len(result)-1] = curr
					dropped++
					log.Printf("[LOCALCACHE] fixConsecutiveRoles: dropped earlier user at output position %d to preserve tool_result boundary", i-1)
					continue
				case !prevHasResult && !currHasResult:
					result[len(result)-1] = mergeSameRoleMessages(prev, curr)
					dropped++
					log.Printf("[LOCALCACHE] fixConsecutiveRoles: merged consecutive user at output position %d", i)
					continue
				}
			}
			dropped++
			log.Printf("[LOCALCACHE] fixConsecutiveRoles: dropped consecutive %s at output position %d", currRole, i)
			continue
		}
		result = append(result, msgs[i])
	}
	return result, dropped
}

// RepairBrokenToolBoundaries is the public wrapper for post-eviction repair.
// Marks orphan tool_use/tool_result messages as references so the outbound
// request sent to Anthropic never contains unpaired tool blocks.
// Also merges any consecutive same-role messages that marking may create.
func (lc *LocalCache) RepairBrokenToolBoundaries() int {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	return lc.repairBrokenToolBoundariesLocked()
}

func (lc *LocalCache) breaksAlternationIfEvictedLocked(start, end int) bool {
	prevRole, hasPrev := lc.nearestRetainedRoleBeforeLocked(start)
	nextRole, hasNext := lc.nearestRetainedRoleAfterLocked(end)
	if !hasPrev || !hasNext {
		return false
	}
	return prevRole == nextRole
}

func (lc *LocalCache) nearestRetainedRoleBeforeLocked(idx int) (string, bool) {
	for i := idx - 1; i >= 0; i-- {
		if lc.messages[i].IsReference {
			continue
		}
		if role := lc.messages[i].Role; role != "" {
			return role, true
		}
	}
	return "", false
}

func (lc *LocalCache) nearestRetainedRoleAfterLocked(idx int) (string, bool) {
	for i := idx + 1; i < len(lc.messages); i++ {
		if lc.messages[i].IsReference {
			continue
		}
		if role := lc.messages[i].Role; role != "" {
			return role, true
		}
	}
	return "", false
}

// --- Helpers ---

func (lc *LocalCache) retainedIndicesLocked() []int {
	indices := make([]int, 0, len(lc.messages))
	for i := range lc.messages {
		if lc.messages[i].IsReference {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func (lc *LocalCache) pinnedAnchorCountLocked(anchorKeep int, requireUserFinal bool) int {
	retained := lc.buildCopiesLocked(len(lc.messages))
	if len(retained) == 0 {
		return 0
	}

	// When the first retained message is a bookmark, use ONLY the bookmark
	// as the anchor. Keeping additional "anchor" messages after the bookmark
	// creates fake continuity — the agent sees bookmark → old assistant
	// response → old user message and concludes the bookmark was already
	// processed. Dropping the stale anchors makes the bookmark the active
	// context-setting message, directly followed by the hot tail.
	if lc.hasBookmarkLocked() {
		return 1
	}

	insertAt := anchorKeep
	if insertAt > len(retained) {
		insertAt = len(retained)
	}
	return adjustReferenceInsertAt(retained, insertAt, requireUserFinal)
}

func (lc *LocalCache) pinnedTailStartPosLocked(retained []int, anchorCount, recentKeep int) int {
	if len(retained) <= anchorCount || anchorCount <= 0 {
		return len(retained)
	}
	if recentKeep <= 0 || recentKeep > postOverflowHotTailKeep {
		recentKeep = postOverflowHotTailKeep
	}

	// Last message in the stable anchor determines the required starting role for the tail.
	anchorRole := lc.messages[retained[anchorCount-1]].Role

	// Start searching for a safe boundary from the preferred tail size.
	pos := len(retained) - recentKeep
	if pos < anchorCount {
		pos = anchorCount
	}

	// Scan backward to find a structurally valid join point.
	// A valid join point must:
	// 1. Alternate roles from the last anchor message.
	// 2. Not start with a user tool_result (which would be orphaned from its assistant tool_use).
	for pos > anchorCount {
		msg := lc.messages[retained[pos]]

		// Rule 1: Role Alternation.
		if msg.Role == anchorRole {
			pos--
			continue
		}

		// Rule 2: No orphaned tool results.
		if msg.Role == "user" && lc.messageHasToolResultLocked(msg.Msg) {
			pos--
			continue
		}

		// Found a safe boundary.
		return pos
	}

	return pos
}

func (lc *LocalCache) isUserToolResultRetainedLocked(idx int) bool {
	if idx < 0 || idx >= len(lc.messages) {
		return false
	}
	msg := lc.messages[idx]
	return msg.Role == "user" && lc.messageHasToolResultLocked(msg.Msg)
}

func (lc *LocalCache) messageHasToolResultLocked(msg map[string]interface{}) bool {
	content, _ := msg["content"].([]interface{})
	for _, blk := range content {
		block, _ := blk.(map[string]interface{})
		if block["type"] == "tool_result" {
			return true
		}
	}
	return false
}

func (lc *LocalCache) buildCopiesLocked(limit int) []interface{} {
	if limit > len(lc.messages) {
		limit = len(lc.messages)
	}
	result := make([]interface{}, 0, limit)
	outputIdx := 0
	for i := 0; i < limit; i++ {
		if lc.messages[i].IsReference {
			continue
		}
		copied := deepCopyMsg(lc.messages[i].Msg)
		copied[requestSourceIndexKey] = outputIdx
		result = append(result, copied)
		outputIdx++
	}
	return result
}

func (lc *LocalCache) buildCopiesFromIndicesLocked(indices []int) []interface{} {
	result := make([]interface{}, 0, len(indices))
	for outputIdx, idx := range indices {
		if idx < 0 || idx >= len(lc.messages) {
			continue
		}
		if lc.messages[idx].IsReference {
			continue
		}
		copied := deepCopyMsg(lc.messages[idx].Msg)
		copied[requestSourceIndexKey] = outputIdx
		result = append(result, copied)
	}
	return result
}

func (lc *LocalCache) normalizeRequestViewLocked(msgs []interface{}, rawAnchor int, rawPrevAnchor int, stripThinking bool) requestView {
	view := requestView{Messages: msgs, AnchorIdx: rawAnchor, PrevAnchorIdx: rawPrevAnchor, ReferenceInsertAt: -1}
	if len(view.Messages) == 0 {
		view.AnchorIdx = -1
		view.PrevAnchorIdx = -1
		return view
	}

	body := map[string]interface{}{"messages": view.Messages}
	// Thinking blocks are now stripped at ingestion time (localcache.Ingest),
	// so build-time stripping is no longer needed. The cache stores clean
	// messages, making output deterministic and prefix cache stable.

	normalized, _ := body["messages"].([]interface{})

	// Fix consecutive same-role messages that the anchor/tail gap can create.
	// When BuildPinnedFrameForRequest selects non-contiguous indices, the last
	// anchor message and first tail message may share the same role. Drop the
	// offender (second of the pair) to restore strict alternation.
	normalized, dropped := fixConsecutiveRoles(normalized)
	view.OrphansFixed += dropped
	body["messages"] = normalized

	// Same-role repair can expose orphan tool_result or dangling tool_use
	// blocks at the selected-view boundary. Run the structural sanitizer on the
	// normalized slice before final validation.
	if repaired := trimmer.FixOrphanToolResults(body); repaired > 0 {
		view.OrphansFixed += repaired
	}
	normalized, _ = body["messages"].([]interface{})

	// The orphan sanitizer can drop leading messages (e.g. assistant without
	// preceding user, user with only orphan tool_results), creating NEW
	// consecutive same-role adjacencies that fixConsecutiveRoles already passed
	// over. Re-run it on the post-sanitizer result.
	normalized, postDrop := fixConsecutiveRoles(normalized)
	if postDrop > 0 {
		view.OrphansFixed += postDrop
		body["messages"] = normalized
		log.Printf("[LOCALCACHE] Post-sanitizer fixConsecutiveRoles repaired %d additional adjacencies", postDrop)
		// The second fixConsecutiveRoles pass may have exposed new orphans.
		// Run one more sanitizer pass to clean up.
		if repaired2 := trimmer.FixOrphanToolResults(body); repaired2 > 0 {
			view.OrphansFixed += repaired2
		}
		normalized, _ = body["messages"].([]interface{})
	}

	view.Messages = normalized
	view.AnchorIdx = mapNormalizedAnchor(normalized, rawAnchor)
	view.PrevAnchorIdx = mapNormalizedAnchor(normalized, rawPrevAnchor)
	if view.AnchorIdx < 0 && len(normalized) >= 2 {
		view.AnchorIdx = len(normalized) - 2
	} else if view.AnchorIdx >= len(normalized) {
		view.AnchorIdx = len(normalized) - 1
	}
	if view.PrevAnchorIdx >= len(normalized) {
		view.PrevAnchorIdx = len(normalized) - 1
	}
	if view.PrevAnchorIdx == view.AnchorIdx {
		view.PrevAnchorIdx = -1
	}
	stripRequestSourceIndexes(normalized)
	return view
}

func mapNormalizedAnchor(msgs []interface{}, rawAnchor int) int {
	if rawAnchor < 0 {
		return -1
	}
	mapped := -1
	for idx, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		src, ok := requestSourceIndex(msg[requestSourceIndexKey])
		if !ok {
			continue
		}
		if src <= rawAnchor {
			mapped = idx
		}
	}
	return mapped
}

func stripRequestSourceIndexes(msgs []interface{}) {
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		delete(msg, requestSourceIndexKey)
	}
}

func requestSourceIndex(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		i := int(n)
		return i, float64(i) == n
	default:
		return 0, false
	}
}

// PreCompressionSnapshot returns deep copies of messages in [0, watermark) that
// have not yet been compressed. The returned map is keyed by message index.
// Callers use this to preserve original content before CompressOldMessages
// mutates messages in place, so the eviction/chapter path gets untruncated text.
func (lc *LocalCache) PreCompressionSnapshot(watermark int) map[int]interface{} {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if watermark <= 0 || watermark > len(lc.messages) {
		return nil
	}

	snap := make(map[int]interface{})
	for i := 0; i < watermark; i++ {
		cm := &lc.messages[i]
		if cm.IsReference || cm.IsCompressed || cm.Msg == nil {
			continue
		}
		snap[i] = deepCopyMsg(cm.Msg)
	}

	if len(snap) == 0 {
		return nil
	}
	return snap
}

// deepCopyMsg creates a deep copy of a message map via JSON round-trip.
// This ensures CC's future mutations to the original don't affect our cached copy.
func deepCopyMsg(msg map[string]interface{}) map[string]interface{} {
	data, err := json.Marshal(msg)
	if err != nil {
		// Fallback: shallow copy (better than nothing)
		copied := make(map[string]interface{}, len(msg))
		for k, v := range msg {
			copied[k] = v
		}
		return copied
	}
	var copied map[string]interface{}
	if err := json.Unmarshal(data, &copied); err != nil {
		copied = make(map[string]interface{}, len(msg))
		for k, v := range msg {
			copied[k] = v
		}
	}
	return copied
}

// stripCacheControlFromContent removes cache_control from individual content blocks.
// CC places cache_control on the last content block of its chosen breakpoint message.
// The proxy controls breakpoint placement, so we strip CC's choices.
func stripCacheControlFromContent(msg map[string]interface{}) {
	content, ok := msg["content"].([]interface{})
	if !ok {
		return
	}
	for _, b := range content {
		block, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		delete(block, "cache_control")
	}
}

func isReferencePlaceholder(msg map[string]interface{}) bool {
	content, _ := msg["content"].(string)
	return strings.Contains(content, "moved to shadow file")
}

func formatPositionRanges(pos []int) string {
	if len(pos) == 0 {
		return ""
	}
	var b strings.Builder
	start := pos[0]
	end := pos[0]
	flush := func() {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if start == end {
			fmt.Fprintf(&b, "%d", start)
			return
		}
		fmt.Fprintf(&b, "%d-%d", start, end)
	}
	for _, p := range pos[1:] {
		if p == end+1 {
			end = p
			continue
		}
		flush()
		start = p
		end = p
	}
	flush()
	return b.String()
}

// cleanSysRemindersInMsg strips <system-reminder> blocks from a single message.
// Called once during ingestion. The cleaned version is cached permanently.
// truncateOversizedToolResults caps individual tool_result text blocks at
// maxToolResultChars. When a user message contains tool_result blocks with
// text exceeding this limit (e.g. from reading a 200KB file), the text is
// truncated with a notice. This prevents a single file read from consuming
// 50K+ tokens of context, which makes eviction useless because the bloated
// messages sit in the recent tail that eviction cannot touch.
//
// The cap is set high enough that normal tool results (grep output, short
// file reads, command output) are unaffected. Only multi-page file dumps
// are truncated.
const maxToolResultChars = 30000 // ~7500 tokens — enough for any reasonable tool output

func truncateOversizedToolResults(msg map[string]interface{}) {
	role, _ := msg["role"].(string)
	if role != "user" {
		return
	}
	blocks, ok := msg["content"].([]interface{})
	if !ok {
		return
	}
	for _, b := range blocks {
		block, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		btype, _ := block["type"].(string)
		if btype != "tool_result" {
			continue
		}
		// tool_result content can be a string or []interface{}
		switch content := block["content"].(type) {
		case string:
			if len(content) > maxToolResultChars {
				block["content"] = content[:maxToolResultChars] +
					"\n\n[TRUNCATED — original " + fmt.Sprintf("%d", len(content)) +
					" chars. Use offset/limit to read specific sections.]"
				log.Printf("[LOCALCACHE] truncated oversized tool_result: %d -> %d chars",
					len(content), maxToolResultChars)
			}
		case []interface{}:
			for _, inner := range content {
				innerBlock, ok := inner.(map[string]interface{})
				if !ok {
					continue
				}
				if innerBlock["type"] != "text" {
					continue
				}
				text, ok := innerBlock["text"].(string)
				if !ok || len(text) <= maxToolResultChars {
					continue
				}
				innerBlock["text"] = text[:maxToolResultChars] +
					"\n\n[TRUNCATED — original " + fmt.Sprintf("%d", len(text)) +
					" chars. Use offset/limit to read specific sections.]"
				log.Printf("[LOCALCACHE] truncated oversized tool_result text block: %d -> %d chars",
					len(text), maxToolResultChars)
			}
		}
	}
}

func cleanSysRemindersInMsg(msg map[string]interface{}) {
	role, _ := msg["role"].(string)
	if role != "user" {
		return
	}

	// Handle string content
	if text, ok := msg["content"].(string); ok {
		if strings.Contains(text, "<system-reminder>") {
			cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(text, ""))
			if cleaned == "" {
				cleaned = "."
			}
			msg["content"] = cleaned
		}
		return
	}

	// Handle array content
	blocks, ok := msg["content"].([]interface{})
	if !ok {
		return
	}
	for j, b := range blocks {
		block, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		btype, _ := block["type"].(string)

		if btype == "text" {
			text, ok := block["text"].(string)
			if !ok {
				continue
			}
			if strings.Contains(text, "<system-reminder>") {
				cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(text, ""))
				if cleaned == "" {
					cleaned = "."
				}
				block["text"] = cleaned
				blocks[j] = block
			}
		}

		// CC injects system-reminders into tool_result content too
		if btype == "tool_result" {
			if trContent, ok := block["content"].(string); ok {
				if strings.Contains(trContent, "<system-reminder>") {
					cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(trContent, ""))
					if cleaned == "" {
						cleaned = "."
					}
					block["content"] = cleaned
					blocks[j] = block
				}
			}
		}
	}
}

// placeBreakpoint sets cache_control at the stable anchor position from the
// LocalCache. This is the end of the "frozen prefix" — everything before it
// should be cached by Anthropic.
//
// STRATEGY: Use the anchor index from LocalCache. The anchor only moves when
// enough new messages accumulate (breakpointAdvanceThreshold). This means the
// breakpoint stays on the SAME message for many turns, keeping the Anthropic
// prefix cache alive.
//
// SAFETY: First strips ALL existing cache_control from ALL messages and content
// blocks, then places exactly one at the anchor. This prevents accumulation
// across calls (which previously caused API 400 "maximum of 4 blocks" errors).
//
// The total cache_control count (system + tools + messages) is clamped to ≤4.
// System prompt typically uses 2, so messages get at most 2.
//
// Breakpoint placement strategy:
//
//	BP1, BP2: system[-1] and tools[-1] (placed by CC, normalized by Glass)
//	BP3: messages[anchor] — the moving cache breakpoint (stable between advances)
//	BP4: messages[watermark] — the deep frozen zone boundary (stable between
//	      compression batches). Provides layered 20-block lookback coverage.
//	      Falls back to prev_anchor or tip when watermark is not useful.
func placeBreakpoint(body map[string]interface{}, anchorIdx, prevAnchorIdx, watermarkIdx int) {
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) < 2 {
		return
	}

	// Phase 1: Strip ALL existing cache_control from ALL messages and content blocks.
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		delete(msg, "cache_control")
		if content, ok := msg["content"].([]interface{}); ok {
			for _, b := range content {
				if block, ok := b.(map[string]interface{}); ok {
					delete(block, "cache_control")
				}
			}
		}
	}

	// Phase 2: Count cache_control blocks already in system prompt and tools.
	normalizeCacheControlTTL(body, ExtendedCacheTTL)
	sysCC := countCacheControl(body["system"])
	toolsCC := countCacheControl(body["tools"])
	existingCC := sysCC + toolsCC

	available := 4 - existingCC
	if available <= 0 {
		log.Printf("[BREAKPOINT] No slots available for message breakpoint: system=%d tools=%d total=%d (max 4)", sysCC, toolsCC, existingCC)
		return
	}

	// Phase 3: Place BP3 at the stable anchor position.
	idx := anchorIdx
	if idx < 0 || idx >= len(msgs) {
		idx = len(msgs) - 2
	}
	if idx < 0 {
		return
	}
	placeCacheControlOnMsg(msgs, idx)
	available--

	if available <= 0 {
		return
	}

	// Phase 4: Place BP4 at the deep frozen zone (watermark boundary).
	// The watermark is the boundary between compressed-and-frozen messages and
	// active messages. Between compression batches, bytes below the watermark
	// are idempotent — this breakpoint provides a deep stable cache hit.
	// Also provides 20-block lookback coverage for the lower prefix.
	//
	// Requirements:
	//   - watermarkIdx must be valid and far enough from the anchor to be useful
	//     (if they're within 3 messages, no point having two breakpoints there)
	//   - watermarkIdx must be > 0 (position 0 would cache nothing new beyond system/tools)
	//   - watermarkIdx must differ from anchorIdx
	wmUseful := watermarkIdx > 0 && watermarkIdx < len(msgs) && watermarkIdx != idx &&
		(idx-watermarkIdx) > 3
	if wmUseful {
		placeCacheControlOnMsg(msgs, watermarkIdx)
		return
	}

	// Fallback: prev_anchor if available, else hot tip.
	if prevAnchorIdx >= 0 && prevAnchorIdx < len(msgs) && prevAnchorIdx != idx {
		placeCacheControlOnMsg(msgs, prevAnchorIdx)
		return
	}
	if len(msgs)-2 > idx+2 {
		placeCacheControlOnMsg(msgs, len(msgs)-2)
	}
}

// placeCacheControlOnMsg places an extended-TTL cache_control marker on a message.
func placeCacheControlOnMsg(msgs []interface{}, idx int) {
	if idx < 0 || idx >= len(msgs) {
		return
	}
	msg, ok := msgs[idx].(map[string]interface{})
	if !ok {
		return
	}
	content, ok := msg["content"].([]interface{})
	if !ok || len(content) == 0 {
		msg["cache_control"] = extendedCacheControl()
	} else {
		if block, ok := content[len(content)-1].(map[string]interface{}); ok {
			block["cache_control"] = extendedCacheControl()
		}
	}
}

// ── Persistence ──────────────────────────────────────────────────────────────
//
// LocalCache is persisted to disk after every Ingest() that adds new messages
// and after every Evict(). On proxy restart, getCache() loads from disk before
// creating an empty cache. This eliminates cold starts — the proxy resumes
// serving the same byte-identical prefix it served before restart.
//
// Persistence file: {shadowDir}/{convID}/localcache.json
// Written atomically: temp file → rename.

// localCacheSnapshot is the on-disk representation of LocalCache.
type localCacheSnapshot struct {
	ConvID               string              `json:"conv_id"`
	Messages             []cachedMsgSnapshot `json:"messages"`
	BreakpointAnchor     int                 `json:"breakpoint_anchor"`
	PrevBreakpointAnchor int                 `json:"prev_breakpoint_anchor"`
	PrevBreakpointValid  bool                `json:"prev_breakpoint_valid"`
	MsgsAtLastAnchor     int                 `json:"msgs_at_last_anchor"`
	FreezePoint          int                 `json:"freeze_point"`
	FreezePointLocked    bool                `json:"freeze_point_locked"`
	LastPrefixHash       string              `json:"last_prefix_hash"`
	SavedAt              time.Time           `json:"saved_at"`
}

// cachedMsgSnapshot is the serializable form of CachedMsg.
type cachedMsgSnapshot struct {
	Msg          map[string]interface{} `json:"msg"`
	Hash         string                 `json:"hash"`
	Role         string                 `json:"role"`
	IsReference  bool                   `json:"is_reference"`
	IsCompressed bool                   `json:"is_compressed,omitempty"`
	Tokens       int                    `json:"tokens"`
	OrigTokens   int                    `json:"orig_tokens,omitempty"`
}

// Save writes the LocalCache to disk atomically.
// Called after every Ingest() that adds messages and after every Evict().
func (lc *LocalCache) Save(shadowDir string) error {
	if shadowDir == "" {
		return nil
	}
	lc.mu.Lock()
	snap := localCacheSnapshot{
		ConvID:               lc.convID,
		BreakpointAnchor:     lc.breakpointAnchor,
		PrevBreakpointAnchor: lc.prevBreakpointAnchor,
		PrevBreakpointValid:  lc.prevBreakpointAnchor >= 0,
		MsgsAtLastAnchor:     lc.msgsAtLastAnchor,
		FreezePoint:          lc.freezePoint,
		FreezePointLocked:    lc.freezePointLocked,
		LastPrefixHash:       lc.lastPrefixHash,
		SavedAt:              time.Now(),
		Messages:             make([]cachedMsgSnapshot, len(lc.messages)),
	}
	for i, cm := range lc.messages {
		snap.Messages[i] = cachedMsgSnapshot{
			Msg:          cm.Msg,
			Hash:         cm.Hash,
			Role:         cm.Role,
			IsReference:  cm.IsReference,
			IsCompressed: cm.IsCompressed,
			Tokens:       cm.Tokens,
			OrigTokens:   cm.OrigTokens,
		}
	}
	lc.mu.Unlock()

	dir := filepath.Join(shadowDir, lc.convID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("localcache save mkdir: %w", err)
	}

	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("localcache marshal: %w", err)
	}

	// Atomic write: temp → rename
	dst := filepath.Join(dir, "localcache.json")
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("localcache write tmp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("localcache rename: %w", err)
	}
	return nil
}

// LoadLocalCache attempts to load a persisted LocalCache from disk.
// Returns (cache, true) on success, (nil, false) if not found or corrupt.
func LoadLocalCache(convID, shadowDir string) (*LocalCache, bool) {
	if shadowDir == "" || convID == "" {
		return nil, false
	}
	path := filepath.Join(shadowDir, convID, "localcache.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false // not found — normal for new conversations
	}

	var snap localCacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		log.Printf("[LOCALCACHE] corrupt snapshot for %s: %v — starting fresh", convID, err)
		return nil, false
	}

	prevAnchor := -1
	if snap.PrevBreakpointValid {
		prevAnchor = snap.PrevBreakpointAnchor
	}

	lc := &LocalCache{
		convID:               snap.ConvID,
		breakpointAnchor:     snap.BreakpointAnchor,
		prevBreakpointAnchor: prevAnchor,
		msgsAtLastAnchor:     snap.MsgsAtLastAnchor,
		freezePoint:          snap.FreezePoint,
		freezePointLocked:    snap.FreezePointLocked,
		lastPrefixHash:       snap.LastPrefixHash,
		loadedFromDisk:       true,
		messages:             make([]CachedMsg, len(snap.Messages)),
	}
	for i, s := range snap.Messages {
		lc.messages[i] = CachedMsg{
			Msg:          s.Msg,
			Hash:         s.Hash,
			Role:         s.Role,
			IsReference:  s.IsReference,
			IsCompressed: s.IsCompressed,
			Tokens:       s.Tokens,
			OrigTokens:   s.OrigTokens,
		}
	}

	nonRef := 0
	for _, cm := range lc.messages {
		if !cm.IsReference {
			nonRef++
		}
	}
	log.Printf("[LOCALCACHE] Loaded from disk: conv=%s msgs=%d (retained=%d ref=%d) anchor=%d saved=%s",
		convID, len(lc.messages), nonRef, len(lc.messages)-nonRef,
		lc.breakpointAnchor, snap.SavedAt.Format("15:04:05"))

	lc.mu.Lock()
	repaired := lc.repairBrokenToolBoundariesLocked()
	lc.mu.Unlock()
	if repaired > 0 {
		log.Printf("[LOCALCACHE] Repaired %d legacy orphaned tool-boundary messages for conv=%s", repaired, convID)
		if err := lc.Save(shadowDir); err != nil {
			log.Printf("[LOCALCACHE] save after repair failed for %s: %v", convID, err)
		}
	}

	return lc, true
}

// countCacheControl counts cache_control blocks in a JSON value (system or tools array).
func countCacheControl(v interface{}) int {
	if v == nil {
		return 0
	}
	arr, ok := v.([]interface{})
	if !ok {
		return 0
	}
	count := 0
	for _, item := range arr {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := m["cache_control"]; has {
			count++
		}
		// Check nested content blocks
		if content, ok := m["content"].([]interface{}); ok {
			for _, b := range content {
				if block, ok := b.(map[string]interface{}); ok {
					if _, has := block["cache_control"]; has {
						count++
					}
				}
			}
		}
	}
	return count
}
