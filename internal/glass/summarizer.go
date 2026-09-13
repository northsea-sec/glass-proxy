// Package glass — RollingSummarizer implements incremental context summarization.
//
// Instead of summarizing the entire evicted batch at eviction time (which could
// be 300K+ tokens), the summarizer processes the conversation in chunks as it
// grows. Each chunk produces a summary file. At eviction time, only the small
// unsummarized delta needs processing, and all chunk summaries are stitched
// into a single recovery file.
//
// The summarizer makes synchronous Opus API calls. Each chunk summary call
// processes ~50K tokens — well within Opus's ability to produce faithful output.
package glass

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SummarizerConfig holds settings for the rolling summarizer.
type SummarizerConfig struct {
	Enabled       bool   `json:"rolling_summarize_enabled"`
	IntervalToken int    `json:"rolling_summarize_interval"` // tokens between chunk triggers (e.g. 50000)
	Model         string `json:"rolling_summarize_model"`    // e.g. "claude-opus-4-6"
	GateEnabled   bool   `json:"recovery_gate_enabled"`
}

// DefaultSummarizerConfig returns sane defaults.
func DefaultSummarizerConfig() SummarizerConfig {
	return SummarizerConfig{
		Enabled:       false,
		IntervalToken: 50000,
		Model:         "claude-opus-4-6",
		GateEnabled:   true,
	}
}

// RollingChunk represents one completed chunk summary.
type RollingChunk struct {
	ChunkNum      int       `json:"chunk_num"`
	MsgIdxStart   int       `json:"msg_idx_start"`   // internal cache index of first msg
	MsgIdxEnd     int       `json:"msg_idx_end"`     // internal cache index of last msg
	TokensCovered int       `json:"tokens_covered"`  // estimated tokens in source batch
	SummaryPath   string    `json:"summary_path"`    // path to summary-chunk-NNN.md
	CreatedAt     time.Time `json:"created_at"`
}

// SummarizerState is persisted per-session alongside the localcache.
type SummarizerState struct {
	ConvID             string         `json:"conv_id"`
	Chunks             []RollingChunk `json:"chunks"`
	LastSummarizedIdx  int            `json:"last_summarized_idx"`  // highest internal msg index summarized
	TokensSummarized   int            `json:"tokens_summarized"`   // cumulative tokens already summarized
	RecoveryGateArmed  bool           `json:"recovery_gate_armed"` // true = agent must read recovery file
	RecoveryFilePath   string         `json:"recovery_file_path"`  // path to current recovery-NNN.md
	RecoveryEvictionNum int           `json:"recovery_eviction_num"`
}

// RollingSummarizer manages incremental summarization for all sessions.
type RollingSummarizer struct {
	mu        sync.Mutex
	states    map[string]*SummarizerState // convID -> state
	cfg       SummarizerConfig
	apiKey    string // captured from first request (same as keepalive pattern)
	isBearer  bool   // true when apiKey is a Bearer token (OAuth)
	captured  bool   // true once credentials captured from first request
	upstream  string
	transport http.RoundTripper
	shadowDir string
	booksDir  string

	// Captured from first request — needed for OAuth Bearer auth to work.
	// Anthropic routes OAuth tokens based on these headers.
	anthropicVersion string
	betas            []string
}

type CapturedAuthStatus struct {
	CapturedAuthAvailable bool     `json:"captured_auth_available"`
	Captured              bool     `json:"captured"`
	AuthType              string   `json:"auth_type"`
	AnthropicVersion      string   `json:"anthropic_version,omitempty"`
	Betas                 []string `json:"betas"`
}

func (rs *RollingSummarizer) AuthStatus() CapturedAuthStatus {
	if rs == nil {
		return CapturedAuthStatus{
			CapturedAuthAvailable: false,
			Captured:              false,
			AuthType:              "unavailable",
			Betas:                 []string{},
		}
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()

	authType := "missing"
	if rs.apiKey != "" {
		if rs.isBearer {
			authType = "bearer"
		} else {
			authType = "x-api-key"
		}
	}

	return CapturedAuthStatus{
		CapturedAuthAvailable: rs.captured && rs.apiKey != "",
		Captured:              rs.captured,
		AuthType:              authType,
		AnthropicVersion:      rs.anthropicVersion,
		Betas:                 append([]string(nil), rs.betas...),
	}
}

// NewRollingSummarizer creates a summarizer. Returns nil if disabled.
// Does NOT require an API key at creation — captures from the first real
// request that passes through the proxy, same pattern as keepalive.
func NewRollingSummarizer(cfg SummarizerConfig, upstream, shadowDir string, transport http.RoundTripper) *RollingSummarizer {
	if !cfg.Enabled {
		log.Printf("[SUMMARIZER] Disabled")
		return nil
	}
	return &RollingSummarizer{
		states:    make(map[string]*SummarizerState),
		cfg:       cfg,
		upstream:  upstream,
		transport: transport,
		shadowDir: shadowDir,
		booksDir:  filepath.Join(filepath.Dir(shadowDir), "glass-books"),
	}
}

// CaptureAuth records the API key/Bearer token and request headers from the
// first real request. Thread-safe; only the first call has effect.
// The anthropic-version and anthropic-beta headers are required for OAuth
// Bearer tokens to route correctly at Anthropic's API.
func (rs *RollingSummarizer) CaptureAuth(meta RequestMeta) {
	if rs == nil || meta.APIKey == "" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.captured {
		return
	}
	rs.apiKey = meta.APIKey
	rs.isBearer = meta.AuthIsBearer
	rs.anthropicVersion = meta.AnthropicVersion
	rs.betas = append([]string(nil), meta.Betas...)
	rs.captured = true
	authType := "x-api-key"
	if rs.isBearer {
		authType = "Bearer/OAuth"
	}
	log.Printf("[SUMMARIZER] Auth captured (%s, version=%s, betas=%d) — rolling summarization active",
		authType, rs.anthropicVersion, len(rs.betas))
}

func (rs *RollingSummarizer) clearCapturedAuthLocked() {
	rs.apiKey = ""
	rs.isBearer = false
	rs.anthropicVersion = ""
	rs.betas = nil
	rs.captured = false
}

func (rs *RollingSummarizer) ForwardWithCapturedAuth(method, path string, payload []byte) ([]byte, int, error) {
	if rs == nil {
		return nil, 0, fmt.Errorf("summarizer unavailable")
	}
	rs.mu.Lock()
	apiKey := rs.apiKey
	isBearer := rs.isBearer
	version := rs.anthropicVersion
	betas := append([]string(nil), rs.betas...)
	transport := rs.transport
	upstream := strings.TrimRight(rs.upstream, "/")
	captured := rs.captured
	rs.mu.Unlock()
	if !captured || apiKey == "" {
		return nil, 0, fmt.Errorf("captured Anthropic auth unavailable")
	}
	if !strings.HasPrefix(path, "/v1/") {
		return nil, 0, fmt.Errorf("unsupported captured-auth path %q", path)
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, upstream+path, body)
	if err != nil {
		return nil, 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if isBearer {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}
	if version == "" {
		version = "2023-06-01"
	}
	req.Header.Set("anthropic-version", version)
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}
	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized && strings.Contains(string(data), "invalid x-api-key") {
		rs.mu.Lock()
		rs.clearCapturedAuthLocked()
		rs.mu.Unlock()
		return nil, resp.StatusCode, fmt.Errorf("captured Anthropic auth rejected upstream; cleared stale auth for recapture")
	}
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return data, resp.StatusCode, nil
}

// getState returns (or loads) the summarizer state for a session.
func (rs *RollingSummarizer) getState(convID string) *SummarizerState {
	rs.mu.Lock()
	defer rs.mu.Unlock()

	if st, ok := rs.states[convID]; ok {
		return st
	}

	// Try loading from disk
	st := rs.loadState(convID)
	if st == nil {
		st = &SummarizerState{ConvID: convID}
	}
	rs.states[convID] = st
	return st
}

func (rs *RollingSummarizer) loadState(convID string) *SummarizerState {
	path := filepath.Join(rs.shadowDir, convID, "summarizer_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var st SummarizerState
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	return &st
}

func (rs *RollingSummarizer) saveState(st *SummarizerState) error {
	dir := filepath.Join(rs.shadowDir, st.ConvID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "summarizer_state.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MaybeChunk checks if a new rolling chunk should be produced.
// Called after each proxy response. Non-blocking: runs the Opus call synchronously
// but only when enough unsummarized tokens have accumulated.
//
// cache: the session's LocalCache (for reading messages)
// Returns true if a chunk was produced.
func (rs *RollingSummarizer) MaybeChunk(convID string, cache *LocalCache) bool {
	if rs == nil || cache == nil {
		return false
	}
	rs.mu.Lock()
	ready := rs.captured
	rs.mu.Unlock()
	if !ready {
		return false
	}

	st := rs.getState(convID)
	if st.RecoveryGateArmed {
		return false // don't summarize while gate is armed
	}

	// Calculate unsummarized tokens
	totalTokens := cache.TotalTokens()
	unsummarized := totalTokens - st.TokensSummarized
	if unsummarized < rs.cfg.IntervalToken {
		return false
	}

	// Extract the unsummarized messages
	cache.mu.Lock()
	var batch []indexedMessage
	batchTokens := 0
	for i := st.LastSummarizedIdx + 1; i < len(cache.messages); i++ {
		if cache.messages[i].IsReference {
			continue
		}
		batch = append(batch, indexedMessage{
			Index:   i,
			Role:    cache.messages[i].Role,
			Content: chapterMessageContent(cache.messages[i].Msg),
			Tokens:  cache.messages[i].Tokens,
		})
		batchTokens += cache.messages[i].Tokens
		// Cap batch at interval size to avoid sending too much
		if batchTokens >= rs.cfg.IntervalToken {
			break
		}
	}
	cache.mu.Unlock()

	if len(batch) == 0 {
		return false
	}

	// Build prior summaries for context continuity
	var priorSummaries string
	for _, chunk := range st.Chunks {
		data, err := os.ReadFile(chunk.SummaryPath)
		if err == nil {
			priorSummaries += string(data) + "\n---\n"
		}
	}

	chunkNum := len(st.Chunks) + 1
	firstIdx := batch[0].Index
	lastIdx := batch[len(batch)-1].Index

	log.Printf("[SUMMARIZER] conv=%s producing chunk %d (msgs %d–%d, ~%d tokens)",
		convID, chunkNum, firstIdx, lastIdx, batchTokens)

	// Call Opus
	summary, err := rs.callOpusSummarize(priorSummaries, batch, chunkNum, firstIdx, lastIdx)
	if err != nil {
		log.Printf("[SUMMARIZER] conv=%s chunk %d Opus call failed: %v", convID, chunkNum, err)
		return false
	}

	// Write chunk file
	sessionDir := rs.sessionDir(convID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		log.Printf("[SUMMARIZER] conv=%s mkdir error: %v", convID, err)
		return false
	}
	chunkPath := filepath.Join(sessionDir, fmt.Sprintf("summary-chunk-%03d.md", chunkNum))
	if err := os.WriteFile(chunkPath, []byte(summary), 0644); err != nil {
		log.Printf("[SUMMARIZER] conv=%s write chunk error: %v", convID, err)
		return false
	}

	// Update state
	st.Chunks = append(st.Chunks, RollingChunk{
		ChunkNum:      chunkNum,
		MsgIdxStart:   firstIdx,
		MsgIdxEnd:     lastIdx,
		TokensCovered: batchTokens,
		SummaryPath:   chunkPath,
		CreatedAt:     time.Now(),
	})
	st.LastSummarizedIdx = lastIdx
	st.TokensSummarized += batchTokens

	if err := rs.saveState(st); err != nil {
		log.Printf("[SUMMARIZER] conv=%s save state error: %v", convID, err)
	}

	log.Printf("[SUMMARIZER] conv=%s chunk %d written (%d bytes) — total summarized: %d tokens",
		convID, chunkNum, len(summary), st.TokensSummarized)
	return true
}

// StitchAndArm produces the final recovery file at eviction time.
// Summarizes any remaining delta, stitches all chunks, arms the read gate.
// Called synchronously during eviction.
func (rs *RollingSummarizer) StitchAndArm(convID string, cache *LocalCache, evicted []map[string]interface{}, evictionNum int, state *SessionState) string {
	if rs == nil {
		return ""
	}

	st := rs.getState(convID)

	// Summarize any unsummarized delta from the evicted messages
	if len(evicted) > 0 {
		var deltaBatch []indexedMessage
		deltaTokens := 0
		firstMsgNum := state.EvictedCount - len(evicted) + 1
		for i, msg := range evicted {
			content := chapterMessageContent(msg)
			if strings.TrimSpace(content) == "" {
				continue
			}
			role, _ := msg["role"].(string)
			tokens := estimateMessageTokens(msg)
			deltaBatch = append(deltaBatch, indexedMessage{
				Index:   firstMsgNum + i,
				Role:    role,
				Content: content,
				Tokens:  tokens,
			})
			deltaTokens += tokens
		}

		if len(deltaBatch) > 0 {
			var priorSummaries string
			for _, chunk := range st.Chunks {
				data, err := os.ReadFile(chunk.SummaryPath)
				if err == nil {
					priorSummaries += string(data) + "\n---\n"
				}
			}

			chunkNum := len(st.Chunks) + 1
			firstIdx := deltaBatch[0].Index
			lastIdx := deltaBatch[len(deltaBatch)-1].Index

			log.Printf("[SUMMARIZER] conv=%s producing delta chunk %d (msgs %d–%d, ~%d tokens)",
				convID, chunkNum, firstIdx, lastIdx, deltaTokens)

			summary, err := rs.callOpusSummarize(priorSummaries, deltaBatch, chunkNum, firstIdx, lastIdx)
			if err != nil {
				log.Printf("[SUMMARIZER] conv=%s delta chunk Opus call failed: %v", convID, err)
			} else {
				sessionDir := rs.sessionDir(convID)
				os.MkdirAll(sessionDir, 0755)
				chunkPath := filepath.Join(sessionDir, fmt.Sprintf("summary-chunk-%03d.md", chunkNum))
				if err := os.WriteFile(chunkPath, []byte(summary), 0644); err != nil {
					log.Printf("[SUMMARIZER] conv=%s write delta chunk error: %v", convID, err)
				} else {
					st.Chunks = append(st.Chunks, RollingChunk{
						ChunkNum:      chunkNum,
						MsgIdxStart:   firstIdx,
						MsgIdxEnd:     lastIdx,
						TokensCovered: deltaTokens,
						SummaryPath:   chunkPath,
						CreatedAt:     time.Now(),
					})
				}
			}
		}
	}

	// Stitch all chunks into recovery-NNN.md
	sessionDir := rs.sessionDir(convID)
	os.MkdirAll(sessionDir, 0755)

	recoveryPath := filepath.Join(sessionDir, fmt.Sprintf("recovery-%03d.md", evictionNum))
	recovery := rs.stitchRecovery(st, convID, evictionNum, sessionDir)
	if err := os.WriteFile(recoveryPath, []byte(recovery), 0644); err != nil {
		log.Printf("[SUMMARIZER] conv=%s write recovery error: %v", convID, err)
		return ""
	}

	// Arm the read gate
	if rs.cfg.GateEnabled {
		st.RecoveryGateArmed = true
		st.RecoveryFilePath = recoveryPath
		st.RecoveryEvictionNum = evictionNum
	}

	// Reset summarizer state for next cycle
	st.LastSummarizedIdx = 0
	st.TokensSummarized = 0
	st.Chunks = nil

	if err := rs.saveState(st); err != nil {
		log.Printf("[SUMMARIZER] conv=%s save state error: %v", convID, err)
	}

	log.Printf("[SUMMARIZER] conv=%s recovery-%03d.md written (%d bytes), gate_armed=%v",
		convID, evictionNum, len(recovery), rs.cfg.GateEnabled)

	return recoveryPath
}

// IsGateArmed returns true if the agent needs to read the recovery file.
func (rs *RollingSummarizer) IsGateArmed(convID string) bool {
	if rs == nil {
		return false
	}
	st := rs.getState(convID)
	return st.RecoveryGateArmed
}

// RecoveryFilePath returns the path the agent must read to clear the gate.
func (rs *RollingSummarizer) RecoveryFilePath(convID string) string {
	if rs == nil {
		return ""
	}
	st := rs.getState(convID)
	return st.RecoveryFilePath
}

// CheckGateClear inspects the agent's request for evidence that the agent
// read the recovery file. Checks both:
//   - assistant tool_use blocks (Read call with file_path matching recovery)
//   - user tool_result blocks (content containing recovery filename/path)
// If found, clears the gate.
func (rs *RollingSummarizer) CheckGateClear(convID string, msgs []interface{}) bool {
	if rs == nil {
		return false
	}
	st := rs.getState(convID)
	if !st.RecoveryGateArmed || st.RecoveryFilePath == "" {
		return false
	}

	recoveryBaseName := filepath.Base(st.RecoveryFilePath)
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, c := range content {
			block, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			tp, _ := block["type"].(string)

			// Check assistant tool_use: Read(file_path=recovery-NNN.md)
			if role == "assistant" && tp == "tool_use" {
				input, _ := block["input"].(map[string]interface{})
				if input != nil {
					filePath, _ := input["file_path"].(string)
					if strings.Contains(filePath, recoveryBaseName) ||
						filePath == st.RecoveryFilePath {
						log.Printf("[SUMMARIZER] conv=%s recovery gate CLEARED — agent called Read(%s)",
							convID, recoveryBaseName)
						st.RecoveryGateArmed = false
						st.RecoveryFilePath = ""
						rs.saveState(st)
						return true
					}
				}
			}

			// Check user tool_result: content containing recovery filename
			if role == "user" && tp == "tool_result" {
				resultText := extractToolResultText(block["content"])
				if strings.Contains(resultText, recoveryBaseName) ||
					strings.Contains(resultText, st.RecoveryFilePath) {
					log.Printf("[SUMMARIZER] conv=%s recovery gate CLEARED — tool_result contains %s",
						convID, recoveryBaseName)
					st.RecoveryGateArmed = false
					st.RecoveryFilePath = ""
					rs.saveState(st)
					return true
				}
			}
		}
	}
	return false
}

// --- internal ---

type indexedMessage struct {
	Index   int
	Role    string
	Content string
	Tokens  int
}

func (rs *RollingSummarizer) sessionDir(convID string) string {
	day := time.Now().Format("2006-01-02")
	return filepath.Join(rs.booksDir, chapterProjectID, day, convID)
}

func (rs *RollingSummarizer) stitchRecovery(st *SummarizerState, convID string, evictionNum int, sessionDir string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# Recovery Summary — Eviction %d\n\n", evictionNum))
	b.WriteString(fmt.Sprintf("Session: %s\n", convID))
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	if len(st.Chunks) == 0 {
		b.WriteString("*No rolling summaries were generated before eviction.*\n")
	} else {
		for _, chunk := range st.Chunks {
			data, err := os.ReadFile(chunk.SummaryPath)
			if err != nil {
				b.WriteString(fmt.Sprintf("## Chunk %d (messages %d–%d)\n\n*Summary file not found: %s*\n\n",
					chunk.ChunkNum, chunk.MsgIdxStart, chunk.MsgIdxEnd, chunk.SummaryPath))
				continue
			}
			b.Write(data)
			b.WriteString("\n\n")
		}
	}

	// Aggregate file list from chapter directory
	b.WriteString("---\n\n")
	b.WriteString("## Source Transcripts\n\n")
	entries, err := os.ReadDir(sessionDir)
	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "chapter-") && strings.HasSuffix(e.Name(), ".md") {
				b.WriteString(fmt.Sprintf("- `%s`\n", filepath.Join(sessionDir, e.Name())))
			}
		}
	}
	b.WriteString(fmt.Sprintf("\nFor exact details, search the chapter files above.\n"))

	return b.String()
}

const summarizerPrompt = `You are summarizing a segment of a coding session conversation between a user and an AI coding assistant.

RULES — ABSOLUTE:
1. Every factual claim MUST cite [msg N] (the message index).
2. User requests and decisions MUST be verbatim quotes with > blockquote.
3. Do NOT infer intent. Do NOT add information not in the source.
4. Do NOT paraphrase user constraints — quote them exactly.
5. Separate FACT sections from your ASSESSMENT section.
6. Be concise. Target 100-200 lines maximum.

OUTPUT FORMAT (follow exactly):

## Chunk %d (messages %d–%d)

### User Requests
- [msg N]: > "exact quote"

### User Constraints & Preferences
- [msg N]: > "exact quote"

### Decisions Made
- [msg N]: decision — rationale from conversation

### Work Done
- [msg N]: file/action — what changed

### State at End of Chunk
- in progress: ...
- blocked: ...

### Assessment
[your interpretation, patterns, recommendations — clearly labeled as YOUR analysis, not fact]
`

func (rs *RollingSummarizer) callOpusSummarize(priorSummaries string, batch []indexedMessage, chunkNum, firstIdx, lastIdx int) (string, error) {
	// Build the user message with the conversation segment
	var msgText strings.Builder
	if priorSummaries != "" {
		msgText.WriteString("PRIOR CONTEXT (summaries of earlier chunks):\n\n")
		msgText.WriteString(priorSummaries)
		msgText.WriteString("\n\n---\n\nMESSAGES TO SUMMARIZE:\n\n")
	} else {
		msgText.WriteString("MESSAGES TO SUMMARIZE:\n\n")
	}

	for _, msg := range batch {
		msgText.WriteString(fmt.Sprintf("[msg %d] [%s]\n", msg.Index, msg.Role))
		// Truncate very long messages to prevent exceeding limits
		content := msg.Content
		if len(content) > 5000 {
			content = content[:4900] + "\n...[truncated, " + fmt.Sprintf("%d", len(msg.Content)) + " chars total]"
		}
		msgText.WriteString(content)
		msgText.WriteString("\n\n")
	}

	prompt := fmt.Sprintf(summarizerPrompt, chunkNum, firstIdx, lastIdx)

	body := map[string]interface{}{
		"model":      rs.cfg.Model,
		"max_tokens": 4096,
		"system": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": prompt,
			},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": msgText.String(),
			},
		},
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal summarizer request: %w", err)
	}

	url := rs.upstream + "/v1/messages"
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create summarizer request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if rs.isBearer {
		req.Header.Set("Authorization", "Bearer "+rs.apiKey)
	} else {
		req.Header.Set("x-api-key", rs.apiKey)
	}
	// Use the captured anthropic-version and betas from the original request.
	// OAuth Bearer tokens require these headers to route correctly at Anthropic.
	version := rs.anthropicVersion
	if version == "" {
		version = "2023-06-01"
	}
	req.Header.Set("anthropic-version", version)
	if len(rs.betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(rs.betas, ","))
	}

	client := &http.Client{
		Timeout:   120 * time.Second, // summaries can take a while
		Transport: rs.transport,
	}

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return "", fmt.Errorf("summarizer request failed: %w (%.0fms)", err, float64(elapsed.Milliseconds()))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("summarizer API %d: %s (%.0fms)",
			resp.StatusCode, bytes.TrimSpace(respBody), float64(elapsed.Milliseconds()))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse summarizer response: %w", err)
	}

	// Extract text from response
	content, _ := result["content"].([]interface{})
	var summary strings.Builder
	for _, block := range content {
		b, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		if tp, _ := b["type"].(string); tp == "text" {
			if text, ok := b["text"].(string); ok {
				summary.WriteString(text)
			}
		}
	}

	// Log usage
	if usage, ok := result["usage"].(map[string]interface{}); ok {
		inputTokens, _ := usage["input_tokens"].(float64)
		outputTokens, _ := usage["output_tokens"].(float64)
		log.Printf("[SUMMARIZER] Opus call complete: input=%d output=%d (%.1fs)",
			int(inputTokens), int(outputTokens), elapsed.Seconds())
	}

	return summary.String(), nil
}
