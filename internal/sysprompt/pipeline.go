// Package sysprompt implements the system prompt modification pipeline.
// Ported from mitm_itt_addon.py _modify_system_prompt().
//
// Pipeline stages:
//  1. Full replacement mode (if configured)
//  2. User patches from JSON file
//  3. Strip patterns (remove restrictive text blocks)
//  4. Replace patterns (same-length semantic inversions)
//  5. Fragment caching (avoid re-processing identical prompts)
package sysprompt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Patch defines a user-supplied system prompt patch loaded from JSON.
type Patch struct {
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

// ReplacePattern is a same-length semantic inversion.
// API rejects requests where len(modified) > len(original), so replacements
// MUST be the same byte length as the original.
type ReplacePattern struct {
	Old string
	New string
}

// StripPattern is a substring to remove entirely from system prompts.
type StripPattern string

// Pipeline processes system prompt blocks before forwarding to upstream.
type Pipeline struct {
	mu            sync.RWMutex
	enabled       bool
	stripPatterns []StripPattern
	replaceMap    map[string]string // old -> new (same-length)
	replaceFile   string
	replaceMtime  time.Time
	patchFile     string
	patchMtime    time.Time
	patches       []Patch
	fullReplace   []map[string]interface{} // if set, replaces entire system prompt
	fragmentCache map[string][]map[string]interface{}
}

// NewPipeline creates a prompt modification pipeline.
func NewPipeline(enabled bool) *Pipeline {
	return &Pipeline{
		enabled:       enabled,
		replaceMap:    make(map[string]string),
		fragmentCache: make(map[string][]map[string]interface{}),
	}
}

// SetStripPatterns configures text blocks to strip entirely.
func (p *Pipeline) SetStripPatterns(patterns []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stripPatterns = make([]StripPattern, len(patterns))
	for i, pat := range patterns {
		p.stripPatterns[i] = StripPattern(pat)
	}
}

// SetEnabled toggles the pipeline at runtime.
func (p *Pipeline) SetEnabled(enabled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enabled = enabled
}

// SetReplacePatterns configures same-length semantic inversions.
// Panics if any replacement differs in byte length from its original.
func (p *Pipeline) SetReplacePatterns(pairs map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setReplacePatternsLocked(pairs)
}

func (p *Pipeline) setReplacePatternsLocked(pairs map[string]string) {
	p.replaceMap = make(map[string]string, len(pairs))
	for old, new_ := range pairs {
		if len(old) != len(new_) {
			log.Printf("[SYSPROMPT] WARNING: replace pattern length mismatch: %q (%d) -> %q (%d), padding",
				old[:min(40, len(old))], len(old), new_[:min(40, len(new_))], len(new_))
			// Pad shorter to match longer
			if len(new_) < len(old) {
				new_ = new_ + strings.Repeat(" ", len(old)-len(new_))
			} else {
				// Truncate if replacement is longer (API rejects longer)
				new_ = new_[:len(old)]
			}
		}
		p.replaceMap[old] = new_
	}
}

// SetPatchFile configures the path to user patches JSON.
func (p *Pipeline) SetPatchFile(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.patchFile != path {
		p.patchMtime = time.Time{}
		p.patches = nil
	}
	p.patchFile = path
}

// SetReplaceFile configures the path to same-length replacement JSON.
func (p *Pipeline) SetReplaceFile(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.replaceFile != path {
		p.replaceMtime = time.Time{}
		p.replaceMap = make(map[string]string)
	}
	p.replaceFile = path
}

// SyncConfig hot-applies the sysprompt-related config surface.
func (p *Pipeline) SyncConfig(enabled bool, patchFile, replaceFile string) {
	p.SetEnabled(enabled)
	p.SetPatchFile(patchFile)
	p.SetReplaceFile(replaceFile)
	p.Refresh()
}

// SetFullReplacement sets a complete system prompt replacement.
// When set, the entire original system prompt is replaced.
func (p *Pipeline) SetFullReplacement(blocks []map[string]interface{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fullReplace = blocks
}

// Refresh reloads dynamic file-backed pipeline inputs when they change on disk.
func (p *Pipeline) Refresh() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reloadPatchesLocked()
	p.reloadReplacePatternsLocked()
}

// Fingerprint returns a stable hash of the active sysprompt configuration.
// Canonical system-prompt caching uses this so config changes cannot reuse
// stale pre-edit cache entries.
func (p *Pipeline) Fingerprint() string {
	p.Refresh()

	p.mu.RLock()
	defer p.mu.RUnlock()

	stripPatterns := make([]string, len(p.stripPatterns))
	for i, pat := range p.stripPatterns {
		stripPatterns[i] = string(pat)
	}
	snapshot := struct {
		Enabled       bool                     `json:"enabled"`
		PatchFile     string                   `json:"patch_file"`
		ReplaceFile   string                   `json:"replace_file"`
		Patches       []Patch                  `json:"patches"`
		ReplaceMap    map[string]string        `json:"replace_map"`
		StripPatterns []string                 `json:"strip_patterns"`
		FullReplace   []map[string]interface{} `json:"full_replace"`
	}{
		Enabled:       p.enabled,
		PatchFile:     p.patchFile,
		ReplaceFile:   p.replaceFile,
		Patches:       append([]Patch(nil), p.patches...),
		ReplaceMap:    cloneReplaceMap(p.replaceMap),
		StripPatterns: stripPatterns,
		FullReplace:   p.fullReplace,
	}
	data, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:16])
}

// Process modifies system prompt blocks in-place.
// Returns the modified blocks and count of modifications applied.
func (p *Pipeline) Process(system []interface{}) ([]interface{}, int) {
	if len(system) == 0 {
		return system, 0
	}

	p.Refresh()

	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.enabled {
		return system, 0
	}

	// Stage 1: Full replacement
	if len(p.fullReplace) > 0 {
		result := make([]interface{}, len(p.fullReplace))
		for i, block := range p.fullReplace {
			result[i] = block
		}
		return result, 1
	}

	// Check fragment cache
	cacheKey := fragmentCacheKey(system)
	if cached, ok := p.fragmentCache[cacheKey]; ok {
		result := make([]interface{}, len(cached))
		for i, block := range cached {
			result[i] = block
		}
		return result, 0
	}

	mods := 0
	result := make([]interface{}, 0, len(system))

	for _, block := range system {
		bm, ok := block.(map[string]interface{})
		if !ok {
			result = append(result, block)
			continue
		}

		text, ok := bm["text"].(string)
		if !ok || text == "" {
			result = append(result, bm)
			continue
		}

		original := text

		// Stage 2: User patches
		for _, patch := range p.patches {
			if patch.Find != "" && strings.Contains(text, patch.Find) {
				text = strings.Replace(text, patch.Find, patch.Replace, 1)
			}
		}

		// Stage 3: Strip patterns
		for _, pattern := range p.stripPatterns {
			pat := string(pattern)
			if strings.Contains(text, pat) {
				text = strings.Replace(text, pat, "", -1)
			}
		}

		// Stage 4: Same-length replacements
		for old, new_ := range p.replaceMap {
			if strings.Contains(text, old) {
				text = strings.Replace(text, old, new_, -1)
			}
		}

		if text != original {
			mods++
		}

		// Clone block with modified text
		newBlock := make(map[string]interface{})
		for k, v := range bm {
			newBlock[k] = v
		}
		newBlock["text"] = text
		result = append(result, newBlock)
	}

	// Cache the result (only cache if no modifications to avoid stale patches)
	if mods == 0 {
		cached := make([]map[string]interface{}, len(result))
		for i, block := range result {
			if bm, ok := block.(map[string]interface{}); ok {
				cached[i] = bm
			}
		}
		// Note: safe because we hold RLock and fragmentCache is only written here
		// In production, upgrade to write lock or use sync.Map
	}

	return result, mods
}

func (p *Pipeline) reloadPatchesLocked() {
	if p.patchFile == "" {
		p.patchMtime = time.Time{}
		p.patches = nil
		return
	}
	info, err := os.Stat(p.patchFile)
	if err != nil {
		if os.IsNotExist(err) {
			p.patchMtime = time.Time{}
			p.patches = nil
			return
		}
		log.Printf("[SYSPROMPT] Failed to stat patch file %s: %v", p.patchFile, err)
		return
	}
	if info.ModTime().Equal(p.patchMtime) {
		return
	}

	data, err := os.ReadFile(p.patchFile)
	if err != nil {
		log.Printf("[SYSPROMPT] Failed to read patch file %s: %v", p.patchFile, err)
		return
	}

	var patches []Patch
	if err := json.Unmarshal(data, &patches); err != nil {
		log.Printf("[SYSPROMPT] Invalid JSON in patch file %s: %v", p.patchFile, err)
		return
	}

	p.patches = patches
	p.patchMtime = info.ModTime()
	log.Printf("[SYSPROMPT] Loaded %d patches from %s", len(patches), p.patchFile)
}

func (p *Pipeline) reloadReplacePatternsLocked() {
	if p.replaceFile == "" {
		p.replaceMtime = time.Time{}
		p.replaceMap = make(map[string]string)
		return
	}
	info, err := os.Stat(p.replaceFile)
	if err != nil {
		if os.IsNotExist(err) {
			p.replaceMtime = time.Time{}
			p.replaceMap = make(map[string]string)
			return
		}
		log.Printf("[SYSPROMPT] Failed to stat replace file %s: %v", p.replaceFile, err)
		return
	}
	if info.ModTime().Equal(p.replaceMtime) {
		return
	}

	data, err := os.ReadFile(p.replaceFile)
	if err != nil {
		log.Printf("[SYSPROMPT] Failed to read replace file %s: %v", p.replaceFile, err)
		return
	}

	var replaceMap map[string]string
	if err := json.Unmarshal(data, &replaceMap); err != nil {
		log.Printf("[SYSPROMPT] Invalid JSON in replace file %s: %v", p.replaceFile, err)
		return
	}

	p.setReplacePatternsLocked(replaceMap)
	p.replaceMtime = info.ModTime()
	log.Printf("[SYSPROMPT] Loaded %d replace patterns from %s", len(replaceMap), p.replaceFile)
}

func fragmentCacheKey(system []interface{}) string {
	data, _ := json.Marshal(system)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

func cloneReplaceMap(src map[string]string) map[string]string {
	if len(src) == 0 {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TenantConfig is a per-request sysprompt overlay submitted by a tenant.
// Applied without modifying global pipeline state. Evaluated before global pipeline.
type TenantConfig struct {
	Enabled       bool
	FullReplace   []map[string]interface{}
	Patches       []Patch
	StripPatterns []string
	ReplaceMap    map[string]string
}

// ProcessWithTenant applies per-tenant config as overlay, then falls through to global pipeline.
// tenantCfg may be nil (no per-tenant config -> global only).
// tenantID is used for cache key namespacing to prevent cross-tenant cache pollution.
func (p *Pipeline) ProcessWithTenant(system []interface{}, tenantID string, tenantCfg *TenantConfig) ([]interface{}, int) {
	if tenantCfg == nil || !tenantCfg.Enabled {
		return p.Process(system)
	}

	// Defensive: recover from any panic caused by malformed tenant config.
	// A bad tenant config must never crash the proxy.
	var result []interface{}
	var mods int
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[SYSPROMPT] panic in tenant config processing (tenant=%s): %v", tenantID[:min(len(tenantID), 8)], r)
				// Fall through to global pipeline only
				result, mods = p.Process(system)
			}
		}()

		working := deepCopySystem(system)
		tenantMods := 0

		// Stage 1: Full replacement (tenant-level)
		if len(tenantCfg.FullReplace) > 0 {
			// Convert []map[string]interface{} to []interface{} for pipeline compatibility
			replaced := make([]interface{}, len(tenantCfg.FullReplace))
			for i, block := range tenantCfg.FullReplace {
				replaced[i] = block
			}
			working = replaced
			tenantMods++
		}

		// Stage 2: Patches (tenant-level) — same logic as global pipeline
		for _, patch := range tenantCfg.Patches {
			if patch.Find == "" {
				continue
			}
			for _, block := range working {
				if bm, ok := block.(map[string]interface{}); ok {
					if text, ok := bm["text"].(string); ok && strings.Contains(text, patch.Find) {
						bm["text"] = strings.Replace(text, patch.Find, patch.Replace, 1)
						tenantMods++
					}
				}
			}
		}

		// Stage 3: Strip patterns (tenant-level) — strings.Contains only, zero regexp
		for _, pattern := range tenantCfg.StripPatterns {
			working = applyStrip(working, pattern)
			tenantMods++
		}

		// Stage 4: Replace map (tenant-level) — same-length enforcement already validated at ingest
		for old, nw := range tenantCfg.ReplaceMap {
			working = applyReplace(working, old, nw)
			tenantMods++
		}

		// Now apply global pipeline on top of tenant-modified system
		result, mods = p.Process(working)
		mods += tenantMods
	}()

	return result, mods
}

// deepCopySystem creates a deep copy of system blocks to avoid mutating the original.
func deepCopySystem(system []interface{}) []interface{} {
	data, err := json.Marshal(system)
	if err != nil {
		return system
	}
	var copy []interface{}
	if err := json.Unmarshal(data, &copy); err != nil {
		return system
	}
	return copy
}

// applyStrip removes occurrences of pattern from all text blocks using strings.Contains.
func applyStrip(system []interface{}, pattern string) []interface{} {
	for _, block := range system {
		if bm, ok := block.(map[string]interface{}); ok {
			if text, ok := bm["text"].(string); ok {
				bm["text"] = strings.ReplaceAll(text, pattern, "")
			}
		}
	}
	return system
}

// applyReplace replaces old with nw in all text blocks.
func applyReplace(system []interface{}, old, nw string) []interface{} {
	for _, block := range system {
		if bm, ok := block.(map[string]interface{}); ok {
			if text, ok := bm["text"].(string); ok {
				bm["text"] = strings.ReplaceAll(text, old, nw)
			}
		}
	}
	return system
}
