package glass

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var topicHintPrefixRE = regexp.MustCompile(`^Messages \d+-\d+:\s*`)

// ShadowIndex tracks eviction batches for a conversation.
type ShadowIndex struct {
	ConvID             string        `json:"conv_id"`
	Batches            []ShadowBatch `json:"batches"`
	TotalEvictedMsgs   int           `json:"total_evicted_messages"`
	TotalEvictedTokens int           `json:"total_evicted_tokens"`
}

// ShadowBatch describes one eviction event.
type ShadowBatch struct {
	BatchID       int       `json:"batch_id"`
	MsgRange      [2]int    `json:"msg_range"` // [first, last] 1-based
	EvictedAt     time.Time `json:"evicted_at"`
	TokensEvicted int       `json:"tokens_evicted"`
	TopicHints    []string  `json:"topic_hints"`
}

// ShadowWriter appends evicted messages to shadow files.
type ShadowWriter struct {
	baseDir string
}

// NewShadowWriter creates a writer backed by the given directory.
func NewShadowWriter(baseDir string) *ShadowWriter {
	return &ShadowWriter{baseDir: baseDir}
}

// Write appends evicted messages to shadow.md and updates shadow_index.json.
func (sw *ShadowWriter) Write(convID string, evicted []map[string]interface{}, state *SessionState) error {
	if len(evicted) == 0 {
		return nil
	}

	dir := filepath.Join(sw.baseDir, convID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create shadow dir: %w", err)
	}

	// Calculate batch metadata
	batchID := state.BatchCount
	firstMsg := state.EvictedCount - len(evicted) + 1
	lastMsg := state.EvictedCount
	tokensEvicted := 0
	for _, msg := range evicted {
		tokensEvicted += estimateMessageTokens(msg)
	}

	// Generate topic hints from first/last messages
	hints := generateTopicHints(evicted, firstMsg, lastMsg)

	// Append to shadow.md
	mdPath := filepath.Join(dir, "shadow.md")
	f, err := os.OpenFile(mdPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open shadow.md: %w", err)
	}
	defer f.Close()

	// Write header on first batch
	info, _ := f.Stat()
	if info != nil && info.Size() == 0 {
		fmt.Fprintf(f, "# Session History — %s\n", convID)
		fmt.Fprintf(f, "## Created: %s\n\n", time.Now().Format(time.RFC3339))
	}

	now := time.Now()
	fmt.Fprintf(f, "---\n### Batch %d (Messages %d-%d, evicted at %s, ~%dK tokens)\n\n",
		batchID, firstMsg, lastMsg, now.Format("15:04:05"), tokensEvicted/1000)

	for i, msg := range evicted {
		role, _ := msg["role"].(string)
		msgNum := firstMsg + i
		fmt.Fprintf(f, "#### Message %d [%s]\n", msgNum, role)

		content := extractTextContent(msg)
		// Truncate very long messages in shadow to keep file manageable
		if len(content) > 5000 {
			content = content[:5000] + "\n[... truncated ...]\n"
		}
		fmt.Fprintf(f, "%s\n\n", content)
	}

	// Update shadow_index.json
	indexPath := filepath.Join(dir, "shadow_index.json")
	index := sw.loadIndex(indexPath, convID)
	index.Batches = append(index.Batches, ShadowBatch{
		BatchID:       batchID,
		MsgRange:      [2]int{firstMsg, lastMsg},
		EvictedAt:     now,
		TokensEvicted: tokensEvicted,
		TopicHints:    hints,
	})
	index.TotalEvictedMsgs = state.EvictedCount
	index.TotalEvictedTokens += tokensEvicted

	indexData, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	if err := os.WriteFile(indexPath, indexData, 0644); err != nil {
		return fmt.Errorf("write index: %w", err)
	}

	// Update topic hints in session state
	state.TopicHints = append(state.TopicHints, hints...)

	log.Printf("[SHADOW] Wrote batch %d: %d messages (~%dK tokens) to %s",
		batchID, len(evicted), tokensEvicted/1000, mdPath)

	return nil
}

func (sw *ShadowWriter) loadIndex(path, convID string) *ShadowIndex {
	data, err := os.ReadFile(path)
	if err != nil {
		return &ShadowIndex{ConvID: convID}
	}
	var idx ShadowIndex
	if json.Unmarshal(data, &idx) != nil {
		return &ShadowIndex{ConvID: convID}
	}
	return &idx
}

// generateTopicHints creates short summaries for a batch of evicted messages.
// Uses first text content of first and last messages as hints.
func generateTopicHints(evicted []map[string]interface{}, firstNum, lastNum int) []string {
	if len(evicted) == 0 {
		return nil
	}

	var hints []string
	// Take first user message content as topic hint
	for _, msg := range evicted {
		role, _ := msg["role"].(string)
		if role == "user" {
			text := sanitizeTopicHintBody(extractTextContent(msg))
			text = clipFactText(text, 100)
			if text != "" {
				hints = append(hints, fmt.Sprintf("Messages %d-%d: %s", firstNum, lastNum, text))
				break
			}
		}
	}
	if len(hints) == 0 {
		hints = append(hints, fmt.Sprintf("Messages %d-%d", firstNum, lastNum))
	}
	return hints
}

func sanitizeTopicHint(hint string) string {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return ""
	}

	prefix := topicHintPrefixRE.FindString(hint)
	body := topicHintPrefixRE.ReplaceAllString(hint, "")
	body = sanitizeTopicHintBody(body)
	if body == "" {
		if prefix == "" {
			return ""
		}
		return strings.TrimSuffix(strings.TrimSpace(prefix), ":")
	}

	if prefix == "" {
		return body
	}
	return strings.TrimSpace(prefix) + " " + body
}

func sanitizeTopicHintBody(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = normalizeSnippet(line)
		if line == "" {
			continue
		}
		if shouldSuppressBootstrapText("user", line) {
			continue
		}
		if isTrivialAck(line) || isAuthorizationText(line) {
			continue
		}
		return line
	}

	text = normalizeSnippet(text)
	if text == "" || shouldSuppressBootstrapText("user", text) || isTrivialAck(text) || isAuthorizationText(text) {
		return ""
	}
	return text
}

// extractTextContent pulls the first text string from a message's content.
func extractTextContent(msg map[string]interface{}) string {
	// Handle string content
	if s, ok := msg["content"].(string); ok {
		return s
	}
	// Handle array content
	blocks, ok := msg["content"].([]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		block, ok := b.(map[string]interface{})
		if !ok {
			continue
		}
		if block["type"] == "text" {
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
