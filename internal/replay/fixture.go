package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/subagent"
)

const FixtureVersion = 1

// HeaderSnapshot stores the non-secret request headers that affect cache behavior.
type HeaderSnapshot struct {
	AnthropicVersion string   `json:"anthropic_version,omitempty"`
	Betas            []string `json:"betas,omitempty"`
	Stream           bool     `json:"stream"`
	HasAPIKey        bool     `json:"has_api_key"`
}

// MetaSnapshot stores the request identity and classification used during capture.
type MetaSnapshot struct {
	ClientPID         int                     `json:"client_pid,omitempty"`
	RequestSessionKey string                  `json:"request_session_key,omitempty"`
	SessionKey        string                  `json:"session_key,omitempty"`
	RequestKey        string                  `json:"request_key,omitempty"`
	AffinityKey       string                  `json:"affinity_key,omitempty"`
	Subagent          subagent.Classification `json:"subagent"`
}

// Fixture stores a single request at the stages needed for offline replay.
type Fixture struct {
	Version      int                 `json:"version"`
	CaptureID    string              `json:"capture_id"`
	Timestamp    string              `json:"timestamp"`
	RequestURI   string              `json:"request_uri,omitempty"`
	Header       HeaderSnapshot      `json:"header"`
	Meta         MetaSnapshot        `json:"meta"`
	Glass        glass.ProcessResult `json:"glass"`
	BodyRaw      json.RawMessage     `json:"body_raw,omitempty"`
	BodyPreGlass json.RawMessage     `json:"body_pre_glass,omitempty"`
	BodyFinal    json.RawMessage     `json:"body_final,omitempty"`
}

// EnsureTimestamp fills Timestamp when omitted.
func (f *Fixture) EnsureTimestamp() {
	if strings.TrimSpace(f.Timestamp) == "" {
		f.Timestamp = time.Now().Format(time.RFC3339Nano)
	}
}

// FileName returns a stable per-fixture file name.
func (f Fixture) FileName() string {
	ts := sanitizeName(strings.ReplaceAll(strings.ReplaceAll(f.Timestamp, ":", "-"), "+", "_"))
	key := f.Meta.RequestKey
	if key == "" {
		key = f.Meta.SessionKey
	}
	if key == "" {
		key = "request"
	}
	key = sanitizeName(key)
	id := sanitizeName(f.CaptureID)
	if id == "" {
		id = "capture"
	}
	return fmt.Sprintf("%s_%s_%s.json", ts, key, id)
}

// WriteFixture writes a fixture atomically to dir.
func WriteFixture(dir string, f Fixture) error {
	if dir == "" {
		return fmt.Errorf("empty fixture dir")
	}
	f.EnsureTimestamp()
	if f.Version == 0 {
		f.Version = FixtureVersion
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, f.FileName())
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, finalPath)
}

// LoadFixtures reads and sorts captured fixtures from dir.
func LoadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if strings.Contains(entry.Name(), "_ws_") || strings.Contains(entry.Name(), "_http_") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var f Fixture
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		fixtures = append(fixtures, f)
	}
	sort.Slice(fixtures, func(i, j int) bool {
		ti := parseFixtureTime(fixtures[i].Timestamp)
		tj := parseFixtureTime(fixtures[j].Timestamp)
		if ti.Equal(tj) {
			return fixtures[i].CaptureID < fixtures[j].CaptureID
		}
		return ti.Before(tj)
	})
	return fixtures, nil
}

func parseFixtureTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t
	}
	return time.Time{}
}

func sanitizeName(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}
