package glass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"proxy.local/app/internal/subagent"
	"proxy.local/app/internal/sysprompt"
)

const (
	// uuidPlaceholder replaces session UUIDs for canonical caching.
	// The UUID is the only per-session dynamic element in the base instructions.
	uuidPlaceholder = "00000000-0000-0000-0000-000000000000"

	// contextBoundary separates base instructions from project context in Fragment 2.
	// gitStatus injection starts with this pattern.
	contextBoundaryMarker = "Contents of"
)

var (
	// Matches CC session UUIDs in scratchpad paths:
	// /tmp/claude-1000/-home-user-nataraja/15109e1a-826c-4d54-ab4a-a2bfea24a099/scratchpad
	uuidRE = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
)

// SyspromptProcessor implements the canonical system prompt cache.
// Ensures byte-identical system prompts across calls within a session,
// and across sessions (except the billing header, which Anthropic separates).
type SyspromptProcessor struct {
	mu             sync.RWMutex
	pipeline       *sysprompt.Pipeline
	canonicalCache map[string][]interface{} // cache_key → processed fragments (excluding billing)
	persistPath    string                   // disk path for canonical cache persistence
	sessionUUID    string                   // this session's UUID (extracted from first call)
	stale          bool                     // true if pipeline applied 0 mods on cache miss
}

// IsStale returns true if the sysprompt replacement targets did not match the current system prompt.
func (sp *SyspromptProcessor) IsStale() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.stale
}

// NewSyspromptProcessor creates a canonical system prompt processor.
func NewSyspromptProcessor(pipeline *sysprompt.Pipeline, persistDir string) *SyspromptProcessor {
	sp := &SyspromptProcessor{
		pipeline:       pipeline,
		canonicalCache: make(map[string][]interface{}),
		persistPath:    filepath.Join(persistDir, "canonical_cache.json"),
	}
	sp.loadFromDisk()
	return sp
}

// Process handles system prompt fragments for Glass.
//
// Flow:
//  1. Check shared subagent classification → bypass isolated small-system calls
//  2. Extract billing header (fragment 0) — preserve as-is
//  3. Strip <system-reminder> blocks from all fragments
//  4. Normalize UUID → placeholder (for cache key stability)
//  5. Apply canonical stable-fact replacement (strip boilerplate + append durable facts)
//  6. Compute cache key from the transformed normalized fragments
//  7. Cache HIT → serve billing + cached fragments (restore UUID)
//  8. Cache MISS → run sysprompt pipeline → cache result → serve
//
// Returns modified system fragments and modification count.
func (sp *SyspromptProcessor) Process(system []interface{}, info subagent.Classification, factContent string) ([]interface{}, int) {
	if len(system) == 0 {
		return system, 0
	}

	// Subagent bypass: isolated small-system prompts are handled outside the
	// canonical main-session path.
	// Don't modify, don't cache — they have completely different content.
	if info.BypassCanonical {
		log.Printf("[GLASS-SYS] Subagent detected (%s, %d chars) — bypassing", info.Type, info.SystemChars)
		if factContent != "" {
			return appendFactFragment(system, factContent), 0
		}
		return system, 0
	}

	// Extract billing header (fragment 0) — preserve verbatim.
	// CC validates this internally; any modification breaks the session.
	var billing interface{}
	fragments := system
	if len(system) >= 2 {
		if block, ok := system[0].(map[string]interface{}); ok {
			if text, ok := block["text"].(string); ok {
				if strings.Contains(text, "billing") || strings.Contains(text, "x-anthropic") {
					billing = system[0]
					fragments = system[1:]
				}
			}
		}
	}

	// Strip <system-reminder> from system fragments
	// (messages are handled separately by glass.stripSystemReminders)
	sysBody := map[string]interface{}{"system": fragments}
	stripSystemReminders(sysBody)
	fragments = sysBody["system"].([]interface{})

	// Extract UUID from fragments (first occurrence)
	sp.mu.Lock()
	if sp.sessionUUID == "" {
		for _, frag := range fragments {
			if block, ok := frag.(map[string]interface{}); ok {
				if text, ok := block["text"].(string); ok {
					if match := uuidRE.FindString(text); match != "" {
						sp.sessionUUID = match
						log.Printf("[GLASS-SYS] Extracted session UUID: %s", match)
						break
					}
				}
			}
		}
	}
	currentUUID := sp.sessionUUID
	sp.mu.Unlock()

	// Normalize UUID → placeholder for cache key computation
	normalized := normalizeUUID(fragments, currentUUID)
	normalized = sp.InjectFacts(normalized, factContent)
	cacheKey := canonicalCacheKey(normalized, sp.pipelineFingerprint())

	// Check canonical cache
	sp.mu.RLock()
	cached, ok := sp.canonicalCache[cacheKey]
	sp.mu.RUnlock()

	if ok {
		// Cache HIT — serve billing + cached fragments (placeholder UUID kept).
		// NOT restoring the real UUID keeps prefix bytes identical across sessions
		// so Anthropic's server-side cache can match prefixes from different conversations.
		result := deepCopyFragments(cached)
		if billing != nil {
			result = append([]interface{}{billing}, result...)
		}
		return result, 0
	}

	// Cache MISS — process through sysprompt pipeline
	log.Printf("[GLASS-SYS] Cache miss (key=%s, %d chars) — processing", cacheKey[:12], info.SystemChars)

	processed, mods := normalized, 0
	if sp.pipeline != nil {
		processed, mods = sp.pipeline.Process(normalized)
	}

	// Staleness detection: cache miss + 0 mods means replacement targets did not match.
	// This happens when Anthropic changes the system prompt in a CC update.
	if mods == 0 {
		log.Printf("[GLASS-SYS] WARNING: sysprompt pipeline applied 0 modifications on cache miss — replacement targets may be stale")
		sp.mu.Lock()
		sp.stale = true
		sp.mu.Unlock()
	} else {
		log.Printf("[GLASS-SYS] Cache miss processed: %d modifications applied", mods)
		sp.mu.Lock()
		sp.stale = false
		sp.mu.Unlock()
	}

	// Cache the processed result (with placeholder UUID, without billing)
	sp.mu.Lock()
	sp.canonicalCache[cacheKey] = deepCopyFragments(processed)
	sp.mu.Unlock()

	// Persist immediately so tests and restarts see a coherent canonical cache.
	sp.saveToDisk()

	// Serve with placeholder UUID (not restored) — keeps prefix identical across sessions.
	result := deepCopyFragments(processed)
	if billing != nil {
		result = append([]interface{}{billing}, result...)
	}

	return result, mods
}

// normalizeUUID replaces session UUIDs with a placeholder for cache key stability.
func normalizeUUID(fragments []interface{}, uuid string) []interface{} {
	if uuid == "" {
		return fragments
	}
	result := make([]interface{}, len(fragments))
	for i, frag := range fragments {
		block, ok := frag.(map[string]interface{})
		if !ok {
			result[i] = frag
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			result[i] = frag
			continue
		}
		// Clone block
		newBlock := make(map[string]interface{})
		for k, v := range block {
			newBlock[k] = v
		}
		newBlock["text"] = strings.ReplaceAll(text, uuid, uuidPlaceholder)
		result[i] = newBlock
	}
	return result
}

// restoreUUID puts the real session UUID back into fragments.
func restoreUUID(fragments []interface{}, uuid string) []interface{} {
	if uuid == "" {
		return fragments
	}
	result := make([]interface{}, len(fragments))
	for i, frag := range fragments {
		block, ok := frag.(map[string]interface{})
		if !ok {
			result[i] = frag
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			result[i] = frag
			continue
		}
		newBlock := make(map[string]interface{})
		for k, v := range block {
			newBlock[k] = v
		}
		newBlock["text"] = strings.ReplaceAll(text, uuidPlaceholder, uuid)
		result[i] = newBlock
	}
	return result
}

// canonicalCacheKey produces a stable hash for normalized fragments.
func canonicalCacheKey(fragments []interface{}, pipelineFingerprint string) string {
	payload := map[string]interface{}{
		"pipeline":  pipelineFingerprint,
		"fragments": fragments,
	}
	data, _ := json.Marshal(payload)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:16])
}

func (sp *SyspromptProcessor) pipelineFingerprint() string {
	if sp == nil || sp.pipeline == nil {
		return ""
	}
	return sp.pipeline.Fingerprint()
}

// deepCopyFragments creates a deep copy via JSON round-trip.
func deepCopyFragments(fragments []interface{}) []interface{} {
	data, err := json.Marshal(fragments)
	if err != nil {
		return fragments
	}
	var copy []interface{}
	if json.Unmarshal(data, &copy) != nil {
		return fragments
	}
	return copy
}

// saveToDisk persists the canonical cache for cross-session stability.
func (sp *SyspromptProcessor) saveToDisk() {
	sp.mu.RLock()
	data, err := json.MarshalIndent(sp.canonicalCache, "", "  ")
	sp.mu.RUnlock()
	if err != nil {
		return
	}
	dir := filepath.Dir(sp.persistPath)
	os.MkdirAll(dir, 0755)
	if err := os.WriteFile(sp.persistPath, data, 0644); err != nil {
		log.Printf("[GLASS-SYS] Failed to persist canonical cache: %v", err)
	}
}

// factReplacementTargets are boilerplate system prompt sections that get replaced
// by operational facts. Strict allowlist — only Anthropic's generic instructions.
// Custom sections (Research disposition, Epistemological operating principles, etc.)
// are NEVER touched.
var factReplacementTargets = []string{
	"# Doing tasks",
	"# Using your tools",
	"# Tone and style",
}

// InjectFacts applies the canonical durable-fact overlay used for cache shaping.
// Target sections (# Doing tasks, # Using your tools, # Tone and style) are stripped
// from the normalized system text, reclaiming Anthropic boilerplate budget while
// preserving custom overlays. Stable facts are appended as a dedicated fragment so
// the canonical cache key tracks the exact upstream bytes Anthropic sees.
func (sp *SyspromptProcessor) InjectFacts(system []interface{}, factContent string) []interface{} {
	if factContent == "" {
		return system
	}
	stripped := stripBoilerplateSections(system)
	return appendFactFragment(stripped, factContent)
}

func appendFactFragment(system []interface{}, factContent string) []interface{} {
	if factContent == "" {
		return system
	}
	factFragment := map[string]interface{}{
		"type": "text",
		"text": factContent,
	}
	return append(system, factFragment)
}

// stripBoilerplateSections removes factReplacementTargets sections from system fragments.
// Each section spans from "\n# Heading\n" to the next "\n# " (any level-1 heading).
// If a target heading is not found, it is silently skipped (no data loss).
func stripBoilerplateSections(system []interface{}) []interface{} {
	result := make([]interface{}, 0, len(system))
	totalStripped := 0

	for _, frag := range system {
		block, ok := frag.(map[string]interface{})
		if !ok {
			result = append(result, frag)
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			result = append(result, frag)
			continue
		}

		modified := text
		for _, target := range factReplacementTargets {
			modified = cutSection(modified, target)
		}

		if modified != text {
			totalStripped += len(text) - len(modified)
			newBlock := make(map[string]interface{})
			for k, v := range block {
				newBlock[k] = v
			}
			newBlock["text"] = modified
			result = append(result, newBlock)
		} else {
			result = append(result, frag)
		}
	}

	if totalStripped > 0 {
		log.Printf("[GLASS-SYS] Stripped %d chars of boilerplate for fact injection", totalStripped)
	}
	return result
}

// cutSection removes a level-1 markdown section from text.
// Finds "\n# heading\n" and cuts everything up to the next "\n# ".
func cutSection(text, heading string) string {
	marker := "\n" + heading + "\n"
	idx := strings.Index(text, marker)
	if idx < 0 {
		// Also try at start of text (no leading \n)
		if strings.HasPrefix(text, heading+"\n") {
			idx = -1 // will be adjusted to 0 below
		} else {
			return text
		}
	}

	// Section starts at the \n before the heading (or 0)
	sectionStart := idx
	if sectionStart < 0 {
		sectionStart = 0
	}

	// Find end of heading line
	headingEnd := strings.Index(text[sectionStart+1:], "\n")
	if headingEnd < 0 {
		// Heading is the last line — cut to end
		return text[:sectionStart]
	}
	afterHeading := sectionStart + 1 + headingEnd + 1

	// Find next level-1 heading: "\n# "
	nextHeading := strings.Index(text[afterHeading:], "\n# ")
	if nextHeading < 0 {
		// No next heading — section runs to end of text
		return text[:sectionStart]
	}

	sectionEnd := afterHeading + nextHeading
	// Include the trailing \n in the cut so we don't leave double-newlines
	return text[:sectionStart] + text[sectionEnd:]
}

// loadFromDisk restores the canonical cache from a previous session.
func (sp *SyspromptProcessor) loadFromDisk() {
	data, err := os.ReadFile(sp.persistPath)
	if err != nil {
		return
	}
	var cache map[string][]interface{}
	if json.Unmarshal(data, &cache) != nil {
		return
	}
	sp.canonicalCache = cache
	log.Printf("[GLASS-SYS] Loaded canonical cache (%d entries) from disk", len(cache))
}
