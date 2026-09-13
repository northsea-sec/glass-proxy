// Package serializer implements batch affinity serialization for concurrent
// Claude Code sessions sharing the same Anthropic API key.
//
// PROBLEM:
//
//	Anthropic's prompt cache is prefix-matched. When sessions A and B interleave:
//	  A → B → A → B → each call rebuilds the entire prefix after the shared system prompt.
//	With 120K+ message bodies, that's 120K cache_creation at 1.25x cost PER CALL.
//	Both sessions see 0% cache hit. Death spiral.
//
// SOLUTION:
//
//	Batch calls by session: AAAAA-BBBBB-CCCCC.
//	Only the FIRST call of each batch breaks cache. Calls 2-N hit cache.
//	With batch_size=5 and 3 sessions: 3 breaks per round vs 15.
//	~5x reduction in cache write cost.
//
// DESIGN:
//   - Active session gets batch_size consecutive API calls
//   - Same-session calls ALWAYS pass through (zero latency)
//   - Different-session calls block on a channel (queued)
//   - Background goroutine: idle timeout → switch, queue timeout → force release
//   - Classified subagents serialize under their parent session
//   - Per-tenant: each tenant gets its own serializer (multi-tenant safe)
package serializer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"proxy.local/app/internal/promptscope"
	"proxy.local/app/internal/subagent"
)

// Config for the serializer. Hot-reloadable.
type Config struct {
	Enabled              bool    `json:"ser_enabled"`
	BatchSize            int     `json:"ser_batch_size"`             // consecutive calls per session (default 5)
	IdleTimeoutSec       float64 `json:"ser_idle_timeout_sec"`       // switch if idle this long (default 3.0)
	MaxQueueSec          float64 `json:"ser_max_queue_sec"`          // force-release after this (default 120.0)
	SubagentMsgThreshold int     `json:"ser_subagent_msg_threshold"` // legacy knob; shared classifier owns detection
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type rawConfig struct {
		EnabledCompat        *bool    `json:"serializer_enabled"`
		Enabled              *bool    `json:"ser_enabled"`
		BatchSize            *int     `json:"ser_batch_size"`
		IdleTimeoutSec       *float64 `json:"ser_idle_timeout_sec"`
		MaxQueueSec          *float64 `json:"ser_max_queue_sec"`
		SubagentMsgThreshold *int     `json:"ser_subagent_msg_threshold"`
	}

	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	if raw.Enabled != nil {
		c.Enabled = *raw.Enabled
	} else if raw.EnabledCompat != nil {
		c.Enabled = *raw.EnabledCompat
	}
	if raw.BatchSize != nil {
		c.BatchSize = *raw.BatchSize
	}
	if raw.IdleTimeoutSec != nil {
		c.IdleTimeoutSec = *raw.IdleTimeoutSec
	}
	if raw.MaxQueueSec != nil {
		c.MaxQueueSec = *raw.MaxQueueSec
	}
	if raw.SubagentMsgThreshold != nil {
		c.SubagentMsgThreshold = *raw.SubagentMsgThreshold
	}
	return nil
}

// DefaultConfig returns sensible defaults.
// IdleTimeoutSec=15 is the A/B-tested optimum for 2-3 concurrent sessions
// (3s=90.9%, 15s=99.4%, 30s=85.2% cache efficiency — see POSTMORTEM-FEB23).
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		BatchSize:            5,
		IdleTimeoutSec:       15.0,
		MaxQueueSec:          120.0,
		SubagentMsgThreshold: 20,
	}
}

// StatsSnapshot is the public, copyable view of serializer counters.
// It deliberately omits the mutex so callers can safely copy by value.
type StatsSnapshot struct {
	RequestsTotal            int `json:"requests_total"`
	RequestsPassthrough      int `json:"requests_passthrough"`
	RequestsQueued           int `json:"requests_queued"`
	RequestsReleased         int `json:"requests_released"`
	RequestsTimeout          int `json:"requests_timeout"`
	RequestsSubagent         int `json:"requests_subagent"`
	RequestsSubagentFallback int `json:"requests_subagent_fallback"`
	RequestsSubagentNoGate   int `json:"requests_subagent_no_gate"`
	RequestsSubagentGateWait int `json:"requests_subagent_gate_wait"`
	BatchSwitches            int `json:"batch_switches"`
}

// Stats tracks serializer activity. Must not be copied (contains mutex).
type Stats struct {
	mu                       sync.Mutex
	RequestsTotal            int `json:"requests_total"`
	RequestsPassthrough      int `json:"requests_passthrough"`
	RequestsQueued           int `json:"requests_queued"`
	RequestsReleased         int `json:"requests_released"`
	RequestsTimeout          int `json:"requests_timeout"`
	RequestsSubagent         int `json:"requests_subagent"`
	RequestsSubagentFallback int `json:"requests_subagent_fallback"`
	RequestsSubagentNoGate   int `json:"requests_subagent_no_gate"`
	RequestsSubagentGateWait int `json:"requests_subagent_gate_wait"`
	BatchSwitches            int `json:"batch_switches"`
}

// queueEntry represents a blocked request waiting for its turn.
type queueEntry struct {
	convID      string
	release     chan struct{} // closed when this request may proceed
	enqueueTime time.Time
	releaseOnce sync.Once // prevents double-close panic when timer and checkerLoop race
}

// tryRelease safely closes the release channel exactly once.
// Returns true if this call actually closed the channel (first caller wins).
// Subsequent calls return false — the channel is already closed.
func (e *queueEntry) tryRelease() bool {
	released := false
	e.releaseOnce.Do(func() {
		close(e.release)
		released = true
	})
	return released
}

// Serializer implements batch affinity for one tenant's API calls.
type Serializer struct {
	mu               sync.Mutex
	cfg              Config
	activeConv       string                   // conv_id of currently active session
	batchRemaining   int                      // calls left in current batch
	lastActivity     time.Time                // last request from active session
	inFlight         int                      // ALL requests currently streaming for active session
	subagentInFlight int                      // subset of inFlight that are subagent requests
	queue            []*queueEntry            // blocked requests from other sessions
	pidToParent      map[int]string           // PID → parent conv_id (subagent mapping)
	subagentGates    map[string]chan struct{} // gate key → buffered chan(1) limiting concurrent subagent requests
	stats            Stats
	stopCh           chan struct{}
}

// New creates a Serializer with the given config and starts the background checker.
func New(cfg Config) *Serializer {
	if cfg.BatchSize == 0 {
		cfg = DefaultConfig()
	}
	s := &Serializer{
		cfg:           cfg,
		pidToParent:   make(map[int]string),
		subagentGates: make(map[string]chan struct{}),
		stopCh:        make(chan struct{}),
	}
	go s.checkerLoop()
	return s
}

// UpdateConfig hot-reloads the config.
func (s *Serializer) UpdateConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 5
	}
	s.cfg = cfg
}

// Stop shuts down the background checker.
func (s *Serializer) Stop() {
	close(s.stopCh)
}

// GetStats returns a snapshot of current stats (safe to copy).
func (s *Serializer) GetStats() StatsSnapshot {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	return StatsSnapshot{
		RequestsTotal:            s.stats.RequestsTotal,
		RequestsPassthrough:      s.stats.RequestsPassthrough,
		RequestsQueued:           s.stats.RequestsQueued,
		RequestsReleased:         s.stats.RequestsReleased,
		RequestsTimeout:          s.stats.RequestsTimeout,
		RequestsSubagent:         s.stats.RequestsSubagent,
		RequestsSubagentFallback: s.stats.RequestsSubagentFallback,
		RequestsSubagentNoGate:   s.stats.RequestsSubagentNoGate,
		RequestsSubagentGateWait: s.stats.RequestsSubagentGateWait,
		BatchSwitches:            s.stats.BatchSwitches,
	}
}

// getSubagentGate returns or creates a buffered channel that limits concurrent
// subagent requests for the given gate key to 1 in-flight at a time.
// Must be called with s.mu held.
func (s *Serializer) getSubagentGate(gateKey string) chan struct{} {
	if g, ok := s.subagentGates[gateKey]; ok {
		return g
	}
	g := make(chan struct{}, 1)
	s.subagentGates[gateKey] = g
	return g
}

// resolveSubagentGateKey returns the strongest available gate key for a
// subagent request. Prefer the mapped parent conversation when PID affinity is
// available; otherwise fall back to the request's own serializer convID.
// Must be called with s.mu held.
func (s *Serializer) resolveSubagentGateKey(convID string, pid int) (string, bool) {
	if pid > 0 {
		if parentConv, ok := s.pidToParent[pid]; ok && parentConv != "" {
			return parentConv, true
		}
	}
	if convID != "" {
		return convID, false
	}
	return "", false
}

// acquireSubagentGateLocked acquires the gate for a subagent request.
// Must be called with s.mu held. Returns with s.mu unlocked.
func (s *Serializer) acquireSubagentGateLocked(gateKey string, pid int, logLabel string) {
	gate := s.getSubagentGate(gateKey)
	s.mu.Unlock()
	gateStart := time.Now()
	select {
	case gate <- struct{}{}:
		// Acquired immediately.
	default:
		log.Printf("[SER] %s_WAIT pid=%d gate=%s", logLabel, pid, gateKey[:min(8, len(gateKey))])
		s.stats.mu.Lock()
		s.stats.RequestsSubagentGateWait++
		s.stats.mu.Unlock()
		gate <- struct{}{}
		log.Printf("[SER] %s_ACQUIRED pid=%d gate=%s wait=%.1fs",
			logLabel, pid, gateKey[:min(8, len(gateKey))], time.Since(gateStart).Seconds())
	}
}

// Acquire blocks until this session's request may proceed.
// convID is the conversation fingerprint.
// msgCount is the number of messages in the request (used for logging/metrics).
// pid is the CC process PID (0 if unknown — parent-session mapping disabled).
// Returns immediately if serialization is disabled or same-session.
func (s *Serializer) Acquire(convID string, msgCount int, pid int, info subagent.Classification) {
	s.mu.Lock()

	if !s.cfg.Enabled {
		// Batch serialization is off — concurrent sessions are unblocked.
		// But the per-parent subagent gate still applies: it prevents
		// parallel subagent requests from the SAME parent from interleaving
		// at Anthropic's cache layer. This gate does NOT block other
		// conversations — only same-parent subagents wait on each other.
		//
		// FIX-2026-3-24: Previously, Enabled=false bypassed ALL logic
		// including the subagent gate, leaving agent_tool subagents
		// unserialized. Production evidence: 15 cross-conv switches/day,
		// 900K cc tokens in rebuilds (TEST 11 confirms gate was dead).
		if info.IsSubagent {
			if gateKey, mappedToParent := s.resolveSubagentGateKey(convID, pid); gateKey != "" {
				logLabel := "SUBAGENT_FALLBACK_GATE_DISABLED"
				if mappedToParent {
					logLabel = "SUBAGENT_GATE_DISABLED"
				} else {
					log.Printf("[SER] SUBAGENT_FALLBACK (disabled mode) pid=%d type=%s gate=%s",
						pid, info.Type, gateKey[:min(8, len(gateKey))])
					s.stats.mu.Lock()
					s.stats.RequestsSubagentFallback++
					s.stats.mu.Unlock()
				}
				s.acquireSubagentGateLocked(gateKey, pid, logLabel)
				return
			}
		}
		// Main sessions: register PID → conv mapping so future subagents
		// from this PID can find their parent for gating.
		if !info.IsSubagent && pid > 0 {
			if len(s.pidToParent) >= 1000 {
				for k := range s.pidToParent {
					delete(s.pidToParent, k)
				}
			}
			s.pidToParent[pid] = convID
		}
		s.mu.Unlock()
		return
	}

	s.stats.mu.Lock()
	s.stats.RequestsTotal++
	s.stats.mu.Unlock()

	// --- Subagent handling ---
	if info.IsSubagent {
		s.stats.mu.Lock()
		s.stats.RequestsSubagent++
		s.stats.mu.Unlock()

		if gateKey, mappedToParent := s.resolveSubagentGateKey(convID, pid); gateKey != "" {
			if mappedToParent {
				// Use parent's conv_id so subagent serializes with parent.
				convID = gateKey
				log.Printf("[SER] SUBAGENT pid=%d type=%s → parent=%s msgs=%d", pid, info.Type, convID[:8], msgCount)

				// Gate: only one subagent in-flight per parent at a time.
				// Prevents parallel subagents from interleaving at Anthropic's
				// cache layer. Matches Anthropic's documented best practice:
				// "wait for first response before sending subsequent requests."
				s.acquireSubagentGateLocked(convID, pid, "SUBAGENT_GATE")
				// Subagents have a DIFFERENT system prompt prefix than main
				// sessions. Anthropic's KV cache has ~2-3 concurrent prefix
				// slots (LRU). A subagent request creates a competing cache
				// entry that can evict the main session's 100K+ cached prefix.
				//
				// Production evidence (2026-03-14): subagent cold start
				// (cache_read=0) caused main session cache_read to drop from
				// 154K → 18K with cache_create=114K (single event = $0.43).
				//
				// Fix: subagents must serialize with their parent via the main
				// batch queue (fall through below). The per-parent gate above
				// prevents parallel subagents; the batch queue below prevents
				// subagents from interleaving with the parent's active batch.
				s.mu.Lock() // re-acquire for the serialization logic below
				// Fall through to main serialization logic with convID = parentConv.
				// This means the subagent will:
				// - Pass through immediately if parent is the active session
				// - Queue and wait if another session is active
				//
				// FIX-2026-3-19: Track subagent inFlight separately so
				// shouldSwitch() can distinguish main vs subagent streams.
				// Subagent streams should NOT prevent batch switching.
				s.subagentInFlight++
				goto serializationLogic
			}

			// Unknown-PID or unmapped subagents still should not queue behind
			// main-session traffic, but a full bypass is too weak. Fall back to a
			// self-gate keyed by this request's own serializer convID so repeated
			// child requests from the same fallback lane cannot interleave freely.
			log.Printf("[SER] SUBAGENT_FALLBACK pid=%d type=%s gate=%s msgs=%d", pid, info.Type, gateKey[:8], msgCount)
			s.stats.mu.Lock()
			s.stats.RequestsSubagentFallback++
			s.stats.mu.Unlock()
			s.acquireSubagentGateLocked(gateKey, pid, "SUBAGENT_FALLBACK_GATE")
			s.stats.mu.Lock()
			s.stats.RequestsPassthrough++
			s.stats.mu.Unlock()
			return
		}

		// No usable gate key. Preserve current non-blocking behavior as the last resort.
		s.stats.mu.Lock()
		s.stats.RequestsSubagentNoGate++
		s.stats.RequestsPassthrough++
		s.stats.mu.Unlock()
		s.mu.Unlock()
		return
	} else if pid > 0 {
		// Main session — register PID → conv mapping.
		// Cap at 1000 entries to prevent unbounded growth from long-running proxies.
		if len(s.pidToParent) >= 1000 {
			for k := range s.pidToParent {
				delete(s.pidToParent, k)
			}
		}
		s.pidToParent[pid] = convID
	}

	// --- Serialization logic ---
serializationLogic:

	// No active session → claim immediately
	if s.activeConv == "" {
		s.activeConv = convID
		s.batchRemaining = s.cfg.BatchSize - 1
		s.lastActivity = time.Now()
		s.inFlight++
		s.stats.mu.Lock()
		s.stats.RequestsPassthrough++
		s.stats.mu.Unlock()
		log.Printf("[SER] CLAIM %s batch=%d", convID[:8], s.cfg.BatchSize)
		s.mu.Unlock()
		return
	}

	// Same session → pass through, decrement batch
	if convID == s.activeConv {
		if s.batchRemaining > 0 {
			s.batchRemaining--
		}
		s.lastActivity = time.Now()
		s.inFlight++
		s.stats.mu.Lock()
		s.stats.RequestsPassthrough++
		s.stats.mu.Unlock()
		s.mu.Unlock()
		return
	}

	// Different session — check if batch should switch now
	if s.shouldSwitch() {
		s.doSwitch(convID)
		s.inFlight++
		s.stats.mu.Lock()
		s.stats.RequestsPassthrough++
		s.stats.mu.Unlock()
		log.Printf("[SER] SWITCH-ON-ARRIVAL →%s batch=%d queued=%d",
			convID[:8], s.cfg.BatchSize, len(s.queue))
		s.mu.Unlock()
		return
	}

	// Queue this request — block outside the lock
	entry := &queueEntry{
		convID:      convID,
		release:     make(chan struct{}),
		enqueueTime: time.Now(),
	}
	s.queue = append(s.queue, entry)
	s.stats.mu.Lock()
	s.stats.RequestsQueued++
	s.stats.mu.Unlock()
	maxQ := s.cfg.MaxQueueSec
	log.Printf("[SER] QUEUE %s (active=%s remaining=%d depth=%d)",
		convID[:8], s.activeConv[:8], s.batchRemaining, len(s.queue))
	s.mu.Unlock()

	// Block until released or timeout
	timer := time.NewTimer(time.Duration(maxQ * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-entry.release:
		// Released by checker or switch — count as in-flight
		s.mu.Lock()
		s.inFlight++
		s.mu.Unlock()
	case <-timer.C:
		log.Printf("[SER] QUEUE_TIMEOUT %s after %.0fs", convID[:8], maxQ)
		s.stats.mu.Lock()
		s.stats.RequestsTimeout++
		s.stats.mu.Unlock()
	case <-s.stopCh:
		// Shutdown
	}
}

// Release signals the serializer that a request has completed.
// Decrements in-flight counter so idle-switch knows streaming is done.
// For subagent requests, also drains the per-parent gate.
func (s *Serializer) Release(convID string, pid int, info subagent.Classification) {
	s.mu.Lock()
	if s.inFlight > 0 {
		s.inFlight--
	}
	if info.IsSubagent {
		if s.subagentInFlight > 0 {
			s.subagentInFlight--
		}
		// Gate was acquired under the strongest available gate key:
		// parent convID when mapping exists, otherwise the subagent's own convID.
		gateKey := convID
		if parentConv, ok := s.pidToParent[pid]; ok && parentConv != "" {
			gateKey = parentConv
		}
		if g, ok := s.subagentGates[gateKey]; ok {
			select {
			case <-g:
			default:
			}
		}
	}
	s.mu.Unlock()
}

// shouldSwitch checks if we should switch away from active session.
// Must be called with s.mu held.
func (s *Serializer) shouldSwitch() bool {
	if s.activeConv == "" {
		return len(s.queue) > 0
	}
	if len(s.queue) == 0 {
		return false
	}
	// FIX-2026-3-19: Only main (non-subagent) in-flight requests block switching.
	// Subagent streams finish asynchronously and do not need the serializer's
	// "active" status to complete. Without this, subagents starve other sessions
	// by keeping inFlight > 0 indefinitely (observed: 120s queue timeouts).
	mainInFlight := s.inFlight - s.subagentInFlight
	if mainInFlight < 0 {
		mainInFlight = 0
	}
	if s.batchRemaining <= 0 && mainInFlight == 0 {
		return true
	}
	// Never idle-switch while a main response is still streaming.
	if mainInFlight > 0 {
		return false
	}
	if time.Since(s.lastActivity) > time.Duration(s.cfg.IdleTimeoutSec*float64(time.Second)) {
		return true
	}
	return false
}

// doSwitch switches to a new conv_id or dequeues the next waiting request.
// Must be called with s.mu held.
func (s *Serializer) doSwitch(newConvID string) {
	s.inFlight = 0         // reset: old conv requests will decrement but harmlessly clamp to 0
	s.subagentInFlight = 0 // reset subagent counter alongside main counter
	if newConvID != "" {
		s.activeConv = newConvID
		s.batchRemaining = s.cfg.BatchSize - 1
		s.lastActivity = time.Now()
		s.stats.mu.Lock()
		s.stats.BatchSwitches++
		s.stats.mu.Unlock()
		return
	}

	// Pop first from queue
	if len(s.queue) > 0 {
		entry := s.queue[0]
		s.queue = s.queue[1:]
		s.activeConv = entry.convID
		s.batchRemaining = s.cfg.BatchSize - 1
		s.lastActivity = time.Now()
		entry.tryRelease() // unblock the waiting goroutine
		s.stats.mu.Lock()
		s.stats.BatchSwitches++
		s.stats.RequestsReleased++
		s.stats.mu.Unlock()
		holdSec := time.Since(entry.enqueueTime).Seconds()
		log.Printf("[SER] RELEASED %s held=%.1fs queued=%d",
			entry.convID[:8], holdSec, len(s.queue))
	} else {
		s.activeConv = ""
		s.batchRemaining = 0
	}
}

// checkerLoop runs in the background, checking for idle switches and queue timeouts.
func (s *Serializer) checkerLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			s.releaseAll("shutdown")
			return
		case <-ticker.C:
			s.mu.Lock()
			if !s.cfg.Enabled {
				s.releaseAll("disabled")
				s.mu.Unlock()
				continue
			}

			// Idle switch
			if s.shouldSwitch() {
				log.Printf("[SER] IDLE-SWITCH away from %s (idle %.1fs, queued=%d)",
					s.activeConv[:8], time.Since(s.lastActivity).Seconds(), len(s.queue))
				s.doSwitch("")
			}

			// Queue timeouts
			maxQ := s.cfg.MaxQueueSec
			now := time.Now()
			var remaining []*queueEntry
			for _, entry := range s.queue {
				if now.Sub(entry.enqueueTime).Seconds() > maxQ {
					if entry.tryRelease() {
						s.stats.mu.Lock()
						s.stats.RequestsTimeout++
						s.stats.mu.Unlock()
						log.Printf("[SER] TIMEOUT force-released %s (%.0fs)",
							entry.convID[:8], now.Sub(entry.enqueueTime).Seconds())
					}
				} else {
					remaining = append(remaining, entry)
				}
			}
			s.queue = remaining
			s.mu.Unlock()
		}
	}
}

// releaseAll unblocks all queued requests. Must be called with s.mu held.
func (s *Serializer) releaseAll(reason string) {
	count := len(s.queue)
	for _, entry := range s.queue {
		if entry.tryRelease() {
			s.stats.mu.Lock()
			s.stats.RequestsReleased++
			s.stats.mu.Unlock()
		}
	}
	s.queue = nil
	s.activeConv = ""
	s.batchRemaining = 0
	s.inFlight = 0
	if count > 0 {
		log.Printf("[SER] RELEASE_ALL (%s): %d requests", reason, count)
	}
}

// ConvIDForClassification extracts a serializer lane fingerprint from an API
// request body using the provided ingress classification. This lets callers
// preserve PID/parent-aware decisions instead of re-classifying with less
// context later in the pipeline.
func ConvIDForClassification(body map[string]interface{}, info subagent.Classification) string {
	scopeKey := promptscope.Signature(body, promptscope.DefaultSystemPrefixChars)

	// Hash: system prefix + shared classifier scope
	scope := "main"
	if info.IsSubagent {
		scope = info.Type
		if scope == "" {
			scope = "sub"
		}
	}

	h := sha256.Sum256([]byte(scopeKey + ":" + scope))
	return hex.EncodeToString(h[:6])
}

// ConvID extracts a serializer lane fingerprint from an API request body.
// It uses a stronger prompt signature than the old "first system block only"
// hash so tool-bearing agent lanes that share a common preamble do not collapse
// into the same serializer scope.
func ConvID(body map[string]interface{}) string {
	return ConvIDForClassification(body, subagent.Classify(body))
}

// ConvIDWithPID returns a per-session convID by appending the PID.
// Two CC sessions with the same system prompt get different convIDs,
// allowing the serializer to batch them sequentially.
func ConvIDWithPID(body map[string]interface{}, pid int) string {
	base := ConvID(body)
	if pid > 0 {
		return fmt.Sprintf("%s_%d", base, pid)
	}
	return base
}

// ConvIDWithPIDAndInfo returns a per-session serializer convID using the
// ingress classification that was already computed for the request.
func ConvIDWithPIDAndInfo(body map[string]interface{}, pid int, info subagent.Classification) string {
	base := ConvIDForClassification(body, info)
	if pid > 0 {
		return fmt.Sprintf("%s_%d", base, pid)
	}
	return base
}

// MsgCount returns the number of messages in a parsed request body.
func MsgCount(body map[string]interface{}) int {
	if msgs, ok := body["messages"].([]interface{}); ok {
		return len(msgs)
	}
	return 0
}

// HealthJSON returns serializer state as JSON for the dashboard.
func (s *Serializer) HealthJSON() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	health := map[string]interface{}{
		"active_conv":     s.activeConv,
		"batch_remaining": s.batchRemaining,
		"queue_depth":     len(s.queue),
		"enabled":         s.cfg.Enabled,
		"batch_size":      s.cfg.BatchSize,
	}

	// Merge stats
	s.stats.mu.Lock()
	health["requests_total"] = s.stats.RequestsTotal
	health["requests_passthrough"] = s.stats.RequestsPassthrough
	health["requests_queued"] = s.stats.RequestsQueued
	health["requests_released"] = s.stats.RequestsReleased
	health["requests_timeout"] = s.stats.RequestsTimeout
	health["requests_subagent"] = s.stats.RequestsSubagent
	health["requests_subagent_fallback"] = s.stats.RequestsSubagentFallback
	health["requests_subagent_no_gate"] = s.stats.RequestsSubagentNoGate
	health["requests_subagent_gate_wait"] = s.stats.RequestsSubagentGateWait
	health["batch_switches"] = s.stats.BatchSwitches
	s.stats.mu.Unlock()

	data, _ := json.Marshal(health)
	return data
}
