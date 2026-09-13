// bridge.go — writes spoofed usage to the active runtime lane's usage bridge file.
// This is the bridge file the current CLI lane reads for usage display.
// Keyed by conv_{fingerprint} so each conversation tracks independently.
package spoofer

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"proxy.local/app/internal/runtimepaths"
)

// Bridge writes spoofed usage data to the shared file that Claude Code reads.
type Bridge struct {
	mu   sync.Mutex
	path string
}

// UsageEntry is a single conversation's usage record in the bridge file.
type UsageEntry struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_input_tokens"`
	CacheCreate  int     `json:"cache_creation_input_tokens"`
	Timestamp    float64 `json:"timestamp"`
	Model        string  `json:"model"`
}

// NewBridge creates a bridge writer. Default path resolves from the active runtime lane/root.
func NewBridge(path string) *Bridge {
	if path == "" {
		path = runtimepaths.Current().UsageBridgePath
	}
	return &Bridge{path: path}
}

// Write updates the bridge file with spoofed usage for a conversation.
func (b *Bridge) Write(convFingerprint string, entry UsageEntry) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Read existing data
	data := make(map[string]UsageEntry)
	if existing, err := os.ReadFile(b.path); err == nil {
		json.Unmarshal(existing, &data)
	}

	// Update entry
	key := "conv_" + convFingerprint
	entry.Timestamp = float64(time.Now().Unix())
	data[key] = entry

	// Write back
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	// Ensure directory exists
	dir := filepath.Dir(b.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Atomic write: tmp file → os.Rename. Prevents half-written files
	// if the process is killed mid-write (Claude Code reads this file).
	tmpPath := b.path + ".tmp"
	if err := os.WriteFile(tmpPath, out, 0644); err != nil {
		log.Printf("[BRIDGE] Failed to write tmp %s: %v", tmpPath, err)
		return err
	}
	if err := os.Rename(tmpPath, b.path); err != nil {
		log.Printf("[BRIDGE] Failed to rename %s -> %s: %v", tmpPath, b.path, err)
		return err
	}

	log.Printf("[BRIDGE] Updated %s: %s = %d input tokens", b.path, key, entry.InputTokens)
	return nil
}
