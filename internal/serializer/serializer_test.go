package serializer

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"proxy.local/app/internal/subagent"
)

func TestConfigAcceptsSerializerEnabledAlias(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false

	if err := json.Unmarshal([]byte(`{"serializer_enabled":true}`), &cfg); err != nil {
		t.Fatalf("unmarshal serializer config: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("serializer_enabled alias should enable serializer")
	}
}

func TestSerializerSameSessionPassthrough(t *testing.T) {
	s := New(DefaultConfig())
	defer s.Stop()

	// Same conv_id should always pass through without blocking
	for i := 0; i < 10; i++ {
		s.Acquire("conv_aaa", 50, 0, subagent.Classification{})
		s.Release("conv_aaa", 0, subagent.Classification{})
	}

	stats := s.GetStats()
	if stats.RequestsPassthrough != 10 {
		t.Errorf("expected 10 passthrough, got %d", stats.RequestsPassthrough)
	}
	if stats.RequestsQueued != 0 {
		t.Errorf("expected 0 queued, got %d", stats.RequestsQueued)
	}
}

func TestSerializerDifferentSessionQueues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BatchSize = 3
	cfg.IdleTimeoutSec = 10.0 // long idle so it doesn't auto-switch
	s := New(cfg)
	defer s.Stop()

	// Session A claims
	s.Acquire("conv_aaa", 50, 0, subagent.Classification{})

	// Session B should queue (batch not exhausted, not idle)
	var queued atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		queued.Store(1)
		s.Acquire("conv_bbb", 50, 0, subagent.Classification{})
		// We get here when released
	}()

	// Give goroutine time to enqueue
	time.Sleep(50 * time.Millisecond)

	stats := s.GetStats()
	if stats.RequestsQueued != 1 {
		t.Errorf("expected 1 queued, got %d", stats.RequestsQueued)
	}

	// Exhaust A's batch (2 more calls = 3 total including initial claim)
	s.Acquire("conv_aaa", 50, 0, subagent.Classification{})
	s.Acquire("conv_aaa", 50, 0, subagent.Classification{})

	// Simulate all A's requests completing (Release decrements in-flight counter)
	noSub := subagent.Classification{}
	s.Release("conv_aaa", 0, noSub)
	s.Release("conv_aaa", 0, noSub)
	s.Release("conv_aaa", 0, noSub)

	// Now A's batch is exhausted and no requests in-flight.
	// Wait for the checker (1s tick) to notice idle+exhausted → switch to B
	time.Sleep(2 * time.Second)

	// B should have been released
	wg.Wait()

	stats = s.GetStats()
	if stats.BatchSwitches == 0 {
		t.Error("expected at least 1 batch switch")
	}
}

func TestSerializerSubagentBypass(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SubagentMsgThreshold = 20
	s := New(cfg)
	defer s.Stop()

	// Session A claims
	s.Acquire("conv_aaa", 50, 100, subagent.Classification{}) // pid=100, main session

	// Subagent with unknown parent → bypass
	s.Acquire("conv_sub", 5, 200, subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem}) // pid=200, not registered as parent

	stats := s.GetStats()
	if stats.RequestsSubagent != 1 {
		t.Errorf("expected 1 subagent request, got %d", stats.RequestsSubagent)
	}
}

func TestSerializerSubagentUnknownPIDBypassesActiveBatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BatchSize = 100
	s := New(cfg)
	defer s.Stop()

	// Keep a main session active so a falling-through subagent would queue.
	s.Acquire("conv_parent", 50, 100, subagent.Classification{})

	done := make(chan struct{})
	go func() {
		s.Acquire("conv_sub_unknown_pid", 5, 0, subagent.Classification{
			IsSubagent: true,
			Type:       subagent.TypeSmallSystem,
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subagent with unknown pid should bypass the active batch")
	}

	stats := s.GetStats()
	if stats.RequestsSubagent != 1 {
		t.Errorf("expected 1 subagent request, got %d", stats.RequestsSubagent)
	}
	if stats.RequestsSubagentFallback != 1 {
		t.Errorf("expected 1 subagent fallback, got %d", stats.RequestsSubagentFallback)
	}
	if stats.RequestsQueued != 0 {
		t.Errorf("expected 0 queued requests, got %d", stats.RequestsQueued)
	}
}

func TestSerializerSubagentUnknownPIDUsesFallbackGate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BatchSize = 100
	s := New(cfg)
	defer s.Stop()

	// Keep a main session active so the fallback path must avoid the global queue.
	s.Acquire("conv_parent", 50, 100, subagent.Classification{})

	subInfo := subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem}
	s.Acquire("conv_sub_unknown_pid", 5, 0, subInfo)

	var acquired atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Acquire("conv_sub_unknown_pid", 5, 0, subInfo)
		acquired.Store(1)
	}()

	time.Sleep(100 * time.Millisecond)
	if acquired.Load() != 0 {
		t.Fatal("second unknown-pid subagent should be blocked by fallback gate")
	}

	stats := s.GetStats()
	if stats.RequestsSubagentGateWait != 1 {
		t.Errorf("expected 1 fallback gate wait, got %d", stats.RequestsSubagentGateWait)
	}
	if stats.RequestsSubagentFallback != 2 {
		t.Errorf("expected 2 subagent fallback acquires, got %d", stats.RequestsSubagentFallback)
	}
	if stats.RequestsQueued != 0 {
		t.Errorf("expected 0 queued requests, got %d", stats.RequestsQueued)
	}

	s.Release("conv_sub_unknown_pid", 0, subInfo)
	wg.Wait()
	if acquired.Load() != 1 {
		t.Fatal("second unknown-pid subagent should acquire after release")
	}
}

func TestSerializerDisabledSubagentUnknownPIDUsesFallbackGate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	s := New(cfg)
	defer s.Stop()

	subInfo := subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem}
	s.Acquire("conv_sub_unknown_pid", 5, 0, subInfo)

	var acquired atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Acquire("conv_sub_unknown_pid", 5, 0, subInfo)
		acquired.Store(1)
	}()

	time.Sleep(100 * time.Millisecond)
	if acquired.Load() != 0 {
		t.Fatal("disabled-mode fallback gate should block parallel unknown-pid subagent")
	}

	stats := s.GetStats()
	if stats.RequestsSubagentFallback != 2 {
		t.Errorf("expected 2 disabled-mode subagent fallback acquires, got %d", stats.RequestsSubagentFallback)
	}

	s.Release("conv_sub_unknown_pid", 0, subInfo)
	wg.Wait()
	if acquired.Load() != 1 {
		t.Fatal("disabled-mode fallback gate should release waiting subagent")
	}
}

func TestSerializerSubagentNoGateTracksLastResortPassthrough(t *testing.T) {
	cfg := DefaultConfig()
	s := New(cfg)
	defer s.Stop()

	s.Acquire("", 5, 0, subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem})

	stats := s.GetStats()
	if stats.RequestsSubagentNoGate != 1 {
		t.Fatalf("expected 1 subagent no-gate fallback, got %d", stats.RequestsSubagentNoGate)
	}
	if stats.RequestsPassthrough != 1 {
		t.Fatalf("expected 1 passthrough for no-gate fallback, got %d", stats.RequestsPassthrough)
	}
}

func TestSerializerSubagentSerializesUnderParent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SubagentMsgThreshold = 20
	cfg.BatchSize = 100 // large batch so no switching
	s := New(cfg)
	defer s.Stop()

	// Register PID 100 as parent with conv "conv_parent"
	s.Acquire("conv_parent", 50, 100, subagent.Classification{})

	// Subagent from same PID (100) with few messages → should serialize under parent
	s.Acquire("conv_sub_xxx", 5, 100, subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem})

	stats := s.GetStats()
	if stats.RequestsSubagent != 1 {
		t.Errorf("expected 1 subagent request, got %d", stats.RequestsSubagent)
	}
	// Should pass through (same as parent = same as active)
	if stats.RequestsPassthrough != 2 {
		t.Errorf("expected 2 passthrough (parent + subagent), got %d", stats.RequestsPassthrough)
	}
}

func TestSerializerDisabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	s := New(cfg)
	defer s.Stop()

	// Should not block at all
	s.Acquire("conv_aaa", 50, 0, subagent.Classification{})
	s.Acquire("conv_bbb", 50, 0, subagent.Classification{})

	stats := s.GetStats()
	// When disabled, requests don't go through serialization at all
	if stats.RequestsTotal != 0 {
		t.Errorf("expected 0 total when disabled, got %d", stats.RequestsTotal)
	}
}

func TestSerializerSubagentGateLimitsParallelism(t *testing.T) {
	cfg := DefaultConfig()
	cfg.BatchSize = 100 // large batch so no switching
	s := New(cfg)
	defer s.Stop()

	parentConvID := "conv_parent"
	subInfo := subagent.Classification{IsSubagent: true, Type: subagent.TypeSmallSystem}

	// Register PID 100 as parent
	s.Acquire(parentConvID, 50, 100, subagent.Classification{})

	// First subagent acquires gate immediately
	s.Acquire("conv_sub_a", 5, 100, subInfo)

	// Second subagent from same PID should block on the gate
	var acquired atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Acquire("conv_sub_b", 5, 100, subInfo)
		acquired.Store(1)
	}()

	// Give goroutine time to block
	time.Sleep(100 * time.Millisecond)

	if acquired.Load() != 0 {
		t.Fatal("second subagent should be blocked by gate")
	}

	stats := s.GetStats()
	if stats.RequestsSubagentGateWait != 1 {
		t.Errorf("expected 1 gate wait, got %d", stats.RequestsSubagentGateWait)
	}

	// Release using the real caller path: subagent convID + parent PID.
	s.Release("conv_sub_a", 100, subInfo)

	wg.Wait()
	if acquired.Load() != 1 {
		t.Fatal("second subagent should have acquired after release")
	}
}
func TestConvID(t *testing.T) {
	// Same system + high msg count → same conv_id
	body1 := map[string]interface{}{
		"system":   "You are a helpful assistant",
		"tools":    []interface{}{map[string]interface{}{"name": "read"}},
		"messages": make([]interface{}, 50),
	}
	body2 := map[string]interface{}{
		"system":   "You are a helpful assistant",
		"tools":    []interface{}{map[string]interface{}{"name": "read"}},
		"messages": make([]interface{}, 60),
	}
	if ConvID(body1) != ConvID(body2) {
		t.Error("same system + both main scope should produce same conv_id")
	}

	// Same system + model-routed subagent → different conv_id (sub scope)
	body3 := map[string]interface{}{
		"system":   "You are a helpful assistant",
		"model":    "claude-haiku-4-5",
		"tools":    []interface{}{map[string]interface{}{"name": "read"}},
		"messages": make([]interface{}, 50),
	}
	if ConvID(body1) == ConvID(body3) {
		t.Error("main scope vs sub scope should produce different conv_id")
	}

	// Different system → different conv_id
	body4 := map[string]interface{}{
		"system":   "You are a code reviewer",
		"tools":    []interface{}{map[string]interface{}{"name": "read"}},
		"messages": make([]interface{}, 50),
	}
	if ConvID(body1) == ConvID(body4) {
		t.Error("different system prompts should produce different conv_id")
	}

	body5 := map[string]interface{}{
		"system": "You are a helpful assistant",
		"tools": []interface{}{
			map[string]interface{}{"name": "read"},
			map[string]interface{}{"name": "bash"},
		},
		"messages": make([]interface{}, 50),
	}
	if ConvID(body1) == ConvID(body5) {
		t.Error("different tool sets should produce different conv_id")
	}
}

func TestConvIDWithPIDAndInfoUsesIngressClassification(t *testing.T) {
	t.Parallel()

	systemPrompt := strings.Repeat("This is the full Claude Code system prompt. ", 300)
	parentBody := map[string]interface{}{
		"system": systemPrompt,
		"tools": []interface{}{
			map[string]interface{}{"name": "read"},
		},
		"messages": make([]interface{}, 50),
	}
	agentToolBody := map[string]interface{}{
		"system": systemPrompt,
		"tools": []interface{}{
			map[string]interface{}{"name": "read"},
		},
		"messages": make([]interface{}, 1),
	}

	parentInfo := subagent.Classify(parentBody)
	agentToolInfo := subagent.Classify(agentToolBody, subagent.ClassifyOpts{HasEstablishedParent: true})
	if !agentToolInfo.IsSubagent || agentToolInfo.Type != subagent.TypeAgentTool {
		t.Fatalf("expected agent_tool classification with established parent, got %+v", agentToolInfo)
	}

	parentConv := ConvIDWithPIDAndInfo(parentBody, 123, parentInfo)
	legacyAgentConv := ConvIDWithPID(agentToolBody, 123)
	ingressAgentConv := ConvIDWithPIDAndInfo(agentToolBody, 123, agentToolInfo)

	if legacyAgentConv != parentConv {
		t.Fatalf("expected legacy conv id to collapse into main scope, got %q want %q", legacyAgentConv, parentConv)
	}
	if ingressAgentConv == parentConv {
		t.Fatalf("expected ingress-aware conv id to separate agent_tool from parent scope, both were %q", ingressAgentConv)
	}
}
