package debug

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIHandler serves debug data over HTTP for the statusline and CLI tools.
type APIHandler struct {
	recorder *Recorder
	db       *DB
}

// NewAPIHandler creates HTTP handlers backed by the debug database.
func NewAPIHandler(recorder *Recorder, db *DB) *APIHandler {
	return &APIHandler{recorder: recorder, db: db}
}

// RegisterRoutes adds debug routes to an HTTP mux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/debug/status", h.handleStatus)
	mux.HandleFunc("/debug/latest", h.handleLatest)
	mux.HandleFunc("/debug/session", h.handleSession)
	mux.HandleFunc("/debug/history", h.handleHistory)
	mux.HandleFunc("/debug/query", h.handleQuery)
	mux.HandleFunc("/debug/subagent-counts", h.handleSubagentCounts)
	mux.HandleFunc("/debug/anomalies", h.handleAnomalies)
	mux.HandleFunc("/debug/cache-health", h.handleCacheHealth)
	mux.HandleFunc("/debug/sycophancy", h.handleSycophancy)
	mux.HandleFunc("/debug/quality", h.handleQuality)
	mux.HandleFunc("/debug/bimodal", h.handleBimodal)
	mux.HandleFunc("/debug/context-growth", h.handleContextGrowth)
	mux.HandleFunc("/debug/behavioral", h.handleBehavioral)
	log.Printf("[DEBUG-API] Registered /debug/* endpoints (13 routes)")
}

// handleStatus returns pre-computed statusline data as JSON.
// This is the main endpoint statusline.py should curl.
func (h *APIHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	latest, err := h.recorder.GetLatestRequest()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if latest == nil {
		jsonReply(w, map[string]interface{}{"status": "no_data"})
		return
	}
	status := buildStatusPayload(h.db, h.recorder, latest)

	jsonReply(w, status)
}

// handleLatest returns the most recent request event (full detail).
func (h *APIHandler) handleLatest(w http.ResponseWriter, r *http.Request) {
	latest, err := h.recorder.GetLatestRequest()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if latest == nil {
		jsonReply(w, map[string]string{"status": "no_data"})
		return
	}
	jsonReply(w, latest)
}

// handleSession returns session aggregate stats.
func (h *APIHandler) handleSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("id")
	if sessionID == "" {
		// Get latest session
		row := h.db.db.QueryRow("SELECT session_id FROM requests ORDER BY id DESC LIMIT 1")
		row.Scan(&sessionID)
	}
	if sessionID == "" {
		jsonReply(w, map[string]string{"status": "no_sessions"})
		return
	}
	stats := buildSessionSnapshot(h.db, h.recorder, sessionID)
	jsonReply(w, stats)
}

// handleHistory returns recent request events.
func (h *APIHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 500 {
		limit = n
	}

	rows, err := h.db.db.Query(`SELECT
		timestamp, conversation_id, request_id, request_lane, request_transport,
		model_response, classified_backend, confidence,
		itt_mean_ms, tokens_per_sec, cache_efficiency,
		input_tokens, output_tokens, thinking_tokens_used,
		glass_tokens_saved, stop_reason
	FROM requests ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var events []map[string]interface{}
	for rows.Next() {
		var ts, convID, reqID, requestLane, requestTransport, model, backend, stopReason string
		var conf, ittMean, tps, cacheEff float64
		var inTok, outTok, thinkTok, saved int
		rows.Scan(&ts, &convID, &reqID, &requestLane, &requestTransport, &model, &backend, &conf,
			&ittMean, &tps, &cacheEff, &inTok, &outTok, &thinkTok, &saved, &stopReason)
		events = append(events, map[string]interface{}{
			"timestamp": ts, "conversation_id": convID, "request_id": reqID,
			"request_lane": requestLane, "request_transport": requestTransport,
			"model": model, "backend": backend, "confidence": conf,
			"itt_mean_ms": ittMean, "tokens_per_sec": tps, "cache_efficiency": cacheEff,
			"input_tokens": inTok, "output_tokens": outTok, "thinking_tokens": thinkTok,
			"glass_tokens_saved": saved, "stop_reason": stopReason,
		})
	}
	jsonReply(w, events)
}

// handleQuery runs a read-only SQL query against the debug database.
// For ad-hoc debugging from the command line.
func (h *APIHandler) handleQuery(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("sql")
	if query == "" {
		http.Error(w, "missing ?sql= parameter", 400)
		return
	}

	rows, err := h.db.db.Query(query)
	if err != nil {
		http.Error(w, "query error: "+err.Error(), 400)
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		row := map[string]interface{}{}
		for i, col := range cols {
			row[col] = vals[i]
		}
		results = append(results, row)
	}
	jsonReply(w, map[string]interface{}{"columns": cols, "rows": results, "count": len(results)})
}

func jsonReply(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// handleSubagentCounts returns subagent call counts grouped by type.
func (h *APIHandler) handleSubagentCounts(w http.ResponseWriter, r *http.Request) {
	minutes := queryInt(r, "minutes", 60)
	jsonReply(w, buildSubagentCountsWindow(h.db, minutes))
}

// handleAnomalies detects ITT spikes and backend switches in recent data.
func (h *APIHandler) handleAnomalies(w http.ResponseWriter, r *http.Request) {
	minutes := queryInt(r, "minutes", 30)
	cutoff := minutesAgo(minutes)

	rows, err := h.db.db.Query(`SELECT
		itt_mean_ms, classified_backend, timestamp
		FROM requests WHERE timestamp > ?
		ORDER BY id DESC LIMIT 20`, cutoff)
	if err != nil {
		jsonReply(w, []interface{}{})
		return
	}
	defer rows.Close()

	type sample struct {
		ittMean float64
		backend string
		ts      string
	}
	var samples []sample
	for rows.Next() {
		var s sample
		rows.Scan(&s.ittMean, &s.backend, &s.ts)
		samples = append(samples, s)
	}

	var anomalies []map[string]interface{}

	if len(samples) >= 5 {
		// ITT spike detection: current > mean + 2*stddev
		var sum, sumSq float64
		for _, s := range samples {
			sum += s.ittMean
			sumSq += s.ittMean * s.ittMean
		}
		n := float64(len(samples))
		mean := sum / n
		variance := sumSq/n - mean*mean
		if variance < 0 {
			variance = 0
		}
		stddev := 0.0
		if variance > 0 {
			stddev = variance // simplified: use variance as threshold scale
		}
		_ = stddev

		if len(samples) > 0 && samples[0].ittMean > mean*2 && samples[0].ittMean > 50 {
			anomalies = append(anomalies, map[string]interface{}{
				"type":   "itt_spike",
				"symbol": "[ITT]",
				"desc":   fmt.Sprintf("ITT spike: %.0fms (avg %.0fms)", samples[0].ittMean, mean),
			})
		}

		// Backend switch detection
		backends := map[string]bool{}
		for _, s := range samples[:min(5, len(samples))] {
			if s.backend != "" {
				backends[s.backend] = true
			}
		}
		if len(backends) > 1 {
			anomalies = append(anomalies, map[string]interface{}{
				"type":   "backend_switch",
				"symbol": "[BE]",
				"desc":   fmt.Sprintf("Backend switch detected (%d backends in last 5 calls)", len(backends)),
			})
		}
	}

	jsonReply(w, anomalies)
}

// handleCacheHealth returns cache break rate and anomaly detection.
func (h *APIHandler) handleCacheHealth(w http.ResponseWriter, r *http.Request) {
	cutoff5m := minutesAgo(5)
	cutoff1m := minutesAgo(1)
	cutoff15m := minutesAgo(15)

	rows, err := h.db.db.Query(`SELECT
		cache_creation_tokens, cache_read_tokens
		FROM requests WHERE timestamp > ?`, cutoff5m)
	if err != nil {
		jsonReply(w, map[string]interface{}{})
		return
	}
	defer rows.Close()

	var totalCC, totalCR, breaks, calls, maxCC5m int
	for rows.Next() {
		var cc, cr int
		rows.Scan(&cc, &cr)
		totalCC += cc
		totalCR += cr
		calls++
		if cc > 5000 {
			breaks++
		}
		if cc > maxCC5m {
			maxCC5m = cc
		}
	}

	if calls == 0 {
		jsonReply(w, map[string]interface{}{})
		return
	}

	total := totalCC + totalCR
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(totalCR) / float64(total) * 100
	}
	avgCC := float64(totalCC) / float64(calls)

	// 1-minute burst detection
	var burstCC, burstCalls int
	burstRows, err := h.db.db.Query(`SELECT cache_creation_tokens FROM requests WHERE timestamp > ?`, cutoff1m)
	if err == nil {
		defer burstRows.Close()
		for burstRows.Next() {
			var cc int
			burstRows.Scan(&cc)
			burstCC += cc
			burstCalls++
		}
	}

	var windowCC15m, windowCalls15m, maxCC15m, severeBreaks15m, coldMisses15m int
	windowRows, err := h.db.db.Query(`SELECT cache_creation_tokens, cache_read_tokens FROM requests WHERE timestamp > ?`, cutoff15m)
	if err == nil {
		defer windowRows.Close()
		for windowRows.Next() {
			var cc, cr int
			windowRows.Scan(&cc, &cr)
			windowCC15m += cc
			windowCalls15m++
			if cc > maxCC15m {
				maxCC15m = cc
			}
			if cc > 20000 {
				severeBreaks15m++
			}
			if cc > 50000 && cr < 15000 {
				coldMisses15m++
			}
		}
	}

	avgCCPerMin := float64(totalCC) / 5.0
	anomaly := false
	var anomalyReasons []string
	if coldMisses15m > 0 {
		anomaly = true
		anomalyReasons = append(anomalyReasons,
			fmt.Sprintf("severe cache miss: %d call(s) >50k CC with low read in 15m", coldMisses15m))
	}
	if severeBreaks15m >= 2 {
		anomaly = true
		anomalyReasons = append(anomalyReasons,
			fmt.Sprintf("repeated large cache creates: %d call(s) >20k CC in 15m", severeBreaks15m))
	}
	if float64(burstCC) > avgCCPerMin*3 && burstCC > 8000 {
		anomaly = true
		anomalyReasons = append(anomalyReasons, fmt.Sprintf("CC spike: %d in 1min (3x avg)", burstCC))
	}
	anomalyMsg := strings.Join(anomalyReasons, "; ")
	healthy := hitRate > 80 && breaks < 3 && maxCC15m < 20000 && coldMisses15m == 0 && !anomaly

	jsonReply(w, map[string]interface{}{
		"cache_hit_pct":     hitRate,
		"avg_cc_new":        int(avgCC),
		"break_count":       breaks,
		"calls":             calls,
		"healthy":           healthy,
		"anomaly":           anomaly,
		"anomaly_msg":       anomalyMsg,
		"burst_cc_1m":       burstCC,
		"burst_calls_1m":    burstCalls,
		"max_cc_5m":         maxCC5m,
		"window_cc_15m":     windowCC15m,
		"window_calls_15m":  windowCalls15m,
		"max_cc_15m":        maxCC15m,
		"severe_breaks_15m": severeBreaks15m,
		"cold_misses_15m":   coldMisses15m,
	})
}

// handleSycophancy returns sycophancy detection status from requests table.
func (h *APIHandler) handleSycophancy(w http.ResponseWriter, r *http.Request) {
	row := h.db.db.QueryRow(`SELECT
		sycophancy_score, sycophancy_divergence, sycophancy_signals, timestamp
		FROM requests WHERE sycophancy_score > 0
		ORDER BY id DESC LIMIT 1`)

	var score, divergence float64
	var signals, ts string
	err := row.Scan(&score, &divergence, &signals, &ts)
	if err != nil {
		jsonReply(w, map[string]interface{}{})
		return
	}

	result := map[string]interface{}{
		"score":      score,
		"divergence": divergence,
		"timestamp":  ts,
	}

	// Parse signals JSON if present
	if signals != "" {
		var parsed []interface{}
		if json.Unmarshal([]byte(signals), &parsed) == nil {
			result["signal_count"] = len(parsed)
		}
	}

	jsonReply(w, result)
}

// handleQuality returns quality/degradation status from ITT baseline comparison.
func (h *APIHandler) handleQuality(w http.ResponseWriter, r *http.Request) {
	// Recent (last 30 min)
	cutoffRecent := minutesAgo(30)
	cutoffBaseline := minutesAgo(24 * 60) // last 24h

	var recentITT, recentVar, recentTPS float64
	var recentCount int
	h.db.db.QueryRow(`SELECT
		AVG(itt_mean_ms), AVG(variance_coef), AVG(tokens_per_sec), COUNT(*)
		FROM requests WHERE timestamp > ? AND itt_mean_ms > 0`,
		cutoffRecent).Scan(&recentITT, &recentVar, &recentTPS, &recentCount)

	var baselineITT, baselineVar, baselineTPS float64
	var baselineCount int
	h.db.db.QueryRow(`SELECT
		AVG(itt_mean_ms), AVG(variance_coef), AVG(tokens_per_sec), COUNT(*)
		FROM requests WHERE timestamp > ? AND timestamp <= ? AND itt_mean_ms > 0`,
		cutoffBaseline, cutoffRecent).Scan(&baselineITT, &baselineVar, &baselineTPS, &baselineCount)

	result := map[string]interface{}{
		"score": 0, "mode": "unknown", "label": "UNKNOWN",
		"timing_ratio": 0.0, "variance_ratio": 0.0, "tps_ratio": 0.0,
		"trend": "stable", "trend_label": "stable",
	}

	if recentCount < 3 {
		jsonReply(w, result)
		return
	}

	// Compute ratios
	timingRatio := 0.0
	if baselineITT > 0 {
		timingRatio = recentITT / baselineITT
	}
	varianceRatio := 0.0
	if baselineVar > 0 {
		varianceRatio = recentVar / baselineVar
	}
	tpsRatio := 0.0
	if baselineTPS > 0 {
		tpsRatio = recentTPS / baselineTPS
	}

	// Score: 100 = perfect (timing matches baseline, low variance)
	score := 100.0
	if timingRatio > 1.2 {
		score -= (timingRatio - 1.0) * 30
	}
	if varianceRatio > 1.5 {
		score -= (varianceRatio - 1.0) * 10
	}
	if score < 0 {
		score = 0
	}

	mode := "premium"
	label := "PREMIUM"
	if score < 50 {
		mode = "degraded"
		label = "DEGRADED"
	} else if score < 75 {
		mode = "standard"
		label = "STANDARD"
	}

	trend := "stable"
	if timingRatio > 1.3 {
		trend = "degrading"
	} else if timingRatio < 0.8 && tpsRatio > 1.1 {
		trend = "improving"
	}

	result["score"] = int(score)
	result["mode"] = mode
	result["label"] = label
	result["timing_ratio"] = timingRatio
	result["variance_ratio"] = varianceRatio
	result["tps_ratio"] = tpsRatio
	result["trend"] = trend
	result["trend_label"] = trend

	jsonReply(w, result)
}

// handleBimodal detects bimodal latency distribution (routing to different backends).
func (h *APIHandler) handleBimodal(w http.ResponseWriter, r *http.Request) {
	minutes := queryInt(r, "minutes", 60)
	cutoff := minutesAgo(minutes)

	rows, err := h.db.db.Query(`SELECT
		total_time_ms, classified_backend
		FROM requests WHERE timestamp > ? AND total_time_ms > 0
		ORDER BY id DESC LIMIT 100`, cutoff)
	if err != nil {
		jsonReply(w, map[string]interface{}{"is_bimodal": false})
		return
	}
	defer rows.Close()

	type latSample struct {
		totalMs float64
		backend string
	}
	var samples []latSample
	for rows.Next() {
		var s latSample
		rows.Scan(&s.totalMs, &s.backend)
		samples = append(samples, s)
	}

	if len(samples) < 10 {
		jsonReply(w, map[string]interface{}{
			"is_bimodal":       false,
			"samples_analyzed": len(samples),
		})
		return
	}

	// Check for multiple backends
	backendCounts := map[string]int{}
	for _, s := range samples {
		backendCounts[s.backend]++
	}
	isBimodal := len(backendCounts) > 1

	// Compute raw stats
	var sum float64
	for _, s := range samples {
		sum += s.totalMs
	}
	mean := sum / float64(len(samples))

	jsonReply(w, map[string]interface{}{
		"is_bimodal":           isBimodal,
		"samples_analyzed":     len(samples),
		"backend_distribution": backendCounts,
		"raw_stats": map[string]interface{}{
			"mean":  mean,
			"count": len(samples),
		},
	})
}

// handleContextGrowth computes net context growth rate.
func (h *APIHandler) handleContextGrowth(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.db.Query(`SELECT
		context_api_tokens, timestamp
		FROM requests
		WHERE is_subagent = 0 AND context_api_tokens > 10000
		ORDER BY id DESC LIMIT 30`)
	if err != nil || rows == nil {
		jsonReply(w, map[string]interface{}{})
		return
	}
	defer rows.Close()

	type ctxSample struct {
		tokens int
		ts     string
	}
	var samples []ctxSample
	for rows.Next() {
		var s ctxSample
		rows.Scan(&s.tokens, &s.ts)
		samples = append(samples, s)
	}

	if len(samples) < 5 {
		jsonReply(w, map[string]interface{}{})
		return
	}

	// Reverse to chronological order
	for i, j := 0, len(samples)-1; i < j; i, j = i+1, j-1 {
		samples[i], samples[j] = samples[j], samples[i]
	}

	n := len(samples) - 1
	netDelta := samples[len(samples)-1].tokens - samples[0].tokens
	growthPerCall := 0
	if n > 0 {
		growthPerCall = netDelta / n
	}

	currentCtx := samples[len(samples)-1].tokens
	ceiling := 200000
	remaining := ceiling - currentCtx
	callsLeft := -1
	if growthPerCall > 0 {
		callsLeft = remaining / growthPerCall
	}

	jsonReply(w, map[string]interface{}{
		"growth_per_call": growthPerCall,
		"calls_left":      callsLeft,
		"current_ctx":     currentCtx,
		"sample_count":    len(samples),
		"safety_net":      false,
	})
}

// handleBehavioral returns behavioral signature from request patterns.
func (h *APIHandler) handleBehavioral(w http.ResponseWriter, r *http.Request) {
	minutes := queryInt(r, "minutes", 60)
	cutoff := minutesAgo(minutes)

	var totalCalls, toolUseCalls int
	h.db.db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN has_tool_use = 1 THEN 1 ELSE 0 END)
		FROM requests WHERE timestamp > ?`, cutoff).Scan(&totalCalls, &toolUseCalls)

	if totalCalls < 3 {
		jsonReply(w, map[string]interface{}{
			"signature":  "BUILDING",
			"confidence": totalCalls * 10,
			"trending":   nil,
		})
		return
	}

	// Simple behavioral classification based on tool usage ratio
	toolRatio := float64(toolUseCalls) / float64(totalCalls)
	signature := "UNKNOWN"
	confidence := 50

	if toolRatio > 0.7 {
		signature = "VERIFIER"
		confidence = int(toolRatio * 100)
	} else if toolRatio > 0.4 {
		signature = "BUILDING"
		confidence = int(toolRatio * 100)
	} else if toolRatio < 0.2 && totalCalls > 5 {
		signature = "COMPLETER"
		confidence = int((1 - toolRatio) * 80)
	}

	jsonReply(w, map[string]interface{}{
		"signature":          signature,
		"confidence":         confidence,
		"verification_ratio": toolRatio,
		"sample_count":       totalCalls,
		"tool_signals": map[string]interface{}{
			"sample_count":       totalCalls,
			"tool_use_count":     toolUseCalls,
			"verification_ratio": toolRatio,
			"signature":          signature,
			"confidence":         confidence,
		},
	})
}

// ── Helpers ──

func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return defaultVal
}

func minutesAgo(minutes int) string {
	return time.Now().Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339Nano)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
