// cache.go — Explicit CachedContent management for the Gemini lane.
//
// Creates CachedContent objects for system instruction + tools prefix via the
// /v1beta/cachedContents API. Referenced in generateContent via the
// "cachedContent" field. Provides 90% token discount on cached prefix.
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// cacheEntry tracks an active CachedContent resource.
type cacheEntry struct {
	Name      string    // e.g. "cachedContents/abc123"
	Model     string    // model used to create the cache
	CreatedAt time.Time // when we created it
	TTL       int       // TTL in seconds at creation
	SysHash   string    // hash of systemInstruction used
	ToolsHash string    // hash of tools used
}

// cacheManager manages explicit CachedContent resources per session.
type cacheManager struct {
	mu       sync.Mutex
	caches   map[string]*cacheEntry // convID -> active cache
	upstream string
	apiKey   string
	captured bool
	ttlSec   int // default TTL for new caches
}

// newCacheManager creates a cache manager. Returns nil if not enabled.
func newCacheManager(enabled bool, upstream string, ttlSec int) *cacheManager {
	if !enabled {
		return nil
	}
	if upstream == "" {
		upstream = "https://generativelanguage.googleapis.com"
	}
	if ttlSec <= 0 {
		ttlSec = 3600
	}
	return &cacheManager{
		caches:   make(map[string]*cacheEntry),
		upstream: upstream,
		ttlSec:   ttlSec,
	}
}

// captureAuth stores the API key. Thread-safe; first call only.
func (cm *cacheManager) captureAuth(apiKey string) {
	if cm == nil || apiKey == "" {
		return
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.captured {
		return
	}
	cm.apiKey = apiKey
	cm.captured = true
}

// ensureCache creates or reuses a CachedContent for the session's system+tools prefix.
// Returns the cachedContent name (e.g. "cachedContents/abc123") or empty string on failure.
func (cm *cacheManager) ensureCache(convID, model string, systemInstruction, tools interface{}) string {
	if cm == nil {
		return ""
	}
	cm.mu.Lock()
	if !cm.captured || cm.apiKey == "" {
		cm.mu.Unlock()
		return ""
	}
	apiKey := cm.apiKey

	// Check if we already have a valid cache for this session.
	sysHash := hashJSON(systemInstruction)
	toolsHash := hashJSON(tools)

	if entry, ok := cm.caches[convID]; ok {
		// Reuse if system+tools haven't changed and cache isn't too old.
		if entry.SysHash == sysHash && entry.ToolsHash == toolsHash {
			age := time.Since(entry.CreatedAt)
			if age < time.Duration(entry.TTL)*time.Second*3/4 {
				cm.mu.Unlock()
				return entry.Name
			}
			// Close to expiry — refresh by creating a new one.
			log.Printf("[GEMINI-CACHE] conv=%s cache approaching expiry (age=%v), recreating", convID, age)
		} else {
			log.Printf("[GEMINI-CACHE] conv=%s system/tools changed, recreating cache", convID)
		}
		// Delete old cache in background (best-effort).
		oldName := entry.Name
		go cm.deleteCache(oldName, apiKey)
	}
	cm.mu.Unlock()

	// Create new CachedContent.
	name, err := cm.createCache(model, systemInstruction, tools, apiKey)
	if err != nil {
		log.Printf("[GEMINI-CACHE] conv=%s create failed: %v", convID, err)
		return ""
	}

	cm.mu.Lock()
	cm.caches[convID] = &cacheEntry{
		Name:      name,
		Model:     model,
		CreatedAt: time.Now(),
		TTL:       cm.ttlSec,
		SysHash:   sysHash,
		ToolsHash: toolsHash,
	}
	cm.mu.Unlock()

	log.Printf("[GEMINI-CACHE] conv=%s created %s (ttl=%ds)", convID, name, cm.ttlSec)
	return name
}

// getCacheName returns the active cache name for a session, or empty string.
func (cm *cacheManager) getCacheName(convID string) string {
	if cm == nil {
		return ""
	}
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if entry, ok := cm.caches[convID]; ok {
		return entry.Name
	}
	return ""
}

// createCache creates a CachedContent via the Gemini API.
func (cm *cacheManager) createCache(model string, systemInstruction, tools interface{}, apiKey string) (string, error) {
	reqBody := map[string]interface{}{
		"model": "models/" + model,
		"ttl":   fmt.Sprintf("%ds", cm.ttlSec),
	}

	if systemInstruction != nil {
		reqBody["systemInstruction"] = systemInstruction
	}
	if tools != nil {
		reqBody["tools"] = tools
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/cachedContents?key=%s", cm.upstream, apiKey)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if result.Name == "" {
		return "", fmt.Errorf("empty name in response")
	}
	return result.Name, nil
}

// deleteCache deletes a CachedContent resource (best-effort).
func (cm *cacheManager) deleteCache(name, apiKey string) {
	if name == "" {
		return
	}
	url := fmt.Sprintf("%s/v1beta/%s?key=%s", cm.upstream, name, apiKey)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[GEMINI-CACHE] delete %s failed: %v", name, err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[GEMINI-CACHE] delete %s returned %d", name, resp.StatusCode)
	}
}

// hashJSON returns a short hash of a JSON value for comparison.
func hashJSON(v interface{}) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return hashContent(map[string]interface{}{"_": string(b)})
}
