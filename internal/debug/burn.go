package debug

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"proxy.local/app/internal/runtimepaths"
)

const (
	burnLookbackSec  = 900.0
	burnFreshnessSec = 180.0
	burnMinWindowSec = 30.0
)

var quotaSamplesPath string

type quotaSample struct {
	T      float64 `json:"t"`
	H5     float64 `json:"h5"`
	D7     float64 `json:"d7"`
	Status string  `json:"status"`
	Bind   string  `json:"bind"`
}

type statuslineBurnMetric struct {
	RatePPHr      float64
	Current5hPct  float64
	HoursLeft     float64
	SamplesUsed   int
	WindowMin     float64
	ResetDetected bool
	Available     bool
}

func defaultQuotaSamplesPath() string {
	if quotaSamplesPath != "" {
		return quotaSamplesPath
	}
	paths := runtimepaths.Current()
	proxyPath := paths.QuotaSamplesPath
	if _, err := os.Stat(proxyPath); err == nil {
		return proxyPath
	}
	return paths.LegacyQuotaSamplesPath
}

func loadStatuslineBurnMetric(now time.Time) statuslineBurnMetric {
	quotaSamplesPath := defaultQuotaSamplesPath()
	if quotaSamplesPath == "" {
		return statuslineBurnMetric{}
	}

	data, err := os.ReadFile(quotaSamplesPath)
	if err != nil {
		return statuslineBurnMetric{}
	}

	var samples []quotaSample
	if err := json.Unmarshal(data, &samples); err != nil {
		return statuslineBurnMetric{}
	}
	if len(samples) < 3 {
		return statuslineBurnMetric{}
	}

	nowUnix := float64(now.Unix())
	latest := samples[len(samples)-1]
	if nowUnix-latest.T > burnFreshnessSec {
		return statuslineBurnMetric{}
	}

	recent := make([]quotaSample, 0, len(samples))
	for _, s := range samples {
		if latest.T-s.T < burnLookbackSec {
			recent = append(recent, s)
		}
	}
	if len(recent) < 2 {
		return statuslineBurnMetric{}
	}

	dt := latest.T - recent[0].T
	if dt < burnMinWindowSec {
		return statuslineBurnMetric{}
	}

	dh5 := latest.H5 - recent[0].H5
	rawRate := dh5 * 100 / (dt / 3600)
	resetDetected := rawRate < -5
	rate := rawRate
	if rate < 0 {
		rate = 0
	}
	remaining := (1.0 - latest.H5) * 100
	hoursLeft := -1.0
	switch {
	case rate > 0.5:
		hoursLeft = remaining / rate
	default:
		hoursLeft = -1
	}

	return statuslineBurnMetric{
		RatePPHr:      roundTo(rate, 1),
		Current5hPct:  roundTo(latest.H5*100, 1),
		HoursLeft:     roundTo(hoursLeft, 2),
		SamplesUsed:   len(recent),
		WindowMin:     roundTo(dt/60, 1),
		ResetDetected: resetDetected,
		Available:     true,
	}
}

func roundTo(v float64, places int) float64 {
	if places <= 0 {
		return float64(int(v + 0.5))
	}
	pow := 1.0
	for i := 0; i < places; i++ {
		pow *= 10
	}
	if v >= 0 {
		return float64(int(v*pow+0.5)) / pow
	}
	return float64(int(v*pow-0.5)) / pow
}

func latestStatuslineBurnMetric(db *DB) map[string]interface{} {
	if db == nil || db.db == nil {
		return nil
	}
	quotaSamplesPath := defaultQuotaSamplesPath()

	var (
		ratePPHr     sql.NullFloat64
		current5hPct sql.NullFloat64
		hoursLeft    sql.NullFloat64
		samplesUsed  sql.NullInt64
		windowMin    sql.NullFloat64
		reset        sql.NullInt64
	)
	err := db.db.QueryRow(`SELECT
		statusline_burn_pp_hr,
		statusline_current_5h_pct,
		statusline_hours_left,
		statusline_samples_used,
		statusline_window_min,
		statusline_reset_detected
		FROM quota_snapshots
		WHERE statusline_burn_pp_hr IS NOT NULL
		ORDER BY id DESC
		LIMIT 1`,
	).Scan(&ratePPHr, &current5hPct, &hoursLeft, &samplesUsed, &windowMin, &reset)
	if err != nil || !ratePPHr.Valid {
		return nil
	}

	return map[string]interface{}{
		"pp_hr":          ratePPHr.Float64,
		"current_5h_pct": current5hPct.Float64,
		"hours_left":     hoursLeft.Float64,
		"samples_used":   samplesUsed.Int64,
		"window_min":     windowMin.Float64,
		"reset_detected": reset.Int64 != 0,
		"source":         filepath.Base(quotaSamplesPath),
	}
}
