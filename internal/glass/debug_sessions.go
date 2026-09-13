package glass

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DebugSessionSnapshot is a lane-neutral snapshot of Glass-owned session state.
type DebugSessionSnapshot struct {
	ConvID               string `json:"conv_id"`
	MessageCount         int    `json:"message_count"`
	TotalTokens          int    `json:"total_tokens"`
	EvictedCount         int    `json:"evicted_count"`
	BatchCount           int    `json:"batch_count"`
	CompressionWatermark int    `json:"compression_watermark"`
	LastAPIInput         int    `json:"last_api_input"`
	LastAPIInputAt       string `json:"last_api_input_at"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
	CachePath            string `json:"cache_path"`
	CachePersisted       bool   `json:"cache_persisted"`
	StatePath            string `json:"state_path"`
	StatePersisted       bool   `json:"state_persisted"`
	ShadowPath           string `json:"shadow_path"`
	ShadowPersisted      bool   `json:"shadow_persisted"`
}

func formatDebugSessionTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func latestDebugSessionTime(times ...time.Time) time.Time {
	var latest time.Time
	for _, ts := range times {
		if ts.After(latest) {
			latest = ts
		}
	}
	return latest
}

func fileModTime(path string) time.Time {
	if path == "" {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func debugPathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (e *Engine) debugSessionKeys() []string {
	if e == nil {
		return nil
	}

	keys := make(map[string]struct{})

	if e.sessions != nil {
		e.sessions.mu.RLock()
		for convID := range e.sessions.sessions {
			if convID != "" {
				keys[convID] = struct{}{}
			}
		}
		e.sessions.mu.RUnlock()
	}

	e.mu.Lock()
	for convID := range e.caches {
		if convID != "" {
			keys[convID] = struct{}{}
		}
	}
	shadowDir := e.cfg.ShadowDir
	e.mu.Unlock()

	if shadowDir != "" {
		if entries, err := os.ReadDir(shadowDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && entry.Name() != "" {
					keys[entry.Name()] = struct{}{}
				}
			}
		}
	}

	out := make([]string, 0, len(keys))
	for convID := range keys {
		out = append(out, convID)
	}
	sort.Strings(out)
	return out
}

func (e *Engine) debugCache(convID string) *LocalCache {
	if e == nil || convID == "" {
		return nil
	}

	e.mu.Lock()
	cache := e.caches[convID]
	shadowDir := e.cfg.ShadowDir
	e.mu.Unlock()
	if cache != nil {
		return cache
	}
	cache, _ = LoadLocalCache(convID, shadowDir)
	return cache
}

func (e *Engine) debugState(convID string) *SessionState {
	if e == nil || convID == "" || e.sessions == nil {
		return nil
	}

	e.sessions.mu.RLock()
	state := e.sessions.sessions[convID]
	e.sessions.mu.RUnlock()
	if state != nil {
		return state
	}
	state, _ = e.loadPersistedSessionState(convID)
	return state
}

func (e *Engine) debugSnapshot(convID string) (DebugSessionSnapshot, bool) {
	if e == nil || convID == "" {
		return DebugSessionSnapshot{}, false
	}

	cache := e.debugCache(convID)
	state := e.debugState(convID)
	shadowDir := e.cfg.ShadowDir

	cachePath := filepath.Join(shadowDir, convID, "localcache.json")
	statePath := filepath.Join(shadowDir, convID, "state.json")
	shadowPath := filepath.Join(shadowDir, convID, "shadow.md")

	cachePersisted := debugPathExists(cachePath)
	statePersisted := debugPathExists(statePath)
	shadowPersisted := debugPathExists(shadowPath)

	if cache == nil && state == nil && !cachePersisted && !statePersisted && !shadowPersisted {
		return DebugSessionSnapshot{}, false
	}

	messageCount := 0
	totalTokens := 0
	if cache != nil {
		messageCount = cache.Len()
		totalTokens = cache.TotalTokens()
	}

	var createdAt time.Time
	var updatedAt time.Time
	snapshot := DebugSessionSnapshot{
		ConvID:          convID,
		MessageCount:    messageCount,
		TotalTokens:     totalTokens,
		CachePath:       cachePath,
		CachePersisted:  cachePersisted,
		StatePath:       statePath,
		StatePersisted:  statePersisted,
		ShadowPath:      shadowPath,
		ShadowPersisted: shadowPersisted,
	}
	if state != nil {
		snapshot.EvictedCount = state.EvictedCount
		snapshot.BatchCount = state.BatchCount
		snapshot.CompressionWatermark = state.CompressionWatermark
		snapshot.LastAPIInput = state.LastAPIInput
		snapshot.LastAPIInputAt = formatDebugSessionTime(state.LastAPIInputAt)
		createdAt = state.CreatedAt
		updatedAt = latestDebugSessionTime(state.UpdatedAt, state.LastAPIInputAt)
	}

	createdAt = latestDebugSessionTime(createdAt, fileModTime(cachePath), fileModTime(statePath), fileModTime(shadowPath))
	updatedAt = latestDebugSessionTime(updatedAt, fileModTime(cachePath), fileModTime(statePath), fileModTime(shadowPath))
	snapshot.CreatedAt = formatDebugSessionTime(createdAt)
	snapshot.UpdatedAt = formatDebugSessionTime(updatedAt)

	return snapshot, true
}

// DebugSessions lists known Glass sessions from memory and persisted shadow state.
func (e *Engine) DebugSessions() []DebugSessionSnapshot {
	keys := e.debugSessionKeys()
	if len(keys) == 0 {
		return nil
	}

	snapshots := make([]DebugSessionSnapshot, 0, len(keys))
	for _, convID := range keys {
		snapshot, ok := e.debugSnapshot(convID)
		if ok {
			snapshots = append(snapshots, snapshot)
		}
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].UpdatedAt > snapshots[j].UpdatedAt
	})
	return snapshots
}

// DebugSessionIDs lists known Glass session ids without loading full snapshots.
func (e *Engine) DebugSessionIDs() []string {
	return e.debugSessionKeys()
}

// DebugSession returns a single Glass session snapshot.
func (e *Engine) DebugSession(convID string) (DebugSessionSnapshot, bool) {
	return e.debugSnapshot(convID)
}

// DebugSessionMessages returns the normalized live request messages for a session.
func (e *Engine) DebugSessionMessages(convID string) ([]interface{}, bool) {
	cache := e.debugCache(convID)
	if cache == nil {
		return nil, false
	}

	e.mu.Lock()
	stripThinking := e.cfg.StripThinking
	e.mu.Unlock()

	view := cache.BuildForRequest(stripThinking)
	return view.Messages, true
}
