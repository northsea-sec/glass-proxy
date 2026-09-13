// summarizer.go — rolling summarizer for the Gemini lane.
// Calls Gemini 2.5 Pro via generateContent (non-streaming) to produce
// incremental context summaries, then stitches them at eviction time.
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type sumChunk struct {
	ChunkNum      int       `json:"chunk_num"`
	IdxStart      int       `json:"idx_start"`
	IdxEnd        int       `json:"idx_end"`
	TokensCovered int       `json:"tokens_covered"`
	SummaryPath   string    `json:"summary_path"`
	CreatedAt     time.Time `json:"created_at"`
}

type sumState struct {
	ConvID            string     `json:"conv_id"`
	Chunks            []sumChunk `json:"chunks"`
	LastSummarizedIdx int        `json:"last_summarized_idx"`
	TokensSummarized  int        `json:"tokens_summarized"`
}

type summarizerConfig struct {
	Enabled       bool
	IntervalToken int    // tokens between chunk triggers (default 80000)
	Model         string // e.g. "gemini-2.5-pro"
	Upstream      string // e.g. "https://generativelanguage.googleapis.com"
	ShadowDir     string
}

type rollingSummarizer struct {
	mu        sync.Mutex
	states    map[string]*sumState
	cfg       summarizerConfig
	apiKey    string
	captured  bool
	transport http.RoundTripper
}

// newRollingSummarizer creates a summarizer. Returns nil if disabled.
func newRollingSummarizer(cfg summarizerConfig, transport http.RoundTripper) *rollingSummarizer {
	if !cfg.Enabled {
		return nil
	}
	if cfg.IntervalToken <= 0 {
		cfg.IntervalToken = 80000
	}
	if cfg.Model == "" {
		cfg.Model = "gemini-2.5-pro"
	}
	if cfg.Upstream == "" {
		cfg.Upstream = "https://generativelanguage.googleapis.com"
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &rollingSummarizer{
		states:    make(map[string]*sumState),
		cfg:       cfg,
		transport: transport,
	}
}

// captureAuth stores the API key. Thread-safe; only first call takes effect.
func (rs *rollingSummarizer) captureAuth(apiKey string) {
	if rs == nil || apiKey == "" {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.captured {
		return
	}
	rs.apiKey = apiKey
	rs.captured = true
}

func (rs *rollingSummarizer) getState(convID string) *sumState {
	if rs == nil {
		return nil
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if st, ok := rs.states[convID]; ok {
		return st
	}
	st := rs.loadState(convID)
	rs.states[convID] = st
	return st
}

func (rs *rollingSummarizer) loadState(convID string) *sumState {
	st := &sumState{ConvID: convID}
	path := filepath.Join(rs.cfg.ShadowDir, convID, "summarizer_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		return &sumState{ConvID: convID}
	}
	return st
}

func (rs *rollingSummarizer) saveState(st *sumState) error {
	if rs == nil || st == nil {
		return nil
	}
	dir := filepath.Join(rs.cfg.ShadowDir, st.ConvID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "summarizer_state.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// maybeChunk checks if enough unsummarized tokens accumulated and produces a chunk.
func (rs *rollingSummarizer) maybeChunk(convID string, sess *session) bool {
	if rs == nil || sess == nil {
		return false
	}
	rs.mu.Lock()
	if !rs.captured || rs.apiKey == "" {
		rs.mu.Unlock()
		return false
	}
	apiKey := rs.apiKey
	rs.mu.Unlock()

	st := rs.getState(convID)

	sess.mu.Lock()
	n := len(sess.contents)
	if n == 0 || st.LastSummarizedIdx >= n {
		sess.mu.Unlock()
		return false
	}

	var unsumTokens int
	for i := st.LastSummarizedIdx; i < n; i++ {
		unsumTokens += sess.contents[i].Tokens
	}
	if unsumTokens < rs.cfg.IntervalToken {
		sess.mu.Unlock()
		return false
	}

	// Extract text for summarization.
	startIdx := st.LastSummarizedIdx
	endIdx := n - 1
	var items []string
	for i := startIdx; i < n; i++ {
		items = append(items, extractContentText(sess.contents[i].Content))
	}
	sess.mu.Unlock()

	if len(items) == 0 {
		return false
	}

	prior := rs.stitchSummary(convID)
	chunkNum := len(st.Chunks) + 1
	summary, err := rs.callGemini(prior, items, chunkNum, startIdx, endIdx, apiKey)
	if err != nil {
		log.Printf("[gemini-summarizer] chunk %d failed for %s: %v", chunkNum, convID, err)
		return false
	}

	dir := filepath.Join(rs.cfg.ShadowDir, convID)
	_ = os.MkdirAll(dir, 0o755)
	chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%03d.md", chunkNum))
	if err := os.WriteFile(chunkPath, []byte(summary), 0o644); err != nil {
		log.Printf("[gemini-summarizer] write chunk failed: %v", err)
		return false
	}

	rs.mu.Lock()
	st.Chunks = append(st.Chunks, sumChunk{
		ChunkNum:      chunkNum,
		IdxStart:      startIdx,
		IdxEnd:        endIdx,
		TokensCovered: unsumTokens,
		SummaryPath:   chunkPath,
		CreatedAt:     time.Now(),
	})
	st.LastSummarizedIdx = n
	st.TokensSummarized += unsumTokens
	rs.mu.Unlock()

	if err := rs.saveState(st); err != nil {
		log.Printf("[gemini-summarizer] save state failed: %v", err)
	}
	return true
}

// stitchSummary concatenates all chunk summaries for a conversation.
func (rs *rollingSummarizer) stitchSummary(convID string) string {
	if rs == nil {
		return ""
	}
	st := rs.getState(convID)
	if len(st.Chunks) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for _, c := range st.Chunks {
		data, err := os.ReadFile(c.SummaryPath)
		if err != nil {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&buf, "## Chunk %d (contents %d\u2013%d)\n\n", c.ChunkNum, c.IdxStart, c.IdxEnd)
		buf.Write(data)
	}
	return buf.String()
}

// resetForConv clears all summarizer state for a conversation.
func (rs *rollingSummarizer) resetForConv(convID string) {
	if rs == nil {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.states, convID)
}

// callGemini calls Gemini 2.5 Pro via generateContent to summarize a chunk.
func (rs *rollingSummarizer) callGemini(priorSummaries string, items []string, chunkNum, startIdx, endIdx int, apiKey string) (string, error) {
	var prompt bytes.Buffer
	prompt.WriteString("You are a session summarizer. Produce a concise summary of this coding agent conversation chunk.\n")
	prompt.WriteString("Preserve: file paths, function names, key decisions, current task state, blockers, errors.\n")
	prompt.WriteString("Omit: tool output details, verbose file contents, repetitive patterns.\n")
	prompt.WriteString("Format: markdown with ## headings for major topics.\n\n")

	if priorSummaries != "" {
		prompt.WriteString("Previous chunks summary:\n")
		prompt.WriteString(priorSummaries)
		prompt.WriteString("\n\n")
	}

	fmt.Fprintf(&prompt, "Chunk %d (contents %d\u2013%d):\n", chunkNum, startIdx, endIdx)
	for _, item := range items {
		prompt.WriteString(item)
		prompt.WriteByte('\n')
	}

	reqBody := map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role":  "user",
				"parts": []interface{}{map[string]interface{}{"text": prompt.String()}},
			},
		},
		"systemInstruction": map[string]interface{}{
			"parts": []interface{}{map[string]interface{}{"text": "You are a precise conversation summarizer for coding agent sessions."}},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s",
		rs.cfg.Upstream, rs.cfg.Model, apiKey)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Transport: rs.transport, Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("Gemini returned %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return parseGeminiResponse(respBody)
}

// parseGeminiResponse extracts text from candidates[0].content.parts[0].text.
func parseGeminiResponse(body []byte) (string, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	for _, c := range resp.Candidates {
		for _, p := range c.Content.Parts {
			if p.Text != "" {
				return p.Text, nil
			}
		}
	}
	return "", fmt.Errorf("no text in Gemini response")
}
