package debug

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"proxy.local/app/internal/runtimepaths"
)

// WriteStatuslineSnapshot writes a consolidated JSON file with all debug data
// the statusline needs. Called after every RecordRequest so the statusline
// can read a file instead of making HTTP calls (~0ms vs ~800ms).
func WriteStatuslineSnapshot(db *DB, rec *Recorder) {
	snapshotPath := runtimepaths.Current().StatuslineSnapshotPath

	snapshot := map[string]interface{}{}

	snapshot["cache_health"] = buildCacheHealth(db)
	snapshot["context_growth"] = buildContextGrowth(db)
	snapshot["subagent_counts"] = buildSubagentCounts(db)
	snapshot["anomalies"] = buildAnomalies(db)
	snapshot["behavioral"] = buildBehavioral(db)
	snapshot["bimodal"] = buildBimodal(db)
	snapshot["sycophancy"] = buildSycophancy(db)
	snapshot["quality"] = buildQuality(db)

	latest, _ := rec.GetLatestRequest()
	if latest != nil {
		snapshot["status"] = buildStatusPayload(db, rec, latest)
		if latest.SessionID != "" {
			snapshot["session"] = buildSessionSnapshot(db, rec, latest.SessionID)
		}
	}

	snapshot["sysprompt_status"] = map[string]interface{}{
		"stale": rec.SyspromptStale,
	}
	snapshot["t"] = float64(time.Now().Unix())

	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	tmpPath := snapshotPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpPath, snapshotPath)
}

func buildCacheHealth(db *DB) map[string]interface{} {
	cutoff5m := minutesAgo(5)
	cutoff1m := minutesAgo(1)
	cutoff15m := minutesAgo(15)

	rows, err := db.db.Query("SELECT cache_creation_tokens, cache_read_tokens FROM requests WHERE timestamp > ?", cutoff5m)
	if err != nil {
		return map[string]interface{}{}
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
		return map[string]interface{}{}
	}

	total := totalCC + totalCR
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(totalCR) / float64(total) * 100
	}
	avgCC := float64(totalCC) / float64(calls)

	var burstCC, burstCalls int
	burstRows, err := db.db.Query("SELECT cache_creation_tokens FROM requests WHERE timestamp > ?", cutoff1m)
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
	windowRows, err := db.db.Query("SELECT cache_creation_tokens, cache_read_tokens FROM requests WHERE timestamp > ?", cutoff15m)
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

	return map[string]interface{}{
		"cache_hit_pct": hitRate, "avg_cc_new": int(avgCC),
		"break_count": breaks, "calls": calls,
		"healthy": healthy, "anomaly": anomaly, "anomaly_msg": anomalyMsg,
		"burst_cc_1m": burstCC, "burst_calls_1m": burstCalls, "max_cc_5m": maxCC5m,
		"window_cc_15m": windowCC15m, "window_calls_15m": windowCalls15m,
		"max_cc_15m": maxCC15m, "severe_breaks_15m": severeBreaks15m,
		"cold_misses_15m": coldMisses15m,
	}
}

func buildContextGrowth(db *DB) map[string]interface{} {
	rows, err := db.db.Query("SELECT context_api_tokens, timestamp FROM requests WHERE is_subagent = 0 AND context_api_tokens > 10000 ORDER BY id DESC LIMIT 30")
	if err != nil {
		return map[string]interface{}{}
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
		return map[string]interface{}{}
	}

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

	return map[string]interface{}{
		"growth_per_call": growthPerCall, "calls_left": callsLeft,
		"current_ctx": currentCtx, "sample_count": len(samples), "safety_net": false,
	}
}

func buildSubagentCounts(db *DB) map[string]interface{} {
	return buildSubagentCountsWindow(db, 60)
}

func buildAnomalies(db *DB) []map[string]interface{} {
	cutoff := minutesAgo(30)
	rows, err := db.db.Query("SELECT itt_mean_ms, classified_backend, timestamp FROM requests WHERE timestamp > ? ORDER BY id DESC LIMIT 20", cutoff)
	if err != nil {
		return nil
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
		var sum float64
		for _, s := range samples {
			sum += s.ittMean
		}
		mean := sum / float64(len(samples))

		if len(samples) > 0 && samples[0].ittMean > mean*2 && samples[0].ittMean > 50 {
			anomalies = append(anomalies, map[string]interface{}{
				"type": "itt_spike", "symbol": "[ITT]",
				"desc": fmt.Sprintf("ITT spike: %.0fms (avg %.0fms)", samples[0].ittMean, mean),
			})
		}

		backends := map[string]bool{}
		for _, s := range samples[:min(5, len(samples))] {
			if s.backend != "" {
				backends[s.backend] = true
			}
		}
		if len(backends) > 1 {
			anomalies = append(anomalies, map[string]interface{}{
				"type": "backend_switch", "symbol": "[BE]",
				"desc": fmt.Sprintf("Backend switch detected (%d backends in last 5 calls)", len(backends)),
			})
		}
	}

	if anomalies == nil {
		anomalies = []map[string]interface{}{}
	}
	return anomalies
}

func buildBehavioral(db *DB) map[string]interface{} {
	cutoff := minutesAgo(60)
	var totalCalls, toolUseCalls int
	db.db.QueryRow("SELECT COUNT(*), SUM(CASE WHEN has_tool_use = 1 THEN 1 ELSE 0 END) FROM requests WHERE timestamp > ?", cutoff).Scan(&totalCalls, &toolUseCalls)

	if totalCalls < 3 {
		return map[string]interface{}{
			"signature": "BUILDING", "confidence": totalCalls * 10, "trending": nil,
		}
	}

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

	return map[string]interface{}{
		"signature": signature, "confidence": confidence,
		"verification_ratio": toolRatio, "sample_count": totalCalls,
		"tool_signals": map[string]interface{}{
			"sample_count": totalCalls, "tool_use_count": toolUseCalls,
			"verification_ratio": toolRatio, "signature": signature, "confidence": confidence,
		},
	}
}

func buildBimodal(db *DB) map[string]interface{} {
	cutoff := minutesAgo(60)
	rows, err := db.db.Query("SELECT total_time_ms, classified_backend FROM requests WHERE timestamp > ? AND total_time_ms > 0 ORDER BY id DESC LIMIT 100", cutoff)
	if err != nil {
		return map[string]interface{}{"is_bimodal": false}
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
		return map[string]interface{}{"is_bimodal": false, "samples_analyzed": len(samples)}
	}

	backendCounts := map[string]int{}
	for _, s := range samples {
		backendCounts[s.backend]++
	}
	isBimodal := len(backendCounts) > 1

	var sum float64
	for _, s := range samples {
		sum += s.totalMs
	}
	mean := sum / float64(len(samples))

	return map[string]interface{}{
		"is_bimodal": isBimodal, "samples_analyzed": len(samples),
		"backend_distribution": backendCounts,
		"raw_stats":            map[string]interface{}{"mean": mean, "count": len(samples)},
	}
}

func buildSycophancy(db *DB) map[string]interface{} {
	row := db.db.QueryRow("SELECT sycophancy_score, sycophancy_divergence, sycophancy_signals, timestamp FROM requests WHERE sycophancy_score > 0 ORDER BY id DESC LIMIT 1")
	var score, divergence float64
	var signals, ts string
	if err := row.Scan(&score, &divergence, &signals, &ts); err != nil {
		return map[string]interface{}{}
	}

	result := map[string]interface{}{"score": score, "divergence": divergence, "timestamp": ts}
	if signals != "" {
		var parsed []interface{}
		if json.Unmarshal([]byte(signals), &parsed) == nil {
			result["signal_count"] = len(parsed)
		}
	}
	return result
}

func buildQuality(db *DB) map[string]interface{} {
	cutoffRecent := minutesAgo(30)
	cutoffBaseline := minutesAgo(24 * 60)

	var recentITT, recentVar, recentTPS float64
	var recentCount int
	db.db.QueryRow("SELECT AVG(itt_mean_ms), AVG(variance_coef), AVG(tokens_per_sec), COUNT(*) FROM requests WHERE timestamp > ? AND itt_mean_ms > 0", cutoffRecent).Scan(&recentITT, &recentVar, &recentTPS, &recentCount)

	var baselineITT, baselineVar, baselineTPS float64
	var baselineCount int
	db.db.QueryRow("SELECT AVG(itt_mean_ms), AVG(variance_coef), AVG(tokens_per_sec), COUNT(*) FROM requests WHERE timestamp > ? AND timestamp <= ? AND itt_mean_ms > 0", cutoffBaseline, cutoffRecent).Scan(&baselineITT, &baselineVar, &baselineTPS, &baselineCount)

	result := map[string]interface{}{
		"score": 0, "mode": "unknown", "label": "UNKNOWN",
		"timing_ratio": 0.0, "variance_ratio": 0.0, "tps_ratio": 0.0,
		"trend": "stable", "trend_label": "stable",
	}

	if recentCount < 3 {
		return result
	}

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

	return result
}
