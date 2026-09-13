package glass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

type outboundPrefixSnapshot struct {
	Hash            string
	SystemHash      string
	ToolsHash       string
	MessageHash     []string
	Measured        int
	Anchor          int
	TailHash        string
	TailMessageHash []string
	TailMeasured    int
	TailAnchor      int
	TailTokens      int
}

type outboundPrefixDiagnosticMeta struct {
	RequestKey         string `json:"request_key,omitempty"`
	PrevAnchor         int    `json:"prev_anchor"`
	PrevTailAnchor     int    `json:"prev_tail_anchor"`
	InjectedReferences bool   `json:"injected_references,omitempty"`
	ReferenceInsertAt  int    `json:"reference_insert_at"`
	VisibleMessages    int    `json:"visible_messages"`
	CacheMessages      int    `json:"cache_messages"`
	EvictedCount       int    `json:"evicted_count"`
	BatchCount         int    `json:"batch_count"`
	RestoredToOriginal bool   `json:"restored_to_original,omitempty"`
}

type outboundPrefixDiagnosticEvent struct {
	Timestamp      time.Time                    `json:"timestamp"`
	ConversationID string                       `json:"conversation_id"`
	ChangeKind     string                       `json:"change_kind"`
	Divergence     string                       `json:"divergence,omitempty"`
	PrevHash       string                       `json:"prev_hash,omitempty"`
	SystemChanged  bool                         `json:"system_changed,omitempty"`
	ToolsChanged   bool                         `json:"tools_changed,omitempty"`
	TailChangeKind string                       `json:"tail_change_kind,omitempty"`
	TailDivergence string                       `json:"tail_divergence,omitempty"`
	Snapshot       outboundPrefixSnapshot       `json:"snapshot"`
	Meta           outboundPrefixDiagnosticMeta `json:"meta"`
}

const (
	outboundPrefixLatestFile = "outbound_prefix_latest.json"
	outboundPrefixEventsFile = "outbound_prefix_events.jsonl"
)

func (e *Engine) logFinalPrefixDiagnostic(convID string, body map[string]interface{}, anchorIdx int, meta outboundPrefixDiagnosticMeta) outboundPrefixDiagnosticEvent {
	if convID == "" || body == nil {
		return outboundPrefixDiagnosticEvent{}
	}
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return outboundPrefixDiagnosticEvent{}
	}
	if anchorIdx < 0 || anchorIdx >= len(msgs) {
		anchorIdx = len(msgs) - 1
	}
	if anchorIdx < 0 {
		return outboundPrefixDiagnosticEvent{}
	}
	meta.VisibleMessages = len(msgs)
	meta.PrevTailAnchor = -1

	tailAnchor, tailHash, tailMessageHash, tailMeasured, tailTokens := measureTailSegment(msgs, anchorIdx)

	prefixMsgs := msgs[:anchorIdx+1]
	snap := outboundPrefixSnapshot{
		Hash:            hashJSON(map[string]interface{}{"tools": body["tools"], "system": body["system"], "messages": prefixMsgs}),
		SystemHash:      hashJSON(body["system"]),
		ToolsHash:       hashJSON(body["tools"]),
		MessageHash:     make([]string, 0, len(prefixMsgs)),
		Measured:        len(prefixMsgs),
		Anchor:          anchorIdx,
		TailHash:        tailHash,
		TailMessageHash: tailMessageHash,
		TailMeasured:    tailMeasured,
		TailAnchor:      tailAnchor,
		TailTokens:      tailTokens,
	}
	for _, raw := range prefixMsgs {
		if msg, ok := raw.(map[string]interface{}); ok {
			snap.MessageHash = append(snap.MessageHash, msgHash(msg))
			continue
		}
		snap.MessageHash = append(snap.MessageHash, hashJSON(raw))
	}

	e.mu.Lock()
	prev := e.outboundPrefixes[convID]
	e.outboundPrefixes[convID] = &snap
	e.mu.Unlock()

	event := outboundPrefixDiagnosticEvent{
		Timestamp:      time.Now(),
		ConversationID: convID,
		ChangeKind:     "init",
		Snapshot:       snap,
		Meta:           meta,
	}

	if prev == nil {
		log.Printf("[GLASS-DIAG] FINAL PREFIX INIT conv=%s hash=%s sys=%s tools=%s msgs=%d anchor=msg[%d]",
			convID, shortHash(snap.Hash), shortHash(snap.SystemHash), shortHash(snap.ToolsHash), snap.Measured, snap.Anchor)
		event.TailChangeKind = "init"
		e.persistFinalPrefixDiagnostic(convID, event)
		return event
	}

	event.PrevHash = prev.Hash
	event.Meta.PrevTailAnchor = prev.TailAnchor
	event.SystemChanged = prev.SystemHash != snap.SystemHash
	event.ToolsChanged = prev.ToolsHash != snap.ToolsHash
	if prev.TailHash == snap.TailHash {
		event.TailChangeKind = "same"
	} else {
		event.TailChangeKind = "changed"
		event.TailDivergence = describeTailDivergence(prev, &snap)
	}
	if prev.Hash == snap.Hash {
		event.ChangeKind = "same"
		if event.TailChangeKind == "changed" {
			log.Printf("[GLASS-DIAG] FINAL PREFIX SAME BUT TAIL CHANGED conv=%s tail=%s->%s diverge_at=%s tail_msgs=%d tail_anchor=msg[%d] prev_tail_anchor=msg[%d]",
				convID,
				shortHash(prev.TailHash),
				shortHash(snap.TailHash),
				event.TailDivergence,
				snap.TailMeasured,
				snap.TailAnchor,
				event.Meta.PrevTailAnchor,
			)
		}
		e.persistFinalPrefixDiagnostic(convID, event)
		return event
	}

	event.ChangeKind = "changed"
	event.Divergence = describePrefixDivergence(prev, &snap)
	log.Printf("[GLASS-DIAG] FINAL PREFIX CHANGED conv=%s hash=%s->%s diverge_at=%s msgs=%d anchor=msg[%d] sys_changed=%t tools_changed=%t",
		convID,
		shortHash(prev.Hash),
		shortHash(snap.Hash),
		event.Divergence,
		snap.Measured,
		snap.Anchor,
		event.SystemChanged,
		event.ToolsChanged,
	)
	e.persistFinalPrefixDiagnostic(convID, event)
	return event
}

func measureTailSegment(msgs []interface{}, anchorIdx int) (tailAnchor int, tailHash string, tailMessageHash []string, tailMeasured int, tailTokens int) {
	tailAnchor = -1
	if len(msgs) == 0 || anchorIdx < 0 || anchorIdx >= len(msgs) {
		return tailAnchor, "", nil, 0, 0
	}

	for _, idx := range findMessageBreakpoints(msgs) {
		if idx > anchorIdx {
			tailAnchor = idx
			break
		}
	}

	tailEnd := len(msgs) - 1
	if tailAnchor > anchorIdx {
		tailEnd = tailAnchor
	}
	if anchorIdx+1 > tailEnd {
		return tailAnchor, hashJSON([]interface{}{}), []string{}, 0, 0
	}

	segment := msgs[anchorIdx+1 : tailEnd+1]
	tailMessageHash = make([]string, 0, len(segment))
	for _, raw := range segment {
		if msg, ok := raw.(map[string]interface{}); ok {
			tailMessageHash = append(tailMessageHash, msgHash(msg))
			continue
		}
		tailMessageHash = append(tailMessageHash, hashJSON(raw))
	}

	return tailAnchor, hashJSON(segment), tailMessageHash, len(segment), estimateTokens(segment)
}

func findMessageBreakpoints(msgs []interface{}) []int {
	anchors := make([]int, 0, 2)
	for idx, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if messageHasCacheControl(msg) {
			anchors = append(anchors, idx)
		}
	}
	return anchors
}

func messageHasCacheControl(msg map[string]interface{}) bool {
	if msg == nil {
		return false
	}
	if _, ok := msg["cache_control"]; ok {
		return true
	}
	content, _ := msg["content"].([]interface{})
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := block["cache_control"]; ok {
			return true
		}
	}
	return false
}

func describePrefixDivergence(prev, current *outboundPrefixSnapshot) string {
	if prev == nil || current == nil {
		return "unknown"
	}
	if prev.SystemHash != current.SystemHash {
		return "system"
	}
	if prev.ToolsHash != current.ToolsHash {
		return "tools"
	}
	limit := len(prev.MessageHash)
	if len(current.MessageHash) < limit {
		limit = len(current.MessageHash)
	}
	for i := 0; i < limit; i++ {
		if prev.MessageHash[i] != current.MessageHash[i] {
			return fmt.Sprintf("msg[%d]", i)
		}
	}
	if len(prev.MessageHash) != len(current.MessageHash) {
		return fmt.Sprintf("msg[%d]", limit)
	}
	return "unknown"
}

func describeTailDivergence(prev, current *outboundPrefixSnapshot) string {
	if prev == nil || current == nil {
		return "unknown"
	}
	limit := len(prev.TailMessageHash)
	if len(current.TailMessageHash) < limit {
		limit = len(current.TailMessageHash)
	}
	for i := 0; i < limit; i++ {
		if prev.TailMessageHash[i] != current.TailMessageHash[i] {
			return fmt.Sprintf("tail_msg[%d]", i)
		}
	}
	if prev.TailAnchor != current.TailAnchor {
		return "tail_anchor"
	}
	if len(prev.TailMessageHash) != len(current.TailMessageHash) {
		return fmt.Sprintf("tail_msg[%d]", limit)
	}
	return "unknown"
}

func (e *Engine) persistFinalPrefixDiagnostic(convID string, event outboundPrefixDiagnosticEvent) {
	if e == nil || e.cfg.ShadowDir == "" || convID == "" {
		return
	}
	latestPath, eventsPath := outboundPrefixDiagnosticPaths(e.cfg.ShadowDir, convID)

	e.diagMu.Lock()
	defer e.diagMu.Unlock()

	if err := writeJSONAtomic(latestPath, event); err != nil {
		log.Printf("[GLASS-DIAG] persist latest snapshot failed conv=%s: %v", convID, err)
		return
	}
	if err := appendJSONLine(eventsPath, event); err != nil {
		log.Printf("[GLASS-DIAG] append event failed conv=%s: %v", convID, err)
	}
}

func (e *Engine) clearPersistedPrefixDiagnostics(convID string) {
	if e == nil || e.cfg.ShadowDir == "" || convID == "" {
		return
	}
	latestPath, _ := outboundPrefixDiagnosticPaths(e.cfg.ShadowDir, convID)

	e.diagMu.Lock()
	defer e.diagMu.Unlock()

	if err := os.Remove(latestPath); err != nil && !os.IsNotExist(err) {
		log.Printf("[GLASS-DIAG] remove latest snapshot failed conv=%s: %v", convID, err)
	}
	if err := os.Remove(latestPath + ".tmp"); err != nil && !os.IsNotExist(err) {
		log.Printf("[GLASS-DIAG] remove temp snapshot failed conv=%s: %v", convID, err)
	}
}

func outboundPrefixDiagnosticPaths(baseDir, convID string) (latestPath, eventsPath string) {
	dir := filepath.Join(baseDir, convID)
	return filepath.Join(dir, outboundPrefixLatestFile), filepath.Join(dir, outboundPrefixEventsFile)
}

func writeJSONAtomic(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func appendJSONLine(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func hashJSON(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:16])
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
