package glass

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// PrefixProfile is the persisted cross-session definition of a reusable
// Anthropic prefix. It contains only shared prefix material, never messages.
type PrefixProfile struct {
	PrefixKey        string        `json:"prefix_key"`
	Model            string        `json:"model"`
	System           []interface{} `json:"system"`
	Tools            []interface{} `json:"tools,omitempty"`
	AnthropicVersion string        `json:"anthropic_version"`
	Betas            []string      `json:"betas,omitempty"`
	FirstSeenAt      time.Time     `json:"first_seen_at"`
	LastSeenAt       time.Time     `json:"last_seen_at"`
	LastWarmAt       time.Time     `json:"last_warm_at,omitempty"`
	LastWarmRead     int           `json:"last_warm_read,omitempty"`
	LastWarmNew      int           `json:"last_warm_new,omitempty"`
	LastStatusCode   int           `json:"last_status_code,omitempty"`
}

// PrefixRegistry stores reusable prefix profiles across sessions.
type PrefixRegistry struct {
	mu       sync.Mutex
	prefixes map[string]*PrefixProfile
	path     string
}

// NewPrefixRegistry loads the shared prefix registry from disk.
func NewPrefixRegistry(baseDir string) *PrefixRegistry {
	r := &PrefixRegistry{
		prefixes: make(map[string]*PrefixProfile),
		path:     filepath.Join(baseDir, "prefixes", "registry.json"),
	}
	r.load()
	return r
}

// Observe records a shared prefix profile, updating last-seen timestamps in memory.
// Newly seen prefixes are persisted immediately.
func (r *PrefixRegistry) Observe(profile PrefixProfile) PrefixProfile {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if existing, ok := r.prefixes[profile.PrefixKey]; ok {
		existing.LastSeenAt = now
		if existing.Model == "" {
			existing.Model = profile.Model
		}
		if existing.AnthropicVersion == "" {
			existing.AnthropicVersion = profile.AnthropicVersion
		}
		if len(profile.Betas) > 0 {
			existing.Betas = append([]string(nil), profile.Betas...)
		}
		if len(existing.System) == 0 && len(profile.System) > 0 {
			existing.System = jsonDeepCopy(profile.System)
		}
		if len(existing.Tools) == 0 && len(profile.Tools) > 0 {
			existing.Tools = jsonDeepCopy(profile.Tools)
		}
		return clonePrefixProfile(existing)
	}

	profile.FirstSeenAt = now
	profile.LastSeenAt = now
	profile.System = jsonDeepCopy(profile.System)
	profile.Tools = jsonDeepCopy(profile.Tools)
	profile.Betas = append([]string(nil), profile.Betas...)
	r.prefixes[profile.PrefixKey] = &profile
	r.saveLocked()
	log.Printf("[PREFIX] Registered shared prefix %s model=%s tools=%d", shortPrefix(profile.PrefixKey), profile.Model, len(profile.Tools))
	return clonePrefixProfile(&profile)
}

// Get returns a deep-copy snapshot of a prefix profile.
func (r *PrefixRegistry) Get(prefixKey string) (PrefixProfile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	profile, ok := r.prefixes[prefixKey]
	if !ok {
		return PrefixProfile{}, false
	}
	return clonePrefixProfile(profile), true
}

// MarkWarm updates the last warm status and persists it.
func (r *PrefixRegistry) MarkWarm(prefixKey string, statusCode, ccRead, ccNew int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	profile, ok := r.prefixes[prefixKey]
	if !ok {
		return
	}
	profile.LastWarmAt = time.Now()
	profile.LastStatusCode = statusCode
	profile.LastWarmRead = ccRead
	profile.LastWarmNew = ccNew
	r.saveLocked()
}

func (r *PrefixRegistry) load() {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}

	var raw map[string]*PrefixProfile
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Printf("[PREFIX] Failed to load prefix registry: %v", err)
		return
	}
	r.prefixes = raw
	log.Printf("[PREFIX] Loaded %d shared prefix profiles", len(r.prefixes))
}

func (r *PrefixRegistry) saveLocked() {
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("[PREFIX] Failed to create registry dir: %v", err)
		return
	}
	data, err := json.MarshalIndent(r.prefixes, "", "  ")
	if err != nil {
		log.Printf("[PREFIX] Failed to marshal prefix registry: %v", err)
		return
	}
	if err := os.WriteFile(r.path, data, 0644); err != nil {
		log.Printf("[PREFIX] Failed to persist prefix registry: %v", err)
	}
}

func clonePrefixProfile(profile *PrefixProfile) PrefixProfile {
	out := *profile
	out.System = jsonDeepCopy(profile.System)
	out.Tools = jsonDeepCopy(profile.Tools)
	out.Betas = append([]string(nil), profile.Betas...)
	return out
}

func shortPrefix(prefixKey string) string {
	if len(prefixKey) <= 12 {
		return prefixKey
	}
	return prefixKey[:12]
}

// PrefixFingerprint computes the shared cross-session prefix identity.
// The billing header is excluded because it is session-local and should not
// partition shared warming state.
func PrefixFingerprint(model string, system, tools []interface{}, anthropicVersion string, betas []string) string {
	if model == "" || len(system) == 0 {
		return ""
	}

	systemForHash := stripBillingHeader(system)
	if anthropicVersion == "" {
		anthropicVersion = "2023-06-01"
	}

	betas = normalizeBetas(betas)
	payload := map[string]interface{}{
		"model":             model,
		"system":            systemForHash,
		"tools":             tools,
		"anthropic_version": anthropicVersion,
		"betas":             betas,
	}
	data, _ := json.Marshal(payload)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:16])
}

func normalizeBetas(betas []string) []string {
	if len(betas) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(betas))
	out := make([]string, 0, len(betas))
	for _, beta := range betas {
		beta = strings.TrimSpace(beta)
		if beta == "" || seen[beta] {
			continue
		}
		seen[beta] = true
		out = append(out, beta)
	}
	sort.Strings(out)
	return out
}

func stripBillingHeader(system []interface{}) []interface{} {
	if len(system) == 0 {
		return nil
	}
	out := jsonDeepCopy(system)
	if len(out) < 2 {
		return out
	}
	first, ok := out[0].(map[string]interface{})
	if !ok {
		return out
	}
	text, _ := first["text"].(string)
	if strings.Contains(text, "billing") || strings.Contains(text, "x-anthropic") {
		return out[1:]
	}
	return out
}
