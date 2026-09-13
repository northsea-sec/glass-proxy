package codex

import (
	"strconv"
	"strings"
	"time"

	"proxy.local/app/internal/debug"
)

type telemetryRecorder interface {
	RecordRequest(*debug.RequestEvent) error
	UpdateSession(sessionID, backend string)
	WriteStatuslineSnapshot()
}

type codexRequestTelemetry struct {
	RequestID      string
	SessionID      string
	ConversationID string
	ModelRequested string
	UserPreview    string
	StartedAt      time.Time
}

func buildCodexRequestTelemetry(convID string, body map[string]interface{}) codexRequestTelemetry {
	return buildCodexRequestTelemetryAt(convID, body, time.Now())
}

func buildCodexRequestTelemetryAt(convID string, body map[string]interface{}, startedAt time.Time) codexRequestTelemetry {
	return codexRequestTelemetry{
		RequestID:      "codex_" + convID + "_" + strconv.FormatInt(startedAt.UnixNano(), 10),
		SessionID:      convID,
		ConversationID: convID,
		ModelRequested: strings.TrimSpace(stringValue(body["model"])),
		UserPreview:    extractCodexUserPreview(body),
		StartedAt:      startedAt,
	}
}

func recordCodexRequestEvent(rec telemetryRecorder, meta codexRequestTelemetry, sess *session, modelResponse, stopReason string, usage codexUsageStats, quota *codexQuotaSnapshot, outputItems []map[string]interface{}, ttft time.Duration) {
	if rec == nil {
		return
	}
	if ttft < 0 {
		ttft = 0
	}

	modelResponse = strings.TrimSpace(modelResponse)
	if modelResponse == "" {
		modelResponse = meta.ModelRequested
	}
	if stopReason == "" {
		stopReason = inferCodexStopReason(outputItems)
	}

	hasToolUse, thinkingEnabled, thinkingChunkCount, outputPreview := summarizeCodexOutput(outputItems)
	totalTime := time.Since(meta.StartedAt)
	contextTokens := 0
	if sess != nil {
		contextTokens = sess.totalTokens()
	}
	if usage.InputTokens+usage.CachedInputTokens > 0 {
		contextTokens = usage.InputTokens + usage.CachedInputTokens
	}

	var rlBindingWindow, rlStatus5h, rlStatus7d, rlOverall string
	var rl5hUtil, rl7dUtil float64
	if quota != nil && quota.Available {
		rlBindingWindow = quota.BindingWindow
		rlStatus5h = quota.Status5h
		rlStatus7d = quota.Status7d
		rlOverall = quota.Overall
		rl5hUtil = quota.Primary.UsedPercent
		rl7dUtil = quota.Secondary.UsedPercent
	}

	evt := &debug.RequestEvent{
		SessionID:          meta.SessionID,
		ConversationID:     meta.ConversationID,
		RequestID:          meta.RequestID,
		RequestLane:        "codex",
		RequestTransport:   "codex_responses",
		ModelRequested:     meta.ModelRequested,
		ModelResponse:      modelResponse,
		ModelMatch:         strings.EqualFold(modelResponse, meta.ModelRequested) || meta.ModelRequested == "",
		HasToolUse:         hasToolUse,
		ThinkingEnabled:    thinkingEnabled,
		ThinkingChunkCount: thinkingChunkCount,
		TextChunkCount:     len(outputItems),
		InputTokens:        usage.InputTokens,
		OutputTokens:       usage.OutputTokens,
		CacheReadTokens:    usage.CachedInputTokens,
		TTFT:               float64(ttft) / float64(time.Millisecond),
		TotalTimeMs:        float64(totalTime) / float64(time.Millisecond),
		ClassifiedBackend:  "codex",
		ContextAPITokens:   contextTokens,
		RLBindingWindow:    rlBindingWindow,
		RL5hUtil:           rl5hUtil,
		RL5hStatus:         rlStatus5h,
		RL7dUtil:           rl7dUtil,
		RL7dStatus:         rlStatus7d,
		RLOverall:          rlOverall,
		StopReason:         stopReason,
		OutputPreview:      outputPreview,
		UserPreview:        meta.UserPreview,
	}

	if err := rec.RecordRequest(evt); err != nil {
		return
	}
	rec.UpdateSession(meta.SessionID, evt.ClassifiedBackend)
	rec.WriteStatuslineSnapshot()
}

func extractCodexUserPreview(body map[string]interface{}) string {
	raw, ok := body["input"].([]interface{})
	if !ok {
		return ""
	}
	for i := len(raw) - 1; i >= 0; i-- {
		item, ok := raw[i].(map[string]interface{})
		if !ok {
			continue
		}
		if itemType, _ := item["type"].(string); itemType != "message" {
			continue
		}
		if role, _ := item["role"].(string); role != "user" {
			continue
		}
		return extractMessageText(item)
	}
	return ""
}

func summarizeCodexOutput(items []map[string]interface{}) (hasToolUse bool, thinkingEnabled bool, thinkingChunkCount int, outputPreview string) {
	for _, item := range items {
		if item == nil {
			continue
		}
		switch itemType, _ := item["type"].(string); itemType {
		case "function_call":
			hasToolUse = true
		case "reasoning":
			thinkingEnabled = true
			thinkingChunkCount++
		case "message":
			if preview := strings.TrimSpace(extractMessageText(item)); preview != "" && outputPreview == "" {
				outputPreview = preview
			}
			if blocks, ok := item["content"].([]interface{}); ok {
				for _, raw := range blocks {
					block, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					blockType, _ := block["type"].(string)
					if blockType == "reasoning" || blockType == "thinking" {
						thinkingEnabled = true
						thinkingChunkCount++
					}
				}
			}
		}
	}
	return hasToolUse, thinkingEnabled, thinkingChunkCount, outputPreview
}

func inferCodexStopReason(outputItems []map[string]interface{}) string {
	for _, item := range outputItems {
		if itemType, _ := item["type"].(string); itemType == "function_call" {
			return "tool_call"
		}
	}
	if len(outputItems) > 0 {
		return "completed"
	}
	return ""
}

func extractCodexResponseMetadata(resp map[string]interface{}) (modelResponse, stopReason string) {
	modelResponse = strings.TrimSpace(stringValue(resp["model"]))
	stopReason = strings.TrimSpace(stringValue(resp["status"]))
	if stopReason != "" {
		return modelResponse, stopReason
	}
	incomplete, ok := resp["incomplete_details"].(map[string]interface{})
	if !ok {
		return modelResponse, ""
	}
	return modelResponse, strings.TrimSpace(stringValue(incomplete["reason"]))
}

func intValue(value interface{}) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}
