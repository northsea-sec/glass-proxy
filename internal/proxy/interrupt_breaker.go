// interrupt_breaker.go — Hard block on runaway loop traffic after user interrupt.
//
// State machine per conversation:
//
//	CLEAR → (stream aborted, no stop_reason) → INTERRUPTED
//	INTERRUPTED → (next request has fresh user input) → ARMED (let it through)
//	INTERRUPTED → (next request has no fresh input) → BLOCK and stay INTERRUPTED
//	ARMED → (next request has no fresh input) → BLOCK with fake end_turn
//	ARMED → (next request has fresh user input) → CLEAR and allow
//	ARMED → (response completes with end_turn) → CLEAR
//	Any state → (response completes with end_turn) → CLEAR
package proxy

import (
	"log"
	"sync"
	"time"
)

type interruptPhase int

const (
	phaseCleared     interruptPhase = iota
	phaseInterrupted                // stream was aborted — next request gets through
	phaseArmed                      // one request was allowed — next tool_use loop gets blocked
)

type interruptTracker struct {
	mu    sync.Mutex
	phase interruptPhase
	at    time.Time // when the interrupt was detected
}

// interruptBreaker manages per-conversation interrupt state.
type interruptBreaker struct {
	convs sync.Map // convID -> *interruptTracker
}

func newInterruptBreaker() *interruptBreaker {
	return &interruptBreaker{}
}

// StreamAborted is called when a stream ends without a stop_reason (client disconnected).
func (ib *interruptBreaker) StreamAborted(convID string) {
	if convID == "" {
		return
	}
	t := ib.getOrCreate(convID)
	t.mu.Lock()
	defer t.mu.Unlock()

	// Only transition from CLEAR → INTERRUPTED.
	// If already ARMED, keep it — the model is still running after the first allowed request.
	if t.phase == phaseCleared {
		t.phase = phaseInterrupted
		t.at = time.Now()
		log.Printf("[INTERRUPT-BREAKER] conv=%s phase=INTERRUPTED (stream aborted)", truncID(convID))
	}
}

// CheckRequest is called before forwarding a request. Returns true if the request should be blocked.
// hasToolUse indicates the request contains tool_use blocks in assistant messages.
// hasFreshInput indicates Glass ingested new messages from the client on this request.
func (ib *interruptBreaker) CheckRequest(convID string, hasToolUse bool, hasFreshInput bool) (block bool) {
	if convID == "" {
		return false
	}
	v, ok := ib.convs.Load(convID)
	if !ok {
		return false
	}
	t := v.(*interruptTracker)
	t.mu.Lock()
	defer t.mu.Unlock()

	switch t.phase {
	case phaseCleared:
		return false

	case phaseInterrupted:
		if !hasFreshInput {
			log.Printf("[INTERRUPT-BREAKER] conv=%s BLOCKING stale post-interrupt request (no fresh input, %.1fs since interrupt)",
				truncID(convID), time.Since(t.at).Seconds())
			return true
		}

		// Allow the first fresh-input request through. This is the user's real
		// follow-up turn, even if historical assistant tool_use blocks are still
		// present in the resent transcript.
		t.phase = phaseArmed
		log.Printf("[INTERRUPT-BREAKER] conv=%s phase=ARMED (allowed fresh post-interrupt request through)", truncID(convID))
		return false

	case phaseArmed:
		if hasFreshInput {
			t.phase = phaseCleared
			log.Printf("[INTERRUPT-BREAKER] conv=%s phase=CLEAR (fresh input while armed)", truncID(convID))
			return false
		}

		reason := "stale continuation"
		if hasToolUse {
			reason = "tool-loop continuation"
		}
		log.Printf("[INTERRUPT-BREAKER] conv=%s BLOCKING %s (armed, %.1fs since interrupt)",
			truncID(convID), reason, time.Since(t.at).Seconds())
		// Stay ARMED — if CC keeps retrying stale continuations, block them all.
		return true
	}

	return false
}

// StreamCompleted is called when a stream finishes with a stop_reason.
func (ib *interruptBreaker) StreamCompleted(convID, stopReason string) {
	if convID == "" || stopReason == "" {
		return
	}
	v, ok := ib.convs.Load(convID)
	if !ok {
		return
	}
	t := v.(*interruptTracker)
	t.mu.Lock()
	defer t.mu.Unlock()

	if (stopReason == "end_turn" || stopReason == "max_tokens") && t.phase != phaseCleared {
		log.Printf("[INTERRUPT-BREAKER] conv=%s phase=CLEAR (stream completed with %s)", truncID(convID), stopReason)
		t.phase = phaseCleared
	}
}

func (ib *interruptBreaker) getOrCreate(convID string) *interruptTracker {
	v, ok := ib.convs.Load(convID)
	if ok {
		return v.(*interruptTracker)
	}
	t := &interruptTracker{}
	v, _ = ib.convs.LoadOrStore(convID, t)
	return v.(*interruptTracker)
}

func truncID(s string) string {
	if len(s) > 20 {
		return s[:20]
	}
	return s
}
