package debug

import (
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"
)

// RequestEvent captures all debug dimensions for a single API call.
// This is the unified struct that replaces both request_events_v2 and audit_samples.
type RequestEvent struct {
	Timestamp        string
	SessionID        string
	ConversationID   string
	RequestID        string
	RequestLane      string
	RequestTransport string

	// Model
	ModelRequested string
	ModelResponse  string
	ModelMatch     bool
	IsSubagent     bool
	SubagentType   string
	HasToolUse     bool

	// Thinking
	ThinkingEnabled     bool
	ThinkingBudget      int
	ThinkingTier        string
	ThinkingChunkCount  int
	ThinkingTokensUsed  int
	ThinkingUtilization float64
	ThinkingDurationMs  float64
	ThinkingITTMean     float64
	ThinkingITTStd      float64

	// Text
	TextChunkCount int
	TextDurationMs float64
	TextITTMean    float64
	TextITTStd     float64

	// Tokens
	InputTokens  int
	OutputTokens int

	// Cache
	CacheCreationTokens int
	CacheReadTokens     int
	CacheEfficiency     float64

	// ITT
	TTFT         float64
	TotalTimeMs  float64
	ITTMean      float64
	ITTStd       float64
	ITTMin       float64
	ITTMax       float64
	ITTP50       float64
	ITTP90       float64
	ITTP99       float64
	TokensPerSec float64
	VarianceCoef float64
	NumChunks    int

	// Backend
	ClassifiedBackend string
	Confidence        float64
	BackendEvidence   map[string][]string
	Location          string
	CFEdgeLocation    string

	// Speculative
	SpeculativeDecoding bool
	SpeculativeType     string

	// Context
	ContextAPITokens int
	ContextAPIPct    float64
	ContextCCPct     float64
	ContextMismatch  bool

	// Rate limits
	RLBindingWindow string
	RL5hUtil        float64
	RL5hStatus      string
	RL7dUtil        float64
	RL7dStatus      string
	RLOverall       string

	// Sycophancy
	SycophancyScore      float64
	SycophancySignals    string
	SycophancyDivergence float64

	// Glass pipeline
	GlassEvictedCount         int
	GlassStrippedCount        int
	GlassOrphansFixed         int
	GlassTokensSaved          int
	GlassShadowBatch          int
	GlassTokensBefore         int
	GlassTokensAfter          int
	GlassEvictionReason       string
	GlassShadowPath           string
	GlassPrefixChangeKind     string
	GlassPrefixDivergence     string
	GlassPrefixSystemChanged  bool
	GlassPrefixToolsChanged   bool
	GlassPrefixAnchor         int
	GlassPrefixPrevAnchor     int
	GlassPrefixMeasuredMsgs   int
	GlassTailChangeKind       string
	GlassTailDivergence       string
	GlassTailAnchor           int
	GlassTailPrevAnchor       int
	GlassTailMeasuredMsgs     int
	GlassTailTokens           int
	GlassTailHash             string
	GlassCompressionWatermark int
	GlassPrefixHash           string
	GlassActivePrefixCount    int
	GlassEvictionDetected     bool

	// Content
	StopReason    string
	OutputPreview string
	UserPreview   string
}

// SubagentEvent captures subagent traffic that never reaches the normal request recorder.
type SubagentEvent struct {
	Timestamp        string
	SessionID        string
	ConversationID   string
	RequestID        string
	RequestLane      string
	RequestTransport string
	ModelRequested   string
	SubagentType     string
	BlockedByModel   bool
}

// DedupEvent captures request dedup gate decisions.
type DedupEvent struct {
	Timestamp      string
	RequestHash    string
	Action         string
	ConversationID string
}

// Recorder writes debug events to the unified database.
type Recorder struct {
	db             *DB
	SyspromptStale bool // set by glass engine when replacement targets are stale
}

// NewRecorder creates a recorder backed by the given database.
func NewRecorder(db *DB) *Recorder {
	return &Recorder{db: db}
}

// WriteStatuslineSnapshot writes the consolidated snapshot file.
func (r *Recorder) WriteStatuslineSnapshot() {
	WriteStatuslineSnapshot(r.db, r)
}

// RecordRequest inserts a request event into the database.
func (r *Recorder) RecordRequest(evt *RequestEvent) error {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().Format(time.RFC3339Nano)
	}

	evidenceJSON := ""
	if evt.BackendEvidence != nil {
		if data, err := json.Marshal(evt.BackendEvidence); err == nil {
			evidenceJSON = string(data)
		}
	}

	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	_, err := r.db.db.Exec(`INSERT INTO requests (
		timestamp, session_id, conversation_id, request_id, request_lane, request_transport,
		model_requested, model_response, model_match, is_subagent, subagent_type, has_tool_use,
		thinking_enabled, thinking_budget, thinking_tier, thinking_chunk_count,
		thinking_tokens_used, thinking_utilization, thinking_duration_ms,
		thinking_itt_mean_ms, thinking_itt_std_ms,
		text_chunk_count, text_duration_ms, text_itt_mean_ms, text_itt_std_ms,
		input_tokens, output_tokens,
		cache_creation_tokens, cache_read_tokens, cache_efficiency,
		ttft_ms, total_time_ms,
		itt_mean_ms, itt_std_ms, itt_min_ms, itt_max_ms,
		itt_p50_ms, itt_p90_ms, itt_p99_ms,
		tokens_per_sec, variance_coef, num_chunks,
		classified_backend, confidence, backend_evidence, location, cf_edge_location,
		speculative_decoding, speculative_type,
		context_api_tokens, context_api_pct, context_cc_pct, context_mismatch,
		rl_binding_window, rl_5h_utilization, rl_5h_status,
		rl_7d_utilization, rl_7d_status, rl_overall_status,
		sycophancy_score, sycophancy_signals, sycophancy_divergence,
		glass_evicted_count, glass_stripped_count, glass_orphans_fixed,
		glass_tokens_saved, glass_shadow_batch,
		glass_prefix_change_kind, glass_prefix_divergence, glass_prefix_system_changed,
		glass_prefix_tools_changed, glass_prefix_anchor, glass_prefix_prev_anchor,
		glass_prefix_measured_msgs,
		glass_tail_change_kind, glass_tail_divergence, glass_tail_anchor,
		glass_tail_prev_anchor, glass_tail_measured_msgs, glass_tail_tokens,
		glass_tail_hash,
		glass_compression_watermark, glass_prefix_hash, glass_active_prefix_count, glass_eviction_detected,
		stop_reason, output_preview, user_preview
	) VALUES (
		?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?,
		?, ?, ?, ?,
		?, ?, ?,
		?, ?,
		?, ?, ?, ?,
		?, ?,
		?, ?, ?,
		?, ?,
		?, ?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?,
		?, ?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?,
		?, ?, ?,
		?, ?, ?,
		?,
		?, ?, ?,
		?, ?, ?,
		?,
		?, ?, ?, ?,
		?, ?, ?
	)`,
		evt.Timestamp, evt.SessionID, evt.ConversationID, evt.RequestID, evt.RequestLane, evt.RequestTransport,
		evt.ModelRequested, evt.ModelResponse, evt.ModelMatch, evt.IsSubagent, evt.SubagentType, evt.HasToolUse,
		evt.ThinkingEnabled, evt.ThinkingBudget, evt.ThinkingTier, evt.ThinkingChunkCount,
		evt.ThinkingTokensUsed, evt.ThinkingUtilization, evt.ThinkingDurationMs,
		evt.ThinkingITTMean, evt.ThinkingITTStd,
		evt.TextChunkCount, evt.TextDurationMs, evt.TextITTMean, evt.TextITTStd,
		evt.InputTokens, evt.OutputTokens,
		evt.CacheCreationTokens, evt.CacheReadTokens, evt.CacheEfficiency,
		evt.TTFT, evt.TotalTimeMs,
		evt.ITTMean, evt.ITTStd, evt.ITTMin, evt.ITTMax,
		evt.ITTP50, evt.ITTP90, evt.ITTP99,
		evt.TokensPerSec, evt.VarianceCoef, evt.NumChunks,
		evt.ClassifiedBackend, evt.Confidence, evidenceJSON, evt.Location, evt.CFEdgeLocation,
		b2i(evt.SpeculativeDecoding), evt.SpeculativeType,
		evt.ContextAPITokens, evt.ContextAPIPct, evt.ContextCCPct, b2i(evt.ContextMismatch),
		evt.RLBindingWindow, evt.RL5hUtil, evt.RL5hStatus,
		evt.RL7dUtil, evt.RL7dStatus, evt.RLOverall,
		evt.SycophancyScore, evt.SycophancySignals, evt.SycophancyDivergence,
		evt.GlassEvictedCount, evt.GlassStrippedCount, evt.GlassOrphansFixed,
		evt.GlassTokensSaved, evt.GlassShadowBatch,
		evt.GlassPrefixChangeKind, evt.GlassPrefixDivergence, b2i(evt.GlassPrefixSystemChanged),
		b2i(evt.GlassPrefixToolsChanged), evt.GlassPrefixAnchor, evt.GlassPrefixPrevAnchor,
		evt.GlassPrefixMeasuredMsgs,
		evt.GlassTailChangeKind, evt.GlassTailDivergence, evt.GlassTailAnchor,
		evt.GlassTailPrevAnchor, evt.GlassTailMeasuredMsgs, evt.GlassTailTokens,
		evt.GlassTailHash,
		evt.GlassCompressionWatermark, evt.GlassPrefixHash, evt.GlassActivePrefixCount, b2i(evt.GlassEvictionDetected),
		evt.StopReason, truncate(evt.OutputPreview, 500), truncate(evt.UserPreview, 500),
	)
	if err != nil {
		log.Printf("[DEBUG-REC] Insert failed: %v", err)
		return err
	}

	if evt.RequestID != "" {
		if _, cerr := r.db.db.Exec(`INSERT INTO cache_events
			(request_id, timestamp, cache_read, cache_create, input_tokens, hit_ratio)
			VALUES (?, ?, ?, ?, ?, ?)`,
			evt.RequestID, evt.Timestamp, evt.CacheReadTokens, evt.CacheCreationTokens, evt.InputTokens, evt.CacheEfficiency,
		); cerr != nil {
			log.Printf("[DEBUG-REC] Cache event insert failed: %v", cerr)
		}
	}

	if evt.GlassEvictedCount > 0 && evt.ConversationID != "" {
		if _, eerr := r.db.db.Exec(`INSERT INTO eviction_events
			(timestamp, conversation_id, batch_number, messages_evicted, tokens_before, tokens_after, trigger_reason, shadow_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			evt.Timestamp, evt.ConversationID, evt.GlassShadowBatch, evt.GlassEvictedCount,
			evt.GlassTokensBefore, evt.GlassTokensAfter, evt.GlassEvictionReason, evt.GlassShadowPath,
		); eerr != nil {
			log.Printf("[DEBUG-REC] Eviction event insert failed: %v", eerr)
		}
	}

	return nil
}

// RecordSubagentEvent inserts a lightweight subagent event without polluting the main request stream.
func (r *Recorder) RecordSubagentEvent(evt *SubagentEvent) error {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().Format(time.RFC3339Nano)
	}

	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	_, err := r.db.db.Exec(`INSERT INTO subagent_events (
		timestamp, session_id, conversation_id, request_id,
		request_lane, request_transport,
		model_requested, subagent_type, blocked_by_model
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		evt.Timestamp, evt.SessionID, evt.ConversationID, evt.RequestID,
		evt.RequestLane, evt.RequestTransport,
		evt.ModelRequested, evt.SubagentType, b2i(evt.BlockedByModel),
	)
	if err != nil {
		log.Printf("[DEBUG-REC] Subagent insert failed: %v", err)
	}
	return err
}

// RecordDedupEvent inserts a lightweight dedup event.
func (r *Recorder) RecordDedupEvent(evt *DedupEvent) error {
	if evt.Timestamp == "" {
		evt.Timestamp = time.Now().Format(time.RFC3339Nano)
	}

	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	_, err := r.db.db.Exec(`INSERT INTO dedup_events
		(timestamp, request_hash, action, conversation_id)
		VALUES (?, ?, ?, ?)`,
		evt.Timestamp, evt.RequestHash, evt.Action, evt.ConversationID,
	)
	if err != nil {
		log.Printf("[DEBUG-REC] Dedup insert failed: %v", err)
	}
	return err
}

// RecordITTSamples inserts raw ITT samples for a request.
func (r *Recorder) RecordITTSamples(requestID string, samples []ITTSample) error {
	if len(samples) == 0 {
		return nil
	}

	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	tx, err := r.db.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO itt_samples (request_id, seq, delta_ms, token, phase, timestamp) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, s := range samples {
		_, err := stmt.Exec(requestID, i, s.DeltaMs, s.Token, s.Phase, s.Timestamp)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RecordEviction inserts a Glass eviction event.
func (r *Recorder) RecordEviction(convID string, batch, msgsEvicted, tokensBefore, tokensAfter int, reason, shadowPath string) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	_, err := r.db.db.Exec(`INSERT INTO eviction_events
		(timestamp, conversation_id, batch_number, messages_evicted, tokens_before, tokens_after, trigger_reason, shadow_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().Format(time.RFC3339Nano), convID, batch, msgsEvicted, tokensBefore, tokensAfter, reason, shadowPath,
	)
	return err
}

// RecordQuota inserts a quota snapshot.
func (r *Recorder) RecordQuota(bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	burn := loadStatuslineBurnMetric(time.Now())

	_, err := r.db.db.Exec(`INSERT INTO quota_snapshots
		(timestamp, binding_window, utilization_5h, utilization_7d, status_5h, status_7d, overall_status,
		 statusline_burn_pp_hr, statusline_current_5h_pct, statusline_hours_left,
		 statusline_samples_used, statusline_window_min, statusline_reset_detected)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().Format(time.RFC3339Nano), bindingWindow, util5h, util7d, status5h, status7d, overall,
		nullFloat(burn.RatePPHr, burn.Available),
		nullFloat(burn.Current5hPct, burn.Available),
		nullFloat(burn.HoursLeft, burn.Available),
		nullInt(burn.SamplesUsed, burn.Available),
		nullFloat(burn.WindowMin, burn.Available),
		b2i(burn.Available && burn.ResetDetected),
	)
	return err
}

// UpdateRequestQuota backfills rate-limit fields on an already-recorded request row.
func (r *Recorder) UpdateRequestQuota(requestID, bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error {
	if strings.TrimSpace(requestID) == "" {
		return nil
	}

	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	_, err := r.db.db.Exec(`UPDATE requests SET
		rl_binding_window = ?,
		rl_5h_utilization = ?,
		rl_5h_status = ?,
		rl_7d_utilization = ?,
		rl_7d_status = ?,
		rl_overall_status = ?
		WHERE request_id = ?`,
		bindingWindow, util5h, status5h, util7d, status7d, overall, requestID,
	)
	return err
}

// UpdateSession updates session lifecycle tracking.
func (r *Recorder) UpdateSession(sessionID, backend string) {
	r.db.mu.Lock()
	defer r.db.mu.Unlock()

	now := time.Now().Format(time.RFC3339Nano)
	_, err := r.db.db.Exec(`INSERT INTO sessions (session_id, started_at, last_seen_at, request_count, dominant_backend)
		VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			last_seen_at = ?,
			request_count = request_count + 1,
			dominant_backend = COALESCE(?, dominant_backend),
			backend_switches = CASE WHEN dominant_backend != ? AND ? != '' THEN backend_switches + 1 ELSE backend_switches END`,
		sessionID, now, now, backend,
		now, backend, backend, backend,
	)
	if err != nil {
		log.Printf("[DEBUG-REC] Session update failed: %v", err)
	}
}

func nullFloat(v float64, ok bool) interface{} {
	if !ok {
		return nil
	}
	return v
}

func nullInt(v int, ok bool) interface{} {
	if !ok {
		return nil
	}
	return v
}

// ITTSample is a single per-token timing measurement.
type ITTSample struct {
	DeltaMs   float64
	Token     string
	Phase     string // "thinking" or "text"
	Timestamp string
}

// GetLatestRequest returns the most recent request event.
func (r *Recorder) GetLatestRequest() (*RequestEvent, error) {
	row := r.db.db.QueryRow(`SELECT
		timestamp, session_id, conversation_id, request_id, request_lane, request_transport,
		model_requested, model_response, model_match, is_subagent, subagent_type, has_tool_use,
		thinking_enabled, thinking_budget, thinking_tier,
		thinking_tokens_used, thinking_utilization, thinking_duration_ms, thinking_itt_mean_ms,
		text_duration_ms, text_itt_mean_ms,
		input_tokens, output_tokens,
		cache_creation_tokens, cache_read_tokens, cache_efficiency,
		ttft_ms, total_time_ms, itt_mean_ms, itt_std_ms, itt_min_ms, itt_max_ms, itt_p50_ms, itt_p90_ms, itt_p99_ms,
		tokens_per_sec, variance_coef, num_chunks,
		classified_backend, confidence, location, cf_edge_location,
		speculative_decoding, speculative_type,
		context_api_pct, context_cc_pct,
		rl_binding_window, rl_5h_utilization, rl_5h_status, rl_7d_utilization, rl_7d_status, rl_overall_status,
		sycophancy_score, sycophancy_divergence,
		glass_evicted_count, glass_stripped_count, glass_tokens_saved,
		stop_reason
	FROM requests ORDER BY id DESC LIMIT 1`)

	evt := &RequestEvent{}
	var specDec int
	err := row.Scan(
		&evt.Timestamp, &evt.SessionID, &evt.ConversationID, &evt.RequestID, &evt.RequestLane, &evt.RequestTransport,
		&evt.ModelRequested, &evt.ModelResponse, &evt.ModelMatch, &evt.IsSubagent, &evt.SubagentType, &evt.HasToolUse,
		&evt.ThinkingEnabled, &evt.ThinkingBudget, &evt.ThinkingTier,
		&evt.ThinkingTokensUsed, &evt.ThinkingUtilization, &evt.ThinkingDurationMs, &evt.ThinkingITTMean,
		&evt.TextDurationMs, &evt.TextITTMean,
		&evt.InputTokens, &evt.OutputTokens,
		&evt.CacheCreationTokens, &evt.CacheReadTokens, &evt.CacheEfficiency,
		&evt.TTFT, &evt.TotalTimeMs, &evt.ITTMean, &evt.ITTStd, &evt.ITTMin, &evt.ITTMax, &evt.ITTP50, &evt.ITTP90, &evt.ITTP99,
		&evt.TokensPerSec, &evt.VarianceCoef, &evt.NumChunks,
		&evt.ClassifiedBackend, &evt.Confidence, &evt.Location, &evt.CFEdgeLocation,
		&specDec, &evt.SpeculativeType,
		&evt.ContextAPIPct, &evt.ContextCCPct,
		&evt.RLBindingWindow, &evt.RL5hUtil, &evt.RL5hStatus, &evt.RL7dUtil, &evt.RL7dStatus, &evt.RLOverall,
		&evt.SycophancyScore, &evt.SycophancyDivergence,
		&evt.GlassEvictedCount, &evt.GlassStrippedCount, &evt.GlassTokensSaved,
		&evt.StopReason,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	evt.SpeculativeDecoding = specDec == 1
	return evt, err
}

// GetSessionStats returns aggregate stats for the current session.
func (r *Recorder) GetSessionStats(sessionID string) map[string]interface{} {
	stats := map[string]interface{}{}

	row := r.db.db.QueryRow(`SELECT
		COUNT(*) as total,
		AVG(cache_efficiency) as avg_cache,
		AVG(itt_mean_ms) as avg_itt,
		AVG(tokens_per_sec) as avg_tps,
		AVG(context_api_pct) as avg_context_api_pct,
		AVG(context_cc_pct) as avg_context_cc_pct,
		SUM(glass_tokens_saved) as total_saved,
		COUNT(DISTINCT classified_backend) as backends_seen
	FROM requests WHERE session_id = ?`, sessionID)

	var total int
	var avgCache, avgITT, avgTPS sql.NullFloat64
	var avgContextAPI, avgContextCC sql.NullFloat64
	var totalSaved sql.NullInt64
	var backendsSeen int
	if err := row.Scan(&total, &avgCache, &avgITT, &avgTPS, &avgContextAPI, &avgContextCC, &totalSaved, &backendsSeen); err == nil {
		stats["request_count"] = total
		stats["avg_cache_efficiency"] = avgCache.Float64
		stats["avg_itt_ms"] = avgITT.Float64
		stats["avg_tps"] = avgTPS.Float64
		stats["avg_context_api_pct"] = avgContextAPI.Float64
		stats["avg_context_cc_pct"] = avgContextCC.Float64
		stats["total_tokens_saved"] = totalSaved.Int64
		stats["backends_seen"] = backendsSeen
	}

	// Backend distribution
	rows, err := r.db.db.Query(`SELECT classified_backend, COUNT(*) as cnt
		FROM requests WHERE session_id = ? AND classified_backend != ''
		GROUP BY classified_backend ORDER BY cnt DESC`, sessionID)
	if err == nil {
		defer rows.Close()
		dist := map[string]int{}
		for rows.Next() {
			var backend string
			var cnt int
			rows.Scan(&backend, &cnt)
			dist[backend] = cnt
		}
		stats["backend_distribution"] = dist
	}

	return stats
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
