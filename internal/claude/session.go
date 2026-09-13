package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/replay"
	"proxy.local/app/internal/runtimepaths"
)

type laneReplayTemplate struct {
	ConvID         string                 `json:"conv_id"`
	Model          string                 `json:"model"`
	RequestURI     string                 `json:"request_uri"`
	ProxyTemplate  map[string]interface{} `json:"proxy_template"`
	HeaderTemplate LaneHeaderTemplate     `json:"header_template,omitempty"`
	UpdatedAt      string                 `json:"updated_at"`
}

type LaneHeaderTemplate struct {
	AnthropicVersion string   `json:"anthropic_version,omitempty"`
	Betas            []string `json:"betas,omitempty"`
}

type LaneSessionSnapshot struct {
	ConvID                 string `json:"conv_id"`
	Model                  string `json:"model"`
	MessageCount           int    `json:"message_count"`
	TotalTokens            int    `json:"total_tokens"`
	EvictedCount           int    `json:"evicted_count"`
	BatchCount             int    `json:"batch_count"`
	CompressionWatermark   int    `json:"compression_watermark"`
	LastAPIInput           int    `json:"last_api_input"`
	LastAPIInputAt         string `json:"last_api_input_at"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
	CachePath              string `json:"cache_path"`
	CachePersisted         bool   `json:"cache_persisted"`
	StatePath              string `json:"state_path"`
	StatePersisted         bool   `json:"state_persisted"`
	ShadowPath             string `json:"shadow_path"`
	ShadowPersisted        bool   `json:"shadow_persisted"`
	ReplayTemplatePath     string `json:"replay_template_path"`
	ReplayTemplateCaptured bool   `json:"replay_template_captured"`
}

type LaneSessionReplay struct {
	Lane                  string                 `json:"lane"`
	ConvID                string                 `json:"conv_id"`
	RequestURI            string                 `json:"request_uri"`
	ProxyTemplate         map[string]interface{} `json:"proxy_template"`
	HeaderTemplate        LaneHeaderTemplate     `json:"header_template,omitempty"`
	LiveMessages          []interface{}          `json:"live_messages"`
	Snapshot              LaneSessionSnapshot    `json:"snapshot"`
	CapturedAuthAvailable bool                   `json:"captured_auth_available"`
	CapturedAuthType      string                 `json:"captured_auth_type"`
}

func replayTemplatePath(baseDir, convID string) string {
	if strings.TrimSpace(baseDir) == "" || strings.TrimSpace(convID) == "" {
		return ""
	}
	return filepath.Join(baseDir, convID, "replay_template.json")
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func deepCopyMap(v map[string]interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func copyHeaderTemplate(in LaneHeaderTemplate) LaneHeaderTemplate {
	out := LaneHeaderTemplate{
		AnthropicVersion: strings.TrimSpace(in.AnthropicVersion),
	}
	if len(in.Betas) > 0 {
		out.Betas = append([]string(nil), in.Betas...)
	}
	return out
}

func replayHeaderTemplate(meta glass.RequestMeta) LaneHeaderTemplate {
	header := LaneHeaderTemplate{
		AnthropicVersion: strings.TrimSpace(meta.AnthropicVersion),
	}
	if header.AnthropicVersion == "" {
		header.AnthropicVersion = "2023-06-01"
	}
	if len(meta.Betas) > 0 {
		header.Betas = append([]string(nil), meta.Betas...)
	}
	return header
}

func loadReplayTemplate(baseDir, convID string) (laneReplayTemplate, bool) {
	path := replayTemplatePath(baseDir, convID)
	if path == "" {
		return laneReplayTemplate{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return laneReplayTemplate{}, false
	}
	var template laneReplayTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return laneReplayTemplate{}, false
	}
	if template.ConvID == "" {
		template.ConvID = convID
	}
	if template.RequestURI == "" || template.ProxyTemplate == nil {
		return laneReplayTemplate{}, false
	}
	return template, true
}

func writeReplayTemplate(baseDir string, replay laneReplayTemplate) bool {
	path := replayTemplatePath(baseDir, replay.ConvID)
	if path == "" {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false
	}
	data, err := json.MarshalIndent(replay, "", "  ")
	if err != nil {
		return false
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false
	}
	return os.Rename(tmp, path) == nil
}

func captureContextDir() string {
	return runtimepaths.Resolve(LaneID).LiveContextDir
}

func contextDumpPath(convID string) string {
	if strings.TrimSpace(convID) == "" {
		return ""
	}
	return filepath.Join(captureContextDir(), convID+".json")
}

func loadContextDumpMessages(convID string) ([]interface{}, time.Time, bool) {
	path := contextDumpPath(convID)
	if path == "" {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	var messages []interface{}
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, time.Time{}, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return messages, time.Time{}, true
	}
	return messages, info.ModTime(), true
}

func listContextDumpSessions() map[string]time.Time {
	dir := captureContextDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return map[string]time.Time{}
	}
	out := make(map[string]time.Time, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		convID := strings.TrimSuffix(entry.Name(), ".json")
		if strings.TrimSpace(convID) == "" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			out[convID] = time.Time{}
			continue
		}
		out[convID] = info.ModTime()
	}
	return out
}

func parseReplayTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return ts
}

func formatReplayTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func newerReplayTime(existing string, candidate time.Time) string {
	if candidate.IsZero() {
		return existing
	}
	current := parseReplayTime(existing)
	if current.IsZero() || candidate.After(current) {
		return formatReplayTime(candidate)
	}
	return existing
}

func parseSessionFamily(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	parts := strings.SplitN(sessionID, "_", 2)
	return parts[0]
}

func parseSessionIDFromFixturePath(path string) string {
	base := filepath.Base(path)
	parts := strings.Split(base, "_")
	if len(parts) < 4 {
		return ""
	}
	return parts[2] + "_" + parts[3]
}

func listLegacyReplayCaptureRoots(shadowDir string) []string {
	baseDir := filepath.Dir(strings.TrimSpace(shadowDir))
	if baseDir == "" {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(baseDir, "glass-replay-capture-*"))
	if err != nil || len(matches) == 0 {
		return nil
	}
	roots := make([]string, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			roots = append(roots, path)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		ii, errI := os.Stat(roots[i])
		jj, errJ := os.Stat(roots[j])
		if errI != nil || errJ != nil {
			return roots[i] < roots[j]
		}
		return ii.ModTime().After(jj.ModTime())
	})
	return roots
}

func loadLegacyFixture(path string) (replay.Fixture, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return replay.Fixture{}, false
	}
	var fixture replay.Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		return replay.Fixture{}, false
	}
	return fixture, true
}

func findLegacyFixturePath(shadowDir, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	pidSuffix := sessionID
	if idx := strings.LastIndex(sessionID, "_"); idx >= 0 && idx+1 < len(sessionID) {
		pidSuffix = sessionID[idx+1:]
	}
	sessionFamily := parseSessionFamily(sessionID)
	for _, root := range listLegacyReplayCaptureRoots(shadowDir) {
		exactMatches, _ := filepath.Glob(filepath.Join(root, "*_"+sessionID+"_*.json"))
		if len(exactMatches) > 0 {
			sort.Slice(exactMatches, func(i, j int) bool {
				ii, _ := os.Stat(exactMatches[i])
				jj, _ := os.Stat(exactMatches[j])
				if ii == nil || jj == nil {
					return exactMatches[i] > exactMatches[j]
				}
				return ii.ModTime().After(jj.ModTime())
			})
			return exactMatches[0]
		}
		if sessionFamily != "" {
			familyMatches, _ := filepath.Glob(filepath.Join(root, "*_"+sessionFamily+"_*.json"))
			filtered := make([]string, 0, len(familyMatches))
			marker := "_" + sessionID + "_"
			for _, path := range familyMatches {
				if !strings.Contains(path, marker) {
					filtered = append(filtered, path)
				}
			}
			if len(filtered) > 0 {
				sort.Slice(filtered, func(i, j int) bool {
					ii, _ := os.Stat(filtered[i])
					jj, _ := os.Stat(filtered[j])
					if ii == nil || jj == nil {
						return filtered[i] > filtered[j]
					}
					return ii.ModTime().After(jj.ModTime())
				})
				return filtered[0]
			}
		}
		pidMatches, _ := filepath.Glob(filepath.Join(root, "*_"+pidSuffix+"_*.json"))
		if len(pidMatches) > 0 {
			sort.Slice(pidMatches, func(i, j int) bool {
				ii, _ := os.Stat(pidMatches[i])
				jj, _ := os.Stat(pidMatches[j])
				if ii == nil || jj == nil {
					return pidMatches[i] > pidMatches[j]
				}
				return ii.ModTime().After(jj.ModTime())
			})
			return pidMatches[0]
		}
	}
	return ""
}

func fixtureSessionID(f replay.Fixture, fallbackPath string) string {
	candidates := []string{
		f.Glass.RequestKey,
		f.Meta.RequestKey,
		f.Glass.SessionKey,
		f.Meta.SessionKey,
		f.Glass.AffinityKey,
		f.Meta.AffinityKey,
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			return candidate
		}
	}
	return parseSessionIDFromFixturePath(fallbackPath)
}

func fixtureProxyTemplate(f replay.Fixture) map[string]interface{} {
	candidates := []json.RawMessage{f.BodyFinal, f.BodyPreGlass}
	for _, raw := range candidates {
		if len(raw) == 0 {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
			continue
		}
		delete(payload, "messages")
		return payload
	}
	return nil
}

func fixtureHeaderTemplate(f replay.Fixture) LaneHeaderTemplate {
	header := LaneHeaderTemplate{
		AnthropicVersion: strings.TrimSpace(f.Header.AnthropicVersion),
	}
	if header.AnthropicVersion == "" {
		header.AnthropicVersion = "2023-06-01"
	}
	if len(f.Header.Betas) > 0 {
		header.Betas = append([]string(nil), f.Header.Betas...)
	}
	return header
}

func replayTemplateFromFixture(convID string, f replay.Fixture) (laneReplayTemplate, bool) {
	template := fixtureProxyTemplate(f)
	if strings.TrimSpace(convID) == "" || template == nil {
		return laneReplayTemplate{}, false
	}
	model := strings.TrimSpace(stringValue(template["model"]))
	if model == "" && len(f.BodyFinal) > 0 {
		var bodyFinal map[string]interface{}
		if err := json.Unmarshal(f.BodyFinal, &bodyFinal); err == nil {
			model = strings.TrimSpace(stringValue(bodyFinal["model"]))
		}
	}
	updatedAt := strings.TrimSpace(f.Timestamp)
	if updatedAt == "" {
		updatedAt = formatReplayTime(time.Now())
	}
	requestURI := strings.TrimSpace(f.RequestURI)
	if requestURI == "" {
		requestURI = "/v1/messages"
	}
	return laneReplayTemplate{
		ConvID:         convID,
		Model:          model,
		RequestURI:     requestURI,
		ProxyTemplate:  template,
		HeaderTemplate: fixtureHeaderTemplate(f),
		UpdatedAt:      updatedAt,
	}, true
}

func syntheticSnapshot(convID, shadowDir string, replay laneReplayTemplate, replayOK bool, dumpTime time.Time) glass.DebugSessionSnapshot {
	shadowPath := filepath.Join(shadowDir, convID, "shadow.md")
	return glass.DebugSessionSnapshot{
		ConvID:          convID,
		CreatedAt:       newerReplayTime(replay.UpdatedAt, dumpTime),
		UpdatedAt:       newerReplayTime(replay.UpdatedAt, dumpTime),
		ShadowPath:      shadowPath,
		ShadowPersisted: pathExists(shadowPath),
	}
}

func (s *Services) migrateLegacyReplayTemplatesLocked() {
	if s == nil || strings.TrimSpace(s.shadowDir) == "" {
		return
	}
	roots := listLegacyReplayCaptureRoots(s.shadowDir)
	if len(roots) == 0 {
		return
	}
	sort.Slice(roots, func(i, j int) bool {
		ii, _ := os.Stat(roots[i])
		jj, _ := os.Stat(roots[j])
		if ii == nil || jj == nil {
			return roots[i] < roots[j]
		}
		return ii.ModTime().Before(jj.ModTime())
	})
	for _, root := range roots {
		fixtures, err := replay.LoadFixtures(root)
		if err != nil {
			continue
		}
		for _, fixture := range fixtures {
			convID := fixtureSessionID(fixture, "")
			if strings.TrimSpace(convID) == "" {
				continue
			}
			if _, ok := loadReplayTemplate(s.shadowDir, convID); ok {
				continue
			}
			template, ok := replayTemplateFromFixture(convID, fixture)
			if !ok {
				continue
			}
			_ = writeReplayTemplate(s.shadowDir, template)
		}
	}
}

func (s *Services) ensureLegacyReplayTemplates() {
	if s == nil {
		return
	}
	s.migrateOnce.Do(func() {
		s.migrateLegacyReplayTemplatesLocked()
	})
}

func (s *Services) ensureReplayTemplate(convID string) (laneReplayTemplate, bool) {
	if s == nil || strings.TrimSpace(convID) == "" {
		return laneReplayTemplate{}, false
	}
	s.ensureLegacyReplayTemplates()
	template, ok := loadReplayTemplate(s.shadowDir, convID)
	if ok && (template.HeaderTemplate.AnthropicVersion != "" || len(template.HeaderTemplate.Betas) > 0) {
		return template, true
	}
	fixturePath := findLegacyFixturePath(s.shadowDir, convID)
	if fixturePath == "" {
		return template, ok
	}
	fixture, fixtureOK := loadLegacyFixture(fixturePath)
	if !fixtureOK {
		return template, ok
	}
	upgraded, upgradedOK := replayTemplateFromFixture(convID, fixture)
	if !upgradedOK {
		return template, ok
	}
	if ok {
		if strings.TrimSpace(template.Model) != "" {
			upgraded.Model = template.Model
		}
		if strings.TrimSpace(template.RequestURI) != "" {
			upgraded.RequestURI = template.RequestURI
		}
		if template.ProxyTemplate != nil {
			upgraded.ProxyTemplate = deepCopyMap(template.ProxyTemplate)
		}
		if strings.TrimSpace(template.UpdatedAt) != "" {
			upgraded.UpdatedAt = template.UpdatedAt
		}
	}
	if writeReplayTemplate(s.shadowDir, upgraded) {
		return upgraded, true
	}
	return template, ok
}

func (s *Services) CaptureReplayTemplate(convID, requestURI string, body map[string]interface{}, meta glass.RequestMeta) {
	if s == nil || strings.TrimSpace(convID) == "" || strings.TrimSpace(requestURI) == "" || body == nil {
		return
	}

	template := deepCopyMap(body)
	if template == nil {
		return
	}
	delete(template, "messages")

	replay := laneReplayTemplate{
		ConvID:         convID,
		Model:          strings.TrimSpace(stringValue(template["model"])),
		RequestURI:     requestURI,
		ProxyTemplate:  template,
		HeaderTemplate: replayHeaderTemplate(meta),
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
	_ = writeReplayTemplate(s.shadowDir, replay)
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func enrichSnapshot(snapshot glass.DebugSessionSnapshot, replay laneReplayTemplate, replayOK bool, shadowDir string) LaneSessionSnapshot {
	out := LaneSessionSnapshot{
		ConvID:               snapshot.ConvID,
		Model:                "",
		MessageCount:         snapshot.MessageCount,
		TotalTokens:          snapshot.TotalTokens,
		EvictedCount:         snapshot.EvictedCount,
		BatchCount:           snapshot.BatchCount,
		CompressionWatermark: snapshot.CompressionWatermark,
		LastAPIInput:         snapshot.LastAPIInput,
		LastAPIInputAt:       snapshot.LastAPIInputAt,
		CreatedAt:            snapshot.CreatedAt,
		UpdatedAt:            snapshot.UpdatedAt,
		CachePath:            snapshot.CachePath,
		CachePersisted:       snapshot.CachePersisted,
		StatePath:            snapshot.StatePath,
		StatePersisted:       snapshot.StatePersisted,
		ShadowPath:           snapshot.ShadowPath,
		ShadowPersisted:      snapshot.ShadowPersisted,
		ReplayTemplatePath:   replayTemplatePath(shadowDir, snapshot.ConvID),
	}
	if replayOK {
		out.Model = replay.Model
		out.UpdatedAt = newerReplayTime(out.UpdatedAt, parseReplayTime(replay.UpdatedAt))
		if strings.TrimSpace(out.CreatedAt) == "" {
			out.CreatedAt = out.UpdatedAt
		}
	}
	if messages, dumpTime, ok := loadContextDumpMessages(snapshot.ConvID); ok && len(messages) > 0 {
		if out.MessageCount == 0 {
			out.MessageCount = len(messages)
		}
		out.UpdatedAt = newerReplayTime(out.UpdatedAt, dumpTime)
		if strings.TrimSpace(out.CreatedAt) == "" {
			out.CreatedAt = formatReplayTime(dumpTime)
		}
		if strings.TrimSpace(out.LastAPIInputAt) == "" {
			out.LastAPIInputAt = formatReplayTime(dumpTime)
		}
	}
	out.ReplayTemplateCaptured = replayOK && pathExists(out.ReplayTemplatePath)
	return out
}

func summarizeSnapshot(snapshot glass.DebugSessionSnapshot, replay laneReplayTemplate, replayOK bool, shadowDir string, dumpTime time.Time, hasDump bool) LaneSessionSnapshot {
	out := LaneSessionSnapshot{
		ConvID:               snapshot.ConvID,
		Model:                "",
		MessageCount:         snapshot.MessageCount,
		TotalTokens:          snapshot.TotalTokens,
		EvictedCount:         snapshot.EvictedCount,
		BatchCount:           snapshot.BatchCount,
		CompressionWatermark: snapshot.CompressionWatermark,
		LastAPIInput:         snapshot.LastAPIInput,
		LastAPIInputAt:       snapshot.LastAPIInputAt,
		CreatedAt:            snapshot.CreatedAt,
		UpdatedAt:            snapshot.UpdatedAt,
		CachePath:            snapshot.CachePath,
		CachePersisted:       snapshot.CachePersisted,
		StatePath:            snapshot.StatePath,
		StatePersisted:       snapshot.StatePersisted,
		ShadowPath:           snapshot.ShadowPath,
		ShadowPersisted:      snapshot.ShadowPersisted,
		ReplayTemplatePath:   replayTemplatePath(shadowDir, snapshot.ConvID),
	}
	if replayOK {
		out.Model = replay.Model
		out.UpdatedAt = newerReplayTime(out.UpdatedAt, parseReplayTime(replay.UpdatedAt))
		if strings.TrimSpace(out.CreatedAt) == "" {
			out.CreatedAt = out.UpdatedAt
		}
	}
	if hasDump {
		if out.MessageCount == 0 {
			// Session-list callers only need to know whether local context exists.
			out.MessageCount = 1
		}
		out.UpdatedAt = newerReplayTime(out.UpdatedAt, dumpTime)
		if strings.TrimSpace(out.CreatedAt) == "" {
			out.CreatedAt = formatReplayTime(dumpTime)
		}
		if strings.TrimSpace(out.LastAPIInputAt) == "" {
			out.LastAPIInputAt = formatReplayTime(dumpTime)
		}
	}
	out.ReplayTemplateCaptured = replayOK && pathExists(out.ReplayTemplatePath)
	return out
}

func (s *Services) LaneSessions() []LaneSessionSnapshot {
	if s == nil || s.engine == nil {
		return nil
	}
	s.ensureLegacyReplayTemplates()
	base := s.engine.DebugSessions()
	baseByID := make(map[string]glass.DebugSessionSnapshot, len(base))
	for _, snapshot := range base {
		baseByID[snapshot.ConvID] = snapshot
	}
	dumpSessions := listContextDumpSessions()
	for convID, dumpTime := range dumpSessions {
		if _, ok := baseByID[convID]; ok {
			continue
		}
		replay, replayOK := loadReplayTemplate(s.shadowDir, convID)
		if !replayOK {
			continue
		}
		baseByID[convID] = syntheticSnapshot(convID, s.shadowDir, replay, replayOK, dumpTime)
	}
	if len(baseByID) == 0 {
		return nil
	}
	out := make([]LaneSessionSnapshot, 0, len(baseByID))
	for _, snapshot := range baseByID {
		replay, ok := loadReplayTemplate(s.shadowDir, snapshot.ConvID)
		dumpTime, hasDump := dumpSessions[snapshot.ConvID]
		out = append(out, summarizeSnapshot(snapshot, replay, ok, s.shadowDir, dumpTime, hasDump))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

func (s *Services) LaneSession(convID string) (LaneSessionSnapshot, bool) {
	if s == nil || s.engine == nil {
		return LaneSessionSnapshot{}, false
	}
	snapshot, ok := s.engine.DebugSession(convID)
	if !ok {
		replay, replayOK := s.ensureReplayTemplate(convID)
		messages, dumpTime, dumpOK := loadContextDumpMessages(convID)
		if !replayOK && !dumpOK {
			return LaneSessionSnapshot{}, false
		}
		if !dumpOK {
			messages = nil
		}
		if len(messages) == 0 {
			dumpTime = parseReplayTime(replay.UpdatedAt)
		}
		snapshot = syntheticSnapshot(convID, s.shadowDir, replay, replayOK, dumpTime)
	}
	replay, replayOK := s.ensureReplayTemplate(convID)
	return enrichSnapshot(snapshot, replay, replayOK, s.shadowDir), true
}

func (s *Services) LaneSessionReplay(convID string) (LaneSessionReplay, bool) {
	if s == nil || s.engine == nil {
		return LaneSessionReplay{}, false
	}
	snapshot, ok := s.LaneSession(convID)
	if !ok {
		return LaneSessionReplay{}, false
	}
	replay, ok := s.ensureReplayTemplate(convID)
	if !ok {
		return LaneSessionReplay{}, false
	}
	liveMessages, liveOK := s.engine.DebugSessionMessages(convID)
	if !liveOK {
		liveMessages, _, liveOK = loadContextDumpMessages(convID)
	}
	if !liveOK {
		liveMessages = nil
	}
	captured, capturedType := false, "unavailable"
	if s.summarizer != nil {
		auth := s.summarizer.AuthStatus()
		captured = auth.CapturedAuthAvailable
		capturedType = auth.AuthType
		if capturedType == "" {
			capturedType = "unavailable"
		}
	}
	return LaneSessionReplay{
		Lane:                  LaneID,
		ConvID:                convID,
		RequestURI:            replay.RequestURI,
		ProxyTemplate:         deepCopyMap(replay.ProxyTemplate),
		HeaderTemplate:        copyHeaderTemplate(replay.HeaderTemplate),
		LiveMessages:          liveMessages,
		Snapshot:              snapshot,
		CapturedAuthAvailable: captured,
		CapturedAuthType:      capturedType,
	}, true
}

func (s *Services) sessionCount() int {
	if s == nil || s.engine == nil {
		return 0
	}
	keys := make(map[string]struct{})
	for _, convID := range s.engine.DebugSessionIDs() {
		if strings.TrimSpace(convID) != "" {
			keys[convID] = struct{}{}
		}
	}
	for convID := range listContextDumpSessions() {
		if strings.TrimSpace(convID) != "" {
			if _, ok := loadReplayTemplate(s.shadowDir, convID); !ok {
				continue
			}
			keys[convID] = struct{}{}
		}
	}
	return len(keys)
}
