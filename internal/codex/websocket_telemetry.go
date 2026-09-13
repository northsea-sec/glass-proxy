package codex

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type webSocketTurnState struct {
	responseID      string
	meta            codexRequestTelemetry
	sess            *session
	quota           *codexQuotaSnapshot
	quotaWritten    bool
	recorded        bool
	completedAt     time.Time
	firstUpstreamAt time.Time
	outputItems     []map[string]interface{}
}

type webSocketTelemetryCollector struct {
	mu            sync.Mutex
	debugRecorder telemetryRecorder
	sessions      *sessionStore
	shadowDir     string
	requestURI    string
	now           func() time.Time
	pending       []*webSocketTurnState
	byResponseID  map[string]*webSocketTurnState
	activeRespID  string
	lastCompleted *webSocketTurnState
}

func newWebSocketTelemetryCollector(debugRecorder telemetryRecorder, sessions *sessionStore, shadowDir, requestURI string) *webSocketTelemetryCollector {
	return &webSocketTelemetryCollector{
		debugRecorder: debugRecorder,
		sessions:      sessions,
		shadowDir:     shadowDir,
		requestURI:    requestURI,
		now:           time.Now,
		byResponseID:  make(map[string]*webSocketTurnState),
	}
}

func (w *webSocketTelemetryCollector) Observe(direction string, messageType int, payload []byte) {
	if w == nil || messageType != websocket.TextMessage || len(payload) == 0 {
		return
	}

	var evt map[string]interface{}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}

	evtType := strings.TrimSpace(stringValue(evt["type"]))
	if evtType == "" {
		return
	}

	switch direction {
	case "client_to_upstream":
		w.observeClientEvent(evtType, evt)
	case "upstream_to_client":
		w.observeUpstreamEvent(evtType, evt)
	}
}

func (w *webSocketTelemetryCollector) observeClientEvent(evtType string, evt map[string]interface{}) {
	if evtType != "response.create" {
		return
	}

	now := w.now()
	convID := computeFingerprint(evt)
	meta := buildCodexRequestTelemetryAt(convID, evt, now)
	var sess *session
	if w.sessions != nil {
		sess = w.sessions.get(convID)
		sess.mu.Lock()
		sess.model = meta.ModelRequested
		sess.updatedAt = now
		sess.mu.Unlock()
		if template := deepCopyItem(evt); template != nil {
			delete(template, "input")
			sess.captureReplayTemplate(w.requestURI, template)
		}
	}

	captureSystemPrompt(w.shadowDir, convID, evt)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastCompleted = nil
	w.pending = append(w.pending, &webSocketTurnState{
		meta: meta,
		sess: sess,
	})
}

func (w *webSocketTelemetryCollector) observeUpstreamEvent(evtType string, evt map[string]interface{}) {
	switch evtType {
	case "response.created":
		if turn := w.assignTurn(evt); turn != nil && turn.firstUpstreamAt.IsZero() {
			turn.firstUpstreamAt = w.now()
		}
	case "codex.rate_limits":
		quotaSnapshot, ok := extractCodexQuotaEvent(evt)
		if !ok {
			return
		}
		if turn := w.activeTurn(); turn != nil {
			turn.quota = quotaPointer(quotaSnapshot, true)
			if !turn.quotaWritten {
				emitCodexQuotaSidecars(w.debugRecorder, quotaSnapshot, 1)
				turn.quotaWritten = true
			}
			return
		}
		if turn := w.recentCompletedTurn(); turn != nil {
			turn.quota = quotaPointer(quotaSnapshot, true)
			if !turn.quotaWritten {
				emitCodexQuotaSidecars(w.debugRecorder, quotaSnapshot, 1)
				turn.quotaWritten = true
			}
			if turn.recorded {
				backfillCodexRequestQuota(w.debugRecorder, turn.meta.RequestID, quotaSnapshot)
			}
			return
		}
		emitCodexQuotaSidecars(w.debugRecorder, quotaSnapshot, 1)
	case "response.output_item.done":
		if turn := w.activeTurn(); turn != nil {
			if turn.firstUpstreamAt.IsZero() {
				turn.firstUpstreamAt = w.now()
			}
			if item, ok := evt["item"].(map[string]interface{}); ok {
				if copied := deepCopyItem(item); copied != nil {
					turn.outputItems = append(turn.outputItems, copied)
				}
			}
		}
	case "response.completed":
		turn := w.assignTurn(evt)
		if turn == nil {
			return
		}
		if turn.firstUpstreamAt.IsZero() {
			turn.firstUpstreamAt = w.now()
		}

		respObj, _ := evt["response"].(map[string]interface{})
		usage := codexUsageStats{}
		modelResponse, stopReason := "", ""
		outputItems := copyOutputItems(turn.outputItems)
		if respObj != nil {
			usage = extractCodexUsage(respObj)
			modelResponse, stopReason = extractCodexResponseMetadata(respObj)
			if respOutput := extractOutputItemsFromResponse(respObj); len(respOutput) > 0 {
				outputItems = respOutput
			}
		}
		if turn.sess != nil && usage.InputTokens > 0 {
			now := w.now()
			turn.sess.mu.Lock()
			turn.sess.lastAPIInput = usage.InputTokens
			turn.sess.lastAPIInputAt = now
			turn.sess.updatedAt = now
			turn.sess.mu.Unlock()
		}
		emitCodexUsageSidecar(turn.meta.ConversationID, firstNonEmpty(modelResponse, turn.meta.ModelRequested), usage)
		if turn.quota != nil && turn.quota.Available && !turn.quotaWritten {
			emitCodexQuotaSidecars(w.debugRecorder, *turn.quota, 1)
			turn.quotaWritten = true
		}

		recordCodexRequestEvent(
			w.debugRecorder,
			turn.meta,
			turn.sess,
			modelResponse,
			stopReason,
			usage,
			turn.quota,
			outputItems,
			turn.firstUpstreamAt.Sub(turn.meta.StartedAt),
		)
		turn.recorded = true
		turn.completedAt = w.now()
		w.finishTurn(turn)
	default:
		if !strings.HasPrefix(evtType, "response.") {
			return
		}
		if turn := w.activeTurn(); turn != nil && turn.firstUpstreamAt.IsZero() {
			turn.firstUpstreamAt = w.now()
		}
	}
}

func (w *webSocketTelemetryCollector) activeTurn() *webSocketTurnState {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.activeRespID == "" {
		return nil
	}
	return w.byResponseID[w.activeRespID]
}

func (w *webSocketTelemetryCollector) recentCompletedTurn() *webSocketTurnState {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastCompleted == nil {
		return nil
	}
	if !w.lastCompleted.completedAt.IsZero() && w.now().Sub(w.lastCompleted.completedAt) > 5*time.Second {
		w.lastCompleted = nil
		return nil
	}
	if len(w.pending) > 0 || w.activeRespID != "" {
		return nil
	}
	return w.lastCompleted
}

func (w *webSocketTelemetryCollector) assignTurn(evt map[string]interface{}) *webSocketTurnState {
	respObj, _ := evt["response"].(map[string]interface{})
	respID := strings.TrimSpace(stringValue(respObj["id"]))

	w.mu.Lock()
	defer w.mu.Unlock()

	if respID != "" {
		if turn := w.byResponseID[respID]; turn != nil {
			w.activeRespID = respID
			return turn
		}
	}
	if len(w.pending) == 0 {
		return nil
	}

	turn := w.pending[0]
	w.pending = w.pending[1:]
	turn.responseID = respID
	if respID != "" {
		w.byResponseID[respID] = turn
		w.activeRespID = respID
	}
	return turn
}

func (w *webSocketTelemetryCollector) finishTurn(turn *webSocketTurnState) {
	if w == nil || turn == nil {
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if turn.responseID != "" {
		delete(w.byResponseID, turn.responseID)
		if w.activeRespID == turn.responseID {
			w.activeRespID = ""
		}
	}
	w.lastCompleted = turn
}

func copyOutputItems(items []map[string]interface{}) []map[string]interface{} {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if copied := deepCopyItem(item); copied != nil {
			out = append(out, copied)
		}
	}
	return out
}

func requestedWebSocketSubprotocols(headers http.Header) string {
	if len(headers) == 0 {
		return ""
	}
	return strings.Join(websocket.Subprotocols(&http.Request{Header: headers}), ",")
}
