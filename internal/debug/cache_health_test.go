package debug

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestHandleCacheHealthFlagsSevereCacheMisses(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	events := []*RequestEvent{
		{RequestID: "req-1", CacheCreationTokens: 121159, CacheReadTokens: 0},
		{RequestID: "req-2", CacheCreationTokens: 121529, CacheReadTokens: 10052},
		{RequestID: "req-3", CacheCreationTokens: 359, CacheReadTokens: 136833},
	}
	for _, evt := range events {
		if err := rec.RecordRequest(evt); err != nil {
			t.Fatalf("record request %s: %v", evt.RequestID, err)
		}
	}

	handler := NewAPIHandler(rec, db)
	req := httptest.NewRequest("GET", "/debug/cache-health", nil)
	w := httptest.NewRecorder()
	handler.handleCacheHealth(w, req)

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	if payload["healthy"].(bool) {
		t.Fatalf("expected unhealthy payload, got %+v", payload)
	}
	if !payload["anomaly"].(bool) {
		t.Fatalf("expected anomaly=true, got %+v", payload)
	}
	if got := int(payload["severe_breaks_15m"].(float64)); got != 2 {
		t.Fatalf("expected 2 severe breaks, got %d", got)
	}
	if got := int(payload["cold_misses_15m"].(float64)); got != 2 {
		t.Fatalf("expected 2 cold misses, got %d", got)
	}
	if msg := payload["anomaly_msg"].(string); msg == "" {
		t.Fatalf("expected anomaly message, got %+v", payload)
	}
}
