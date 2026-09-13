package codex

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

// sumChunk records a single summarization chunk written to disk.
type sumChunk struct {
	ChunkNum      int       `json:"chunk_num"`
	ItemIdxStart  int       `json:"item_idx_start"`
	ItemIdxEnd    int       `json:"item_idx_end"`
	TokensCovered int       `json:"tokens_covered"`
	SummaryPath   string    `json:"summary_path"`
	CreatedAt     time.Time `json:"created_at"`
}

// sumState tracks summarization progress for a single conversation.
type sumState struct {
	ConvID            string     `json:"conv_id"`
	Chunks            []sumChunk `json:"chunks"`
	LastSummarizedIdx int        `json:"last_summarized_idx"`
	TokensSummarized  int        `json:"tokens_summarized"`
}

// summarizerConfig controls the rolling summarizer behavior.
type summarizerConfig struct {
	Enabled         bool
	IntervalToken   int    // tokens between chunk triggers (default 50000)
	Model           string // e.g. "gpt-5.4"
	Upstream        string // e.g. "https://api.openai.com"
	ChatGPTUpstream string // e.g. "https://chatgpt.com/backend-api/codex"
	ShadowDir       string // e.g. "~/.codex/glass-proxy/shadow/"
}

// rollingSummarizer produces incremental GPT-5.4 summaries of conversation
// chunks and stitches them together at eviction time.
type rollingSummarizer struct {
	mu               sync.Mutex
	states           map[string]*sumState
	cfg              summarizerConfig
	authHeader       string
	orgHeader        string
	projHeader       string
	transport        http.RoundTripper
	chatgptTransport http.RoundTripper
}

// itemForSummary is a flattened representation of a Responses API item
// suitable for building summarization prompts.
type itemForSummary struct {
	Index   int
	Type    string
	Role    string
	Content string // extracted text content
	Tokens  int
}

// newRollingSummarizer creates a summarizer. Returns nil if not enabled.
func newRollingSummarizer(cfg summarizerConfig, transport http.RoundTripper) *rollingSummarizer {
	if !cfg.Enabled {
		return nil
	}
	if cfg.IntervalToken <= 0 {
		cfg.IntervalToken = 50000
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-5.4"
	}
	if cfg.Upstream == "" {
		cfg.Upstream = defaultOpenAIUpstream
	}
	if cfg.ChatGPTUpstream == "" {
		cfg.ChatGPTUpstream = defaultChatGPTCodexUpstream
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &rollingSummarizer{
		states:           make(map[string]*sumState),
		cfg:              cfg,
		transport:        transport,
		chatgptTransport: newChatGPTUpstreamTransport(),
	}
}

// captureAuth stores the current upstream auth for summarizer LLM calls.
func (rs *rollingSummarizer) captureAuth(authHeader, orgHeader, projHeader string) {
	if rs == nil {
		return
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if strings.TrimSpace(authHeader) != "" {
		rs.authHeader = authHeader
	}
	if strings.TrimSpace(orgHeader) != "" {
		rs.orgHeader = orgHeader
	}
	if strings.TrimSpace(projHeader) != "" {
		rs.projHeader = projHeader
	}
}

// getState returns the summarizer state for a conversation,
// loading from disk if not already cached in memory.
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

// loadState reads summarizer state from disk. Returns a fresh state if
// the file doesn't exist or can't be parsed. Caller must hold rs.mu.
func (rs *rollingSummarizer) loadState(convID string) *sumState {
	st := &sumState{ConvID: convID}
	dir := filepath.Join(rs.cfg.ShadowDir, convID)
	path := filepath.Join(dir, "summarizer_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		log.Printf("[codex-summarizer] corrupt state file %s: %v", path, err)
		return &sumState{ConvID: convID}
	}
	return st
}

// saveState persists summarizer state to disk via atomic tmp+rename.
func (rs *rollingSummarizer) saveState(st *sumState) error {
	if rs == nil || st == nil {
		return nil
	}
	dir := filepath.Join(rs.cfg.ShadowDir, st.ConvID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	target := filepath.Join(dir, "summarizer_state.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, target, err)
	}
	return nil
}

// maybeChunk checks whether enough unsummarized tokens have accumulated
// (>= IntervalToken) and, if so, summarizes the new items as a chunk.
// Returns true if a chunk was produced.
func (rs *rollingSummarizer) maybeChunk(convID string, sess *session) bool {
	if rs == nil || sess == nil {
		return false
	}
	rs.mu.Lock()
	authHeader := rs.authHeader
	orgHeader := rs.orgHeader
	projHeader := rs.projHeader
	rs.mu.Unlock()
	if strings.TrimSpace(authHeader) == "" {
		return false
	}

	st := rs.getState(convID)

	// Count unsummarized tokens from items after LastSummarizedIdx.
	sess.mu.Lock()
	itemCount := len(sess.items)
	if itemCount == 0 || st.LastSummarizedIdx >= itemCount {
		sess.mu.Unlock()
		return false
	}

	var unsummarizedTokens int
	for i := st.LastSummarizedIdx; i < itemCount; i++ {
		unsummarizedTokens += sess.items[i].Tokens
	}

	if unsummarizedTokens < rs.cfg.IntervalToken {
		sess.mu.Unlock()
		return false
	}

	// Extract items for summarization while holding the lock.
	startIdx := st.LastSummarizedIdx
	endIdx := itemCount - 1
	items := make([]itemForSummary, 0, itemCount-startIdx)
	for i := startIdx; i < itemCount; i++ {
		ci := sess.items[i]
		items = append(items, itemForSummary{
			Index:   i,
			Type:    ci.Type,
			Role:    ci.Role,
			Content: extractItemContent(ci.Item),
			Tokens:  ci.Tokens,
		})
	}
	sess.mu.Unlock()

	if len(items) == 0 {
		return false
	}

	// Stitch prior summaries as context for the new chunk.
	priorSummaries := rs.stitchSummary(convID)

	chunkNum := len(st.Chunks) + 1
	summary, err := rs.callGPT54(priorSummaries, items, chunkNum, authHeader, orgHeader, projHeader)
	if err != nil {
		log.Printf("[codex-summarizer] GPT-5.4 call failed for %s chunk %d: %v", convID, chunkNum, err)
		return false
	}

	// Write chunk summary to disk.
	dir := filepath.Join(rs.cfg.ShadowDir, convID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[codex-summarizer] mkdir failed: %v", err)
		return false
	}
	chunkPath := filepath.Join(dir, fmt.Sprintf("chunk_%03d.md", chunkNum))
	if err := os.WriteFile(chunkPath, []byte(summary), 0o644); err != nil {
		log.Printf("[codex-summarizer] write chunk file failed: %v", err)
		return false
	}

	// Update state.
	rs.mu.Lock()
	st.Chunks = append(st.Chunks, sumChunk{
		ChunkNum:      chunkNum,
		ItemIdxStart:  startIdx,
		ItemIdxEnd:    endIdx,
		TokensCovered: unsummarizedTokens,
		SummaryPath:   chunkPath,
		CreatedAt:     time.Now(),
	})
	st.LastSummarizedIdx = itemCount
	st.TokensSummarized += unsummarizedTokens
	rs.mu.Unlock()

	if err := rs.saveState(st); err != nil {
		log.Printf("[codex-summarizer] save state failed: %v", err)
		// State is updated in memory even if disk write fails.
	}
	return true
}

// stitchSummary reads all chunk summary files for a conversation and
// concatenates them with separators. Returns the combined summary string.
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
			log.Printf("[codex-summarizer] read chunk %d failed: %v", c.ChunkNum, err)
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&buf, "## Chunk %d (items %d–%d)\n\n", c.ChunkNum, c.ItemIdxStart, c.ItemIdxEnd)
		buf.Write(data)
	}
	return buf.String()
}

// callGPT54 calls GPT-5.4 via the Responses API to summarize a chunk of
// conversation items. Uses non-streaming mode with store: false.
func (rs *rollingSummarizer) callGPT54(priorSummaries string, items []itemForSummary, chunkNum int, authHeader, orgHeader, projHeader string) (string, error) {
	if rs == nil {
		return "", fmt.Errorf("summarizer is nil")
	}

	// Build the summarization prompt.
	var prompt bytes.Buffer
	prompt.WriteString("You are a session summarizer. Produce a concise summary of this coding agent conversation chunk.\n")
	prompt.WriteString("Preserve: file paths, function names, key decisions, current task state, any blockers or errors.\n")
	prompt.WriteString("Omit: tool output details, verbose file contents, repetitive patterns.\n")
	prompt.WriteString("Format: markdown with ## headings for major topics.\n\n")

	if priorSummaries != "" {
		prompt.WriteString("Previous chunks summary:\n")
		prompt.WriteString(priorSummaries)
		prompt.WriteString("\n\n")
	}

	if len(items) > 0 {
		fmt.Fprintf(&prompt, "Chunk %d (items %d-%d):\n", chunkNum, items[0].Index, items[len(items)-1].Index)
		for _, it := range items {
			label := it.Type
			if it.Role != "" {
				label = it.Role
			}
			fmt.Fprintf(&prompt, "%s: %s\n", label, it.Content)
		}
	}

	// Build the Responses API request body.
	reqBody := map[string]interface{}{
		"model": rs.cfg.Model,
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type": "input_text",
						"text": prompt.String(),
					},
				},
			},
		},
		"instructions": "You are a precise conversation summarizer for coding agent sessions.",
		"stream":       false,
		"store":        false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := resolveResponsesUpstream(rs.cfg.Upstream, rs.cfg.ChatGPTUpstream, authHeader)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)
	if strings.TrimSpace(orgHeader) != "" {
		req.Header.Set("OpenAI-Organization", orgHeader)
	}
	if strings.TrimSpace(projHeader) != "" {
		req.Header.Set("OpenAI-Project", projHeader)
	}

	client := &http.Client{
		Transport: rs.transportForAuth(authHeader),
		Timeout:   120 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GPT-5.4 request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("GPT-5.4 returned %d: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return parseGPT54Response(respBody)
}

// parseGPT54Response extracts the text output from a non-streaming
// Responses API response. Looks for output[].content[].text.
func parseGPT54Response(body []byte) (string, error) {
	var resp struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	// Find the first text content item across all output entries.
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				return c.Text, nil
			}
		}
	}
	// Fallback: accept any non-empty text.
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				return c.Text, nil
			}
		}
	}
	return "", fmt.Errorf("no text content in GPT-5.4 response")
}

func (rs *rollingSummarizer) transportForAuth(authHeader string) http.RoundTripper {
	if rs != nil && shouldUseChatGPTUpstream(authHeader) && rs.chatgptTransport != nil {
		return rs.chatgptTransport
	}
	return rs.transport
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

// extractItemContent extracts human-readable text from a Responses API item.
func extractItemContent(item map[string]interface{}) string {
	if item == nil {
		return ""
	}
	typ, _ := item["type"].(string)
	switch typ {
	case "message":
		return extractMessageText(item)
	case "function_call":
		name, _ := item["name"].(string)
		args, _ := item["arguments"].(string)
		return fmt.Sprintf("Called %s with %s", name, args)
	case "function_call_output":
		output, _ := item["output"].(string)
		return output
	case "reasoning":
		return "[reasoning]"
	default:
		// Unknown type; marshal what we can.
		b, err := json.Marshal(item)
		if err != nil {
			return "[unknown]"
		}
		return string(b)
	}
}

// extractMessageText joins text content parts from a message item.
func extractMessageText(item map[string]interface{}) string {
	content, ok := item["content"]
	if !ok {
		return ""
	}
	parts, ok := content.([]interface{})
	if !ok {
		// Sometimes content is a plain string.
		if s, ok := content.(string); ok {
			return s
		}
		return ""
	}
	var buf bytes.Buffer
	for _, p := range parts {
		part, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		// Accept both "input_text" and "text" content types.
		partType, _ := part["type"].(string)
		if partType == "input_text" || partType == "text" || partType == "output_text" {
			text, _ := part["text"].(string)
			if text != "" {
				if buf.Len() > 0 {
					buf.WriteByte('\n')
				}
				buf.WriteString(text)
			}
		}
	}
	return buf.String()
}
