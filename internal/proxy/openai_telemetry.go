package proxy

import (
	"strconv"
	"strings"
	"time"

	"proxy.local/app/internal/debug"
)

type openAITelemetryRecorder interface {
	RecordRequest(*debug.RequestEvent) error
	UpdateSession(sessionID, backend string)
	WriteStatuslineSnapshot()
}

type openAIRequestTelemetry struct {
	RequestID      string
	SessionID      string
	ConversationID string
	ModelRequested string
	UserPreview    string
	StartedAt      time.Time
}

type openAIResponseTelemetry struct {
	ModelResponse   string
	StopReason      string
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	HasToolUse      bool
	OutputPreview   string
	TTFT            time.Duration
}

func buildOpenAIRequestTelemetry(convID string, body map[string]interface{}) openAIRequestTelemetry {
	return buildOpenAIRequestTelemetryAt(convID, body, time.Now())
}

func buildOpenAIRequestTelemetryAt(convID string, body map[string]interface{}, startedAt time.Time) openAIRequestTelemetry {
	model, _ := body["model"].(string)
	return openAIRequestTelemetry{
		RequestID:      "openai_" + convID + "_" + strconv.FormatInt(startedAt.UnixNano(), 10),
		SessionID:      convID,
		ConversationID: convID,
		ModelRequested: strings.TrimSpace(model),
		UserPreview:    extractOpenAIUserPreview(body),
		StartedAt:      startedAt,
	}
}

func recordOpenAIRequestEvent(rec openAITelemetryRecorder, meta openAIRequestTelemetry, sess *openAISession, response openAIResponseTelemetry) {
	if rec == nil {
		return
	}
	if response.TTFT < 0 {
		response.TTFT = 0
	}
	modelResponse := strings.TrimSpace(response.ModelResponse)
	if modelResponse == "" {
		modelResponse = meta.ModelRequested
	}
	totalTime := time.Since(meta.StartedAt)
	contextTokens := 0
	if sess != nil {
		contextTokens = sess.totalTokens()
	}
	if response.InputTokens+response.CacheReadTokens > 0 {
		contextTokens = response.InputTokens + response.CacheReadTokens
	}
	evt := &debug.RequestEvent{
		SessionID:         meta.SessionID,
		ConversationID:    meta.ConversationID,
		RequestID:         meta.RequestID,
		RequestLane:       "openai",
		RequestTransport:  "openai_chat",
		ModelRequested:    meta.ModelRequested,
		ModelResponse:     modelResponse,
		ModelMatch:        strings.EqualFold(modelResponse, meta.ModelRequested) || meta.ModelRequested == "",
		HasToolUse:        response.HasToolUse,
		TextChunkCount:    openAIBoolCount(response.OutputPreview != ""),
		InputTokens:       response.InputTokens,
		OutputTokens:      response.OutputTokens,
		CacheReadTokens:   response.CacheReadTokens,
		TTFT:              float64(response.TTFT) / float64(time.Millisecond),
		TotalTimeMs:       float64(totalTime) / float64(time.Millisecond),
		ClassifiedBackend: "openai",
		ContextAPITokens:  contextTokens,
		StopReason:        response.StopReason,
		OutputPreview:     response.OutputPreview,
		UserPreview:       meta.UserPreview,
	}
	if err := rec.RecordRequest(evt); err != nil {
		return
	}
	rec.UpdateSession(meta.SessionID, evt.ClassifiedBackend)
	rec.WriteStatuslineSnapshot()
}

func extractOpenAIResponseTelemetry(payload map[string]interface{}) openAIResponseTelemetry {
	telemetry := openAIResponseTelemetry{}
	if model, _ := payload["model"].(string); strings.TrimSpace(model) != "" {
		telemetry.ModelResponse = strings.TrimSpace(model)
	}
	if usage, ok := payload["usage"].(map[string]interface{}); ok {
		telemetry.InputTokens = openAIIntValue(usage["prompt_tokens"])
		telemetry.OutputTokens = openAIIntValue(usage["completion_tokens"])
		if details, ok := usage["prompt_tokens_details"].(map[string]interface{}); ok {
			telemetry.CacheReadTokens = openAIIntValue(details["cached_tokens"])
		}
	}
	choices, _ := payload["choices"].([]interface{})
	if len(choices) == 0 {
		return telemetry
	}
	choice, _ := choices[0].(map[string]interface{})
	if stopReason, _ := choice["finish_reason"].(string); strings.TrimSpace(stopReason) != "" {
		telemetry.StopReason = strings.TrimSpace(stopReason)
	}
	message, _ := choice["message"].(map[string]interface{})
	if message == nil {
		return telemetry
	}
	telemetry.HasToolUse, telemetry.OutputPreview = summarizeOpenAIAssistantMessage(message)
	return telemetry
}

func summarizeOpenAIAssistantMessage(msg map[string]interface{}) (hasToolUse bool, outputPreview string) {
	if msg == nil {
		return false, ""
	}
	if toolCalls, _ := msg["tool_calls"].([]interface{}); len(toolCalls) > 0 {
		hasToolUse = true
		if toolCall, ok := toolCalls[0].(map[string]interface{}); ok {
			outputPreview = renderOpenAIToolCall(toolCall)
		}
	}
	contentPreview := previewOpenAIContent(msg["content"])
	if contentPreview != "" {
		if outputPreview == "" {
			outputPreview = contentPreview
		} else {
			outputPreview = strings.TrimSpace(contentPreview + " " + outputPreview)
		}
	}
	return hasToolUse, outputPreview
}

func extractOpenAIUserPreview(body map[string]interface{}) string {
	raw, ok := body["messages"].([]interface{})
	if !ok {
		return ""
	}
	for i := len(raw) - 1; i >= 0; i-- {
		msg, ok := raw[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := msg["role"].(string); role != "user" {
			continue
		}
		return previewOpenAIContent(msg["content"])
	}
	return ""
}

func previewOpenAIContent(content interface{}) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []interface{}:
		fragments := make([]string, 0, len(typed))
		for _, raw := range typed {
			switch block := raw.(type) {
			case string:
				if strings.TrimSpace(block) != "" {
					fragments = append(fragments, strings.TrimSpace(block))
				}
			case map[string]interface{}:
				if text, _ := block["text"].(string); strings.TrimSpace(text) != "" {
					fragments = append(fragments, strings.TrimSpace(text))
				}
			}
		}
		return strings.TrimSpace(strings.Join(fragments, " "))
	default:
		return ""
	}
}

func openAIIntValue(value interface{}) int {
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

func openAIBoolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}
