package glass

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"proxy.local/app/internal/subagent"
	"proxy.local/app/internal/sysprompt"
	"proxy.local/app/internal/trimmer"
)

// Engine is the main Glass orchestrator.
// Thread-safe: each method locks per-conversation state.
type Engine struct {
	sessions         *SessionStore
	shadow           *ShadowWriter
	sysproc          *SyspromptProcessor
	cfg              GlassConfig
	startedAt        time.Time
	mu               sync.Mutex
	diagMu           sync.Mutex
	caches           map[string]*LocalCache // conv_id -> local message cache
	outboundPrefixes map[string]*outboundPrefixSnapshot
	seenClientPIDs   sync.Map
	pidSessionPins   sync.Map

	// coldGates serializes the first request for a new cache state.
	// When multiple requests arrive simultaneously for the same conv_id
	// (e.g., opus subagents sharing parent PID), the first one goes through
	// to Anthropic and establishes the cached prefix. The rest wait on the
	// gate and then hit 100% cache read instead of 3x cache creation.
	coldGates     map[string]*coldGate
	prefixWarmer  *PrefixWarmer
	chapterWriter *ChapterWriter
	summarizer    *RollingSummarizer
}

const (
	lastAPIInputFreshWindow               = 10 * time.Minute
	postEvictionTargetHeadroomTokens      = 5000
	maxPostEvictionObservedOverheadTokens = 40000
)

// coldGate serializes cold-start requests for a conversation.
// The first request goes through immediately. Concurrent requests block
// until the first one completes (signaled by Release).
type coldGate struct {
	mu       sync.Mutex
	warming  bool          // true while first request is in-flight
	warmCh   chan struct{} // closed when the first request completes
	lastWarm time.Time     // when the gate last opened
}

// NewEngine creates a Glass engine with the given config.
// pipeline may be nil (no system prompt modification).
func NewEngine(cfg GlassConfig, pipeline ...*sysprompt.Pipeline) *Engine {
	var pipe *sysprompt.Pipeline
	if len(pipeline) > 0 && pipeline[0] != nil {
		pipe = pipeline[0]
	} else {
		pipe = sysprompt.NewPipeline(false)
	}

	return &Engine{
		sessions:         NewSessionStore(cfg.ShadowDir),
		shadow:           NewShadowWriter(cfg.ShadowDir),
		sysproc:          NewSyspromptProcessor(pipe, cfg.ShadowDir),
		cfg:              cfg,
		startedAt:        time.Now(),
		caches:           make(map[string]*LocalCache),
		outboundPrefixes: make(map[string]*outboundPrefixSnapshot),
		coldGates:        make(map[string]*coldGate),
		chapterWriter:    NewChapterWriter(cfg.ShadowDir),
	}
}

// getCache returns the LocalCache for a conversation.
// On first access, tries to load from disk (eliminates cold starts after restart).
// Falls back to a new empty cache if not found.
func (e *Engine) getCache(convID string) *LocalCache {
	e.mu.Lock()
	defer e.mu.Unlock()

	if c, ok := e.caches[convID]; ok {
		return c
	}

	// Try loading persisted cache from disk before creating empty one.
	if c, ok := LoadLocalCache(convID, e.cfg.ShadowDir); ok {
		e.caches[convID] = c
		return c
	}

	c := NewLocalCache(convID)
	e.caches[convID] = c
	return c
}

func (e *Engine) replaceCache(convID string, cache *LocalCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.caches[convID] = cache
}

func (e *Engine) hasConversationArtifacts(convID string) bool {
	if convID == "" {
		return false
	}

	e.mu.Lock()
	if _, ok := e.caches[convID]; ok {
		e.mu.Unlock()
		return true
	}
	e.mu.Unlock()

	for _, name := range []string{"localcache.json", "state.json", "shadow.md", "outbound_prefix_latest.json"} {
		if _, err := os.Stat(filepath.Join(e.cfg.ShadowDir, convID, name)); err == nil {
			return true
		}
	}
	return false
}

func (e *Engine) resolvePersistedSessionKey(sessionKey string, body map[string]interface{}, meta RequestMeta, classification subagent.Classification) string {
	if sessionKey == "" || meta.ClientPID <= 0 {
		return sessionKey
	}
	if !classification.IsolateSession {
		if pinned, ok := e.pidSessionPins.Load(meta.ClientPID); ok {
			if pinnedKey, ok := pinned.(string); ok && pinnedKey != "" {
				return pinnedKey
			}
		}
	}

	if _, seen := e.seenClientPIDs.LoadOrStore(meta.ClientPID, struct{}{}); seen {
		return sessionKey
	}

	candidate := e.findPreRestartPIDSession(meta.ClientPID, classification, sessionKey, body)
	if !classification.IsolateSession {
		if candidate != "" {
			e.pidSessionPins.Store(meta.ClientPID, candidate)
		} else {
			e.pidSessionPins.Store(meta.ClientPID, sessionKey)
		}
	}
	if candidate == "" {
		return sessionKey
	}

	log.Printf("[GLASS] Reusing pre-restart persisted session lane pid=%d requested=%s adopted=%s",
		meta.ClientPID, sessionKey, candidate)
	return candidate
}

type persistedSessionCandidate struct {
	name         string
	evictedCount int
	refCount     int
	updatedAt    time.Time
	systemHash   string
	toolsHash    string
}

func (e *Engine) findPreRestartPIDSession(pid int, classification subagent.Classification, requested string, body map[string]interface{}) string {
	entries, err := os.ReadDir(e.cfg.ShadowDir)
	if err != nil {
		return ""
	}

	suffix := fmt.Sprintf("_%d", pid)
	if classification.IsolateSession && classification.SessionSuffix != "" {
		suffix += classification.SessionSuffix
	}

	var matches []persistedSessionCandidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == requested || !strings.HasSuffix(name, suffix) {
			continue
		}
		if !e.hasConversationArtifacts(name) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidate := persistedSessionCandidate{
			name:      name,
			updatedAt: info.ModTime(),
		}
		if state, ok := e.loadPersistedSessionState(name); ok {
			candidate.evictedCount = state.EvictedCount
			if !state.UpdatedAt.IsZero() {
				candidate.updatedAt = state.UpdatedAt
			}
		}
		if refCount, ok := e.loadPersistedLocalCacheRefCount(name); ok {
			candidate.refCount = refCount
		}
		if candidate.updatedAt.After(e.startedAt) {
			continue
		}
		// Ref-bearing/shadow-backed snapshots are intentionally reset on restart,
		// so adopting them into a new live lane only creates remap noise and a
		// guaranteed cold start.
		if candidate.evictedCount > 0 || candidate.refCount > 0 {
			continue
		}
		if diag, ok := e.loadPersistedPrefixDiagnostic(name); ok {
			candidate.systemHash = diag.Snapshot.SystemHash
			candidate.toolsHash = diag.Snapshot.ToolsHash
		}
		matches = append(matches, candidate)
	}

	if len(matches) == 0 {
		return ""
	}

	targetSystem := hashJSON(body["system"])
	targetTools := hashJSON(body["tools"])
	bestName := ""
	bestScore := -1
	bestUpdated := time.Time{}
	bestEvicted := int(^uint(0) >> 1)

	scoreCandidate := func(c persistedSessionCandidate) int {
		score := 0
		if targetSystem != "" && c.systemHash != "" && c.systemHash == targetSystem {
			score += 4
		}
		if targetTools != "" && c.toolsHash != "" && c.toolsHash == targetTools {
			score += 4
		}
		if c.evictedCount == 0 {
			score += 2
		}
		if c.name == requested {
			score += 1
		}
		return score
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].name < matches[j].name
	})
	for _, candidate := range matches {
		score := scoreCandidate(candidate)
		if score > bestScore ||
			(score == bestScore && candidate.evictedCount < bestEvicted) ||
			(score == bestScore && candidate.evictedCount == bestEvicted && candidate.updatedAt.After(bestUpdated)) {
			bestName = candidate.name
			bestScore = score
			bestEvicted = candidate.evictedCount
			bestUpdated = candidate.updatedAt
		}
	}

	if bestScore <= 0 {
		if len(matches) == 1 {
			return matches[0].name
		}
		return requested
	}
	return bestName
}

func (e *Engine) loadPersistedSessionState(convID string) (*SessionState, bool) {
	path := filepath.Join(e.cfg.ShadowDir, convID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var st SessionState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, false
	}
	return &st, true
}

func (e *Engine) loadPersistedLocalCacheRefCount(convID string) (int, bool) {
	path := filepath.Join(e.cfg.ShadowDir, convID, "localcache.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var snap localCacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return 0, false
	}
	refs := 0
	for _, msg := range snap.Messages {
		if msg.IsReference {
			refs++
		}
	}
	return refs, true
}

func (e *Engine) loadPersistedPrefixDiagnostic(convID string) (outboundPrefixDiagnosticEvent, bool) {
	path, _ := outboundPrefixDiagnosticPaths(e.cfg.ShadowDir, convID)
	data, err := os.ReadFile(path)
	if err != nil {
		return outboundPrefixDiagnosticEvent{}, false
	}
	var diag outboundPrefixDiagnosticEvent
	if err := json.Unmarshal(data, &diag); err != nil {
		return outboundPrefixDiagnosticEvent{}, false
	}
	return diag, true
}

func (e *Engine) clearConversationDiagnostics(convID string) {
	var gate *coldGate

	e.mu.Lock()
	delete(e.outboundPrefixes, convID)
	if existing, ok := e.coldGates[convID]; ok {
		gate = existing
	}
	e.mu.Unlock()

	if gate != nil {
		gate.mu.Lock()
		gate.lastWarm = time.Time{}
		gate.mu.Unlock()
	}

	e.clearPersistedPrefixDiagnostics(convID)
}

func (e *Engine) resetConversationState(convID string, state *SessionState, reason string) *LocalCache {
	log.Printf("[GLASS] conv=%s resetting persisted cache/state: %s", convID, reason)
	e.mu.Lock()
	existingCache := e.caches[convID]
	e.mu.Unlock()
	if e.chapterWriter != nil && state != nil {
		if err := e.chapterWriter.Seal(convID, state, existingCache, reason); err != nil {
			log.Printf("[GLASS] chapter seal error: %v", err)
		}
	}
	if state != nil {
		state.ResetForFreshCache()
		if err := e.sessions.Save(state); err != nil {
			log.Printf("[GLASS] reset state save error: %v", err)
		}
	}
	fresh := NewLocalCache(convID)
	e.replaceCache(convID, fresh)
	if err := fresh.Save(e.cfg.ShadowDir); err != nil {
		log.Printf("[GLASS] reset localcache save error: %v", err)
	}
	e.clearConversationDiagnostics(convID)
	return fresh
}

func (e *Engine) ensureCacheStateCompatible(convID string, cache *LocalCache, state *SessionState) *LocalCache {
	if cache == nil || state == nil {
		return cache
	}
	refCount := cache.ReferenceCount()
	evictionRefCount := cache.EvictionReferenceCount()
	if cache.loadedFromDisk && evictionRefCount > 0 {
		return e.resetConversationState(convID, state,
			fmt.Sprintf("persisted evicted cache snapshot is unsafe to resume (refs=%d eviction_refs=%d)", refCount, evictionRefCount))
	}
	if (evictionRefCount == 0 && state.EvictedCount > 0) || (evictionRefCount > 0 && state.EvictedCount == 0) {
		return e.resetConversationState(convID, state,
			fmt.Sprintf("stale cache/state mismatch (refs=%d eviction_refs=%d evicted=%d)", refCount, evictionRefCount, state.EvictedCount))
	}
	// Structural validation: build a test view and check for message structure
	// violations. A lane with valid refCount/evictedCount but corrupted message
	// ordering (consecutive same-role, orphan tool results) will crash the
	// repair pipeline on every request, creating an infinite loop.
	if cache.loadedFromDisk && cache.Len() > 0 {
		testView := cache.BuildForRequest(e.cfg.StripThinking)
		if issues := validateMessageStructure(testView.Messages); len(issues) > 0 {
			return e.resetConversationState(convID, state,
				fmt.Sprintf("persisted cache has structural violations: %s", summarizeValidationIssues(issues)))
		}
	}
	return cache
}

// getColdGate returns the cold gate for a conversation, creating one if needed.
func (e *Engine) getColdGate(convID string) *coldGate {
	e.mu.Lock()
	defer e.mu.Unlock()

	if g, ok := e.coldGates[convID]; ok {
		return g
	}
	g := &coldGate{}
	e.coldGates[convID] = g
	return g
}

// acquireColdGate blocks until this request may proceed.
// Returns true if this request is the "warmer" (first through the gate).
// The warmer MUST call releaseColdGate when its response arrives.
// Non-warmers are released automatically when the warmer finishes.
func (e *Engine) acquireColdGate(convID string) bool {
	gate := e.getColdGate(convID)

	gate.mu.Lock()

	// If the gate was warmed recently (within 5 min), skip gating — prefix is warm.
	if !gate.lastWarm.IsZero() && time.Since(gate.lastWarm) < 5*time.Minute && !gate.warming {
		gate.mu.Unlock()
		return false
	}

	if !gate.warming {
		// First request — become the warmer
		gate.warming = true
		gate.warmCh = make(chan struct{})
		// Set a safety timeout: if ReleaseColdGate is never called (e.g., in tests
		// or if the proxy path fails silently), auto-release after 10 seconds.
		go func() {
			time.Sleep(10 * time.Second)
			gate.mu.Lock()
			if gate.warming {
				gate.warming = false
				gate.lastWarm = time.Now()
				close(gate.warmCh)
				log.Printf("[COLD-GATE] conv=%s AUTO-RELEASE after 10s safety timeout", convID)
			}
			gate.mu.Unlock()
		}()
		gate.mu.Unlock()
		log.Printf("[COLD-GATE] conv=%s WARMING — first request goes through", convID)
		return true
	}

	// Another request while warming — wait for the warmer to finish
	ch := gate.warmCh
	gate.mu.Unlock()

	log.Printf("[COLD-GATE] conv=%s WAITING — blocked behind warmer", convID)
	select {
	case <-ch:
		log.Printf("[COLD-GATE] conv=%s RELEASED — warmer finished, proceeding", convID)
	case <-time.After(30 * time.Second):
		log.Printf("[COLD-GATE] conv=%s TIMEOUT — proceeding anyway after 30s", convID)
	}
	return false
}

// releaseColdGate signals that the warming request completed.
// All blocked requests are released.
func (e *Engine) releaseColdGate(convID string) {
	gate := e.getColdGate(convID)

	gate.mu.Lock()
	defer gate.mu.Unlock()

	if gate.warming {
		gate.warming = false
		gate.lastWarm = time.Now()
		close(gate.warmCh) // release all waiters
		log.Printf("[COLD-GATE] conv=%s WARMED — gate open", convID)
	}
}

// Process runs the full Glass pipeline on a request body.
//
// LOCAL CACHE ARCHITECTURE:
//
//	The proxy OWNS the conversation state. CC's messages are ingested into
//	a per-session local cache on first encounter. On subsequent calls, CC's
//	re-sent messages are ignored — the cache serves its own stored copy.
//	This guarantees byte-identical serialization between calls.
//
// COLD-START SERIALIZATION:
//
//	When the cache state changes (new messages ingested or first call),
//	concurrent requests are gated: only the first goes to Anthropic to
//	establish the cached prefix. The rest wait and then hit 100% cache read.
//	This prevents the 3x simultaneous cache_creation spike from parallel
//	subagent spawns.
//
// Pipeline order:
//  0. Shared request classification + session identity
//  1. System prompt: canonical cache + same-length replacement + sysreminder strip
//     1b. Subagent isolation: apply suffix to the session key for isolated subagents
//  2. Ingest new messages from CC into local cache (sysreminder + cache_control stripped on ingest)
//  3. Check token budget -> evict from cache if needed -> write shadow
//  4. Serve cached messages (frozen prefix)
//  5. Inject reference message pair (if evictions exist)
//  6. Strip thinking blocks consistently from assistant messages
//  7. Place cache_control breakpoint (stable anchor from LocalCache metadata)
//  8. Validate final message structure; fail open to original CC messages if invalid
//
// Returns estimated tokens saved. Sets body["_cold_warmer"] = true if this
// request is the cold-gate warmer (caller must call ReleaseColdGate after response).
// ProcessResult holds stats from a single Glass pipeline run.
type ProcessResult struct {
	TokensSaved          int
	TokenEstimateBefore  int
	TokenEstimateAfter   int
	NewMessagesAdded     int
	HasToolResults       bool // True if incoming messages contain tool_result blocks
	EvictedCount         int
	StrippedCount        int
	OrphansFixed         int
	ShadowBatch          int
	EvictionReason       string
	ShadowPath           string
	SessionKey           string
	RequestKey           string
	AffinityKey          string
	PrefixKey            string
	ColdWarmerSessionKey string
	InvalidRequestError  string
	RecoveryGateBlocked  bool   // True if agent must read recovery file before proceeding
	RecoveryFilePath     string // Path to recovery file agent must read
	Subagent             subagent.Classification
	PrefixChangeKind     string
	PrefixDivergence     string
	PrefixSystemChanged  bool
	PrefixToolsChanged   bool
	PrefixAnchor         int
	PrefixPrevAnchor     int
	PrefixMeasuredMsgs   int
	PrefixTailChangeKind string
	PrefixTailDivergence string
	PrefixTailAnchor     int
	PrefixTailPrevAnchor int
	PrefixTailMeasured   int
	PrefixTailTokens     int
	PrefixTailHash       string
	CompressionWatermark int    // Watermark position at time of this request
	PrefixHash           string // SHA-256 hex of frozen prefix
	ContextCacheMode     string // Effective cache mode for this request ("full", "off", "context_api")
}

type evictionBudget struct {
	TotalTokens       int
	Threshold         int
	UsedRealAPIInput  bool
	UsedStaggerMargin bool
	ObservedOverhead  int
}

func shouldUseRealAPITokenBudget(state *SessionState) bool {
	if state == nil || state.LastAPIInput <= 0 {
		return false
	}
	// Real API token counts are always more accurate than the json.Marshal/4
	// estimate (which overestimates by ~1.8x). No freshness window needed —
	// a stale real count is still better than an inflated estimate.
	return true
}

func computeEvictionBudget(estimatedTokens int, state *SessionState, triggerTokens int) evictionBudget {
	budget := evictionBudget{
		TotalTokens: estimatedTokens,
		Threshold:   triggerTokens,
	}
	if !shouldUseRealAPITokenBudget(state) {
		return budget
	}
	// Always prefer real API token counts when fresh. The crude JSON/4 estimate
	// overestimates by ~1.8x, causing premature eviction at 28% of actual window.
	// Using real Anthropic token counts fixes this immediately.
	budget.TotalTokens = state.LastAPIInput
	budget.UsedRealAPIInput = true

	// Track overhead for diagnostics (real tokens vs estimate)
	if state.LastAPIInput > estimatedTokens {
		budget.ObservedOverhead = state.LastAPIInput - estimatedTokens
		if budget.ObservedOverhead > maxPostEvictionObservedOverheadTokens {
			budget.ObservedOverhead = maxPostEvictionObservedOverheadTokens
		}
	}
	if state.LastAPIInput > triggerTokens-10000 {
		budget.Threshold = triggerTokens - 10000
		budget.UsedStaggerMargin = true
	}
	return budget
}

func computeTargetMsgTokens(cfg GlassConfig, state *SessionState, sysTokens, toolTokens int, budget evictionBudget) int {
	targetMsgTokens := cfg.EvictTargetTokens - sysTokens - toolTokens
	if targetMsgTokens <= 0 {
		return 0
	}
	if state == nil || state.EvictedCount == 0 {
		return targetMsgTokens
	}

	// Once a session is already in shadow-backed mode, use the most recent real
	// API sample to compensate for hidden overhead that LocalCache.TotalTokens()
	// does not see. This makes the next cut decisively deeper instead of
	// stair-stepping through repeated 2-message batches.
	headroom := postEvictionTargetHeadroomTokens + budget.ObservedOverhead
	if headroom >= targetMsgTokens {
		return 0
	}
	return targetMsgTokens - headroom
}

func describeEvictionBudget(budget evictionBudget, nominalTrigger int) string {
	if !budget.UsedRealAPIInput && !budget.UsedStaggerMargin {
		return ""
	}
	mode := func() string {
		if budget.UsedRealAPIInput && budget.UsedStaggerMargin {
			return "real_api_input + stagger_margin"
		}
		if budget.UsedRealAPIInput {
			return "real_api_input"
		}
		return "stagger_margin"
	}()
	desc := " (effective budget: " + mode +
		", nominal_trigger=" + fmt.Sprintf("%d", nominalTrigger) +
		", effective_trigger=" + fmt.Sprintf("%d", budget.Threshold)
	if budget.ObservedOverhead > 0 {
		desc += ", observed_overhead=" + fmt.Sprintf("%d", budget.ObservedOverhead)
	}
	return desc + ")"
}

func (e *Engine) Process(body map[string]interface{}, metas ...RequestMeta) ProcessResult {
	var result ProcessResult
	var meta RequestMeta
	if len(metas) > 0 {
		meta = metas[0]
	}

	classification := meta.Subagent
	if classification == (subagent.Classification{}) {
		classification = subagent.Classify(body)
	}
	result.Subagent = classification
	result.ContextCacheMode = e.cfg.CacheMode()

	// Capture auth for rolling summarizer (first request only, like keepalive)
	if e.summarizer != nil && meta.APIKey != "" {
		e.summarizer.CaptureAuth(meta)
	}

	// Step 0: Identify session using the shared classification.
	sessionKey := meta.SessionKey
	if sessionKey == "" {
		sessionKey = trimmer.SessionFingerprint(body, meta.ClientPID)
		sessionKey = subagent.ApplySessionSuffix(sessionKey, classification)
	}
	sessionKey = e.resolvePersistedSessionKey(sessionKey, body, meta, classification)
	if sessionKey == "" {
		log.Printf("[GLASS] No conv fingerprint — skipping glass pipeline")
		return result
	}
	result.SessionKey = sessionKey

	requestKey := meta.RequestKey
	if requestKey == "" {
		requestKey = sessionKey
	} else if meta.SessionKey != "" && requestKey == meta.SessionKey && sessionKey != meta.SessionKey {
		requestKey = sessionKey
	}
	result.RequestKey = requestKey

	affinityKey := meta.AffinityKey
	if affinityKey == "" {
		affinityKey = sessionKey
	}
	result.AffinityKey = affinityKey

	tokensBefore := estimateTokens(body)
	result.TokenEstimateBefore = tokensBefore
	var originalMessages []interface{}
	if rawMsgs, ok := body["messages"].([]interface{}); ok {
		originalMessages = deepCopyMessages(rawMsgs)
	}
	requireUserFinal := originalRequestRequiresUserFinal(originalMessages)

	var originalSystem []interface{}
	if rawSystem, ok := body["system"].([]interface{}); ok {
		originalSystem = deepCopyFragments(rawSystem)
	}
	state := e.sessions.Get(sessionKey)
	chapterStateDirty := false

	// Step 1: System prompt processing (canonical cache + replacement + sysreminder strip)
	if _, ok := body["system"].([]interface{}); ok {
		if len(originalSystem) > 0 {
			modified, mods := e.sysproc.Process(deepCopyFragments(originalSystem), classification, "")
			if mods > 0 {
				log.Printf("[GLASS] System prompt: %d modifications applied", mods)
			}
			body["system"] = modified
		}
	}

	// Distinct small-system subagents should not create their own Anthropic
	// cache lane and should not accumulate a Glass-owned message history. Keep
	// them parent-affine, strip all cache markers, and leave the request body
	// otherwise close to what CC sent.
	if classification.BypassMessageCache {
		strippedCacheControls := StripAllCacheControl(body)
		stripSystemRemindersFromMessages(body)
		// Thinking blocks stripped at ingestion (localcache.Ingest), not here.
		if restored, finalIssues, originalIssues := fallbackToOriginalMessagesIfInvalid(body, originalMessages, requireUserFinal); len(finalIssues) > 0 {
			if restored {
				result.StrippedCount = 0
				result.OrphansFixed = 0
				log.Printf("[GLASS] Invalid message structure after uncached pass-through conv=%s issues=%s — falling back to original CC messages (original_valid=%t)",
					sessionKey, summarizeValidationIssues(finalIssues), len(originalIssues) == 0)
			} else {
				log.Printf("[GLASS] Invalid message structure after uncached pass-through conv=%s issues=%s — original CC messages also unavailable/invalid (%s)",
					sessionKey, summarizeValidationIssues(finalIssues), summarizeValidationIssues(originalIssues))
			}
		}
		if requireUserFinal {
			if msgs, ok := body["messages"].([]interface{}); ok {
				if outboundIssues := validateOutboundRequestMessages(msgs); len(outboundIssues) > 0 {
					result.InvalidRequestError = summarizeValidationIssues(outboundIssues)
					log.Printf("[GLASS] Invalid outbound request after uncached pass-through conv=%s issues=%s",
						sessionKey, summarizeValidationIssues(outboundIssues))
				}
			}
		}
		tokensAfter := estimateTokens(body)
		result.TokenEstimateAfter = tokensAfter
		result.TokensSaved = tokensBefore - tokensAfter
		if result.TokensSaved < 0 {
			result.TokensSaved = 0
		}
		log.Printf("[GLASS] Uncached subagent pass-through type=%s affinity=%s request=%s stripped_cache_controls=%d",
			classification.Type, affinityKey, sessionKey, strippedCacheControls)
		return result
	}

	// Step 1b: Subagent isolation — give subagents their own cache to prevent
	// the parent's prefix from being corrupted by 28K subagent contexts.
	if classification.IsolateSession {
		log.Printf("[GLASS] Subagent detected — using isolated cache: %s", sessionKey)
	}

	// Step 2: Get local cache and session state
	cache := e.getCache(sessionKey)
	cache = e.ensureCacheStateCompatible(sessionKey, cache, state)
	preCacheClassification := classification
	classification = subagent.WithCacheContext(classification, cache.Len())
	if classification.IsSubagent != preCacheClassification.IsSubagent || classification.Type != preCacheClassification.Type {
		log.Printf("[GLASS] WithCacheContext changed classification conv=%s: sub=%v→%v type=%s→%s msgs=%d cacheLen=%d",
			sessionKey, preCacheClassification.IsSubagent, classification.IsSubagent,
			preCacheClassification.Type, classification.Type,
			classification.MessageCount, cache.Len())
	}
	if !classification.IsSubagent && classification.MessageCount > 0 && cache.Len() > 10 && float64(classification.MessageCount) < float64(cache.Len())*0.8 {
		log.Printf("[GLASS] WithCacheContext SHOULD have fired but didn't: conv=%s msgs=%d cacheLen=%d isolate=%v sub=%v type=%s",
			sessionKey, classification.MessageCount, cache.Len(), classification.IsolateSession, classification.IsSubagent, classification.Type)
	}
	// Subagent-origin conversations must never create Anthropic cache entries,
	// regardless of how they're currently classified. Once a conversation gets
	// an isolated cache (session key contains _sub_), it stays subagent-origin
	// even if message count grows past the classifier's threshold.
	if strings.Contains(sessionKey, "_sub_") &&
		!classification.DisableUpstreamCaching {
		classification.DisableUpstreamCaching = true
		log.Printf("[GLASS] Forcing DisableUpstreamCaching for subagent-origin session: %s (msgs=%d)",
			sessionKey, classification.MessageCount)
	}
	result.Subagent = classification

	// Step 3: Ingest new messages from CC's request into local cache.
	// System-reminders are stripped on ingestion. cache_control is removed.
	// Messages already in the cache (by position) are ignored.
	newMsgsAdded := 0
	ccMsgs, _ := body["messages"].([]interface{})
	if ccMsgs != nil {
		newMsgsAdded = cache.Ingest(ccMsgs)
		if newMsgsAdded > 0 {
			log.Printf("[GLASS] conv=%s ingested %d new messages", sessionKey, newMsgsAdded)
			// Invalidate cold gate: prefix changed, next batch of concurrent
			// calls (subagents) must re-warm Anthropic cache.
			gate := e.getColdGate(sessionKey)
			gate.mu.Lock()
			gate.lastWarm = time.Time{} // zero = not warm
			gate.mu.Unlock()
			// Persist cache to disk so restarts don't cause cold starts.
			if err := cache.Save(e.cfg.ShadowDir); err != nil {
				log.Printf("[GLASS] localcache save error: %v", err)
			}
		}
	}
	result.NewMessagesAdded = newMsgsAdded
	result.HasToolResults = requestHasToolResults(ccMsgs)

	// Recovery gate: check if agent has read the recovery file (clears gate)
	if e.summarizer != nil && e.summarizer.IsGateArmed(sessionKey) && ccMsgs != nil {
		e.summarizer.CheckGateClear(sessionKey, ccMsgs)
	}

	// Rolling summarization: produce a chunk if enough unsummarized tokens accumulated.
	// Runs ASYNC in background — never blocks the user's request.
	if e.summarizer != nil && newMsgsAdded > 0 {
		go func(convID string, c *LocalCache) {
			if e.summarizer.MaybeChunk(convID, c) {
				log.Printf("[GLASS] conv=%s rolling chunk produced (async)", convID)
			}
		}(sessionKey, cache)
	}

	// Chapter writing only happens during eviction (below), not on every request.
	cacheMode := e.cfg.CacheMode()
	result.ContextCacheMode = cacheMode

	// Compression: batch-advance the watermark to match nataraja's proven design.
	// Between watermark advances, all prefix bytes are IDENTICAL (compression is
	// idempotent on already-compressed messages). This gives 100% cache hits between
	// batches, with ONE cache break per batch advance.
	//
	// POSTMORTEM-FEB19 L-2: "All modifications to messages[0:watermark] must produce
	// IDENTICAL bytes between watermark advances."
	//
	// The old eager design (advancing watermark by 2 every request) violated this:
	// each creep compressed 2 new messages inside the cached prefix, causing a
	// separate cache break. Live data (session 84386, 22:46-22:47) proved
	// compression breaks are independent of anchor advances.
	//
	// Gating: skip compression entirely until tokens exceed CompressionTriggerTokens.
	// Small conversations don't need it — they won't hit context limits.
	//
	// Skipped in "off" and "context_api" modes — no client-side compression.
	if cacheMode == CacheModeFull && newMsgsAdded > 0 {
		currentLen := cache.Len()
		compressionTrigger := e.cfg.CompressionTrigger()
		compressionBatch := e.cfg.CompressionBatch()

		// Gate: don't compress until estimated total tokens justify it.
		totalEstTokens := cache.TotalTokens()
		if compressionTrigger > 0 && totalEstTokens < compressionTrigger {
			goto skipCompression
		}

		{
			newWatermark := currentLen - e.cfg.RecentKeepMsgs
			if newWatermark < 0 {
				newWatermark = 0
			}

			// Clamp below the anchor — never compress messages that
			// Anthropic may have cached uncompressed.
			if prev := cache.RawPrevBreakpointAnchor(); prev > 0 && newWatermark > prev {
				newWatermark = prev
			} else if anchor := cache.RawBreakpointAnchor(); anchor > 0 && newWatermark > anchor {
				newWatermark = anchor
			}

			// Batch gate: only advance when the gap is >= batchSize.
			// Between batches, CompressOldMessages is a no-op (idempotent
			// on already-compressed messages) — prefix bytes are identical.
			delta := newWatermark - state.CompressionWatermark
			if delta <= 0 {
				goto skipCompression
			}
			if compressionBatch > 0 && delta < compressionBatch {
				// Not enough messages accumulated for a batch yet.
				// Re-run compression at the CURRENT watermark to ensure
				// idempotent bytes (defensive, costs ~nothing).
				if state.CompressionWatermark > 0 {
					cache.CompressOldMessages(state.CompressionWatermark, DefaultCompressionOpts())
				}
				goto skipCompression
			}

			// Batch advance — ONE cache break for the entire batch.
			state.PreCompressionSnapshot = cache.PreCompressionSnapshot(newWatermark)
			saved, compressed := cache.CompressOldMessages(newWatermark, DefaultCompressionOpts())
			if saved > 0 {
				log.Printf("[GLASS] conv=%s batch-compressed %d messages (watermark %d\u2192%d, batch=%d, ~%d tokens saved)",
					sessionKey, compressed, state.CompressionWatermark, newWatermark, delta, saved)
			}
			state.CompressionWatermark = newWatermark
			if err := e.sessions.Save(state); err != nil {
				log.Printf("[GLASS] state save after compression error: %v", err)
			}
		}
	skipCompression:
	}

	// Check saturation: flush compressed messages when EITHER:
	//   1. Compression ratio >= 85% (most messages are compressed stubs)
	//   2. Total tokens exceed CompressionFlushTokens (approaching degradation zone)
	// This keeps effective context in the ~200K trained range where attention works,
	// regardless of the 1M API window.
	// Only applicable in "full" mode — no compressed messages exist in other modes.
	flushTokenThreshold := e.cfg.CompressionFlushTokens
	if flushTokenThreshold == 0 {
		flushTokenThreshold = 200000
	}
	ratio := cache.InformationLossRatio()
	totalTokens := cache.TotalTokens()
	needsFlush := cacheMode == CacheModeFull && (ratio >= 0.85 || totalTokens > flushTokenThreshold) && cache.HasCompressedMessages()
	if needsFlush && e.summarizer != nil {
		st := e.summarizer.getState(sessionKey)
		if len(st.Chunks) > 0 {
			// Build summary from existing chunk summaries
			var summaryParts []string
			for _, chunk := range st.Chunks {
				data, err := os.ReadFile(chunk.SummaryPath)
				if err == nil {
					summaryParts = append(summaryParts, string(data))
				}
			}
			if len(summaryParts) > 0 {
				summary := "## Session Recovery (auto-flush)\n\n" +
					"Context was compressed and flushed to free space. " +
					"CONTINUE the work described below — do NOT ask what to do next.\n\n" +
					strings.Join(summaryParts, "\n---\n")
				removed := cache.FlushCompressed(summary)
				if removed > 0 {
					trigger := "ratio"
					if totalTokens > flushTokenThreshold {
						trigger = fmt.Sprintf("tokens(%dk>%dk)", totalTokens/1000, flushTokenThreshold/1000)
					}
					log.Printf("[GLASS] conv=%s saturation flush: %s ratio=%.2f tokens=%dk, removed %d compressed messages, summary injected in-context",
						sessionKey, trigger, ratio, totalTokens/1000, removed)
					state.CompressionWatermark = 0
					// Reset summarizer chunks for next cycle
					st.Chunks = nil
					st.LastSummarizedIdx = 0
					st.TokensSummarized = 0
					if err := e.summarizer.saveState(st); err != nil {
						log.Printf("[GLASS] summarizer state save after flush error: %v", err)
					}
					if err := e.sessions.Save(state); err != nil {
						log.Printf("[GLASS] state save after flush error: %v", err)
					}
					if err := cache.Save(e.cfg.ShadowDir); err != nil {
						log.Printf("[GLASS] cache save after flush error: %v", err)
					}
				}
			}
		}
	}

	// Heal already-poisoned caches before building the Anthropic-visible view.
	// This recovers from persisted user/user or assistant/assistant splits
	// without requiring a conversation reset.
	if repaired := cache.RepairBrokenToolBoundaries(); repaired > 0 {
		log.Printf("[GLASS] Repaired %d invalid tool/role boundaries before request build for conv=%s", repaired, sessionKey)
		gate := e.getColdGate(sessionKey)
		gate.mu.Lock()
		gate.lastWarm = time.Time{}
		gate.mu.Unlock()
		if err := cache.Save(e.cfg.ShadowDir); err != nil {
			log.Printf("[GLASS] localcache repair save error: %v", err)
		}
	}

	// Step 3b: Cold-start serialization.
	// Gate concurrent requests when MULTIPLE callers share the same cache but
	// none of them added new messages (subagent pattern: CC sends fewer msgs
	// than cache has, Ingest returns 0, Build serves the full cache).
	// The first request goes to Anthropic to establish the cached prefix.
	// The rest wait and then hit 100% cache read.
	// Cold gate warming is Anthropic-specific (prompt cache pre-warming).
	isWarmer := false
	if newMsgsAdded == 0 && cache.Len() > 2 {
		isWarmer = e.acquireColdGate(sessionKey)
		if isWarmer {
			result.ColdWarmerSessionKey = sessionKey
		}
	}

	// Step 4: Check token budget — evict from cache if needed
	sysTokens := estimateTokens(body["system"])
	toolTokens := estimateTokens(body["tools"])
	msgTokens := cache.TotalTokens()
	budget := computeEvictionBudget(sysTokens+toolTokens+msgTokens, state, e.cfg.EvictTriggerTokens)

	var evictedBatch []map[string]interface{}
	if budget.TotalTokens > budget.Threshold {
		if state.EvictedCount > 0 {
			result.EvictionReason = "pinned_frame_overflow " + describeEvictionBudget(budget, e.cfg.EvictTriggerTokens)
			// Once a session is already in shadow-backed mode, use the V2 Pinned Frame
			// strategy. Instead of chasing a token target, it decisively archives
			// the "bridge" between the stable anchor and the hot tail.
			evictedBatch = cache.PinFrame(e.cfg.AnchorKeepMsgs, e.cfg.RecentKeepMsgs, requireUserFinal)
		} else {
			result.EvictionReason = "initial_overflow " + describeEvictionBudget(budget, e.cfg.EvictTriggerTokens)
			// First overflow: use the standard target-based eviction to establish
			// the initial shadow lane.
			targetMsgTokens := computeTargetMsgTokens(e.cfg, state, sysTokens, toolTokens, budget)
			log.Printf("[GLASS] conv=%s eviction triggered: %d tokens > %d trigger%s",
				sessionKey, budget.TotalTokens, budget.Threshold, describeEvictionBudget(budget, e.cfg.EvictTriggerTokens))

			evictedBatch = cache.Evict(targetMsgTokens, e.cfg.AnchorKeepMsgs, e.cfg.RecentKeepMsgs)
		}
	}

	if len(evictedBatch) > 0 {
		state.EvictedCount += len(evictedBatch)
		state.BatchCount++
		state.ClearAPITokenBudget()
		result.EvictedCount += len(evictedBatch)
		result.ShadowBatch = state.BatchCount

		// Write evicted messages to shadow file
		if err := e.shadow.Write(sessionKey, evictedBatch, state); err != nil {
			log.Printf("[GLASS] Shadow write error: %v", err)
		} else {
			result.ShadowPath = filepath.Join(e.cfg.ShadowDir, sessionKey, "shadow.md")
		}

		if e.chapterWriter != nil {
			chapterDir, err := e.chapterWriter.WriteEviction(sessionKey, state, evictedBatch)
			if err != nil {
				log.Printf("[GLASS] Chapter write error: %v", err)
			} else if chapterDir != "" {
				chapterStateDirty = true
			}
		}

		// Rolling summarizer: stitch chunk summaries + delta → recovery file, arm gate
		if e.summarizer != nil {
			recoveryPath := e.summarizer.StitchAndArm(sessionKey, cache, evictedBatch, state.BatchCount, state)
			if recoveryPath != "" {
				log.Printf("[GLASS] Recovery file written: %s (gate_armed=%v)",
					recoveryPath, e.summarizer.IsGateArmed(sessionKey))
			}
		}

		// Repair any orphan tool_use/tool_result pairs that eviction missed.
		if repaired := cache.RepairBrokenToolBoundaries(); repaired > 0 {
			log.Printf("[GLASS] Repaired %d orphan tool boundaries after eviction for conv=%s", repaired, sessionKey)
		}

		// Bake bookmark pointing at the session chapter directory.
		// ChapterMDPath now holds the session dir (set by WriteEviction).
		if state.ChapterMDPath != "" {
			log.Printf("[GLASS] Baking eviction bookmark for conv=%s path=%s", sessionKey, state.ChapterMDPath)
			cache.BakeEvictionBookmark(state.ChapterMDPath)
		}

		if err := e.sessions.Save(state); err != nil {
			log.Printf("[GLASS] State save error: %v", err)
		}
		// Persist updated cache (bookmark baked in) so
		// restart after eviction also skips cold start.
		if err := cache.Save(e.cfg.ShadowDir); err != nil {
			log.Printf("[GLASS] localcache post-eviction save error: %v", err)
		}
	}

	if !classification.IsolateSession && e.prefixWarmer != nil {
		tools, _ := body["tools"].([]interface{})
		model, _ := body["model"].(string)
		system, _ := body["system"].([]interface{})
		result.PrefixKey = e.prefixWarmer.Observe(meta, system, tools, model)
	}

	// Step 5: Build the canonical Anthropic request view from the cache.
	// After eviction, the cache already contains the bookmark as a real message —
	// BuildForRequest serves it directly without any dynamic injection.
	var view requestView
	if classification.UseFrozenPrefix {
		log.Printf("[GLASS] Opus subagent detected: CC sent %d msgs, cache has %d msgs",
			classification.MessageCount, cache.Len())
		// Opus subagents share the parent's cache but get a frozen prefix
		// First opus subagent locks the freeze point
		cache.SetFreezePoint()
		view = cache.BuildFrozenForRequest(e.cfg.StripThinking)
		log.Printf("[GLASS] Serving frozen prefix to opus subagent")
	} else if e.HasEvictedState(sessionKey) && !isWarmer {
		// CRITICAL FIX: Use BuildPinnedFrameForRequest for evicted sessions.
		// BuildForRequest returns the entire retained history, which might be large if
		// Evict() left a bridge or if ingestion restored messages.
		// Pinned frame forces a bounded view (Anchors + Tail).
		view = cache.BuildPinnedFrameForRequest(e.cfg.StripThinking, e.cfg.AnchorKeepMsgs, e.cfg.RecentKeepMsgs, requireUserFinal)
	} else {
		view = cache.BuildForRequest(e.cfg.StripThinking)
	}
	body["messages"] = view.Messages
	result.StrippedCount = view.StrippedCount
	result.OrphansFixed = view.OrphansFixed

	// Step 6: (removed) Reference and chapter replay injection. Using pinned-frame
	// view and reference injection as intended.

	if result.OrphansFixed > 0 {
		log.Printf("[GLASS] Fixed %d orphan tool results", result.OrphansFixed)
	}

	// Step 7: Place cache_control breakpoint at STABLE ANCHOR position.
	// Only in "full" mode — Glass owns cache_control placement.
	// In "off" mode, no message-level cache_control is placed; Anthropic's implicit
	// prefix caching still operates on system/tools (which retain CC's markers).
	// In "context_api" mode, Anthropic manages caching server-side.
	anchorIdx := view.AnchorIdx
	prevAnchorIdx := view.PrevAnchorIdx
	// Compute watermark position in output space for deep-stable breakpoint.
	watermarkIdx := cache.CompressionWatermarkOutput(state.CompressionWatermark)
	if cacheMode == CacheModeFull {
		placeBreakpoint(body, anchorIdx, prevAnchorIdx, watermarkIdx)
	} else {
		// Still normalize system/tool cache_control TTL for extended cache lifetime,
		// but do NOT place any message-level breakpoints.
		normalizeCacheControlTTL(body, ExtendedCacheTTL)
		log.Printf("[GLASS] conv=%s cache_mode=%s — skipped message breakpoint placement", sessionKey, cacheMode)
	}

	// Step 7b: Inject context_management block when using Anthropic's Context Editing API.
	// The block tells Anthropic's server to clear old tool results (and optionally thinking)
	// before the prompt reaches Claude. This replaces Glass's client-side compression.
	if cacheMode == CacheModeContextAPI {
		trigger, keep, clearAtLeast, clearThinking := e.cfg.ContextAPIConfig()
		edits := []interface{}{}
		if clearThinking {
			edits = append(edits, map[string]interface{}{
				"type": "clear_thinking_20251015",
				"keep": map[string]interface{}{"type": "thinking_turns", "value": 1},
			})
		}
		edits = append(edits, map[string]interface{}{
			"type":           "clear_tool_uses_20250919",
			"trigger":        map[string]interface{}{"type": "input_tokens", "tokens": trigger},
			"keep":           map[string]interface{}{"type": "tool_uses", "value": keep},
			"clear_at_least": map[string]interface{}{"type": "tokens", "tokens": clearAtLeast},
		})
		body["context_management"] = map[string]interface{}{"edits": edits}
		log.Printf("[GLASS] conv=%s injected context_management: trigger=%d keep=%d clear_at_least=%d thinking=%v",
			sessionKey, trigger, keep, clearAtLeast, clearThinking)
	}

	restoredToOriginal := false
	if restored, finalIssues, originalIssues := fallbackToOriginalMessagesIfInvalid(body, originalMessages, requireUserFinal); len(finalIssues) > 0 {
		if restored {
			restoredToOriginal = true
			result.StrippedCount = 0
			result.OrphansFixed = 0
			anchorIdx = lastExplicitCacheControlAnchor(body)
			if anchorIdx < 0 {
				if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) > 0 {
					anchorIdx = len(msgs) - 1
				}
			}
			log.Printf("[GLASS] Invalid message structure after Glass mutations conv=%s issues=%s — falling back to original CC messages (original_valid=%t)",
				sessionKey, summarizeValidationIssues(finalIssues), len(originalIssues) == 0)
		} else {
			log.Printf("[GLASS] Invalid message structure after Glass mutations conv=%s issues=%s — original CC messages also unavailable/invalid (%s)",
				sessionKey, summarizeValidationIssues(finalIssues), summarizeValidationIssues(originalIssues))
		}
	}
	if restoredToOriginal && state.EvictedCount > 0 && cache.EvictionReferenceCount() > 0 {
		if len(evictedBatch) > 0 {
			// Eviction itself succeeded but the build step produced invalid
			// output. Don't nuke the eviction state — the cache already has
			// the correct references. On the next call fixConsecutiveRoles
			// will repair the view. Resetting here causes a destructive loop:
			// evict → invalid build → reset → re-evict → repeat forever.
			log.Printf("[GLASS] conv=%s skipping reset: eviction succeeded this call (%d msgs), build step failed — will retry next call",
				sessionKey, len(evictedBatch))
		} else {
			cache = e.resetConversationState(sessionKey, state,
				"glass restored original request after invalid mutated outbound")
		}
	}
	if requireUserFinal {
		if msgs, ok := body["messages"].([]interface{}); ok {
			if outboundIssues := validateOutboundRequestMessages(msgs); len(outboundIssues) > 0 {
				result.InvalidRequestError = summarizeValidationIssues(outboundIssues)
				log.Printf("[GLASS] Invalid outbound request after Glass mutations conv=%s issues=%s",
					sessionKey, summarizeValidationIssues(outboundIssues))
				if newMsgsAdded == 0 && state.EvictedCount > 0 && cache.EvictionReferenceCount() > 0 && len(evictedBatch) == 0 {
					cache = e.resetConversationState(sessionKey, state,
						"replayed persisted cache produced invalid outbound request")
				}
				return result
			}
		}
	}
	diag := e.logFinalPrefixDiagnostic(sessionKey, body, anchorIdx, outboundPrefixDiagnosticMeta{
		RequestKey:         requestKey,
		PrevAnchor:         prevAnchorIdx,
		InjectedReferences: false,
		ReferenceInsertAt:  -1,
		CacheMessages:      cache.Len(),
		EvictedCount:       state.EvictedCount,
		BatchCount:         state.BatchCount,
		RestoredToOriginal: restoredToOriginal,
	})
	result.PrefixChangeKind = diag.ChangeKind
	result.PrefixDivergence = diag.Divergence
	result.PrefixSystemChanged = diag.SystemChanged
	result.PrefixToolsChanged = diag.ToolsChanged
	result.PrefixAnchor = diag.Snapshot.Anchor
	result.PrefixPrevAnchor = diag.Meta.PrevAnchor
	result.PrefixMeasuredMsgs = diag.Snapshot.Measured
	result.PrefixTailChangeKind = diag.TailChangeKind
	result.PrefixTailDivergence = diag.TailDivergence
	result.PrefixTailAnchor = diag.Snapshot.TailAnchor
	result.PrefixTailPrevAnchor = diag.Meta.PrevTailAnchor
	result.PrefixTailMeasured = diag.Snapshot.TailMeasured
	result.PrefixTailTokens = diag.Snapshot.TailTokens
	result.PrefixTailHash = diag.Snapshot.TailHash
	result.CompressionWatermark = state.CompressionWatermark
	result.PrefixHash = cache.PrefixHash()

	tokensAfter := estimateTokens(body)
	result.TokenEstimateAfter = tokensAfter
	result.TokensSaved = tokensBefore - tokensAfter
	if result.TokensSaved < 0 {
		result.TokensSaved = 0
	}

	if result.TokensSaved > 0 || cache.Len() > 0 {
		log.Printf("[GLASS] conv=%s cached=%d saved=%d evicted_total=%d anchor=msg[%d] sub=%v type=%s warmer=%v",
			sessionKey, cache.Len(), result.TokensSaved, state.EvictedCount, anchorIdx, classification.IsSubagent, classification.Type, isWarmer)
	}

	if chapterStateDirty {
		if err := e.sessions.Save(state); err != nil {
			log.Printf("[GLASS] state save after chapter/fact update failed: %v", err)
		}
	}

	// Recovery gate: signal to proxy that this request should be blocked
	// until the agent reads the recovery file.
	if e.summarizer != nil && e.summarizer.IsGateArmed(sessionKey) {
		result.RecoveryGateBlocked = true
		result.RecoveryFilePath = e.summarizer.RecoveryFilePath(sessionKey)
		log.Printf("[GLASS] conv=%s recovery gate ARMED — agent must read %s",
			sessionKey, result.RecoveryFilePath)
	}

	// Note: if isWarmer=true, proxy.go will call ReleaseColdGate on first SSE event
	// (or on error). The 10s safety goroutine in acquireColdGate ensures release
	// even if proxy fails.

	return result
}

// ReleaseColdGate releases the cold gate for a conversation.
// Must be called by proxy.go when a warming request's response arrives.
func (e *Engine) ReleaseColdGate(convID string) {
	e.releaseColdGate(convID)
}

// ResetConversation discards the current persisted and in-memory state for a
// conversation so the next request rebuilds from a clean lane.
func (e *Engine) ResetConversation(convID string, reason string) {
	if convID == "" {
		return
	}
	state := e.sessions.Get(convID)
	e.resetConversationState(convID, state, reason)
}

// HasEvictedState reports whether a conversation is currently operating in a
// shadow-backed mode and therefore eligible for post-overflow safety guards.
func (e *Engine) HasEvictedState(convID string) bool {
	if convID == "" {
		return false
	}
	state := e.sessions.Get(convID)
	if state != nil && state.EvictedCount > 0 {
		return true
	}
	e.mu.Lock()
	cache := e.caches[convID]
	e.mu.Unlock()
	if cache == nil {
		return false
	}
	return cache.EvictionReferenceCount() > 0
}

// ProcessSystemPrompt runs only the system prompt pipeline (for use
// when system prompt processing should happen separately from message processing).
func (e *Engine) ProcessSystemPrompt(system []interface{}) ([]interface{}, int) {
	return e.sysproc.Process(system, subagent.Classify(map[string]interface{}{"system": system}), "")
}

// UpdateAPITokens records the real API input token count from the response.
func (e *Engine) UpdateAPITokens(convID string, inputTokens int) {
	if convID == "" || inputTokens <= 0 {
		return
	}
	state := e.sessions.Get(convID)
	state.LastAPIInput = inputTokens
	state.LastAPIInputAt = time.Now()
	if err := e.sessions.Save(state); err != nil {
		log.Printf("[GLASS] state save after token update failed: %v", err)
	}
}

// SetPrefixWarmer attaches a shared prefix warmer manager to the engine.
// IsSyspromptStale returns true if the replacement pipeline targets did not match.
func (e *Engine) IsSyspromptStale() bool {
	if e.sysproc == nil {
		return false
	}
	return e.sysproc.IsStale()
}

func (e *Engine) SetPrefixWarmer(pw *PrefixWarmer) {
	e.prefixWarmer = pw
}

// SetSummarizer attaches the rolling summarizer to the engine.
func (e *Engine) SetSummarizer(rs *RollingSummarizer) {
	e.summarizer = rs
}

// Summarizer returns the engine's rolling summarizer (for proxy gate checks).
func (e *Engine) Summarizer() *RollingSummarizer {
	return e.summarizer
}

// requestHasToolResults returns true if the request messages contain any tool_result blocks.
// This is used to detect legitimate tool result processing vs. infinite loops.
func requestHasToolResults(msgs []interface{}) bool {
	if msgs == nil {
		return false
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, c := range content {
			block, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			if tp, _ := block["type"].(string); tp == "tool_result" {
				return true
			}
		}
	}
	return false
}

// Config returns the current Glass configuration.
func (e *Engine) Config() GlassConfig {
	return e.cfg
}

// CacheStats returns diagnostic info about a conversation's cache.
func (e *Engine) CacheStats(convID string) map[string]interface{} {
	e.mu.Lock()
	cache, ok := e.caches[convID]
	e.mu.Unlock()
	if !ok {
		return nil
	}

	return map[string]interface{}{
		"conv_id":           convID,
		"cached_messages":   cache.Len(),
		"total_tokens":      cache.TotalTokens(),
		"breakpoint_anchor": cache.BreakpointAnchor(),
	}
}
