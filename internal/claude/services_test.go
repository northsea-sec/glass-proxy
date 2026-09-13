package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"proxy.local/app/internal/glass"
	"proxy.local/app/internal/replay"
	"proxy.local/app/internal/runtimepaths"
)

func buildTestConversation(engine *glass.Engine, convID string) {
	body := map[string]interface{}{
		"model": "claude-opus-4-1",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": strings.Repeat("Stay on task and preserve the full operator context. ", 180)},
		},
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "hello"},
				},
			},
		},
	}
	engine.Process(body, glass.RequestMeta{
		SessionKey:  convID,
		RequestKey:  convID,
		AffinityKey: convID,
	})
	engine.UpdateAPITokens(convID, 77)
}

func TestServicesAuthStatusDefaultsWhenDisabled(t *testing.T) {
	svc := NewServices(ServicesConfig{
		Enabled:   false,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: "/tmp/claude-shadow",
	})

	status := svc.AuthStatus()
	if status.Lane != LaneID || status.Transport != TransportID {
		t.Fatalf("unexpected lane identity: %+v", status)
	}
	if status.Upstream != "https://api.anthropic.com" || !status.UpstreamConfigured {
		t.Fatalf("unexpected upstream status: %+v", status)
	}
	if status.PrefixWarmerEnabled || status.RollingSummarizerEnabled {
		t.Fatalf("disabled services should not report background jobs: %+v", status)
	}
	if status.CapturedAuthAvailable || status.Captured {
		t.Fatalf("disabled services should not report captured auth: %+v", status)
	}
	if status.CapturedAuthType != "unavailable" {
		t.Fatalf("expected unavailable captured auth type, got %+v", status)
	}
}

func TestServicesAttachAndAuthStatusMirrorSummarizer(t *testing.T) {
	tempDir := t.TempDir()
	svc := NewServices(ServicesConfig{
		Enabled:   true,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: tempDir,
		RollingSummarizer: glass.SummarizerConfig{
			Enabled:       true,
			IntervalToken: 50000,
			Model:         "claude-opus-4-6",
			GateEnabled:   true,
		},
	})
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = tempDir
	engine := glass.NewEngine(glassCfg)
	svc.Attach(engine)
	if engine.Summarizer() == nil {
		t.Fatal("expected attached rolling summarizer")
	}

	engine.Summarizer().CaptureAuth(glass.RequestMeta{
		APIKey:           "token-123",
		AuthIsBearer:     true,
		AnthropicVersion: "2023-06-01",
		Betas:            []string{"beta-a", "beta-b"},
	})

	status := svc.AuthStatus()
	if !status.RollingSummarizerEnabled {
		t.Fatalf("expected rolling summarizer enabled: %+v", status)
	}
	if !status.Captured || !status.CapturedAuthAvailable {
		t.Fatalf("expected captured auth to be reflected: %+v", status)
	}
	if status.AuthType != "bearer" || status.CapturedAuthType != "bearer" {
		t.Fatalf("expected bearer auth status, got %+v", status)
	}
	if status.AnthropicVersion != "2023-06-01" {
		t.Fatalf("unexpected anthropic version: %+v", status)
	}
	if len(status.Betas) != 2 {
		t.Fatalf("expected betas to be preserved: %+v", status)
	}
}

func TestServicesLaneSessionReplayUsesGlassSnapshots(t *testing.T) {
	tempDir := t.TempDir()
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = tempDir
	engine := glass.NewEngine(glassCfg)
	buildTestConversation(engine, "conv-replay")

	svc := NewServices(ServicesConfig{
		Enabled:   true,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: tempDir,
		RollingSummarizer: glass.SummarizerConfig{
			Enabled:       true,
			IntervalToken: 50000,
			Model:         "claude-opus-4-1",
		},
	})
	svc.Attach(engine)
	engine.Summarizer().CaptureAuth(glass.RequestMeta{
		APIKey:           "oauth-token",
		AuthIsBearer:     true,
		AnthropicVersion: "2023-06-01",
	})
	svc.CaptureReplayTemplate("conv-replay", "/v1/messages", map[string]interface{}{
		"model": "claude-opus-4-1",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "Stay on task."},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hello"}}},
		},
		"stream": true,
	}, glass.RequestMeta{
		AnthropicVersion: "2023-06-01",
		Betas:            []string{"beta-a", "beta-b"},
	})

	status := svc.AuthStatus()
	if status.SessionCount != 1 {
		t.Fatalf("expected 1 session, got %+v", status)
	}

	snapshot, ok := svc.LaneSession("conv-replay")
	if !ok {
		t.Fatal("expected lane snapshot")
	}
	if snapshot.Model != "claude-opus-4-1" {
		t.Fatalf("expected replay-backed model, got %+v", snapshot)
	}
	if !snapshot.ReplayTemplateCaptured {
		t.Fatalf("expected replay template to be marked captured, got %+v", snapshot)
	}
	if snapshot.MessageCount != 1 || snapshot.LastAPIInput != 77 {
		t.Fatalf("unexpected snapshot %+v", snapshot)
	}

	replay, ok := svc.LaneSessionReplay("conv-replay")
	if !ok {
		t.Fatal("expected lane replay")
	}
	if replay.RequestURI != "/v1/messages" {
		t.Fatalf("unexpected replay URI %+v", replay)
	}
	if _, ok := replay.ProxyTemplate["messages"]; ok {
		t.Fatalf("replay template should not persist live messages: %+v", replay.ProxyTemplate)
	}
	if replay.ProxyTemplate["model"] != "claude-opus-4-1" {
		t.Fatalf("expected replay model, got %+v", replay.ProxyTemplate)
	}
	if replay.HeaderTemplate.AnthropicVersion != "2023-06-01" {
		t.Fatalf("expected replay header version, got %+v", replay.HeaderTemplate)
	}
	if len(replay.HeaderTemplate.Betas) != 2 || replay.HeaderTemplate.Betas[0] != "beta-a" || replay.HeaderTemplate.Betas[1] != "beta-b" {
		t.Fatalf("expected replay header betas, got %+v", replay.HeaderTemplate)
	}
	if len(replay.LiveMessages) != 1 {
		t.Fatalf("expected 1 live message, got %+v", replay)
	}
	if !replay.CapturedAuthAvailable || replay.CapturedAuthType != "bearer" {
		t.Fatalf("expected captured bearer auth, got %+v", replay)
	}
}

func TestServicesLaneSessionReplayLoadsPersistedTemplateAfterRestart(t *testing.T) {
	tempDir := t.TempDir()
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = tempDir
	engine := glass.NewEngine(glassCfg)
	buildTestConversation(engine, "conv-restart")

	svc := NewServices(ServicesConfig{
		Enabled:   false,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: tempDir,
	})
	svc.Attach(engine)
	svc.CaptureReplayTemplate("conv-restart", "/v1/messages", map[string]interface{}{
		"model": "claude-sonnet-4-5",
		"system": []interface{}{
			map[string]interface{}{"type": "text", "text": "Restart safe."},
		},
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": []interface{}{map[string]interface{}{"type": "text", "text": "hello"}}},
		},
	}, glass.RequestMeta{
		AnthropicVersion: "2023-06-01",
		Betas:            []string{"beta-restart"},
	})

	restartedEngine := glass.NewEngine(glassCfg)
	restartedSvc := NewServices(ServicesConfig{
		Enabled:   false,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: tempDir,
	})
	restartedSvc.Attach(restartedEngine)

	replay, ok := restartedSvc.LaneSessionReplay("conv-restart")
	if !ok {
		t.Fatal("expected replay after restart")
	}
	if replay.RequestURI != "/v1/messages" {
		t.Fatalf("unexpected replay URI %+v", replay)
	}
	if replay.Snapshot.Model != "claude-sonnet-4-5" {
		t.Fatalf("expected persisted template model, got %+v", replay.Snapshot)
	}
	if replay.HeaderTemplate.AnthropicVersion != "2023-06-01" {
		t.Fatalf("expected persisted replay header version, got %+v", replay.HeaderTemplate)
	}
	if len(replay.HeaderTemplate.Betas) != 1 || replay.HeaderTemplate.Betas[0] != "beta-restart" {
		t.Fatalf("expected persisted replay betas, got %+v", replay.HeaderTemplate)
	}
	if replay.CapturedAuthAvailable || replay.CapturedAuthType != "unavailable" {
		t.Fatalf("expected no captured auth after restart without summarizer, got %+v", replay)
	}
}

func TestServicesLaneSessionReplayMigratesLegacyFixtureAndContextDump(t *testing.T) {
	tempDir := t.TempDir()
	shadowDir := filepath.Join(tempDir, "glass")
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = shadowDir
	engine := glass.NewEngine(glassCfg)

	captureRoot := filepath.Join(tempDir, "live")
	t.Setenv(runtimepaths.CaptureRootEnv, captureRoot)

	contextDir := filepath.Join(captureRoot, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("mkdir context dir: %v", err)
	}
	contextMessages := []interface{}{
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "legacy hello"},
			},
		},
	}
	data, err := json.Marshal(contextMessages)
	if err != nil {
		t.Fatalf("marshal context messages: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "conv-legacy.json"), data, 0o644); err != nil {
		t.Fatalf("write context dump: %v", err)
	}

	fixtureDir := filepath.Join(tempDir, "glass-replay-capture-legacy")
	if err := replay.WriteFixture(fixtureDir, replay.Fixture{
		Version:    replay.FixtureVersion,
		CaptureID:  "legacy-cap",
		Timestamp:  "2026-04-20T10:00:00Z",
		RequestURI: "/v1/messages",
		Header: replay.HeaderSnapshot{
			AnthropicVersion: "2024-10-22",
			Betas:            []string{"beta-a", "beta-b"},
		},
		Meta: replay.MetaSnapshot{
			RequestKey: "conv-legacy",
			SessionKey: "conv-legacy",
		},
		Glass: glass.ProcessResult{
			RequestKey: "conv-legacy",
			SessionKey: "conv-legacy",
		},
		BodyFinal: json.RawMessage(`{
			"model":"claude-opus-4-1",
			"system":[{"type":"text","text":"legacy migrated system"}],
			"messages":[{"role":"user","content":[{"type":"text","text":"stale"}]}],
			"max_tokens":1024
		}`),
	}); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	svc := NewServices(ServicesConfig{
		Enabled:   false,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: shadowDir,
	})
	svc.Attach(engine)

	snapshot, ok := svc.LaneSession("conv-legacy")
	if !ok {
		t.Fatal("expected migrated lane snapshot")
	}
	if snapshot.Model != "claude-opus-4-1" {
		t.Fatalf("expected migrated replay model, got %+v", snapshot)
	}
	if snapshot.MessageCount != 1 {
		t.Fatalf("expected message count from context dump, got %+v", snapshot)
	}
	if !snapshot.ReplayTemplateCaptured {
		t.Fatalf("expected migrated replay template to be persisted, got %+v", snapshot)
	}

	replayed, ok := svc.LaneSessionReplay("conv-legacy")
	if !ok {
		t.Fatal("expected migrated lane replay")
	}
	if replayed.HeaderTemplate.AnthropicVersion != "2024-10-22" {
		t.Fatalf("expected migrated replay headers, got %+v", replayed.HeaderTemplate)
	}
	if len(replayed.HeaderTemplate.Betas) != 2 {
		t.Fatalf("expected migrated replay betas, got %+v", replayed.HeaderTemplate)
	}
	if len(replayed.LiveMessages) != 1 {
		t.Fatalf("expected live messages from context dump, got %+v", replayed)
	}
}

func TestServicesLaneSessionReplayUpgradesOlderReplayTemplateHeadersFromFixture(t *testing.T) {
	tempDir := t.TempDir()
	shadowDir := filepath.Join(tempDir, "glass")
	glassCfg := glass.DefaultGlassConfig()
	glassCfg.ShadowDir = shadowDir
	engine := glass.NewEngine(glassCfg)

	oldReplay := laneReplayTemplate{
		ConvID:        "conv-upgrade",
		Model:         "claude-old",
		RequestURI:    "/v1/messages",
		ProxyTemplate: map[string]interface{}{"model": "claude-old"},
		UpdatedAt:     "2026-04-20T09:00:00Z",
	}
	if !writeReplayTemplate(shadowDir, oldReplay) {
		t.Fatal("expected old replay template write to succeed")
	}

	fixtureDir := filepath.Join(tempDir, "glass-replay-capture-upgrade")
	if err := replay.WriteFixture(fixtureDir, replay.Fixture{
		Version:    replay.FixtureVersion,
		CaptureID:  "upgrade-cap",
		Timestamp:  "2026-04-20T10:00:00Z",
		RequestURI: "/v1/messages",
		Header: replay.HeaderSnapshot{
			AnthropicVersion: "2024-10-22",
			Betas:            []string{"beta-upgrade"},
		},
		Meta: replay.MetaSnapshot{
			RequestKey: "conv-upgrade",
			SessionKey: "conv-upgrade",
		},
		Glass: glass.ProcessResult{
			RequestKey: "conv-upgrade",
			SessionKey: "conv-upgrade",
		},
		BodyFinal: json.RawMessage(`{"model":"claude-new","messages":[]}`),
	}); err != nil {
		t.Fatalf("write upgrade fixture: %v", err)
	}

	svc := NewServices(ServicesConfig{
		Enabled:   false,
		Upstream:  "https://api.anthropic.com",
		ShadowDir: shadowDir,
	})
	svc.Attach(engine)

	replayed, ok := svc.LaneSessionReplay("conv-upgrade")
	if !ok {
		t.Fatal("expected upgraded lane replay")
	}
	if replayed.HeaderTemplate.AnthropicVersion != "2024-10-22" {
		t.Fatalf("expected upgraded header version, got %+v", replayed.HeaderTemplate)
	}
	if len(replayed.HeaderTemplate.Betas) != 1 || replayed.HeaderTemplate.Betas[0] != "beta-upgrade" {
		t.Fatalf("expected upgraded header betas, got %+v", replayed.HeaderTemplate)
	}
	if replayed.ProxyTemplate["model"] != "claude-old" {
		t.Fatalf("expected existing proxy template to be preserved, got %+v", replayed.ProxyTemplate)
	}
}
