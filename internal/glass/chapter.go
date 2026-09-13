package glass

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	chapterProjectID       = "global"
	chapterQuoteMaxChars   = 300
	chapterReplayMaxChars  = 60000
	chapterReplaySpanChars = 30000
	chapterReplayRadius    = 2
	chapterCheckpointLimit = 3
)

type ChapterCheckpoint struct {
	ID       string `json:"id"`
	MsgRange [2]int `json:"msg_range"`
	Role     string `json:"role,omitempty"`
	Quote    string `json:"quote"`
}

type ChapterMessage struct {
	MsgNum  int    `json:"msg_num"`
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChapterDocument struct {
	ChapterID        string           `json:"chapter_id"`
	ChapterLabel     string           `json:"chapter_label"`
	ProjectID        string           `json:"project_id"`
	ConvID           string           `json:"conv_id"`
	Date             string           `json:"date"`
	StartTime        time.Time        `json:"start_time"`
	UpdatedAt        time.Time        `json:"updated_at"`
	Status           string           `json:"status"`
	SealReason       string           `json:"seal_reason,omitempty"`
	ShadowPath       string           `json:"shadow_path,omitempty"`
	MessageCount     int              `json:"message_count"`
	EvictedCount     int              `json:"evicted_count"`
	EvictionNum      int              `json:"eviction_num"`
	MsgRangeStart    int              `json:"msg_range_start"`
	MsgRangeEnd      int              `json:"msg_range_end"`
	ArchivedMessages []ChapterMessage `json:"archived_messages,omitempty"`

	// Kept for JSON compat / index but no longer populated.
	Checkpoints      []ChapterCheckpoint `json:"checkpoints,omitempty"`
	RetainedMessages []ChapterMessage    `json:"retained_messages,omitempty"`
}

type ChapterIndexRecord struct {
	ChapterID      string              `json:"chapter_id"`
	ChapterLabel   string              `json:"chapter_label"`
	ConvID         string              `json:"conv_id"`
	Date           string              `json:"date"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Status         string              `json:"status"`
	EvictionNum    int                 `json:"eviction_num"`
	MsgRange       [2]int              `json:"msg_range"`
	CheckpointRefs []ChapterCheckpoint `json:"checkpoint_refs,omitempty"`
	PathMD         string              `json:"path_md"`
	PathJSON       string              `json:"path_json"`
}

type ChapterIndex struct {
	ProjectID string               `json:"project_id"`
	Date      string               `json:"date"`
	Chapters  []ChapterIndexRecord `json:"chapters"`
}

type ChapterWriter struct {
	shadowDir string
	booksDir  string
}

func NewChapterWriter(shadowDir string) *ChapterWriter {
	if shadowDir == "" {
		return nil
	}
	return &ChapterWriter{
		shadowDir: shadowDir,
		booksDir:  filepath.Join(filepath.Dir(shadowDir), "glass-books"),
	}
}

// WriteEviction writes one chapter file for a single eviction event.
// Each eviction gets its own numbered file. Written once, never modified.
// Returns the session chapter directory path (for the bookmark).
func (cw *ChapterWriter) WriteEviction(convID string, state *SessionState, evicted []map[string]interface{}) (chapterDir string, err error) {
	if cw == nil || state == nil || convID == "" || len(evicted) == 0 {
		return "", nil
	}

	chapterDate := time.Now()
	if !state.CreatedAt.IsZero() {
		chapterDate = state.CreatedAt
	}
	day := chapterDate.Format("2006-01-02")

	// Session dir: glass-books/global/2026-03-14/<convID>/
	sessionDir := filepath.Join(cw.booksDir, chapterProjectID, day, convID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return "", fmt.Errorf("create chapter session dir: %w", err)
	}

	evictionNum := state.BatchCount
	firstMsgNum := state.EvictedCount - len(evicted) + 1
	lastMsgNum := state.EvictedCount

	chapterID := fmt.Sprintf("%s-%s-eviction-%03d", day, convID, evictionNum)
	chapterLabel := fmt.Sprintf("Session %s — Eviction %d (messages %d–%d)",
		convID, evictionNum, firstMsgNum, lastMsgNum)

	// Build archived messages
	archived := make([]ChapterMessage, 0, len(evicted))
	for i, msg := range evicted {
		content := chapterMessageContent(msg)
		if strings.TrimSpace(content) == "" {
			continue
		}
		role, _ := msg["role"].(string)
		archived = append(archived, ChapterMessage{
			MsgNum:  firstMsgNum + i,
			Role:    role,
			Content: content,
		})
	}

	doc := &ChapterDocument{
		ChapterID:        chapterID,
		ChapterLabel:     chapterLabel,
		ProjectID:        chapterProjectID,
		ConvID:           convID,
		Date:             day,
		StartTime:        chapterDate,
		UpdatedAt:        time.Now(),
		Status:           "sealed",
		ShadowPath:       filepath.Join(cw.shadowDir, convID, "shadow.md"),
		MessageCount:     len(archived),
		EvictedCount:     state.EvictedCount,
		EvictionNum:      evictionNum,
		MsgRangeStart:    firstMsgNum,
		MsgRangeEnd:      lastMsgNum,
		ArchivedMessages: archived,
	}

	baseName := fmt.Sprintf("chapter-%03d", evictionNum)
	mdPath := filepath.Join(sessionDir, baseName+".md")
	jsonPath := filepath.Join(sessionDir, baseName+".json")

	docData, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal chapter json: %w", err)
	}
	if err := os.WriteFile(jsonPath, docData, 0644); err != nil {
		return "", fmt.Errorf("write chapter json: %w", err)
	}

	if err := os.WriteFile(mdPath, []byte(renderChapterMarkdown(doc)), 0644); err != nil {
		return "", fmt.Errorf("write chapter markdown: %w", err)
	}

	// Update the day-level index
	indexPath := filepath.Join(cw.booksDir, chapterProjectID, day, "index.json")
	if err := cw.upsertIndex(indexPath, ChapterIndexRecord{
		ChapterID:    chapterID,
		ChapterLabel: chapterLabel,
		ConvID:       convID,
		Date:         day,
		UpdatedAt:    doc.UpdatedAt,
		Status:       "sealed",
		EvictionNum:  evictionNum,
		MsgRange:     [2]int{firstMsgNum, lastMsgNum},
		PathMD:       mdPath,
		PathJSON:     jsonPath,
	}); err != nil {
		log.Printf("[GLASS] Chapter index update error: %v", err)
	}

	// Update state to point at session dir (not individual file)
	state.ChapterID = chapterID
	state.ChapterMDPath = sessionDir
	state.ChapterJSONPath = jsonPath
	state.ChapterStatus = "sealed"
	state.ChapterUpdatedAt = doc.UpdatedAt

	log.Printf("[GLASS] Chapter written: %s (%d messages, msgs %d–%d)",
		mdPath, len(archived), firstMsgNum, lastMsgNum)

	// Recovery summary is now handled by the RollingSummarizer (summarizer.go).
	// It produces recovery-NNN.md via Opus subagent calls with strict guardrails.

	return sessionDir, nil
}

// Seal is kept for session-end cleanup. Now a no-op since chapters are
// already sealed on write.
func (cw *ChapterWriter) Seal(convID string, state *SessionState, cache *LocalCache, reason string) error {
	return nil
}

// Update is the old API — now delegates to WriteEviction for the evicted batch only.
// Calls with nil evicted batch are no-ops (no more continuous rewriting).
func (cw *ChapterWriter) Update(convID string, state *SessionState, cache *LocalCache, evicted []map[string]interface{}) error {
	if len(evicted) == 0 {
		return nil
	}
	_, err := cw.WriteEviction(convID, state, evicted)
	return err
}

// --- rendering (unchanged format) ---

func renderChapterMarkdown(doc *ChapterDocument) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(doc.ChapterLabel)
	b.WriteString("\n\n")
	b.WriteString("Session: ")
	b.WriteString(doc.ConvID)
	b.WriteString(" | Date: ")
	b.WriteString(doc.Date)
	if doc.Status != "" {
		b.WriteString(" | Status: ")
		b.WriteString(doc.Status)
	}
	b.WriteString("\n")
	if doc.ShadowPath != "" {
		b.WriteString("Shadow archive: ")
		b.WriteString(doc.ShadowPath)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	for _, msg := range doc.ArchivedMessages {
		content := strings.ReplaceAll(msg.Content, "\x00", "")
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("#### Message %d [%s]\n", msg.MsgNum, msg.Role))
		switch msg.Role {
		case "user":
			b.WriteString("\u2770 ")
			b.WriteString(content)
		case "assistant":
			b.WriteString("\u25cf ")
			b.WriteString(content)
		default:
			b.WriteString(content)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

// --- message content extraction (unchanged) ---

func chapterMessageContent(msg map[string]interface{}) string {
	if msg == nil {
		return ""
	}
	role, _ := msg["role"].(string)
	parts := make([]string, 0, 4)
	if reasoning, _ := msg["reasoning_content"].(string); strings.TrimSpace(reasoning) != "" {
		parts = append(parts, cleanChapterText(reasoning))
	}
	if text, ok := msg["content"].(string); ok {
		if role == "tool" {
			if placeholder := renderChapterToolResult(msg, text); placeholder != "" {
				parts = append(parts, placeholder)
			}
			return joinChapterParts(parts)
		}
		if cleaned := cleanChapterText(text); cleaned != "" {
			parts = append(parts, cleaned)
		}
		return joinChapterParts(parts)
	}
	content, ok := msg["content"].([]interface{})
	if !ok {
		return joinChapterParts(parts)
	}

	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch blockType, _ := block["type"].(string); blockType {
		case "text":
			if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, cleanChapterText(text))
			}
		case "tool_use":
			parts = append(parts, renderToolUse(block))
		case "server_tool_use":
			parts = append(parts, renderToolUse(block))
		case "tool_result":
			if placeholder := renderChapterToolResult(block, block["content"]); placeholder != "" {
				parts = append(parts, placeholder)
			}
		}
	}

	return joinChapterParts(parts)
}

func cleanChapterText(text string) string {
	return strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
}

func joinChapterParts(parts []string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanChapterText(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.TrimSpace(strings.Join(filtered, "\n"))
}

func renderChapterToolResult(attrs map[string]interface{}, content interface{}) string {
	detail := "tool result"
	if name, _ := attrs["name"].(string); strings.TrimSpace(name) != "" {
		detail = name + "()"
	} else if id, _ := attrs["tool_use_id"].(string); strings.TrimSpace(id) != "" {
		detail = "tool_use_id=" + clipChapterQuote(id, 24)
	} else if id, _ := attrs["tool_call_id"].(string); strings.TrimSpace(id) != "" {
		detail = "tool_call_id=" + clipChapterQuote(id, 24)
	}

	chars := chapterContentChars(content)
	if chars > 0 {
		return fmt.Sprintf("[tool result omitted: %s; original %d chars]", detail, chars)
	}
	return fmt.Sprintf("[tool result omitted: %s]", detail)
}

func chapterContentChars(content interface{}) int {
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

func renderToolUse(block map[string]interface{}) string {
	name, _ := block["name"].(string)
	if name == "" {
		name = "tool"
	}
	input, _ := block["input"].(map[string]interface{})
	if input == nil {
		return name + "()"
	}
	param := pickToolParam(name, input)
	if param == "" {
		return name + "()"
	}
	if len(param) > 200 {
		param = param[:197] + "..."
	}
	return name + "(" + param + ")"
}

func collapseToolUse(block map[string]interface{}) string {
	return renderToolUse(block)
}

func pickToolParam(toolName string, input map[string]interface{}) string {
	for _, key := range []string{
		"file_path", "command", "pattern", "query", "url",
		"path", "glob", "skill", "prompt",
	} {
		if v, ok := input[key].(string); ok && v != "" {
			return v
		}
	}
	for _, v := range input {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func extractToolResultText(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, raw := range v {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if blockType, _ := block["type"].(string); blockType != "text" {
				continue
			}
			if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// --- index management ---

func (cw *ChapterWriter) upsertIndex(indexPath string, record ChapterIndexRecord) error {
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return fmt.Errorf("create chapter index dir: %w", err)
	}

	index := ChapterIndex{
		ProjectID: chapterProjectID,
		Date:      record.Date,
	}
	if data, err := os.ReadFile(indexPath); err == nil {
		_ = json.Unmarshal(data, &index)
		if index.ProjectID == "" {
			index.ProjectID = chapterProjectID
		}
		if index.Date == "" {
			index.Date = record.Date
		}
	}

	replaced := false
	for i := range index.Chapters {
		if index.Chapters[i].ChapterID == record.ChapterID {
			index.Chapters[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		index.Chapters = append(index.Chapters, record)
	}
	sort.Slice(index.Chapters, func(i, j int) bool {
		return index.Chapters[i].UpdatedAt.Before(index.Chapters[j].UpdatedAt)
	})

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chapter index: %w", err)
	}
	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("write chapter index: %w", err)
	}

	mdPath := filepath.Join(filepath.Dir(indexPath), "index.md")
	if err := os.WriteFile(mdPath, []byte(renderChapterIndexMarkdown(index)), 0644); err != nil {
		return fmt.Errorf("write chapter index markdown: %w", err)
	}

	return nil
}

func renderChapterIndexMarkdown(index ChapterIndex) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(index.Date)
	b.WriteString("\n\n")
	for _, chapter := range index.Chapters {
		b.WriteString("- ")
		b.WriteString(chapter.ChapterLabel)
		b.WriteString("\n")
		b.WriteString("  Path: ")
		b.WriteString(chapter.PathMD)
		b.WriteString("\n\n")
	}
	return b.String()
}

// --- compat stubs for code that references old API ---

func computeChapterCheckpoints(archived, retained []ChapterMessage) []ChapterCheckpoint {
	return nil
}

func clipChapterQuote(text string, maxChars int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return ""
	}
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	return strings.TrimSpace(text[:maxChars-3]) + "..."
}

func hasChapterSourceMessages(cache *LocalCache) bool {
	return false
}

func extractRetainedChapterMessages(cache *LocalCache) []ChapterMessage {
	return nil
}

func (cw *ChapterWriter) LoadDocumentFromState(state *SessionState) (*ChapterDocument, error) {
	if cw == nil || state == nil || state.ChapterJSONPath == "" {
		return nil, nil
	}
	return cw.loadDocument(state.ChapterJSONPath)
}

func (cw *ChapterWriter) loadDocument(path string) (*ChapterDocument, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc ChapterDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (cw *ChapterWriter) ResolveLatestChapter() (*ChapterDocument, error) {
	if cw == nil {
		return nil, nil
	}

	entries, err := os.ReadDir(filepath.Join(cw.booksDir, chapterProjectID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() > entries[j].Name()
	})
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		indexPath := filepath.Join(cw.booksDir, chapterProjectID, entry.Name(), "index.json")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			continue
		}
		var index ChapterIndex
		if json.Unmarshal(data, &index) != nil || len(index.Chapters) == 0 {
			continue
		}
		sort.Slice(index.Chapters, func(i, j int) bool {
			return index.Chapters[i].UpdatedAt.After(index.Chapters[j].UpdatedAt)
		})
		for _, record := range index.Chapters {
			doc, err := cw.loadDocument(record.PathJSON)
			if err == nil && doc != nil {
				return doc, nil
			}
		}
	}
	return nil, nil
}

func adoptChapterDocumentIntoState(state *SessionState, doc *ChapterDocument, mdPath, jsonPath string) {
	if state == nil || doc == nil {
		return
	}
	state.ChapterID = doc.ChapterID
	state.ChapterMDPath = mdPath
	state.ChapterJSONPath = jsonPath
	state.ChapterStatus = doc.Status
	state.ChapterCheckpointRefs = append([]ChapterCheckpoint(nil), doc.Checkpoints...)
	state.ChapterUpdatedAt = doc.UpdatedAt
	state.ChapterSealReason = doc.SealReason
}

func allChapterMessages(doc *ChapterDocument) []ChapterMessage {
	if doc == nil {
		return nil
	}
	all := make([]ChapterMessage, 0, len(doc.ArchivedMessages)+len(doc.RetainedMessages))
	all = append(all, doc.ArchivedMessages...)
	all = append(all, doc.RetainedMessages...)
	sort.Slice(all, func(i, j int) bool {
		return all[i].MsgNum < all[j].MsgNum
	})
	return all
}

func renderFullChapterReplay(doc *ChapterDocument) string {
	var b strings.Builder
	b.WriteString("CHAPTER REHYDRATION\n")
	b.WriteString("Authoritative archived context follows. Use these exact transcript lines before resuming older work.\n\n")
	for _, msg := range allChapterMessages(doc) {
		b.WriteString(fmt.Sprintf("### Message %d [%s]\n", msg.MsgNum, msg.Role))
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func renderCheckpointChapterReplay(doc *ChapterDocument) string {
	return ""
}

func buildChapterReplayText(doc *ChapterDocument) string {
	if doc == nil {
		return ""
	}
	full := renderFullChapterReplay(doc)
	if full != "" && len(full) <= chapterReplayMaxChars {
		return full
	}
	return ""
}
