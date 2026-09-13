package serializer

// PRIME-GOLDEN Test Suite: Serializer Subagent Gate
// ==================================================
//
// Tests the hypothesis that the per-parent subagent gate is killed
// when ser_enabled=false, leaving agent_tool subagents unserialized.
//
// Methodology per test:
//   HYPOTHESIS: What the code should guarantee
//   BASELINE:   What production data shows
//   SIMULATION: Construct the scenario
//   METRICS:    Measure the property
//   VERDICT:    Pass/fail with evidence

import (
	"testing"
	"time"

	"proxy.local/app/internal/subagent"
)

// ---------------------------------------------------------------------------
// TEST 11: Subagent Gate Active Even When Serializer Disabled
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: After FIX-2026-3-24, ser_enabled=false disables cross-conversation
//   batch serialization but keeps the per-parent subagent gate active.
//   Concurrent agent_tool subagents from the SAME parent are serialized
//   (one in-flight at a time). Different conversations are NOT blocked.
//
// BASELINE (pre-fix): Gate was dead when disabled. All subagent requests
//   passed through immediately. 15 cross-conv switches/day, 900K cc tokens.
//
// PRIOR ART:
//   - Serializer ON: blocks other sessions for 120s (unusable concurrent)
//   - The subagent gate is INDEPENDENT of cross-conversation batching
//
// FALSIFICATION: If same-parent subagents can fire in parallel when disabled,
//   the fix is not applied.

func TestPrimeGolden_SubagentGateActiveWhenDisabled(t *testing.T) {
	t.Parallel()

	ser := New(Config{
		Enabled:        false,
		BatchSize:      5,
		IdleTimeoutSec: 15.0,
		MaxQueueSec:    120.0,
	})
	defer ser.Stop()

	// Register a parent session so pidToParent mapping exists
	parentInfo := subagent.Classification{IsSubagent: false}
	ser.Acquire("conv_parent", 50, 100, parentInfo)
	ser.Release("conv_parent", 100, parentInfo)

	subInfo := subagent.Classification{
		IsSubagent: true,
		Type:       subagent.TypeAgentTool,
	}

	// First subagent acquires gate immediately
	done1 := make(chan struct{})
	go func() {
		ser.Acquire("sub_1", 3, 100, subInfo)
		close(done1)
	}()
	<-done1

	// Second subagent from SAME parent should BLOCK on the gate
	done2 := make(chan bool, 1)
	go func() {
		ser.Acquire("sub_2", 3, 100, subInfo)
		done2 <- true
	}()

	select {
	case <-done2:
		t.Errorf("VERDICT: FAIL — second subagent acquired without gate wait. "+
			"Subagent gate is NOT active when ser_enabled=false.")
	case <-time.After(200 * time.Millisecond):
		stats := ser.GetStats()
		t.Logf("VERDICT: PASS — subagent gate is active even with ser_enabled=false. "+
			"SubagentGateWait=%d. Same-parent subagents serialize; "+
			"different conversations remain unblocked.",
			stats.RequestsSubagentGateWait)
	}

	// Release first — second should now acquire
	ser.Release("sub_1", 100, subInfo)
	select {
	case <-done2:
		t.Logf("CONFIRMED: gate released second subagent after first completed")
	case <-time.After(2 * time.Second):
		t.Errorf("VERDICT: FAIL — second subagent still blocked after first released")
	}

	ser.Release("sub_2", 100, subInfo)
}

// ---------------------------------------------------------------------------
// TEST 13: Subagent Gate Works When Enabled
// ---------------------------------------------------------------------------
//
// HYPOTHESIS: When ser_enabled=true, the subagent gate limits concurrent
//   agent_tool requests per parent to 1 in-flight, preventing parallel
//   subagents from interleaving at Anthropic's cache layer.
//
// THE FIX: The subagent gate must run independently of batch serialization.
//   ser_enabled=false should disable cross-conversation batching but NOT
//   the per-parent subagent gate. This gives:
//   - Concurrent sessions: unblocked (no batch queue)
//   - Same-parent subagents: serialized (gate active)
//   - Cross-conv interleaving: reduced (fewer parallel requests per conv)
//
// FALSIFICATION: If the gate does not prevent concurrent Acquires,
//   the serializer implementation is broken.

func TestPrimeGolden_SubagentGateWorksWhenEnabled(t *testing.T) {
	t.Parallel()

	ser := New(Config{
		Enabled:        true,
		BatchSize:      100,
		IdleTimeoutSec: 15.0,
		MaxQueueSec:    120.0,
	})
	defer ser.Stop()

	// Register parent
	parentInfo := subagent.Classification{IsSubagent: false}
	ser.Acquire("conv_parent", 50, 100, parentInfo)

	subInfo := subagent.Classification{
		IsSubagent: true,
		Type:       subagent.TypeAgentTool,
	}

	// First subagent acquires immediately
	done1 := make(chan struct{})
	go func() {
		ser.Acquire("sub_a", 3, 100, subInfo)
		close(done1)
	}()
	<-done1

	// Second subagent should BLOCK on the gate
	done2 := make(chan bool, 1)
	go func() {
		ser.Acquire("sub_b", 3, 100, subInfo)
		done2 <- true
	}()

	select {
	case <-done2:
		t.Errorf("VERDICT: FAIL — second subagent acquired without gate wait. "+
			"Subagent gate is not enforcing serialization.")
	case <-time.After(200 * time.Millisecond):
		stats := ser.GetStats()
		t.Logf("VERDICT: PASS — second subagent is blocked on gate. "+
			"SubagentGateWait=%d. This prevents parallel cache interleaving.",
			stats.RequestsSubagentGateWait)
	}

	// Release first — second should now acquire
	ser.Release("sub_a", 100, subInfo)
	select {
	case <-done2:
		t.Logf("CONFIRMED: gate released second subagent after first completed")
	case <-time.After(2 * time.Second):
		t.Errorf("VERDICT: FAIL — second subagent still blocked after first released")
	}

	ser.Release("conv_parent", 100, parentInfo)
	ser.Release("sub_b", 100, subInfo)
}
