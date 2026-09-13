// watermark.go — per-conversation watermark tracking.
// Controls which messages have been trimmed to prevent incremental trim
// boundary creep that would break prompt caching.
package trimmer

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// WatermarkStore manages per-conversation watermark state with optional persistence.
type WatermarkStore struct {
	entries map[string]watermarkEntry
	path    string // optional persistence path
}

// NewWatermarkStore creates a watermark store, optionally loading from disk.
func NewWatermarkStore(path string) *WatermarkStore {
	ws := &WatermarkStore{
		entries: make(map[string]watermarkEntry),
		path:    path,
	}
	if path != "" {
		ws.load()
	}
	return ws
}

// Get returns the current watermark index for a conversation.
func (ws *WatermarkStore) Get(convID string) int {
	if entry, ok := ws.entries[convID]; ok {
		return entry.Idx
	}
	return 0
}

// Advance updates the watermark to a new index.
func (ws *WatermarkStore) Advance(convID string, idx int) {
	ws.entries[convID] = watermarkEntry{Idx: idx, Ts: time.Now()}
	if ws.path != "" {
		ws.save()
	}
}

// Reset removes the watermark for a conversation (after message drop).
func (ws *WatermarkStore) Reset(convID string) {
	delete(ws.entries, convID)
	if ws.path != "" {
		ws.save()
	}
}

// Cleanup removes expired watermarks older than maxAge.
func (ws *WatermarkStore) Cleanup(maxAge time.Duration) {
	now := time.Now()
	for id, entry := range ws.entries {
		if now.Sub(entry.Ts) > maxAge {
			delete(ws.entries, id)
		}
	}
}

func (ws *WatermarkStore) load() {
	data, err := os.ReadFile(ws.path)
	if err != nil {
		return
	}
	var entries map[string]watermarkEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("[WATERMARK] Failed to parse %s: %v", ws.path, err)
		return
	}
	now := time.Now()
	for id, entry := range entries {
		if now.Sub(entry.Ts) < wmMaxAge {
			ws.entries[id] = entry
		}
	}
	log.Printf("[WATERMARK] Loaded %d watermarks from %s", len(ws.entries), ws.path)
}

func (ws *WatermarkStore) save() {
	data, err := json.Marshal(ws.entries)
	if err != nil {
		return
	}
	if err := os.WriteFile(ws.path, data, 0644); err != nil {
		log.Printf("[WATERMARK] Failed to save %s: %v", ws.path, err)
	}
}
