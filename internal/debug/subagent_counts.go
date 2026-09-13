package debug

import (
	"database/sql"
	"strings"

	"proxy.local/app/internal/runtimepaths"
)

func buildSubagentCountsWindow(db *DB, minutes int) map[string]interface{} {
	lane := inferDebugLane(db)
	includeLegacyAliases := lane == "claude"
	return buildSubagentCountsWindowForLane(db, lane, minutes, includeLegacyAliases)
}

func buildSubagentCountsWindowForLane(db *DB, lane string, minutes int, includeLegacyAliases bool) map[string]interface{} {
	lane = normalizeDebugLane(lane)
	result := map[string]interface{}{
		"total_subagent":     0,
		"total_direct":       0,
		"total_all":          0,
		"by_type":            map[string]int{},
		"recent_counts":      map[string]int{},
		"last_subagent_time": "",
	}
	if includeLegacyAliases {
		result["haiku_subagent"] = 0
		result["sonnet_subagent"] = 0
		result["small_system_subagent"] = 0
		result["opus_subset_subagent"] = 0
		result["haiku_count"] = 0
		result["sonnet_count"] = 0
		result["subagent_count"] = 0
		result["total_count"] = 0
	}
	if db == nil || db.db == nil {
		return result
	}

	cutoff := minutesAgo(minutes)
	recentCutoff := minutesAgo(15)

	var totalSub, totalDirect, totalAll int
	byType := map[string]int{}
	addTypeCount := func(stype string, cnt int) {
		totalSub += cnt
		totalAll += cnt
		stype = strings.TrimSpace(stype)
		if stype == "" {
			stype = "unknown"
		}
		byType[stype] += cnt
	}

	rows, err := db.db.Query(`SELECT
		COALESCE(subagent_type, '') as stype,
		COUNT(*) as cnt
		FROM requests
		WHERE timestamp > ? AND is_subagent = 1 AND
		CASE
			WHEN TRIM(COALESCE(request_lane, '')) != '' THEN LOWER(TRIM(request_lane))
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'codex' THEN 'codex'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'gemini' THEN 'gemini'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'openai' THEN 'openai'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'ollama' THEN 'ollama'
			ELSE 'claude'
		END = ?
		GROUP BY subagent_type`, cutoff, lane)
	if err == nil {
		for rows.Next() {
			var stype string
			var cnt int
			rows.Scan(&stype, &cnt)
			addTypeCount(stype, cnt)
		}
		rows.Close()
	}

	rows, err = db.db.Query(`SELECT
		COALESCE(subagent_type, '') as stype,
		COUNT(*) as cnt
		FROM subagent_events
		WHERE timestamp > ? AND
		CASE
			WHEN TRIM(COALESCE(request_lane, '')) != '' THEN LOWER(TRIM(request_lane))
			ELSE 'claude'
		END = ?
		GROUP BY subagent_type`, cutoff, lane)
	if err == nil {
		for rows.Next() {
			var stype string
			var cnt int
			rows.Scan(&stype, &cnt)
			addTypeCount(stype, cnt)
		}
		rows.Close()
	}

	_ = db.db.QueryRow(`SELECT COUNT(*) FROM requests
		WHERE timestamp > ? AND is_subagent = 0 AND
		CASE
			WHEN TRIM(COALESCE(request_lane, '')) != '' THEN LOWER(TRIM(request_lane))
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'codex' THEN 'codex'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'gemini' THEN 'gemini'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'openai' THEN 'openai'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'ollama' THEN 'ollama'
			ELSE 'claude'
		END = ?`, cutoff, lane).Scan(&totalDirect)
	totalAll += totalDirect

	result["total_subagent"] = totalSub
	result["total_direct"] = totalDirect
	result["total_all"] = totalAll
	result["by_type"] = byType

	recent := map[string]int{}
	recentRows, err := db.db.Query(`SELECT
		COALESCE(subagent_type, '') as stype, COUNT(*) as cnt
		FROM requests
		WHERE timestamp > ? AND is_subagent = 1 AND
		CASE
			WHEN TRIM(COALESCE(request_lane, '')) != '' THEN LOWER(TRIM(request_lane))
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'codex' THEN 'codex'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'gemini' THEN 'gemini'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'openai' THEN 'openai'
			WHEN LOWER(TRIM(COALESCE(classified_backend, ''))) = 'ollama' THEN 'ollama'
			ELSE 'claude'
		END = ?
		GROUP BY subagent_type`, recentCutoff, lane)
	if err == nil {
		for recentRows.Next() {
			var stype string
			var cnt int
			recentRows.Scan(&stype, &cnt)
			if strings.TrimSpace(stype) == "" {
				stype = "unknown"
			}
			recent[stype] += cnt
		}
		recentRows.Close()
	}
	recentRows, err = db.db.Query(`SELECT
		COALESCE(subagent_type, '') as stype, COUNT(*) as cnt
		FROM subagent_events
		WHERE timestamp > ? AND
		CASE
			WHEN TRIM(COALESCE(request_lane, '')) != '' THEN LOWER(TRIM(request_lane))
			ELSE 'claude'
		END = ?
		GROUP BY subagent_type`, recentCutoff, lane)
	if err == nil {
		for recentRows.Next() {
			var stype string
			var cnt int
			recentRows.Scan(&stype, &cnt)
			if strings.TrimSpace(stype) == "" {
				stype = "unknown"
			}
			recent[stype] += cnt
		}
		recentRows.Close()
	}
	result["recent_counts"] = recent

	var reqLast, eventLast string
	_ = db.db.QueryRow(`SELECT COALESCE(MAX(timestamp), '') FROM requests WHERE is_subagent = 1`).Scan(&reqLast)
	_ = db.db.QueryRow(`SELECT COALESCE(MAX(timestamp), '') FROM subagent_events`).Scan(&eventLast)
	if eventLast > reqLast {
		result["last_subagent_time"] = eventLast
	} else {
		result["last_subagent_time"] = reqLast
	}

	if includeLegacyAliases {
		result["haiku_subagent"] = byType["haiku"]
		result["sonnet_subagent"] = byType["sonnet"]
		result["small_system_subagent"] = byType["small_system"]
		result["opus_subset_subagent"] = byType["opus_subset"]
		result["haiku_count"] = byType["haiku"]
		result["sonnet_count"] = byType["sonnet"]
		result["subagent_count"] = totalSub
		result["total_count"] = totalAll
	}

	return result
}

func inferDebugLane(db *DB) string {
	if db != nil && db.db != nil {
		var requestLane sql.NullString
		err := db.db.QueryRow(`SELECT request_lane
			FROM requests
			WHERE request_lane IS NOT NULL AND TRIM(request_lane) != ''
			ORDER BY id DESC
			LIMIT 1`).Scan(&requestLane)
		if err == nil && requestLane.Valid {
			if lane := normalizeDebugLane(requestLane.String); lane != "" {
				return lane
			}
		}

		var backend sql.NullString
		err = db.db.QueryRow(`SELECT classified_backend
			FROM requests
			WHERE classified_backend IS NOT NULL AND TRIM(classified_backend) != ''
			ORDER BY id DESC
			LIMIT 1`).Scan(&backend)
		if err == nil && backend.Valid {
			if lane := normalizeDebugLane(backend.String); lane != "" {
				return lane
			}
		}
	}

	return normalizeDebugLane(runtimepaths.Current().Lane)
}

func normalizeDebugLane(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "anthropic", "claude":
		return "claude"
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	case "openai":
		return "openai"
	case "ollama":
		return "ollama"
	default:
		return "claude"
	}
}
