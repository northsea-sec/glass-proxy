package debug

func buildSessionSnapshot(db *DB, rec *Recorder, sessionID string) map[string]interface{} {
	if sessionID == "" {
		return map[string]interface{}{}
	}

	stats := rec.GetSessionStats(sessionID)
	stats["session_id"] = sessionID

	if dist, ok := stats["backend_distribution"].(map[string]int); ok {
		stats["sample_count"] = stats["request_count"]
		stats["trainium_count"] = dist["trainium"]
		stats["gpu_count"] = dist["gpu"]
		stats["tpu_count"] = dist["tpu"]
	}

	var switches int
	db.db.QueryRow(`SELECT COALESCE(backend_switches, 0) FROM sessions WHERE session_id = ?`, sessionID).Scan(&switches)
	stats["backend_switches"] = switches

	return stats
}

func buildStatusPayload(db *DB, rec *Recorder, latest *RequestEvent) map[string]interface{} {
	if latest == nil {
		return nil
	}

	status := map[string]interface{}{
		"session_id":        latest.SessionID,
		"request_lane":      latest.RequestLane,
		"request_transport": latest.RequestTransport,

		// Fingerprint
		"model_request":          latest.ModelRequested,
		"model_response":         latest.ModelResponse,
		"model_match":            latest.ModelMatch,
		"is_subagent":            latest.IsSubagent,
		"subagent_type":          latest.SubagentType,
		"has_tool_use":           latest.HasToolUse,
		"backend_classification": latest.ClassifiedBackend,
		"confidence":             latest.Confidence,
		"location":               latest.Location,
		"cf_edge_location":       latest.CFEdgeLocation,

		// ITT
		"itt_mean_ms":    latest.ITTMean,
		"itt_std_ms":     latest.ITTStd,
		"itt_min_ms":     latest.ITTMin,
		"itt_max_ms":     latest.ITTMax,
		"itt_p50_ms":     latest.ITTP50,
		"itt_p90_ms":     latest.ITTP90,
		"itt_p99_ms":     latest.ITTP99,
		"tokens_per_sec": latest.TokensPerSec,
		"variance_coef":  latest.VarianceCoef,
		"num_chunks":     latest.NumChunks,
		"ttft_ms":        latest.TTFT,
		"total_time_ms":  latest.TotalTimeMs,

		// Thinking
		"thinking_enabled":     latest.ThinkingEnabled,
		"thinking_budget":      latest.ThinkingBudget,
		"thinking_tier":        latest.ThinkingTier,
		"thinking_tokens":      latest.ThinkingTokensUsed,
		"thinking_utilization": latest.ThinkingUtilization,
		"thinking_duration_ms": latest.ThinkingDurationMs,
		"thinking_itt_mean_ms": latest.ThinkingITTMean,
		"text_duration_ms":     latest.TextDurationMs,
		"text_itt_mean_ms":     latest.TextITTMean,

		// Cache
		"cache_read_tokens":     latest.CacheReadTokens,
		"cache_creation_tokens": latest.CacheCreationTokens,
		"cache_efficiency":      latest.CacheEfficiency,

		// Tokens
		"input_tokens":  latest.InputTokens,
		"output_tokens": latest.OutputTokens,

		// Context
		"context_api_pct": latest.ContextAPIPct,
		"context_cc_pct":  latest.ContextCCPct,

		// Rate limits
		"rl_5h_utilization": latest.RL5hUtil,
		"rl_5h_status":      latest.RL5hStatus,
		"rl_7d_utilization": latest.RL7dUtil,
		"rl_7d_status":      latest.RL7dStatus,
		"rl_overall":        latest.RLOverall,
		"rl_binding_window": latest.RLBindingWindow,

		// Sycophancy
		"sycophancy_score":      latest.SycophancyScore,
		"sycophancy_divergence": latest.SycophancyDivergence,

		// Speculation
		"speculative_decoding": latest.SpeculativeDecoding,
		"speculative_type":     latest.SpeculativeType,

		// Glass
		"glass_evicted":      latest.GlassEvictedCount,
		"glass_stripped":     latest.GlassStrippedCount,
		"glass_tokens_saved": latest.GlassTokensSaved,

		"stop_reason": latest.StopReason,
		"timestamp":   latest.Timestamp,
	}

	if latest.SessionID != "" {
		sessionStats := buildSessionSnapshot(db, rec, latest.SessionID)
		status["session"] = sessionStats
		if avg, ok := sessionStats["avg_cache_efficiency"]; ok {
			status["cache_session_avg"] = avg
		}
	}

	if burn := latestStatuslineBurnMetric(db); burn != nil {
		status["statusline_burn"] = burn
		status["statusline_burn_pp_hr"] = burn["pp_hr"]
		status["statusline_burn_window_min"] = burn["window_min"]
		status["statusline_burn_hours_left"] = burn["hours_left"]
		status["statusline_burn_current_5h_pct"] = burn["current_5h_pct"]
		status["statusline_burn_samples_used"] = burn["samples_used"]
	}

	return status
}
