package gemini

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

type geminiRequestTelemetry struct {
	RequestID      string
	SessionID      string
	ConversationID string
	ModelRequested string
	UserPreview    string
	StartedAt      time.Time
}

type geminiResponseTelemetry struct {
	ModelResponse   string
	StopReason      string
	InputTokens     int
	OutputTokens    int
	CacheReadTokens int
	HasToolUse      bool
	OutputPreview   string
	TTFT            time.Duration
}

func (g *geminiResponseTelemetry) merge(other geminiResponseTelemetry) {
	if other.ModelResponse != "" {
		g.ModelResponse = other.ModelResponse
	}
	if other.StopReason != "" {
		g.StopReason = other.StopReason
	}
	if other.InputTokens > 0 {
		g.InputTokens = other.InputTokens
	}
	if other.OutputTokens > 0 {
		g.OutputTokens = other.OutputTokens
	}
	if other.CacheReadTokens > 0 {
		g.CacheReadTokens = other.CacheReadTokens
	}
	if other.HasToolUse {
		g.HasToolUse = true
	}
	if other.OutputPreview != "" {
		g.OutputPreview = other.OutputPreview
	}
	if other.TTFT > 0 && g.TTFT == 0 {
		g.TTFT = other.TTFT
	}
}

func buildGeminiRequestTelemetry(convID, model string, body map[string]interface{}) geminiRequestTelemetry {
	return buildGeminiRequestTelemetryAt(convID, model, body, time.Now())
}

func buildGeminiRequestTelemetryAt(convID, model string, body map[string]interface{}, startedAt time.Time) geminiRequestTelemetry {
	return geminiRequestTelemetry{
		RequestID:      "gemini_" + convID + "_" + strconv.FormatInt(startedAt.UnixNano(), 10),
		SessionID:      convID,
		ConversationID: convID,
		ModelRequested: strings.TrimSpace(model),
		UserPreview:    extractGeminiUserPreview(body),
		StartedAt:      startedAt,
	}
}

func recordGeminiRequestEvent(rec telemetryRecorder, meta geminiRequestTelemetry, sess *session, response geminiResponseTelemetry) {
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
		RequestLane:       "gemini",
		RequestTransport:  "gemini_generate",
		ModelRequested:    meta.ModelRequested,
		ModelResponse:     modelResponse,
		ModelMatch:        strings.EqualFold(modelResponse, meta.ModelRequested) || meta.ModelRequested == "",
		HasToolUse:        response.HasToolUse,
		TextChunkCount:    geminiBoolCount(response.OutputPreview != ""),
		InputTokens:       response.InputTokens,
		OutputTokens:      response.OutputTokens,
		CacheReadTokens:   response.CacheReadTokens,
		TTFT:              float64(response.TTFT) / float64(time.Millisecond),
		TotalTimeMs:       float64(totalTime) / float64(time.Millisecond),
		ClassifiedBackend: "gemini",
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

func extractGeminiResponseTelemetry(payload map[string]interface{}) geminiResponseTelemetry {
	telemetry := geminiResponseTelemetry{
		ModelResponse: strings.TrimSpace(geminiStringValue(payload["modelVersion"])),
	}
	if meta, ok := payload["usageMetadata"].(map[string]interface{}); ok {
		telemetry.InputTokens = geminiIntValue(meta["promptTokenCount"])
		telemetry.OutputTokens = geminiIntValue(meta["candidatesTokenCount"])
		telemetry.CacheReadTokens = geminiIntValue(meta["cachedContentTokenCount"])
	}
	candidates, _ := payload["candidates"].([]interface{})
	if len(candidates) == 0 {
		return telemetry
	}
	candidate, _ := candidates[0].(map[string]interface{})
	telemetry.StopReason = strings.TrimSpace(geminiStringValue(candidate["finishReason"]))
	content, _ := candidate["content"].(map[string]interface{})
	if content == nil {
		return telemetry
	}
	telemetry.HasToolUse, telemetry.OutputPreview = summarizeGeminiContent(content)
	return telemetry
}

func extractGeminiUserPreview(body map[string]interface{}) string {
	raw, ok := body["contents"].([]interface{})
	if !ok {
		return ""
	}
	for i := len(raw) - 1; i >= 0; i-- {
		content, ok := raw[i].(map[string]interface{})
		if !ok {
			continue
		}
		if role, _ := content["role"].(string); role != "user" {
			continue
		}
		_, preview := summarizeGeminiContent(content)
		return preview
	}
	return ""
}

func summarizeGeminiContent(content map[string]interface{}) (hasToolUse bool, preview string) {
	parts, _ := content["parts"].([]interface{})
	fragments := make([]string, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if text, _ := part["text"].(string); strings.TrimSpace(text) != "" {
			fragments = append(fragments, strings.TrimSpace(text))
			continue
		}
		if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			hasToolUse = true
			name := strings.TrimSpace(geminiStringValue(fc["name"]))
			if name == "" {
				name = "function"
			}
			fragments = append(fragments, "[call "+name+"]")
			continue
		}
		if fr, ok := part["functionResponse"].(map[string]interface{}); ok {
			name := strings.TrimSpace(geminiStringValue(fr["name"]))
			if name == "" {
				name = "function"
			}
			fragments = append(fragments, "[response "+name+"]")
		}
	}
	return hasToolUse, strings.TrimSpace(strings.Join(fragments, " "))
}

func geminiIntValue(value interface{}) int {
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

func geminiStringValue(value interface{}) string {
	text, _ := value.(string)
	return text
}

func geminiBoolCount(ok bool) int {
	if ok {
		return 1
	}
	return 0
}
