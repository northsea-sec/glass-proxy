package glass

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"proxy.local/app/internal/subagent"
)

func TestResolvePersistedSessionKeyReusesPreRestartPIDLane(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	legacy := "legacylane_123"
	cache := NewLocalCache(legacy)
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
	}
	if err := cache.Save(cfg.ShadowDir); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	state := &SessionState{
		ConvID:        legacy,
		EvictedHashes: make(map[string]bool),
		CreatedAt:     engine.startedAt.Add(-time.Minute),
		UpdatedAt:     engine.startedAt.Add(-time.Minute),
	}
	if err := writeTestStateFile(cfg.ShadowDir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	old := engine.startedAt.Add(-time.Minute)
	for _, path := range []string{
		filepath.Join(cfg.ShadowDir, legacy),
		filepath.Join(cfg.ShadowDir, legacy, "localcache.json"),
		filepath.Join(cfg.ShadowDir, legacy, "state.json"),
	} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	got := engine.resolvePersistedSessionKey("newlane_123", nil, RequestMeta{ClientPID: 123}, subagent.Classification{})
	if got != legacy {
		t.Fatalf("expected legacy lane %q, got %q", legacy, got)
	}
}

func TestResolvePersistedSessionKeyPinsPIDToAdoptedLaneWithinProcess(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	legacy := "legacylane_456"
	cache := NewLocalCache(legacy)
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
	}
	if err := cache.Save(cfg.ShadowDir); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	state := &SessionState{
		ConvID:        legacy,
		EvictedHashes: make(map[string]bool),
		CreatedAt:     engine.startedAt.Add(-time.Minute),
		UpdatedAt:     engine.startedAt.Add(-time.Minute),
	}
	if err := writeTestStateFile(cfg.ShadowDir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	old := engine.startedAt.Add(-time.Minute)
	for _, path := range []string{
		filepath.Join(cfg.ShadowDir, legacy),
		filepath.Join(cfg.ShadowDir, legacy, "localcache.json"),
		filepath.Join(cfg.ShadowDir, legacy, "state.json"),
	} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	first := engine.resolvePersistedSessionKey("newlane_456", nil, RequestMeta{ClientPID: 456}, subagent.Classification{})
	if first != legacy {
		t.Fatalf("expected first request to adopt legacy lane %q, got %q", legacy, first)
	}

	second := engine.resolvePersistedSessionKey("otherlane_456", nil, RequestMeta{ClientPID: 456}, subagent.Classification{})
	if second != legacy {
		t.Fatalf("expected later requests in same pid to stay pinned to %q, got %q", legacy, second)
	}
}

func TestResolvePersistedSessionKeyPrefersMatchingHealthyLegacyLane(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	body := map[string]interface{}{
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "same live session system"},
		},
		"tools": []interface{}{
			map[string]interface{}{"name": "read"},
		},
	}
	targetSystem := hashJSON(body["system"])
	targetTools := hashJSON(body["tools"])

	writeCandidate := func(name string, evicted int, updated time.Time) {
		t.Helper()
		cache := NewLocalCache(name)
		cache.messages = []CachedMsg{{Msg: userTextMessage("hello"), Role: "user", Tokens: 10}}
		if err := cache.Save(cfg.ShadowDir); err != nil {
			t.Fatalf("save cache %s: %v", name, err)
		}
		state := &SessionState{
			ConvID:        name,
			EvictedHashes: make(map[string]bool),
			EvictedCount:  evicted,
			CreatedAt:     updated,
			UpdatedAt:     updated,
		}
		if err := writeTestStateFile(cfg.ShadowDir, state); err != nil {
			t.Fatalf("save state %s: %v", name, err)
		}
		diag := outboundPrefixDiagnosticEvent{
			Timestamp:      updated,
			ConversationID: name,
			ChangeKind:     "same",
			Snapshot: outboundPrefixSnapshot{
				Hash:       "prefix-" + name,
				SystemHash: targetSystem,
				ToolsHash:  targetTools,
			},
		}
		data, err := json.MarshalIndent(diag, "", "  ")
		if err != nil {
			t.Fatalf("marshal diag %s: %v", name, err)
		}
		dir := filepath.Join(cfg.ShadowDir, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, outboundPrefixLatestFile), data, 0644); err != nil {
			t.Fatalf("write diag %s: %v", name, err)
		}
		for _, path := range []string{
			dir,
			filepath.Join(dir, "localcache.json"),
			filepath.Join(dir, "state.json"),
			filepath.Join(dir, outboundPrefixLatestFile),
		} {
			if err := os.Chtimes(path, updated, updated); err != nil {
				t.Fatalf("chtimes %s: %v", path, err)
			}
		}
	}

	old := engine.startedAt.Add(-2 * time.Minute)
	writeCandidate("legacylane_789", 0, old)
	writeCandidate("remaplane_789", 34, old.Add(time.Minute))

	got := engine.resolvePersistedSessionKey("remaplane_789", body, RequestMeta{ClientPID: 789, SessionKey: "remaplane_789"}, subagent.Classification{})
	if got != "legacylane_789" {
		t.Fatalf("expected healthy matching legacy lane, got %q", got)
	}
}

func TestResolvePersistedSessionKeySkipsShadowBackedLegacyLane(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	legacy := "shadowlane_321"
	cache := NewLocalCache(legacy)
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("head"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("shadow"), Role: "assistant", Tokens: 50, IsReference: true},
		{Msg: userTextMessage("tail"), Role: "user", Tokens: 10},
	}
	if err := cache.Save(cfg.ShadowDir); err != nil {
		t.Fatalf("save cache: %v", err)
	}
	state := &SessionState{
		ConvID:        legacy,
		EvictedHashes: make(map[string]bool),
		EvictedCount:  3,
		CreatedAt:     engine.startedAt.Add(-time.Minute),
		UpdatedAt:     engine.startedAt.Add(-time.Minute),
	}
	if err := writeTestStateFile(cfg.ShadowDir, state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	old := engine.startedAt.Add(-time.Minute)
	for _, path := range []string{
		filepath.Join(cfg.ShadowDir, legacy),
		filepath.Join(cfg.ShadowDir, legacy, "localcache.json"),
		filepath.Join(cfg.ShadowDir, legacy, "state.json"),
	} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
	}

	got := engine.resolvePersistedSessionKey("newlane_321", nil, RequestMeta{ClientPID: 321}, subagent.Classification{})
	if got != "newlane_321" {
		t.Fatalf("expected shadow-backed legacy lane to be skipped, got %q", got)
	}
}

func writeTestStateFile(baseDir string, st *SessionState) error {
	dir := filepath.Join(baseDir, st.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "state.json"), data, 0644)
}

func TestComputeEvictionBudgetUsesFreshRealAPIInputWhenRecent(t *testing.T) {
	state := &SessionState{
		LastAPIInput:   160000,
		LastAPIInputAt: time.Now(),
	}

	budget := computeEvictionBudget(140000, state, 165000)
	if !budget.UsedRealAPIInput || !budget.UsedStaggerMargin {
		t.Fatalf("expected fresh real API input and stagger margin, got %+v", budget)
	}
	if got, want := budget.TotalTokens, 160000; got != want {
		t.Fatalf("expected total tokens %d, got %d", want, got)
	}
	if got, want := budget.Threshold, 155000; got != want {
		t.Fatalf("expected threshold %d, got %d", want, got)
	}
	if got, want := budget.ObservedOverhead, 20000; got != want {
		t.Fatalf("expected observed overhead %d, got %d", want, got)
	}

	state.EvictedCount = 4
	budget = computeEvictionBudget(140000, state, 165000)
	if !budget.UsedRealAPIInput || !budget.UsedStaggerMargin {
		t.Fatalf("expected evicted live sessions to keep using fresh real-token budget, got %+v", budget)
	}
	if got, want := budget.TotalTokens, 160000; got != want {
		t.Fatalf("expected estimated tokens %d, got %d", want, got)
	}
	if got, want := budget.Threshold, 155000; got != want {
		t.Fatalf("expected effective threshold %d, got %d", want, got)
	}
	if got, want := budget.ObservedOverhead, 20000; got != want {
		t.Fatalf("expected observed overhead %d after eviction, got %d", want, got)
	}
}

func TestComputeEvictionBudgetAlwaysUsesRealAPIInputWhenAvailable(t *testing.T) {
	// Real API tokens are always preferred over the json.Marshal/4 estimate,
	// regardless of staleness. A stale real count is still better than an
	// inflated estimate (which overestimates by ~1.8x).
	state := &SessionState{
		LastAPIInput:   161000,
		LastAPIInputAt: time.Now().Add(-lastAPIInputFreshWindow - time.Second),
	}

	budget := computeEvictionBudget(142000, state, 165000)
	if !budget.UsedRealAPIInput {
		t.Fatalf("expected real API input to be used even when stale, got %+v", budget)
	}
	if got, want := budget.TotalTokens, 161000; got != want {
		t.Fatalf("expected real API tokens %d, got %d", want, got)
	}
}

func TestComputeTargetMsgTokensAddsPostEvictionHeadroom(t *testing.T) {
	cfg := DefaultGlassConfig()
	state := &SessionState{EvictedCount: 8}
	budget := evictionBudget{ObservedOverhead: 18000}

	got := computeTargetMsgTokens(cfg, state, 10000, 5000, budget)
	if want := 97000; got != want {
		t.Fatalf("expected deeper post-eviction target %d, got %d", want, got)
	}

	got = computeTargetMsgTokens(cfg, &SessionState{}, 10000, 5000, budget)
	if want := 120000; got != want {
		t.Fatalf("expected pre-eviction target %d, got %d", want, got)
	}
}

func TestEnsureCacheStateCompatibleResetsMismatchedPersistedState(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("world"), Role: "assistant", Tokens: 10},
	}
	state := &SessionState{
		ConvID:        "conv",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  5,
		LastAPIInput:  160000,
		ReferenceUser: "stale",
		ReferenceAsst: "stale",
	}

	fresh := engine.ensureCacheStateCompatible("conv", cache, state)
	if fresh == cache {
		t.Fatal("expected mismatched persisted cache to be replaced")
	}
	if got := fresh.Len(); got != 0 {
		t.Fatalf("expected fresh cache to be empty, got %d messages", got)
	}
	if state.EvictedCount != 0 || state.LastAPIInput != 0 {
		t.Fatalf("expected state counters to reset, got evicted=%d last_api=%d", state.EvictedCount, state.LastAPIInput)
	}
	if state.ReferenceUser != "" || state.ReferenceAsst != "" {
		t.Fatalf("expected stale reference pair to be cleared, got %q / %q", state.ReferenceUser, state.ReferenceAsst)
	}
}

func TestEnsureCacheStateCompatibleResetsPersistedReferenceSnapshots(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.loadedFromDisk = true
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("head"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("shadow"), Role: "assistant", Tokens: 50, IsReference: true},
		{Msg: userTextMessage("tail"), Role: "user", Tokens: 10},
	}
	state := &SessionState{
		ConvID:         "conv",
		EvictedHashes:  make(map[string]bool),
		EvictedCount:   3,
		ReferenceUser:  "ref-user",
		ReferenceAsst:  "ref-asst",
		LastAPIInput:   150000,
		LastAPIInputAt: time.Now(),
	}

	fresh := engine.ensureCacheStateCompatible("conv", cache, state)
	if fresh == cache {
		t.Fatal("expected persisted ref-bearing cache to be replaced")
	}
	if got := fresh.Len(); got != 0 {
		t.Fatalf("expected fresh cache to be empty, got %d messages", got)
	}
	if state.EvictedCount != 0 || state.LastAPIInput != 0 {
		t.Fatalf("expected state counters to reset, got evicted=%d last_api=%d", state.EvictedCount, state.LastAPIInput)
	}
}

func TestEnsureCacheStateCompatibleKeepsPersistedRepairReferences(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.loadedFromDisk = true
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("head"), Role: "user", Hash: "head", Tokens: 10},
		{
			Msg:         userTextMessage("[Message 1 hidden by Glass repair: merged into msg[0] to fix consecutive user]"),
			Hash:        "repair_1_deadbeef",
			Role:        "user",
			Tokens:      50,
			IsReference: true,
		},
		{Msg: assistantTextMessage("tail"), Role: "assistant", Hash: "tail", Tokens: 10},
	}
	state := &SessionState{
		ConvID:        "conv",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  0,
		LastAPIInput:  120000,
	}

	if got := cache.ReferenceCount(); got != 1 {
		t.Fatalf("expected one total reference, got %d", got)
	}
	if got := cache.EvictionReferenceCount(); got != 0 {
		t.Fatalf("expected no eviction references, got %d", got)
	}
	got := engine.ensureCacheStateCompatible("conv", cache, state)
	if got != cache {
		t.Fatal("expected persisted repair-only cache to be resumable")
	}
	if state.LastAPIInput != 120000 {
		t.Fatalf("expected state to remain intact, got last_api=%d", state.LastAPIInput)
	}
}

func TestEnsureCacheStateCompatibleKeepsLiveReferenceSnapshots(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("head"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("shadow"), Role: "assistant", Tokens: 50, IsReference: true},
		{Msg: userTextMessage("tail"), Role: "user", Tokens: 10},
	}
	state := &SessionState{
		ConvID:        "conv",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  3,
	}

	got := engine.ensureCacheStateCompatible("conv", cache, state)
	if got != cache {
		t.Fatal("expected live in-memory ref-bearing cache to be kept")
	}
}

func TestProcessHandlesMissingSystemWithoutPanicking(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)
	engine.prefixWarmer = NewPrefixWarmer(PrefixWarmerConfig{
		ShadowDir:   cfg.ShadowDir,
		IntervalSec: 240,
		Upstream:    "https://api.anthropic.com",
	})
	defer engine.prefixWarmer.Stop()

	body := map[string]interface{}{
		"model":    "claude-opus-4-6",
		"messages": []interface{}{userTextMessage("ping")},
	}

	result := engine.Process(body, RequestMeta{SessionKey: "conv", RequestKey: "conv"})
	if result.InvalidRequestError != "" {
		t.Fatalf("unexpected invalid request error: %s", result.InvalidRequestError)
	}
}

func TestProcessResetsPoisonedCacheAfterInvalidOutboundRequest(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("ref"), Role: "user", Tokens: 10, IsReference: true},
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
		{Msg: assistantTextMessage("world"), Role: "assistant", Tokens: 10},
	}
	engine.caches["conv"] = cache

	state := &SessionState{
		ConvID:        "conv",
		EvictedHashes: make(map[string]bool),
		EvictedCount:  1,
	}
	engine.sessions.sessions["conv"] = state

	body := map[string]interface{}{
		"model": "claude-opus-4-6",
		"messages": []interface{}{
			userTextMessage("hello"),
			assistantTextMessage("world"),
			userTextMessage("next"),
		},
	}

	result := engine.Process(body, RequestMeta{SessionKey: "conv", RequestKey: "conv"})
	if result.InvalidRequestError != "" {
		t.Fatalf("expected outbound request to be valid after sanitizer repair, got %q", result.InvalidRequestError)
	}
	msgs, _ := body["messages"].([]interface{})
	if issues := validateOutboundRequestMessages(msgs); len(issues) > 0 {
		t.Fatalf("expected outbound request to validate cleanly, got %v", issues)
	}
}

func TestProcessRepairsPoisonedConsecutiveUserCacheBeforeBuild(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.messages = []CachedMsg{
		cachedTestMsg(userTextMessage("start"), 10),
		cachedTestMsg(assistantToolUseMessage("toolu_a", "bash"), 10),
		cachedTestMsg(userToolResultMessage("toolu_a", "done"), 10),
		cachedTestMsg(userTextMessage("continue"), 10),
	}
	engine.caches["conv"] = cache

	body := map[string]interface{}{
		"model": "claude-opus-4-6",
		"messages": []interface{}{
			userTextMessage("start"),
			assistantToolUseMessage("toolu_a", "bash"),
			userToolResultMessage("toolu_a", "done"),
			userTextMessage("continue"),
		},
	}

	result := engine.Process(body, RequestMeta{SessionKey: "conv", RequestKey: "conv"})
	if result.InvalidRequestError != "" {
		t.Fatalf("expected repair to avoid outbound request error, got %q", result.InvalidRequestError)
	}

	msgs, _ := body["messages"].([]interface{})
	if issues := validateOutboundRequestMessages(msgs); len(issues) > 0 {
		t.Fatalf("expected repaired outbound request to validate cleanly, got %v", issues)
	}
	if got, want := len(msgs), 3; got != want {
		t.Fatalf("expected merged outbound view length %d, got %d", want, got)
	}
	if !cache.messages[3].IsReference {
		t.Fatal("expected absorbed trailing user message to remain as a reference placeholder")
	}
}

func TestResetConversationClearsDiagnosticsAndEvictedState(t *testing.T) {
	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	cache := NewLocalCache("conv")
	cache.messages = []CachedMsg{
		{Msg: userTextMessage("hello"), Role: "user", Tokens: 10},
	}
	engine.caches["conv"] = cache
	engine.logFinalPrefixDiagnostic("conv", map[string]interface{}{
		"system":   mainSessionSystem(),
		"tools":    sharedTools(),
		"messages": []interface{}{userTextMessage("hello"), assistantTextMessage("world"), userTextMessage("next")},
	}, 2, outboundPrefixDiagnosticMeta{RequestKey: "req-reset", CacheMessages: cache.Len(), EvictedCount: 3, BatchCount: 1})
	engine.coldGates["conv"] = &coldGate{lastWarm: time.Now()}
	engine.sessions.sessions["conv"] = &SessionState{
		ConvID:        "conv",
		EvictedHashes: map[string]bool{"deadbeef": true},
		EvictedCount:  3,
		LastAPIInput:  150000,
		ReferenceUser: "ref-user",
		ReferenceAsst: "ref-asst",
		CreatedAt:     time.Now(),
	}

	if !engine.HasEvictedState("conv") {
		t.Fatal("expected conversation to be shadow-backed before reset")
	}
	latestPath, eventsPath := outboundPrefixDiagnosticPaths(cfg.ShadowDir, "conv")
	if _, err := os.Stat(latestPath); err != nil {
		t.Fatalf("expected persisted latest prefix snapshot, got %v", err)
	}
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("expected persisted prefix event history, got %v", err)
	}

	engine.ResetConversation("conv", "test reset")

	if engine.HasEvictedState("conv") {
		t.Fatal("expected conversation to be fresh after reset")
	}
	if got := engine.caches["conv"].Len(); got != 0 {
		t.Fatalf("expected empty cache after reset, got %d", got)
	}
	if _, ok := engine.outboundPrefixes["conv"]; ok {
		t.Fatal("expected outbound prefix diagnostics to be cleared")
	}
	if _, err := os.Stat(latestPath); !os.IsNotExist(err) {
		t.Fatalf("expected latest prefix snapshot to be removed on reset, got err=%v", err)
	}
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("expected prefix event history to be preserved, got %v", err)
	}
	if gate := engine.coldGates["conv"]; gate != nil && !gate.lastWarm.IsZero() {
		t.Fatalf("expected cold gate warm marker to be cleared, got %v", gate.lastWarm)
	}
	state := engine.sessions.sessions["conv"]
	if state.EvictedCount != 0 || state.LastAPIInput != 0 {
		t.Fatalf("expected session state counters to reset, got evicted=%d last_api=%d", state.EvictedCount, state.LastAPIInput)
	}
	if state.ReferenceUser != "" || state.ReferenceAsst != "" {
		t.Fatalf("expected reference pair to be cleared, got %q / %q", state.ReferenceUser, state.ReferenceAsst)
	}
	if _, err := os.Stat(filepath.Join(cfg.ShadowDir, "conv", "localcache.json")); err != nil {
		t.Fatalf("expected fresh localcache snapshot after reset, got %v", err)
	}
}
