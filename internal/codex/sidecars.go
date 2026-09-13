package codex

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"proxy.local/app/internal/debug"
	"proxy.local/app/internal/runtimepaths"
	"proxy.local/app/internal/spoofer"
)

type codexUsageStats struct {
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	ReasoningTokens   int
}

type codexQuotaWindow struct {
	Available      bool
	UsedPercent    float64
	WindowMinutes  int
	ResetAfterSec  int
	ResetAtUnixSec int64
}

type codexQuotaSnapshot struct {
	Available     bool
	Allowed       bool
	LimitReached  bool
	PlanType      string
	Primary       codexQuotaWindow
	Secondary     codexQuotaWindow
	Status5h      string
	Status7d      string
	Overall       string
	BindingWindow string
}

type quotaRecorder interface {
	RecordQuota(bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error
}

type requestQuotaBackfiller interface {
	UpdateRequestQuota(requestID, bindingWindow string, util5h, util7d float64, status5h, status7d, overall string) error
}

func extractCodexUsage(resp map[string]interface{}) codexUsageStats {
	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		return codexUsageStats{}
	}
	stats := codexUsageStats{
		InputTokens:  intValue(usage["input_tokens"]),
		OutputTokens: intValue(usage["output_tokens"]),
	}
	if details, ok := usage["input_tokens_details"].(map[string]interface{}); ok {
		stats.CachedInputTokens = intValue(details["cached_tokens"])
	}
	if details, ok := usage["output_tokens_details"].(map[string]interface{}); ok {
		stats.ReasoningTokens = intValue(details["reasoning_tokens"])
	}
	return stats
}

func extractCodexQuotaHeaders(headers http.Header) (codexQuotaSnapshot, bool) {
	if len(headers) == 0 {
		return codexQuotaSnapshot{}, false
	}
	snapshot := codexQuotaSnapshot{
		Allowed:      true,
		PlanType:     strings.TrimSpace(headers.Get("X-Codex-Plan-Type")),
		Primary:      parseCodexQuotaWindowHeaders(headers, "X-Codex-Primary"),
		Secondary:    parseCodexQuotaWindowHeaders(headers, "X-Codex-Secondary"),
		LimitReached: parseBoolHeader(headers.Get("X-Codex-Limit-Reached")),
	}
	snapshot.Available = snapshot.Primary.Available || snapshot.Secondary.Available || snapshot.PlanType != "" || snapshot.LimitReached
	if !snapshot.Available {
		return codexQuotaSnapshot{}, false
	}
	if allowed := strings.TrimSpace(headers.Get("X-Codex-Allowed")); allowed != "" {
		snapshot.Allowed = parseBoolHeader(allowed)
	}
	finalizeCodexQuotaSnapshot(&snapshot)
	return snapshot, true
}

func extractCodexQuotaEvent(evt map[string]interface{}) (codexQuotaSnapshot, bool) {
	if strings.TrimSpace(stringValue(evt["type"])) != "codex.rate_limits" {
		return codexQuotaSnapshot{}, false
	}
	snapshot := codexQuotaSnapshot{
		Allowed:      true,
		PlanType:     strings.TrimSpace(stringValue(evt["plan_type"])),
		LimitReached: boolValue(evt["limit_reached"]),
	}
	if allowed, ok := evt["allowed"].(bool); ok {
		snapshot.Allowed = allowed
	}
	if limits, ok := evt["rate_limits"].(map[string]interface{}); ok {
		if allowed, ok := limits["allowed"].(bool); ok {
			snapshot.Allowed = allowed
		}
		if reached, ok := limits["limit_reached"].(bool); ok {
			snapshot.LimitReached = reached
		}
		snapshot.Primary = parseCodexQuotaWindowMap(limits["primary"])
		snapshot.Secondary = parseCodexQuotaWindowMap(limits["secondary"])
	}
	snapshot.Available = snapshot.Primary.Available || snapshot.Secondary.Available || snapshot.PlanType != "" || snapshot.LimitReached
	if !snapshot.Available {
		return codexQuotaSnapshot{}, false
	}
	finalizeCodexQuotaSnapshot(&snapshot)
	return snapshot, true
}

func emitCodexUsageSidecar(convID, model string, usage codexUsageStats) {
	if strings.TrimSpace(convID) == "" {
		return
	}
	_ = spoofer.NewBridge(runtimepaths.Current().UsageBridgePath).Write(convID, spoofer.UsageEntry{
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		CacheRead:    usage.CachedInputTokens,
		CacheCreate:  0,
		Model:        strings.TrimSpace(model),
	})
}

func emitCodexQuotaSidecars(rec telemetryRecorder, quota codexQuotaSnapshot, inflight int64) {
	if !quota.Available {
		return
	}
	paths := runtimepaths.Current()
	sidecars := debug.QuotaSidecars{
		Available:     true,
		Utilization5h: quota.Primary.UsedPercent / 100.0,
		Utilization7d: quota.Secondary.UsedPercent / 100.0,
		Status5h:      quota.Status5h,
		Status7d:      quota.Status7d,
		Overall:       quota.Overall,
		BindingWindow: quota.BindingWindow,
	}
	_ = debug.WriteQuotaSidecars(paths, sidecars, inflight, time.Now())
	if rec == nil {
		return
	}
	if quotaRec, ok := rec.(quotaRecorder); ok {
		_ = quotaRec.RecordQuota(
			quota.BindingWindow,
			quota.Primary.UsedPercent,
			quota.Secondary.UsedPercent,
			quota.Status5h,
			quota.Status7d,
			quota.Overall,
		)
	}
}

func backfillCodexRequestQuota(rec telemetryRecorder, requestID string, quota codexQuotaSnapshot) {
	if rec == nil || !quota.Available || strings.TrimSpace(requestID) == "" {
		return
	}
	backfiller, ok := rec.(requestQuotaBackfiller)
	if !ok {
		return
	}
	if err := backfiller.UpdateRequestQuota(
		requestID,
		quota.BindingWindow,
		quota.Primary.UsedPercent,
		quota.Secondary.UsedPercent,
		quota.Status5h,
		quota.Status7d,
		quota.Overall,
	); err != nil {
		return
	}
	rec.WriteStatuslineSnapshot()
}

func finalizeCodexQuotaSnapshot(snapshot *codexQuotaSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Status5h = codexQuotaStatus(snapshot.Allowed, snapshot.LimitReached, snapshot.Primary.UsedPercent, snapshot.Primary.Available)
	snapshot.Status7d = codexQuotaStatus(snapshot.Allowed, snapshot.LimitReached, snapshot.Secondary.UsedPercent, snapshot.Secondary.Available)
	snapshot.Overall = codexOverallQuotaStatus(snapshot.Status5h, snapshot.Status7d)
	snapshot.BindingWindow = codexBindingWindow(snapshot.Primary, snapshot.Secondary)
}

func codexQuotaStatus(allowed, limitReached bool, usedPercent float64, available bool) string {
	if !available {
		return ""
	}
	if !allowed || limitReached || usedPercent >= 100 {
		return "rate_limited"
	}
	if usedPercent >= 75 {
		return "allowed_warning"
	}
	return "allowed"
}

func codexOverallQuotaStatus(statuses ...string) string {
	overall := ""
	for _, status := range statuses {
		switch status {
		case "rate_limited":
			return status
		case "allowed_warning":
			overall = status
		case "allowed":
			if overall == "" {
				overall = status
			}
		}
	}
	return overall
}

func codexBindingWindow(primary, secondary codexQuotaWindow) string {
	switch {
	case primary.Available && secondary.Available:
		if primary.UsedPercent >= secondary.UsedPercent {
			return codexWindowName(primary.WindowMinutes)
		}
		return codexWindowName(secondary.WindowMinutes)
	case primary.Available:
		return codexWindowName(primary.WindowMinutes)
	case secondary.Available:
		return codexWindowName(secondary.WindowMinutes)
	default:
		return ""
	}
}

func codexWindowName(minutes int) string {
	switch minutes {
	case 300:
		return "five_hour"
	case 10080:
		return "seven_day"
	case 0:
		return ""
	default:
		return strconv.Itoa(minutes) + "_minute"
	}
}

func parseCodexQuotaWindowHeaders(headers http.Header, prefix string) codexQuotaWindow {
	usedRaw := strings.TrimSpace(headers.Get(prefix + "-Used-Percent"))
	windowRaw := strings.TrimSpace(headers.Get(prefix + "-Window-Minutes"))
	resetAfterRaw := strings.TrimSpace(headers.Get(prefix + "-Reset-After-Seconds"))
	resetAtRaw := strings.TrimSpace(headers.Get(prefix + "-Reset-At"))
	if usedRaw == "" && windowRaw == "" && resetAfterRaw == "" && resetAtRaw == "" {
		return codexQuotaWindow{}
	}
	return codexQuotaWindow{
		Available:      true,
		UsedPercent:    parseFloat(usedRaw),
		WindowMinutes:  parseInt(windowRaw),
		ResetAfterSec:  parseInt(resetAfterRaw),
		ResetAtUnixSec: int64(parseInt(resetAtRaw)),
	}
}

func parseCodexQuotaWindowMap(raw interface{}) codexQuotaWindow {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return codexQuotaWindow{}
	}
	available := false
	if _, ok := m["used_percent"]; ok {
		available = true
	}
	if _, ok := m["window_minutes"]; ok {
		available = true
	}
	if _, ok := m["reset_after_seconds"]; ok {
		available = true
	}
	if _, ok := m["reset_at"]; ok {
		available = true
	}
	if !available {
		return codexQuotaWindow{}
	}
	return codexQuotaWindow{
		Available:      true,
		UsedPercent:    floatValue(m["used_percent"]),
		WindowMinutes:  intValue(m["window_minutes"]),
		ResetAfterSec:  intValue(m["reset_after_seconds"]),
		ResetAtUnixSec: int64(intValue(m["reset_at"])),
	}
}

func parseBoolHeader(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func parseFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

func parseInt(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}

func floatValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func boolValue(value interface{}) bool {
	typed, _ := value.(bool)
	return typed
}

func quotaPointer(snapshot codexQuotaSnapshot, ok bool) *codexQuotaSnapshot {
	if !ok {
		return nil
	}
	copied := snapshot
	return &copied
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
