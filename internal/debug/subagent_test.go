package debug

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"proxy.local/app/internal/runtimepaths"
)

func TestGetLatestRequestIncludesSubagentType(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{
		SessionID:    "sess-1",
		RequestID:    "req-1",
		IsSubagent:   true,
		SubagentType: "small_system",
	}); err != nil {
		t.Fatalf("record request: %v", err)
	}

	latest, err := rec.GetLatestRequest()
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest == nil {
		t.Fatal("expected latest request")
	}
	if !latest.IsSubagent || latest.SubagentType != "small_system" {
		t.Fatalf("unexpected latest subagent payload: %+v", latest)
	}
}

func TestGetLatestRequestIgnoresStandaloneSubagentEvents(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{
		SessionID:      "sess-1",
		RequestID:      "req-main",
		ModelRequested: "claude-opus-4-6",
	}); err != nil {
		t.Fatalf("record request: %v", err)
	}
	if err := rec.RecordSubagentEvent(&SubagentEvent{
		SessionID:      "sess-1",
		RequestID:      "req-blocked",
		ModelRequested: "claude-haiku-4-5",
		SubagentType:   "haiku",
		BlockedByModel: true,
	}); err != nil {
		t.Fatalf("record subagent event: %v", err)
	}

	latest, err := rec.GetLatestRequest()
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest == nil {
		t.Fatal("expected latest request")
	}
	if latest.RequestID != "req-main" {
		t.Fatalf("expected main request to remain latest status source, got %+v", latest)
	}
}

func TestGetLatestRequestIncludesQuotaAndStatusFields(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{
		SessionID:        "sess-1",
		RequestID:        "req-1",
		RequestLane:      "codex",
		RequestTransport: "codex_responses",
		HasToolUse:       true,
		CFEdgeLocation:   "AMS",
		RLBindingWindow:  "seven_day",
		RL5hUtil:         27,
		RL5hStatus:       "allowed",
		RL7dUtil:         31,
		RL7dStatus:       "allowed",
		RLOverall:        "allowed",
	}); err != nil {
		t.Fatalf("record request: %v", err)
	}

	latest, err := rec.GetLatestRequest()
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if latest == nil {
		t.Fatal("expected latest request")
	}
	if !latest.HasToolUse || latest.CFEdgeLocation != "AMS" {
		t.Fatalf("expected latest request to include tool/cf fields, got %+v", latest)
	}
	if latest.RLBindingWindow != "seven_day" || latest.RLOverall != "allowed" {
		t.Fatalf("expected latest request to include quota fields, got %+v", latest)
	}
	if latest.RequestLane != "codex" || latest.RequestTransport != "codex_responses" {
		t.Fatalf("expected lane metadata on latest request, got %+v", latest)
	}
}

func TestHandleSubagentCountsIncludesGenericTypes(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	events := []*RequestEvent{
		{RequestID: "req-1", IsSubagent: true, SubagentType: "haiku"},
		{RequestID: "req-2", IsSubagent: true, SubagentType: "small_system"},
		{RequestID: "req-3", IsSubagent: true, SubagentType: "opus_subset"},
		{RequestID: "req-4", IsSubagent: false},
	}
	for _, evt := range events {
		if err := rec.RecordRequest(evt); err != nil {
			t.Fatalf("record request %s: %v", evt.RequestID, err)
		}
	}

	handler := NewAPIHandler(rec, db)
	req := httptest.NewRequest("GET", "/debug/subagent-counts?minutes=60", nil)
	w := httptest.NewRecorder()
	handler.handleSubagentCounts(w, req)

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if got := int(payload["total_subagent"].(float64)); got != 3 {
		t.Fatalf("expected 3 subagents, got %d", got)
	}
	if got := int(payload["small_system_subagent"].(float64)); got != 1 {
		t.Fatalf("expected 1 small_system subagent, got %d", got)
	}
	if got := int(payload["opus_subset_subagent"].(float64)); got != 1 {
		t.Fatalf("expected 1 opus_subset subagent, got %d", got)
	}

	byType := payload["by_type"].(map[string]interface{})
	if got := int(byType["haiku"].(float64)); got != 1 {
		t.Fatalf("expected by_type[haiku]=1, got %d", got)
	}
	if got := int(byType["small_system"].(float64)); got != 1 {
		t.Fatalf("expected by_type[small_system]=1, got %d", got)
	}
	if got := int(byType["opus_subset"].(float64)); got != 1 {
		t.Fatalf("expected by_type[opus_subset]=1, got %d", got)
	}
}

func TestHandleSubagentCountsIncludesBlockedSubagentEvents(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{RequestID: "req-1", IsSubagent: false}); err != nil {
		t.Fatalf("record request: %v", err)
	}
	if err := rec.RecordSubagentEvent(&SubagentEvent{
		RequestID:      "req-blocked-1",
		ModelRequested: "claude-haiku-4-5",
		SubagentType:   "haiku",
		BlockedByModel: true,
	}); err != nil {
		t.Fatalf("record subagent event: %v", err)
	}
	if err := rec.RecordSubagentEvent(&SubagentEvent{
		RequestID:      "req-blocked-2",
		ModelRequested: "claude-sonnet-4-5",
		SubagentType:   "sonnet",
		BlockedByModel: true,
	}); err != nil {
		t.Fatalf("record subagent event: %v", err)
	}

	handler := NewAPIHandler(rec, db)
	req := httptest.NewRequest("GET", "/debug/subagent-counts?minutes=60", nil)
	w := httptest.NewRecorder()
	handler.handleSubagentCounts(w, req)

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if got := int(payload["total_direct"].(float64)); got != 1 {
		t.Fatalf("expected 1 direct request, got %d", got)
	}
	if got := int(payload["total_subagent"].(float64)); got != 2 {
		t.Fatalf("expected 2 blocked subagents, got %d", got)
	}
	if got := int(payload["total_all"].(float64)); got != 3 {
		t.Fatalf("expected total_all=3, got %d", got)
	}

	byType := payload["by_type"].(map[string]interface{})
	if got := int(byType["haiku"].(float64)); got != 1 {
		t.Fatalf("expected by_type[haiku]=1, got %d", got)
	}
	if got := int(byType["sonnet"].(float64)); got != 1 {
		t.Fatalf("expected by_type[sonnet]=1, got %d", got)
	}
}

func TestHandleSubagentCountsOmitsClaudeAliasesForCodexLane(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	t.Setenv(runtimepaths.RuntimeLaneEnv, "codex")

	rec := NewRecorder(db)
	events := []*RequestEvent{
		{RequestID: "req-1", RequestLane: "codex", RequestTransport: "codex_responses", ClassifiedBackend: "codex", IsSubagent: true, SubagentType: "small_system"},
		{RequestID: "req-2", RequestLane: "codex", RequestTransport: "codex_responses", ClassifiedBackend: "codex", IsSubagent: false},
	}
	for _, evt := range events {
		if err := rec.RecordRequest(evt); err != nil {
			t.Fatalf("record request %s: %v", evt.RequestID, err)
		}
	}

	handler := NewAPIHandler(rec, db)
	req := httptest.NewRequest("GET", "/debug/subagent-counts?minutes=60", nil)
	w := httptest.NewRecorder()
	handler.handleSubagentCounts(w, req)

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if _, exists := payload["haiku_subagent"]; exists {
		t.Fatalf("expected codex payload to omit Claude alias fields, got %+v", payload)
	}
	if _, exists := payload["sonnet_subagent"]; exists {
		t.Fatalf("expected codex payload to omit Claude alias fields, got %+v", payload)
	}
	if payload["total_subagent"].(float64) != 1 {
		t.Fatalf("expected total_subagent=1, got %+v", payload)
	}
}

func TestWriteStatuslineSnapshotOmitsClaudeAliasesForCodexLane(t *testing.T) {
	root := t.TempDir()
	t.Setenv(runtimepaths.RuntimeLaneEnv, "codex")
	t.Setenv(runtimepaths.RuntimeRootEnv, root)

	db, err := OpenDB(filepath.Join(root, "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{
		SessionID:         "sess-1",
		RequestID:         "req-1",
		RequestLane:       "codex",
		RequestTransport:  "codex_responses",
		ClassifiedBackend: "codex",
		IsSubagent:        true,
		SubagentType:      "small_system",
	}); err != nil {
		t.Fatalf("record request: %v", err)
	}

	rec.WriteStatuslineSnapshot()

	var snapshot map[string]interface{}
	data, err := os.ReadFile(filepath.Join(root, "statusline_snapshot.json"))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}

	counts := snapshot["subagent_counts"].(map[string]interface{})
	if _, exists := counts["haiku_subagent"]; exists {
		t.Fatalf("expected codex snapshot to omit Claude alias fields, got %+v", counts)
	}
	if _, exists := counts["sonnet_subagent"]; exists {
		t.Fatalf("expected codex snapshot to omit Claude alias fields, got %+v", counts)
	}
	status := snapshot["status"].(map[string]interface{})
	if status["request_lane"] != "codex" || status["request_transport"] != "codex_responses" {
		t.Fatalf("expected status lane metadata, got %+v", status)
	}
}
