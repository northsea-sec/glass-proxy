package proxy

import "testing"

func TestInterruptBreakerBlocksStaleRequestsUntilFreshInputArrives(t *testing.T) {
	t.Parallel()

	ib := newInterruptBreaker()
	convID := "conv"
	ib.StreamAborted(convID)

	if !ib.CheckRequest(convID, false, false) {
		t.Fatal("expected stale no-input request to be blocked while interrupted")
	}

	if ib.CheckRequest(convID, true, true) {
		t.Fatal("expected fresh input with historical tool_use to be allowed after interrupt")
	}

	if !ib.CheckRequest(convID, false, false) {
		t.Fatal("expected stale continuation to be blocked while armed")
	}
}

func TestInterruptBreakerAllowsFreshInputWhileArmedAndClears(t *testing.T) {
	t.Parallel()

	ib := newInterruptBreaker()
	convID := "conv"
	ib.StreamAborted(convID)

	if ib.CheckRequest(convID, false, true) {
		t.Fatal("expected first fresh request after interrupt to be allowed")
	}

	if ib.CheckRequest(convID, true, true) {
		t.Fatal("expected fresh follow-up input to be allowed while armed")
	}

	if ib.CheckRequest(convID, false, false) {
		t.Fatal("expected breaker to be cleared after fresh follow-up input")
	}
}

func TestInterruptBreakerClearsOnCompletedStream(t *testing.T) {
	t.Parallel()

	ib := newInterruptBreaker()
	convID := "conv"
	ib.StreamAborted(convID)

	if ib.CheckRequest(convID, false, true) {
		t.Fatal("expected fresh request after interrupt to be allowed")
	}

	ib.StreamCompleted(convID, "end_turn")

	if ib.CheckRequest(convID, false, false) {
		t.Fatal("expected completed stream to clear breaker state")
	}
}
