// Package proxy — OpenAI-compatible lane handler.
//
// Completely isolated from the Anthropic pipeline. Handles:
//   - /v1/chat/completions requests (OpenAI Chat Completions format)
//   - Simple eviction-based context management
//   - Direct forwarding to an OpenAI-compatible upstream (e.g. llama.cpp, ollama)
//   - SSE relay for streaming responses
//
// Does NOT use: subagent classifier, ForceMode, SyspromptProcessor, serializer,
// cold gates, prefix warmer, usage spoofing, rate-limit spoofing, ITT fingerprinting,
// sycophancy detection, MCP cache, dedup, or any Anthropic-specific pipeline step.
package proxy

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// openAISession holds per-conversation state for an OpenAI lane.
type openAISession struct {
	mu              sync.Mutex
	messages        []openAIMsg // stored messages
	convID          string
	model           string
	requestURI      string
	requestTemplate map[string]interface{}
	lastAPIInput    int // last prompt_tokens from upstream
	lastAPIInputAt  time.Time
	evictedCount    int
	createdAt       time.Time
	updatedAt       time.Time
}

// openAIMsg is a cached message in OpenAI format.
type openAIMsg struct {
	Msg    map[string]interface{} // original OpenAI message (role, content, tool_calls, etc.)
	Tokens int                    // estimated token count
}

type openAITrimResult struct {
	Messages             []interface{}
	MessageTokens        int
	TotalTokens          int
	Evicted              int
	CompactedToolResults int
}

type openAIIngressCleanup struct {
	StrippedReasoning int
}

const (
	openAIToolResultOmittedMarker = "[tool result omitted:"
	openAIKeepRecentToolResults   = 2
	openAIKeepRecentUserTurns     = 4
	openAIToolHintMaxChars        = 96
)

// openAISessions manages per-conversation OpenAI caches.
type openAISessions struct {
	mu               sync.Mutex
	sessions         map[string]*openAISession
	authObserved     bool
	lastSeenAuthType string
	authHeader       string
}

type OpenAILaneAuthStatus struct {
	Lane                     string `json:"lane"`
	Upstream                 string `json:"upstream"`
	UpstreamConfigured       bool   `json:"upstream_configured"`
	RequestAuthPassthrough   bool   `json:"request_auth_passthrough"`
	AuthObserved             bool   `json:"auth_observed"`
	LastSeenAuthType         string `json:"last_seen_auth_type"`
	CapturedAuthAvailable    bool   `json:"captured_auth_available"`
	CapturedAuthType         string `json:"captured_auth_type"`
	AuthlessUpstreamLikely   bool   `json:"authless_upstream_likely"`
	SessionOwner             string `json:"session_owner"`
	SessionPersistence       string `json:"session_persistence"`
	SupportsStatelessProbe   bool   `json:"supports_stateless_probe"`
	SupportsLiveSessionProbe bool   `json:"supports_live_session_probe"`
	SupportsResumableSession bool   `json:"supports_resumable_session"`
	SessionCount             int    `json:"session_count"`
}

type OpenAILaneSessionSnapshot struct {
	ConvID         string `json:"conv_id"`
	Model          string `json:"model"`
	MessageCount   int    `json:"message_count"`
	TotalTokens    int    `json:"total_tokens"`
	EvictedCount   int    `json:"evicted_count"`
	LastAPIInput   int    `json:"last_api_input"`
	LastAPIInputAt string `json:"last_api_input_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type OpenAILaneSessionReplay struct {
	Lane                  string                    `json:"lane"`
	ConvID                string                    `json:"conv_id"`
	RequestURI            string                    `json:"request_uri"`
	ProxyTemplate         map[string]interface{}    `json:"proxy_template"`
	LiveMessages          []interface{}             `json:"live_messages"`
	Snapshot              OpenAILaneSessionSnapshot `json:"snapshot"`
	CapturedAuthAvailable bool                      `json:"captured_auth_available"`
	CapturedAuthType      string                    `json:"captured_auth_type"`
}

func newOpenAISessions() *openAISessions {
	return &openAISessions{
		sessions: make(map[string]*openAISession),
	}
}

func (s *openAISessions) get(convID string) *openAISession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[convID]; ok {
		return sess
	}
	now := time.Now()
	sess := &openAISession{convID: convID, createdAt: now, updatedAt: now}
	s.sessions[convID] = sess
	return sess
}

func (s *openAISessions) observeAuth(header string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authObserved = true
	s.lastSeenAuthType = classifyOpenAIAuthHeader(header)
	if strings.TrimSpace(header) != "" {
		s.authHeader = header
	}
}

func (s *openAISessions) capturedAuthStatus() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	authHeader := strings.TrimSpace(s.authHeader)
	if authHeader == "" {
		return false, "unavailable"
	}
	return true, classifyOpenAIAuthHeader(authHeader)
}

func openAIUpstreamLikelyAuthless(upstream string) bool {
	upstream = strings.TrimSpace(upstream)
	if upstream == "" {
		return false
	}
	parsed, err := url.Parse(upstream)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		host = strings.ToLower(strings.TrimSpace(parsed.Host))
	}
	if host == "localhost" || host == "0.0.0.0" || host == "host.docker.internal" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func (s *openAISessions) authStatus(upstream string) OpenAILaneAuthStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	lastSeen := s.lastSeenAuthType
	if lastSeen == "" {
		lastSeen = "unknown"
	}
	captured := strings.TrimSpace(s.authHeader) != ""
	capturedType := "unavailable"
	if captured {
		capturedType = classifyOpenAIAuthHeader(s.authHeader)
	}
	authlessLikely := openAIUpstreamLikelyAuthless(upstream)
	return OpenAILaneAuthStatus{
		Lane:                     "openai",
		Upstream:                 upstream,
		UpstreamConfigured:       strings.TrimSpace(upstream) != "",
		RequestAuthPassthrough:   true,
		AuthObserved:             s.authObserved,
		LastSeenAuthType:         lastSeen,
		CapturedAuthAvailable:    captured,
		CapturedAuthType:         capturedType,
		AuthlessUpstreamLikely:   authlessLikely,
		SessionOwner:             "glass_openai_lane",
		SessionPersistence:       "live_memory_with_replay_template",
		SupportsStatelessProbe:   true,
		SupportsLiveSessionProbe: true,
		SupportsResumableSession: false,
		SessionCount:             len(s.sessions),
	}
}

func (s *openAISessions) applyCapturedAuth(req *http.Request) {
	if req == nil {
		return
	}
	s.mu.Lock()
	authHeader := strings.TrimSpace(s.authHeader)
	s.mu.Unlock()
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
}

func (s *openAISessions) snapshots() []OpenAILaneSessionSnapshot {
	s.mu.Lock()
	sessions := make([]*openAISession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()

	snapshots := make([]OpenAILaneSessionSnapshot, 0, len(sessions))
	for _, sess := range sessions {
		snapshots = append(snapshots, sess.snapshot())
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].UpdatedAt > snapshots[j].UpdatedAt
	})
	return snapshots
}

func (s *openAISessions) snapshot(convID string) (OpenAILaneSessionSnapshot, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[convID]
	s.mu.Unlock()
	if !ok {
		return OpenAILaneSessionSnapshot{}, false
	}
	return sess.snapshot(), true
}

func (s *openAISessions) replay(convID string) (OpenAILaneSessionReplay, bool) {
	s.mu.Lock()
	sess, ok := s.sessions[convID]
	s.mu.Unlock()
	if !ok {
		return OpenAILaneSessionReplay{}, false
	}
	return sess.replay(), true
}

// openAIFingerprint computes a session ID from the system message + model.
func openAIFingerprint(body map[string]interface{}) string {
	h := sha256.New()

	// Hash the system message (first message if role=system)
	if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) > 0 {
		if first, ok := msgs[0].(map[string]interface{}); ok {
			if role, _ := first["role"].(string); role == "system" {
				if content, _ := first["content"].(string); content != "" {
					h.Write([]byte(content))
				}
			}
		}
	}

	// Hash the model name
	if model, _ := body["model"].(string); model != "" {
		h.Write([]byte(model))
	}

	return fmt.Sprintf("%x", h.Sum(nil))[:12]
}

// estimateOpenAITokens estimates token count via json.Marshal / 4.
func estimateOpenAITokens(v interface{}) int {
	data, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(data) / 4
}

// ingestOpenAIMessages adds new messages from the request into the session cache.
// Returns number of new messages added.
func (sess *openAISession) ingest(msgs []interface{}) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	if len(msgs) <= len(sess.messages) {
		return 0
	}

	added := 0
	for i := len(sess.messages); i < len(msgs); i++ {
		raw, ok := msgs[i].(map[string]interface{})
		if !ok {
			continue
		}
		// Deep copy to avoid mutation
		copied := deepCopyOpenAIMsg(raw)
		sess.messages = append(sess.messages, openAIMsg{
			Msg:    copied,
			Tokens: estimateOpenAITokens(copied),
		})
		added++
	}
	if added > 0 {
		sess.updatedAt = time.Now()
	}
	return added
}

// totalTokens returns total estimated tokens across all cached messages.
func (sess *openAISession) totalTokens() int {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	total := 0
	for _, m := range sess.messages {
		total += m.Tokens
	}
	return total
}

// evict removes oldest messages (after system) until under targetTokens.
// Keeps the first message (system) and last anchorKeep messages.
func (sess *openAISession) evict(targetTokens, anchorKeep int) int {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	total := 0
	for _, m := range sess.messages {
		total += m.Tokens
	}
	if total <= targetTokens {
		return 0
	}

	// Find system message boundary (skip index 0 if system)
	start := 0
	if len(sess.messages) > 0 {
		if role, _ := sess.messages[0].Msg["role"].(string); role == "system" {
			start = 1
		}
	}

	// Keep last anchorKeep messages
	keepFrom := len(sess.messages) - anchorKeep
	if keepFrom <= start {
		return 0
	}

	evicted := 0
	for total > targetTokens && start < keepFrom {
		total -= sess.messages[start].Tokens
		start++
		evicted++
	}

	if evicted > 0 {
		// Rebuild: keep system (if any) + remaining
		var kept []openAIMsg
		if start > 0 {
			// Check if message[0] was system
			if role, _ := sess.messages[0].Msg["role"].(string); role == "system" {
				kept = append(kept, sess.messages[0])
			}
		}
		// Find actual eviction boundary accounting for system message
		sysOffset := 0
		if len(kept) > 0 {
			sysOffset = 1
		}
		evictEnd := sysOffset + evicted
		kept = append(kept, sess.messages[evictEnd:]...)
		sess.messages = kept
		sess.evictedCount += evicted
		sess.updatedAt = time.Now()
	}
	return evicted
}

// buildMessages returns the current cached messages for the request.
func (sess *openAISession) buildMessages() []interface{} {
	sess.mu.Lock()
	defer sess.mu.Unlock()

	result := make([]interface{}, len(sess.messages))
	for i, m := range sess.messages {
		result[i] = m.Msg
	}
	return result
}

func (sess *openAISession) snapshot() OpenAILaneSessionSnapshot {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	total := 0
	for _, msg := range sess.messages {
		total += msg.Tokens
	}
	return OpenAILaneSessionSnapshot{
		ConvID:         sess.convID,
		Model:          sess.model,
		MessageCount:   len(sess.messages),
		TotalTokens:    total,
		EvictedCount:   sess.evictedCount,
		LastAPIInput:   sess.lastAPIInput,
		LastAPIInputAt: formatOpenAILaneTime(sess.lastAPIInputAt),
		CreatedAt:      formatOpenAILaneTime(sess.createdAt),
		UpdatedAt:      formatOpenAILaneTime(sess.updatedAt),
	}
}

func (sess *openAISession) captureReplayTemplate(requestURI string, template map[string]interface{}) {
	if requestURI == "" || template == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.requestURI = requestURI
	sess.requestTemplate = deepCopyOpenAIMsg(template)
	sess.updatedAt = time.Now()
}

func (sess *openAISession) replay() OpenAILaneSessionReplay {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	template := deepCopyOpenAIMsg(sess.requestTemplate)
	liveMessages := make([]interface{}, len(sess.messages))
	for i, msg := range sess.messages {
		liveMessages[i] = deepCopyOpenAIMsg(msg.Msg)
	}
	return OpenAILaneSessionReplay{
		Lane:          "openai",
		ConvID:        sess.convID,
		RequestURI:    sess.requestURI,
		ProxyTemplate: template,
		LiveMessages:  liveMessages,
		Snapshot: OpenAILaneSessionSnapshot{
			ConvID:         sess.convID,
			Model:          sess.model,
			MessageCount:   len(sess.messages),
			TotalTokens:    sumOpenAIMsgTokens(sess.messages),
			EvictedCount:   sess.evictedCount,
			LastAPIInput:   sess.lastAPIInput,
			LastAPIInputAt: formatOpenAILaneTime(sess.lastAPIInputAt),
			CreatedAt:      formatOpenAILaneTime(sess.createdAt),
			UpdatedAt:      formatOpenAILaneTime(sess.updatedAt),
		},
	}
}

func (sess *openAISession) recordPromptTokens(promptTokens int) {
	if promptTokens <= 0 {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.lastAPIInput = promptTokens
	sess.lastAPIInputAt = time.Now()
	sess.updatedAt = time.Now()
}

func (sess *openAISession) appendAssistantMessage(msg map[string]interface{}) {
	if msg == nil {
		return
	}
	copied := deepCopyOpenAIMsg(msg)
	if copied == nil {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.messages = append(sess.messages, openAIMsg{
		Msg:    copied,
		Tokens: estimateOpenAITokens(copied),
	})
	sess.updatedAt = time.Now()
}

func formatOpenAILaneTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func classifyOpenAIAuthHeader(header string) string {
	header = strings.TrimSpace(header)
	switch {
	case header == "":
		return "none"
	case strings.HasPrefix(strings.ToLower(header), "bearer "):
		return "bearer"
	default:
		return "other"
	}
}

func deepCopyOpenAIMsg(msg map[string]interface{}) map[string]interface{} {
	data, err := json.Marshal(msg)
	if err != nil {
		return msg
	}
	var copy map[string]interface{}
	json.Unmarshal(data, &copy)
	return copy
}

func buildOpenAIRequestMessages(msgs []interface{}, toolTokens, triggerTokens, targetTokens int) (openAITrimResult, error) {
	cached := make([]openAIMsg, 0, len(msgs))
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		copied := deepCopyOpenAIMsg(msg)
		cached = append(cached, openAIMsg{
			Msg:    copied,
			Tokens: estimateOpenAITokens(copied),
		})
	}

	msgTokens := sumOpenAIMsgTokens(cached)
	result := openAITrimResult{
		Messages:      openAIMsgsToInterfaces(cached),
		MessageTokens: msgTokens,
		TotalTokens:   msgTokens + toolTokens,
	}
	if result.TotalTokens <= triggerTokens {
		return result, nil
	}
	if len(cached) == 0 {
		return result, fmt.Errorf("request (%d estimated tokens) still exceeds the configured live context budget (%d estimated tokens) after dropping %d old messages", result.TotalTokens, triggerTokens, result.Evicted)
	}

	start := 0
	if role, _ := cached[0].Msg["role"].(string); role == "system" {
		start = 1
	}
	tailStart := findOpenAILiveTailStart(cached, start)
	evictStart := start
	for msgTokens+toolTokens > targetTokens && evictStart < tailStart {
		msgTokens -= cached[evictStart].Tokens
		evictStart++
		result.Evicted++
	}

	kept := make([]openAIMsg, 0, len(cached)-result.Evicted)
	if start == 1 {
		kept = append(kept, cached[0])
	}
	if evictStart < len(cached) {
		kept = append(kept, cached[evictStart:]...)
	}

	msgTokens = sumOpenAIMsgTokens(kept)
	result.Messages = openAIMsgsToInterfaces(kept)
	result.MessageTokens = msgTokens
	result.TotalTokens = msgTokens + toolTokens
	if result.TotalTokens > targetTokens {
		msgTargetTokens := targetTokens - toolTokens
		if msgTargetTokens < 0 {
			msgTargetTokens = 0
		}
		kept, result.CompactedToolResults = compactOpenAIToolResults(kept, msgTargetTokens)
		msgTokens = sumOpenAIMsgTokens(kept)
		result.Messages = openAIMsgsToInterfaces(kept)
		result.MessageTokens = msgTokens
		result.TotalTokens = msgTokens + toolTokens
	}
	if result.TotalTokens > triggerTokens {
		return result, fmt.Errorf("request (%d estimated tokens) still exceeds the configured live context budget (%d estimated tokens) after dropping %d old messages", result.TotalTokens, triggerTokens, result.Evicted)
	}
	return result, nil
}

func sumOpenAIMsgTokens(msgs []openAIMsg) int {
	total := 0
	for _, msg := range msgs {
		total += msg.Tokens
	}
	return total
}

func openAIMsgsToInterfaces(msgs []openAIMsg) []interface{} {
	result := make([]interface{}, len(msgs))
	for i, msg := range msgs {
		result[i] = msg.Msg
	}
	return result
}

func findOpenAILiveTailStart(msgs []openAIMsg, defaultStart int) int {
	if len(msgs) == 0 {
		return 0
	}

	firstUser := -1
	seenUsers := 0
	for i := len(msgs) - 1; i >= defaultStart; i-- {
		role, _ := msgs[i].Msg["role"].(string)
		if role == "user" {
			firstUser = i
			seenUsers++
			if seenUsers >= openAIKeepRecentUserTurns {
				return i
			}
		}
	}
	if firstUser >= 0 {
		return firstUser
	}
	if defaultStart < len(msgs) {
		return len(msgs) - 1
	}
	return 0
}

func compactOpenAIToolResults(msgs []openAIMsg, targetTokens int) ([]openAIMsg, int) {
	if len(msgs) == 0 {
		return msgs, 0
	}

	toolIdxs := openAIToolMessageIndices(msgs)
	if len(toolIdxs) == 0 {
		return msgs, 0
	}

	keepFrom := len(toolIdxs) - openAIKeepRecentToolResults
	if keepFrom < 0 {
		keepFrom = 0
	}

	compacted := 0
	for _, phase := range [][]int{toolIdxs[:keepFrom], toolIdxs[keepFrom:]} {
		for _, idx := range phase {
			if sumOpenAIMsgTokens(msgs) <= targetTokens {
				return msgs, compacted
			}
			updated, changed := compactOpenAIToolMessage(msgs, idx)
			if !changed {
				continue
			}
			msgs[idx] = updated
			compacted++
		}
	}

	return msgs, compacted
}

func openAIToolMessageIndices(msgs []openAIMsg) []int {
	idxs := make([]int, 0, len(msgs))
	for i, msg := range msgs {
		role, _ := msg.Msg["role"].(string)
		if role == "tool" {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

func compactOpenAIToolMessage(msgs []openAIMsg, idx int) (openAIMsg, bool) {
	if idx < 0 || idx >= len(msgs) {
		return openAIMsg{}, false
	}

	msg := msgs[idx]
	role, _ := msg.Msg["role"].(string)
	if role != "tool" {
		return msg, false
	}

	if content, _ := msg.Msg["content"].(string); strings.HasPrefix(content, openAIToolResultOmittedMarker) {
		return msg, false
	}

	placeholder := buildOpenAIToolResultPlaceholder(msgs, idx)
	if placeholder == "" {
		return msg, false
	}

	copied := deepCopyOpenAIMsg(msg.Msg)
	copied["content"] = placeholder
	updated := openAIMsg{
		Msg:    copied,
		Tokens: estimateOpenAITokens(copied),
	}
	if updated.Tokens >= msg.Tokens {
		return msg, false
	}
	return updated, true
}

func buildOpenAIToolResultPlaceholder(msgs []openAIMsg, idx int) string {
	if idx < 0 || idx >= len(msgs) {
		return ""
	}
	msg := msgs[idx].Msg
	desc := describeOpenAIToolCall(msgs, idx)
	if desc == "" {
		desc = "tool call"
	}
	chars := openAIToolResultChars(msg["content"])
	if chars > 0 {
		return fmt.Sprintf("%s %s; original %d chars]", openAIToolResultOmittedMarker, desc, chars)
	}
	return fmt.Sprintf("%s %s]", openAIToolResultOmittedMarker, desc)
}

func describeOpenAIToolCall(msgs []openAIMsg, idx int) string {
	msg := msgs[idx].Msg
	if name, _ := msg["name"].(string); name != "" {
		return name + "()"
	}

	toolCallID, _ := msg["tool_call_id"].(string)
	for i := idx - 1; i >= 0; i-- {
		role, _ := msgs[i].Msg["role"].(string)
		if role != "assistant" {
			continue
		}
		if desc := lookupOpenAIToolCallDescription(msgs[i].Msg, toolCallID); desc != "" {
			return desc
		}
	}

	if toolCallID != "" {
		return "tool_call_id=" + trimOpenAIHint(toolCallID, 24)
	}
	return ""
}

func lookupOpenAIToolCallDescription(msg map[string]interface{}, toolCallID string) string {
	toolCalls, _ := msg["tool_calls"].([]interface{})
	if len(toolCalls) == 0 {
		return ""
	}

	if toolCallID != "" {
		for _, raw := range toolCalls {
			toolCall, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if id, _ := toolCall["id"].(string); id == toolCallID {
				return renderOpenAIToolCall(toolCall)
			}
		}
	}

	if len(toolCalls) == 1 {
		if toolCall, ok := toolCalls[0].(map[string]interface{}); ok {
			return renderOpenAIToolCall(toolCall)
		}
	}

	return ""
}

func renderOpenAIToolCall(toolCall map[string]interface{}) string {
	fn, _ := toolCall["function"].(map[string]interface{})
	name, _ := fn["name"].(string)
	if name == "" {
		name = "tool"
	}

	argHint := pickOpenAIToolParam(parseOpenAIToolArguments(fn["arguments"]))
	if argHint == "" {
		return name + "()"
	}
	return name + "(" + trimOpenAIHint(argHint, openAIToolHintMaxChars) + ")"
}

func parseOpenAIToolArguments(raw interface{}) map[string]interface{} {
	switch v := raw.(type) {
	case map[string]interface{}:
		return v
	case string:
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			return parsed
		}
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return map[string]interface{}{"arguments": v}
	default:
		return nil
	}
}

func pickOpenAIToolParam(input map[string]interface{}) string {
	if input == nil {
		return ""
	}

	for _, key := range []string{
		"path", "file_path", "command", "query", "url",
		"pattern", "glob", "offset", "limit", "arguments",
	} {
		switch v := input[key].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return key + "=" + v
			}
		case float64:
			return fmt.Sprintf("%s=%g", key, v)
		case int:
			return fmt.Sprintf("%s=%d", key, v)
		}
	}

	for key, raw := range input {
		if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
			return key + "=" + s
		}
	}

	return ""
}

func openAIToolResultChars(content interface{}) int {
	switch v := content.(type) {
	case string:
		return len(v)
	case []interface{}:
		total := 0
		for _, raw := range v {
			switch inner := raw.(type) {
			case string:
				total += len(inner)
			case map[string]interface{}:
				if text, _ := inner["text"].(string); text != "" {
					total += len(text)
					continue
				}
				if data, err := json.Marshal(inner); err == nil {
					total += len(data)
				}
			default:
				if data, err := json.Marshal(inner); err == nil {
					total += len(data)
				}
			}
		}
		return total
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return 0
		}
		return len(data)
	}
}

func trimOpenAIHint(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max-3] + "..."
}

func cleanOpenAIHarnessMessages(msgs []interface{}) ([]interface{}, openAIIngressCleanup) {
	cleaned := make([]interface{}, 0, len(msgs))
	var stats openAIIngressCleanup

	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			cleaned = append(cleaned, raw)
			continue
		}

		copied := deepCopyOpenAIMsg(msg)
		if _, ok := copied["reasoning_content"]; ok {
			delete(copied, "reasoning_content")
			stats.StrippedReasoning++
		}
		cleaned = append(cleaned, copied)
	}

	return cleaned, stats
}

// stripOpenAIToolsToAllowlist filters body["tools"] down to the allowlist
// (matched on function name). Empty allowlist = keep everything. Returns
// the number of schemas removed.
func stripOpenAIToolsToAllowlist(body map[string]interface{}, allowlist []string) int {
	if len(allowlist) == 0 {
		return 0
	}
	tools, ok := body["tools"].([]interface{})
	if !ok || len(tools) == 0 {
		return 0
	}
	allowed := make(map[string]bool, len(allowlist))
	for _, name := range allowlist {
		allowed[strings.TrimSpace(name)] = true
	}
	kept := make([]interface{}, 0, len(tools))
	stripped := 0
	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			kept = append(kept, tool)
			continue
		}
		fn, _ := tm["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		if !allowed[name] {
			stripped++
			continue
		}
		kept = append(kept, tool)
	}
	body["tools"] = kept
	return stripped
}

// handleOpenAIRequest handles /v1/chat/completions — completely isolated from Anthropic lane.
func (p *Proxy) handleOpenAIRequest(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	laneService := p.OpenAICompatibleLaneService()
	if laneService == nil || laneService.Upstream() == "" {
		proxyErrorJSON(w, r, "configuration_error", "no OpenAI upstream configured", http.StatusBadGateway)
		return
	}
	laneService.ObserveAuth(r.Header.Get("Authorization"))

	// Read body
	const maxBody = 10 * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	bodyBytes, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		proxyErrorJSON(w, r, "invalid_request_error", "request body too large or unreadable", http.StatusBadRequest)
		return
	}

	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		proxyErrorJSON(w, r, "invalid_request_error", "invalid JSON body", http.StatusBadRequest)
		return
	}
	cfg := p.configLoader.Get()
	if err := p.sanitizeOpenAIRequestBody(r.Context(), body, cfg); err != nil {
		log.Printf("[SECURITY] openai ingress scan failed: %v", err)
		if cfg.SecurityGuardEnabled && cfg.SecurityGuardFailClosed {
			proxyErrorJSON(w, r, "blocked", "security guard rejected unsafe tool output", http.StatusForbidden)
			return
		}
	}

	msgs, _ := body["messages"].([]interface{})
	msgs, cleanup := cleanOpenAIHarnessMessages(msgs)
	body["messages"] = msgs

	convID := openAIFingerprint(body)
	model, _ := body["model"].(string)
	reqTelemetry := buildOpenAIRequestTelemetryAt(convID, body, startedAt)
	log.Printf("[OPENAI] %s conv=%s from %s", model, convID, r.RemoteAddr)
	var sess *openAISession
	if laneService != nil {
		sess = laneService.getSession(convID)
		sess.mu.Lock()
		sess.model = model
		sess.updatedAt = time.Now()
		sess.mu.Unlock()
	}
	if cleanup.StrippedReasoning > 0 {
		log.Printf("[OPENAI] conv=%s stripped reasoning from %d assistant steps",
			convID, cleanup.StrippedReasoning)
	}

	// Check eviction thresholds
	triggerTokens := p.glassEngine.Config().OpenAIEvictTriggerTokens
	targetTokens := p.glassEngine.Config().OpenAIEvictTargetTokens
	if triggerTokens == 0 {
		triggerTokens = 55000
	}
	if targetTokens == 0 {
		targetTokens = 45000
	}

	// Tool-schema budget: with a configured allowlist, strip schemas
	// outside it BEFORE the context budget counts. Schemas are large and
	// constant per role (e.g. an advisor role needs no execution tools).
	// Idempotent: already-stripped requests strip nothing on later turns,
	// so the token-prefix cache upstream stays stable.
	strippedTools := stripOpenAIToolsToAllowlist(body, p.glassEngine.Config().OpenAIToolAllowlist)
	if strippedTools > 0 {
		log.Printf("[OPENAI] conv=%s stripped %d tool schemas outside allowlist",
			convID, strippedTools)
	}

	toolTokens := estimateOpenAITokens(body["tools"])
	trimmed, trimErr := buildOpenAIRequestMessages(msgs, toolTokens, triggerTokens, targetTokens)
	if trimErr != nil {
		proxyOpenAIError(w, http.StatusBadRequest, "exceed_context_size_error", trimErr.Error(), map[string]interface{}{
			"n_prompt_tokens":          trimmed.TotalTokens,
			"n_ctx_budget":             triggerTokens,
			"n_evicted":                trimmed.Evicted,
			"n_compacted_tool_results": trimmed.CompactedToolResults,
		})
		return
	}
	if trimmed.Evicted > 0 {
		log.Printf("[OPENAI] conv=%s evicted %d messages (%d tokens -> %d, trigger=%d, target=%d)",
			convID, trimmed.Evicted, estimateOpenAITokens(msgs)+toolTokens, trimmed.TotalTokens, triggerTokens, targetTokens)
	}
	if trimmed.CompactedToolResults > 0 {
		log.Printf("[OPENAI] conv=%s compacted %d tool results inside live tail (total=%d, target=%d)",
			convID, trimmed.CompactedToolResults, trimmed.TotalTokens, targetTokens)
	}

	// Rebuild body with the request-authoritative trimmed view.
	body["messages"] = trimmed.Messages
	if sess != nil {
		if template := deepCopyOpenAIMsg(body); template != nil {
			delete(template, "messages")
			sess.captureReplayTemplate(r.URL.RequestURI(), template)
		}
		sess.ingest(trimmed.Messages)
	}

	modifiedBody, err := json.Marshal(body)
	if err != nil {
		proxyErrorJSON(w, r, "api_error", "internal error", http.StatusInternalServerError)
		return
	}

	log.Printf("[OPENAI] conv=%s forwarding %d bytes to %s", convID, len(modifiedBody), laneService.Upstream())

	stream, _ := body["stream"].(bool)
	var respTelemetry openAIResponseTelemetry
	if stream {
		respTelemetry = p.handleOpenAIStreaming(w, r, modifiedBody, convID, model, startedAt)
	} else {
		respTelemetry = p.handleOpenAINonStreaming(w, r, modifiedBody, convID, model, startedAt)
	}
	if p.debugRecorder != nil {
		recordOpenAIRequestEvent(p.debugRecorder, reqTelemetry, sess, respTelemetry)
	}
}

func appendOpenAIResponseToSession(p *Proxy, convID string, promptTokens int, assistantMsg map[string]interface{}) {
	if p == nil || p.OpenAICompatibleLaneService() == nil {
		return
	}
	sess := p.OpenAICompatibleLaneService().getSession(convID)
	sess.recordPromptTokens(promptTokens)
	if assistantMsg != nil {
		sess.appendAssistantMessage(assistantMsg)
	}
}

func extractOpenAIAssistantMessage(payload map[string]interface{}) map[string]interface{} {
	choices, _ := payload["choices"].([]interface{})
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]interface{})
	message, _ := choice["message"].(map[string]interface{})
	if message == nil {
		return nil
	}
	role, _ := message["role"].(string)
	if role == "" {
		role = "assistant"
		message["role"] = role
	}
	return deepCopyOpenAIMsg(message)
}

// handleOpenAIStreaming handles streaming SSE responses from an OpenAI-compatible upstream.
//
// Ollama (and other local inference servers) can take 30-60+ seconds before
// emitting the first SSE event while the model processes input (prompt eval).
// To prevent the OMP client's stall detector from timing out during this gap,
// we immediately commit 200 + SSE headers to the client and start sending
// harmless empty OpenAI-style chunk events before the upstream request completes.
func (p *Proxy) handleOpenAIStreaming(w http.ResponseWriter, r *http.Request, body []byte, convID string, model string, startedAt time.Time) openAIResponseTelemetry {
	telemetry := openAIResponseTelemetry{ModelResponse: model}
	targetURL := strings.TrimRight(p.OpenAICompatibleLaneService().Upstream(), "/") + "/v1/chat/completions"

	// Commit SSE response headers immediately — the client must see activity
	// within its stall window, and the upstream can take 30+ seconds to respond.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[OPENAI] ResponseWriter does not support flushing")
		telemetry.StopReason = "streaming_unsupported"
		return telemetry
	}
	flusher.Flush()

	// Mutex guards all writes to the ResponseWriter. Required because the
	// heartbeat goroutine writes concurrently with the main SSE relay loop.
	var writeMu sync.Mutex

	writeEvent := func(lines []string) {
		writeMu.Lock()
		defer writeMu.Unlock()
		for _, line := range lines {
			w.Write([]byte(line + "\n"))
		}
		w.Write([]byte("\n"))
		flusher.Flush()
	}

	// Emit an immediate heartbeat event, then continue emitting empty chunks
	// until the stream finishes. Some clients ignore SSE comments entirely, so
	// the heartbeat needs to look like a real OpenAI chunk event.
	writeEvent(buildOpenAIHeartbeatEvent(convID, model))

	kaDone := make(chan struct{})
	var kaOnce sync.Once
	stopKeepalive := func() {
		kaOnce.Do(func() {
			close(kaDone)
		})
	}
	go func() {
		ticker := time.NewTicker(p.openAIStreamHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-kaDone:
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
				writeEvent(buildOpenAIHeartbeatEvent(convID, model))
			}
		}
	}()
	defer stopKeepalive()

	// Now make the upstream request (may block for 30+ seconds during prompt eval)
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		stopKeepalive()
		writeEvent([]string{"data: " + openAIErrorJSON("api_error", "internal error")})
		writeEvent([]string{"data: [DONE]"})
		telemetry.StopReason = "request_build_error"
		return telemetry
	}

	upReq.Header.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		upReq.Header.Set("Authorization", auth)
	}
	upReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("[OPENAI] upstream error: %v", err)
		stopKeepalive()
		writeEvent([]string{"data: " + openAIErrorJSON("upstream_error", "upstream connection failed: "+err.Error())})
		writeEvent([]string{"data: [DONE]"})
		telemetry.StopReason = "upstream_error"
		return telemetry
	}
	defer resp.Body.Close()

	log.Printf("[OPENAI] upstream responded %d %s (content-type: %s)", resp.StatusCode, resp.Status, resp.Header.Get("Content-Type"))

	// For error responses, wrap the body in an SSE error event (we already sent 200).
	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		log.Printf("[OPENAI] upstream error body: %s", errBody)
		stopKeepalive()
		writeEvent([]string{"data: " + openAIErrorJSON("upstream_error", string(errBody))})
		writeEvent([]string{"data: [DONE]"})
		telemetry.StopReason = fmt.Sprintf("http_%d", resp.StatusCode)
		return telemetry
	}

	cfg := p.configLoader.Get()
	toolGuard := newOpenAIToolStreamGuard(p, r.Context(), cfg)

	// SSE relay: forward complete events, block unsafe streamed tool calls,
	// and extract usage from final chunks.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var assistantRole = "assistant"
	var assistantContent strings.Builder
	var eventLines []string

	processEvent := func(lines []string) {
		if len(lines) == 0 {
			return
		}

		toEmit, err := toolGuard.Process(lines)
		if err != nil {
			log.Printf("[SECURITY] openai streaming tool scan failed: %v", err)
			if cfg.SecurityGuardEnabled && cfg.SecurityGuardFailClosed {
				writeEvent(buildOpenAIBlockedToolEvent("", "", 0, []string{"security verifier failed"}))
			} else {
				writeEvent(lines)
			}
			return
		}

		for _, event := range toEmit {
			for _, line := range event {
				if strings.HasPrefix(line, "data: ") {
					data := strings.TrimPrefix(line, "data: ")
					if data != "[DONE]" {
						if telemetry.TTFT == 0 {
							telemetry.TTFT = time.Since(startedAt)
						}
						var chunk map[string]interface{}
						if json.Unmarshal([]byte(data), &chunk) == nil {
							parsed := extractOpenAIResponseTelemetry(chunk)
							if parsed.ModelResponse != "" {
								telemetry.ModelResponse = parsed.ModelResponse
							}
							if parsed.StopReason != "" {
								telemetry.StopReason = parsed.StopReason
							}
							if parsed.InputTokens > 0 {
								telemetry.InputTokens = parsed.InputTokens
							}
							if parsed.OutputTokens > 0 {
								telemetry.OutputTokens = parsed.OutputTokens
							}
							if parsed.CacheReadTokens > 0 {
								telemetry.CacheReadTokens = parsed.CacheReadTokens
							}
							if parsed.HasToolUse {
								telemetry.HasToolUse = true
							}
							if choices, ok := chunk["choices"].([]interface{}); ok {
								for _, rawChoice := range choices {
									choice, _ := rawChoice.(map[string]interface{})
									delta, _ := choice["delta"].(map[string]interface{})
									if delta == nil {
										continue
									}
									if role, _ := delta["role"].(string); role != "" {
										assistantRole = role
									}
									if toolCalls, _ := delta["tool_calls"].([]interface{}); len(toolCalls) > 0 {
										telemetry.HasToolUse = true
									}
									if content, _ := delta["content"].(string); content != "" {
										assistantContent.WriteString(content)
									}
								}
							}
						}
					}
				}
			}
			writeEvent(event)
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			processEvent(eventLines)
			eventLines = eventLines[:0]
			continue
		}
		eventLines = append(eventLines, line)
	}
	stopKeepalive()
	processEvent(eventLines)
	writeMu.Lock()
	flusher.Flush()
	writeMu.Unlock()

	assistantMsg := map[string]interface{}(nil)
	if assistantContent.Len() > 0 {
		assistantMsg = map[string]interface{}{"role": assistantRole, "content": assistantContent.String()}
		if telemetry.OutputPreview == "" {
			_, telemetry.OutputPreview = summarizeOpenAIAssistantMessage(assistantMsg)
		}
	}
	appendOpenAIResponseToSession(p, convID, telemetry.InputTokens, assistantMsg)
	if telemetry.InputTokens > 0 {
		log.Printf("[OPENAI] conv=%s stream done: prompt=%d completion=%d", convID, telemetry.InputTokens, telemetry.OutputTokens)
	}
	return telemetry
}

// handleOpenAINonStreaming handles non-streaming responses from an OpenAI-compatible upstream.
func (p *Proxy) handleOpenAINonStreaming(w http.ResponseWriter, r *http.Request, body []byte, convID string, model string, startedAt time.Time) openAIResponseTelemetry {
	telemetry := openAIResponseTelemetry{ModelResponse: model, TTFT: time.Since(startedAt)}
	targetURL := strings.TrimRight(p.OpenAICompatibleLaneService().Upstream(), "/") + "/v1/chat/completions"

	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		proxyErrorJSON(w, r, "api_error", "internal error", http.StatusInternalServerError)
		telemetry.StopReason = "request_build_error"
		return telemetry
	}

	upReq.Header.Set("Content-Type", "application/json")
	if auth := r.Header.Get("Authorization"); auth != "" {
		upReq.Header.Set("Authorization", auth)
	}

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(upReq)
	if err != nil {
		log.Printf("[OPENAI] upstream error: %v", err)
		proxyErrorJSON(w, r, "upstream_error", "upstream connection failed: "+err.Error(), http.StatusBadGateway)
		telemetry.StopReason = "upstream_error"
		return telemetry
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		proxyErrorJSON(w, r, "api_error", "failed to read upstream response", http.StatusBadGateway)
		telemetry.StopReason = "read_error"
		return telemetry
	}
	cfg := p.configLoader.Get()
	securedBody, secErr := p.secureOpenAINonStreamingResponse(r.Context(), respBody, cfg)
	if secErr != nil {
		log.Printf("[SECURITY] openai response scan failed: %v", secErr)
		if cfg.SecurityGuardEnabled && cfg.SecurityGuardFailClosed {
			proxyErrorJSON(w, r, "blocked", "security guard blocked unsafe tool call", http.StatusForbidden)
			telemetry.StopReason = "security_blocked"
			return telemetry
		}
	} else {
		respBody = securedBody
	}
	var parsedResp map[string]interface{}
	if json.Unmarshal(respBody, &parsedResp) == nil {
		telemetry = extractOpenAIResponseTelemetry(parsedResp)
		if telemetry.ModelResponse == "" {
			telemetry.ModelResponse = model
		}
		telemetry.TTFT = time.Since(startedAt)
		appendOpenAIResponseToSession(p, convID, telemetry.InputTokens, extractOpenAIAssistantMessage(parsedResp))
	}

	// Forward response
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)

	log.Printf("[OPENAI] conv=%s non-streaming done: %d bytes model=%s", convID, len(respBody), model)
	if resp.StatusCode >= http.StatusBadRequest && telemetry.StopReason == "" {
		telemetry.StopReason = fmt.Sprintf("http_%d", resp.StatusCode)
	}
	return telemetry
}

// openAIErrorJSON returns a minimal OpenAI-format SSE error payload.
// Used when we've already committed 200 headers and need to report an error
// via the SSE stream instead of an HTTP status code.
func openAIErrorJSON(errType, message string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    errType,
		},
	})
	return string(b)
}

func proxyOpenAIError(w http.ResponseWriter, status int, errType, message string, extra map[string]interface{}) {
	errBody := map[string]interface{}{
		"code":    status,
		"message": message,
		"type":    errType,
	}
	for key, value := range extra {
		errBody[key] = value
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": errBody})
}

// buildOpenAIHeartbeatEvent emits an empty chat.completion.chunk event.
// Event-based clients treat this as activity, while compatible consumers
// can safely ignore it because it carries no text or tool deltas.
func buildOpenAIHeartbeatEvent(convID string, model string) []string {
	payload := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-glass-heartbeat-%s", convID),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index":         0,
				"delta":         map[string]interface{}{},
				"finish_reason": nil,
			},
		},
	}
	lines, _ := marshalSSEEvent("", payload)
	return lines
}
