package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"proxy.local/app/internal/runtimepaths"
)

// QuotaSidecars captures the normalized quota state written to runtime sidecar files.
// Utilization values use 0..1 fractions so the file format matches existing samples.
type QuotaSidecars struct {
	Available     bool
	Utilization5h float64
	Utilization7d float64
	Status5h      string
	Status7d      string
	Overall       string
	BindingWindow string
}

// WriteQuotaSidecars updates the shared runtime quota and health files for the active lane.
func WriteQuotaSidecars(paths runtimepaths.Paths, quota QuotaSidecars, inflight int64, now time.Time) error {
	if err := os.MkdirAll(paths.RootDir, 0755); err != nil {
		return err
	}

	unixNow := float64(now.Unix())
	if quota.Available {
		var samples []map[string]interface{}
		if data, err := os.ReadFile(paths.QuotaSamplesPath); err == nil {
			_ = json.Unmarshal(data, &samples)
		}
		samples = append(samples, map[string]interface{}{
			"t":      unixNow,
			"h5":     quota.Utilization5h,
			"d7":     quota.Utilization7d,
			"status": quota.Overall,
			"bind":   quota.BindingWindow,
		})
		if len(samples) > 500 {
			samples = samples[len(samples)-500:]
		}
		if err := writeJSONSidecar(paths.QuotaSamplesPath, samples); err != nil {
			return err
		}
	}

	interleavingData := map[string]interface{}{
		"active_inflight":      inflight,
		"interleaving_active":  inflight > 1,
		"active_conversations": 1,
		"conv_switches_total":  0,
		"t":                    unixNow,
	}
	if err := writeJSONSidecar(paths.InterleavingHealthPath, interleavingData); err != nil {
		return err
	}

	degraded := false
	reason := "ok"
	switch {
	case quota.Available && quota.Utilization5h > 0.8:
		degraded = true
		reason = "5h_utilization_high"
	case quota.Available && quota.Utilization7d > 0.8:
		degraded = true
		reason = "7d_utilization_high"
	}
	burnData := map[string]interface{}{
		"degraded":        degraded,
		"sample_degraded": false,
		"zero_streak":     0,
		"threshold":       0.8,
		"headers_present": quota.Available,
		"h5":              quota.Utilization5h,
		"d7":              quota.Utilization7d,
		"status":          quota.Overall,
		"bind":            quota.BindingWindow,
		"reason":          reason,
		"t":               unixNow,
	}
	return writeJSONSidecar(paths.BurnTelemetryHealthPath, burnData)
}

func writeJSONSidecar(path string, value interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
