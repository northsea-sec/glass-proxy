package debug

import (
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordRequestPopulatesCacheAndEvictionTables(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	evt := &RequestEvent{
		Timestamp:                "2026-03-10T16:00:00Z",
		ConversationID:           "conv-1",
		RequestID:                "req-1",
		InputTokens:              1234,
		CacheCreationTokens:      456,
		CacheReadTokens:          789,
		CacheEfficiency:          61.5,
		GlassEvictedCount:        12,
		GlassShadowBatch:         2,
		GlassTokensBefore:        90000,
		GlassTokensAfter:         64000,
		GlassEvictionReason:      "initial_overflow threshold=80000",
		GlassShadowPath:          "/tmp/shadow.md",
		GlassPrefixChangeKind:    "changed",
		GlassPrefixDivergence:    "msg[5]",
		GlassPrefixSystemChanged: true,
		GlassPrefixToolsChanged:  false,
		GlassPrefixAnchor:        42,
		GlassPrefixPrevAnchor:    34,
		GlassPrefixMeasuredMsgs:  57,
		GlassTailChangeKind:      "changed",
		GlassTailDivergence:      "tail_msg[1]",
		GlassTailAnchor:          84,
		GlassTailPrevAnchor:      76,
		GlassTailMeasuredMsgs:    9,
		GlassTailTokens:          1234,
		GlassTailHash:            "tail-hash-1",
	}
	if err := rec.RecordRequest(evt); err != nil {
		t.Fatalf("record request: %v", err)
	}

	var cacheRows int
	if err := db.Raw().QueryRow(`SELECT COUNT(*) FROM cache_events WHERE request_id = ?`, evt.RequestID).Scan(&cacheRows); err != nil {
		t.Fatalf("count cache_events: %v", err)
	}
	if cacheRows != 1 {
		t.Fatalf("expected 1 cache_events row, got %d", cacheRows)
	}

	var evicted, before, after, batch int
	var reason, shadowPath string
	if err := db.Raw().QueryRow(`SELECT messages_evicted, tokens_before, tokens_after, batch_number, trigger_reason, shadow_path
		FROM eviction_events WHERE conversation_id = ?`, evt.ConversationID).Scan(&evicted, &before, &after, &batch, &reason, &shadowPath); err != nil {
		t.Fatalf("select eviction_events: %v", err)
	}
	if evicted != evt.GlassEvictedCount || before != evt.GlassTokensBefore || after != evt.GlassTokensAfter || batch != evt.GlassShadowBatch {
		t.Fatalf("unexpected eviction row: evicted=%d before=%d after=%d batch=%d", evicted, before, after, batch)
	}
	if reason != evt.GlassEvictionReason || shadowPath != evt.GlassShadowPath {
		t.Fatalf("unexpected eviction metadata: reason=%q shadow=%q", reason, shadowPath)
	}

	var changeKind, divergence string
	var systemChanged, toolsChanged, anchor, prevAnchor, measured int
	var tailChangeKind, tailDivergence, tailHash string
	var tailAnchor, tailPrevAnchor, tailMeasured, tailTokens int
	if err := db.Raw().QueryRow(`SELECT
		glass_prefix_change_kind, glass_prefix_divergence,
		glass_prefix_system_changed, glass_prefix_tools_changed,
		glass_prefix_anchor, glass_prefix_prev_anchor, glass_prefix_measured_msgs,
		glass_tail_change_kind, glass_tail_divergence, glass_tail_anchor,
		glass_tail_prev_anchor, glass_tail_measured_msgs, glass_tail_tokens,
		glass_tail_hash
		FROM requests WHERE request_id = ?`, evt.RequestID,
	).Scan(&changeKind, &divergence, &systemChanged, &toolsChanged, &anchor, &prevAnchor, &measured,
		&tailChangeKind, &tailDivergence, &tailAnchor, &tailPrevAnchor, &tailMeasured, &tailTokens, &tailHash); err != nil {
		t.Fatalf("select prefix fields: %v", err)
	}
	if changeKind != evt.GlassPrefixChangeKind || divergence != evt.GlassPrefixDivergence {
		t.Fatalf("unexpected prefix fields: kind=%q divergence=%q", changeKind, divergence)
	}
	if systemChanged != 1 || toolsChanged != 0 || anchor != evt.GlassPrefixAnchor || prevAnchor != evt.GlassPrefixPrevAnchor || measured != evt.GlassPrefixMeasuredMsgs {
		t.Fatalf("unexpected prefix metadata: sys=%d tools=%d anchor=%d prev=%d measured=%d", systemChanged, toolsChanged, anchor, prevAnchor, measured)
	}
	if tailChangeKind != evt.GlassTailChangeKind || tailDivergence != evt.GlassTailDivergence || tailHash != evt.GlassTailHash {
		t.Fatalf("unexpected tail fields: kind=%q divergence=%q hash=%q", tailChangeKind, tailDivergence, tailHash)
	}
	if tailAnchor != evt.GlassTailAnchor || tailPrevAnchor != evt.GlassTailPrevAnchor || tailMeasured != evt.GlassTailMeasuredMsgs || tailTokens != evt.GlassTailTokens {
		t.Fatalf("unexpected tail metadata: anchor=%d prev=%d measured=%d tokens=%d", tailAnchor, tailPrevAnchor, tailMeasured, tailTokens)
	}
}

func TestRecordDedupEventPersists(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordDedupEvent(&DedupEvent{
		Timestamp:      "2026-03-10T16:00:01Z",
		RequestHash:    "single:deadbeef",
		Action:         "block",
		ConversationID: "conv-2",
	}); err != nil {
		t.Fatalf("record dedup: %v", err)
	}

	var action, convID string
	if err := db.Raw().QueryRow(`SELECT action, conversation_id FROM dedup_events WHERE request_hash = ?`, "single:deadbeef").Scan(&action, &convID); err != nil {
		t.Fatalf("select dedup_events: %v", err)
	}
	if action != "block" || convID != "conv-2" {
		t.Fatalf("unexpected dedup row: action=%q conv=%q", action, convID)
	}
}

func TestOpenDBMigratesExistingRequestsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "glass_debug.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		session_id TEXT,
		conversation_id TEXT,
		request_id TEXT,
		classified_backend TEXT
	)`); err != nil {
		t.Fatalf("create legacy requests table: %v", err)
	}
	raw.Close()

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB migration failed: %v", err)
	}
	defer db.Close()

	for _, column := range []string{
		"request_lane",
		"request_transport",
		"glass_prefix_change_kind",
		"glass_prefix_divergence",
		"glass_prefix_system_changed",
		"glass_prefix_tools_changed",
		"glass_prefix_anchor",
		"glass_prefix_prev_anchor",
		"glass_prefix_measured_msgs",
		"glass_tail_change_kind",
		"glass_tail_divergence",
		"glass_tail_anchor",
		"glass_tail_prev_anchor",
		"glass_tail_measured_msgs",
		"glass_tail_tokens",
		"glass_tail_hash",
	} {
		var count int
		if err := db.Raw().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('requests') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatalf("lookup column %s: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("expected migrated column %s", column)
		}
	}

	for _, column := range []string{
		"request_lane",
		"request_transport",
	} {
		var count int
		if err := db.Raw().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('subagent_events') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatalf("lookup subagent_events column %s: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("expected migrated subagent_events column %s", column)
		}
	}
}

func TestRecordQuotaPersistsStatuslineBurnMetric(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	origQuotaSamplesPath := quotaSamplesPath
	defer func() { quotaSamplesPath = origQuotaSamplesPath }()

	now := time.Now()
	quotaSamplesPath = filepath.Join(t.TempDir(), "quota_samples.json")
	samples := []map[string]interface{}{
		{"t": float64(now.Add(-10 * time.Minute).Unix()), "h5": 0.30, "d7": 0.75, "status": "allowed_warning", "bind": "seven_day"},
		{"t": float64(now.Add(-5 * time.Minute).Unix()), "h5": 0.35, "d7": 0.76, "status": "allowed_warning", "bind": "seven_day"},
		{"t": float64(now.Unix()), "h5": 0.40, "d7": 0.77, "status": "allowed_warning", "bind": "seven_day"},
	}
	data, err := json.Marshal(samples)
	if err != nil {
		t.Fatalf("marshal quota samples: %v", err)
	}
	if err := os.WriteFile(quotaSamplesPath, data, 0644); err != nil {
		t.Fatalf("write quota samples: %v", err)
	}

	rec := NewRecorder(db)
	if err := rec.RecordQuota("seven_day", 40, 77, "allowed", "allowed", "allowed_warning"); err != nil {
		t.Fatalf("record quota: %v", err)
	}

	var (
		ratePPHr     sql.NullFloat64
		current5hPct sql.NullFloat64
		hoursLeft    sql.NullFloat64
		samplesUsed  sql.NullInt64
		windowMin    sql.NullFloat64
		reset        int
	)
	if err := db.Raw().QueryRow(`SELECT
		statusline_burn_pp_hr,
		statusline_current_5h_pct,
		statusline_hours_left,
		statusline_samples_used,
		statusline_window_min,
		statusline_reset_detected
		FROM quota_snapshots
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&ratePPHr, &current5hPct, &hoursLeft, &samplesUsed, &windowMin, &reset); err != nil {
		t.Fatalf("select quota_snapshots: %v", err)
	}

	if !ratePPHr.Valid || math.Abs(ratePPHr.Float64-60.0) > 0.01 {
		t.Fatalf("expected statusline burn 60.0 pp/hr, got %+v", ratePPHr)
	}
	if !current5hPct.Valid || math.Abs(current5hPct.Float64-40.0) > 0.01 {
		t.Fatalf("expected current 5h pct 40.0, got %+v", current5hPct)
	}
	if !hoursLeft.Valid || math.Abs(hoursLeft.Float64-1.0) > 0.01 {
		t.Fatalf("expected hours_left 1.0, got %+v", hoursLeft)
	}
	if !samplesUsed.Valid || samplesUsed.Int64 != 3 {
		t.Fatalf("expected 3 samples used, got %+v", samplesUsed)
	}
	if !windowMin.Valid || math.Abs(windowMin.Float64-10.0) > 0.01 {
		t.Fatalf("expected 10.0 minute window, got %+v", windowMin)
	}
	if reset != 0 {
		t.Fatalf("expected reset_detected=0, got %d", reset)
	}
}

func TestUpdateRequestQuotaBackfillsRecordedRequest(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	rec := NewRecorder(db)
	if err := rec.RecordRequest(&RequestEvent{
		RequestID: "req-1",
	}); err != nil {
		t.Fatalf("record request: %v", err)
	}
	if err := rec.UpdateRequestQuota("req-1", "seven_day", 27, 31, "allowed", "allowed", "allowed"); err != nil {
		t.Fatalf("update request quota: %v", err)
	}

	var bindingWindow, status5h, status7d, overall string
	var util5h, util7d float64
	if err := db.Raw().QueryRow(`SELECT
		rl_binding_window, rl_5h_utilization, rl_5h_status,
		rl_7d_utilization, rl_7d_status, rl_overall_status
		FROM requests WHERE request_id = ?`, "req-1").Scan(
		&bindingWindow, &util5h, &status5h, &util7d, &status7d, &overall,
	); err != nil {
		t.Fatalf("select request quota fields: %v", err)
	}
	if bindingWindow != "seven_day" || util5h != 27 || util7d != 31 || status5h != "allowed" || status7d != "allowed" || overall != "allowed" {
		t.Fatalf("unexpected backfilled request quota row: bind=%q 5h=%v 7d=%v status5=%q status7=%q overall=%q", bindingWindow, util5h, util7d, status5h, status7d, overall)
	}
}

func TestRecordQuotaClampsNegativeBurnAfterResetLikeDrop(t *testing.T) {
	db, err := OpenDB(filepath.Join(t.TempDir(), "glass_debug.db"))
	if err != nil {
		t.Fatalf("open debug db: %v", err)
	}
	defer db.Close()

	origQuotaSamplesPath := quotaSamplesPath
	defer func() { quotaSamplesPath = origQuotaSamplesPath }()

	now := time.Now()
	quotaSamplesPath = filepath.Join(t.TempDir(), "quota_samples.json")
	samples := []map[string]interface{}{
		{"t": float64(now.Add(-10 * time.Minute).Unix()), "h5": 0.25, "d7": 0.85, "status": "allowed_warning", "bind": "seven_day"},
		{"t": float64(now.Add(-5 * time.Minute).Unix()), "h5": 0.24, "d7": 0.85, "status": "allowed_warning", "bind": "seven_day"},
		{"t": float64(now.Unix()), "h5": 0.02, "d7": 0.85, "status": "allowed_warning", "bind": "seven_day"},
	}
	data, err := json.Marshal(samples)
	if err != nil {
		t.Fatalf("marshal quota samples: %v", err)
	}
	if err := os.WriteFile(quotaSamplesPath, data, 0644); err != nil {
		t.Fatalf("write quota samples: %v", err)
	}

	rec := NewRecorder(db)
	if err := rec.RecordQuota("seven_day", 2, 85, "allowed", "allowed_warning", "allowed_warning"); err != nil {
		t.Fatalf("record quota: %v", err)
	}

	var (
		ratePPHr  sql.NullFloat64
		hoursLeft sql.NullFloat64
		reset     int
	)
	if err := db.Raw().QueryRow(`SELECT
		statusline_burn_pp_hr,
		statusline_hours_left,
		statusline_reset_detected
		FROM quota_snapshots
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&ratePPHr, &hoursLeft, &reset); err != nil {
		t.Fatalf("select quota_snapshots: %v", err)
	}

	if !ratePPHr.Valid || math.Abs(ratePPHr.Float64-0.0) > 0.01 {
		t.Fatalf("expected clamped statusline burn 0.0 pp/hr after reset-like drop, got %+v", ratePPHr)
	}
	if !hoursLeft.Valid || math.Abs(hoursLeft.Float64-(-1.0)) > 0.01 {
		t.Fatalf("expected hours_left -1.0 after reset-like drop, got %+v", hoursLeft)
	}
	if reset != 1 {
		t.Fatalf("expected reset_detected=1, got %d", reset)
	}
}

func TestOpenDBMigratesExistingQuotaSnapshotsTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "glass_debug.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE quota_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		binding_window TEXT,
		utilization_5h REAL DEFAULT 0,
		utilization_7d REAL DEFAULT 0,
		status_5h TEXT,
		status_7d TEXT,
		overall_status TEXT
	)`); err != nil {
		t.Fatalf("create legacy quota_snapshots table: %v", err)
	}
	raw.Close()

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("OpenDB migration failed: %v", err)
	}
	defer db.Close()

	for _, column := range []string{
		"statusline_burn_pp_hr",
		"statusline_current_5h_pct",
		"statusline_hours_left",
		"statusline_samples_used",
		"statusline_window_min",
		"statusline_reset_detected",
	} {
		var count int
		if err := db.Raw().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('quota_snapshots') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatalf("lookup column %s: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("expected migrated column %s", column)
		}
	}
}
