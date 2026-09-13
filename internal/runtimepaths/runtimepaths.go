package runtimepaths

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	RuntimeLaneEnv = "GLASS_RUNTIME_LANE"
	RuntimeRootEnv = "GLASS_RUNTIME_ROOT"
	CaptureRootEnv = "GLASS_CAPTURE_ROOT"
)

type Paths struct {
	Lane                    string
	RootDir                 string
	ConfigPath              string
	DebugDBPath             string
	StatuslineSnapshotPath  string
	UsageBridgePath         string
	QuotaSamplesPath        string
	LegacyQuotaSamplesPath  string
	InterleavingHealthPath  string
	BurnTelemetryHealthPath string
	GlassShadowDir          string
	ContextPatchesPath      string
	LiveCaptureDir          string
	LiveContextDir          string
}

func Current() Paths {
	return Resolve(os.Getenv(RuntimeLaneEnv))
}

func Resolve(lane string) Paths {
	lane = normalizeLane(lane)
	root := runtimeRoot(lane)
	liveCaptureDir := liveCaptureDirForLane(lane, root)
	return Paths{
		Lane:                    lane,
		RootDir:                 root,
		ConfigPath:              filepath.Join(root, "glass_config.json"),
		DebugDBPath:             filepath.Join(root, "glass_debug.db"),
		StatuslineSnapshotPath:  filepath.Join(root, "statusline_snapshot.json"),
		UsageBridgePath:         filepath.Join(root, "api_usage_shared.json"),
		QuotaSamplesPath:        filepath.Join(root, "glass_quota_samples.json"),
		LegacyQuotaSamplesPath:  filepath.Join(root, "quota_samples.json"),
		InterleavingHealthPath:  filepath.Join(root, "interleaving_health.json"),
		BurnTelemetryHealthPath: filepath.Join(root, "burn_telemetry_health.json"),
		GlassShadowDir:          filepath.Join(root, "glass"),
		ContextPatchesPath:      filepath.Join(root, "context_patches.json"),
		LiveCaptureDir:          liveCaptureDir,
		LiveContextDir:          filepath.Join(liveCaptureDir, "context"),
	}
}

func CodexHomeDir() string {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return expandUserPath(home)
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".codex")
	}
	return filepath.Join(home, ".codex")
}

func CodexStorageRoot() string {
	if normalizeLane(os.Getenv(RuntimeLaneEnv)) == "codex" {
		if root := strings.TrimSpace(os.Getenv(RuntimeRootEnv)); root != "" {
			return expandUserPath(root)
		}
	}
	return filepath.Join(CodexHomeDir(), "glass-proxy")
}

func normalizeLane(lane string) string {
	switch strings.ToLower(strings.TrimSpace(lane)) {
	case "anthropic", "claude":
		return "claude"
	case "codex":
		return "codex"
	case "gemini":
		return "gemini"
	case "openai":
		return "openai"
	case "ollama":
		return "ollama"
	default:
		return "claude"
	}
}

func runtimeRoot(lane string) string {
	if root := strings.TrimSpace(os.Getenv(RuntimeRootEnv)); root != "" {
		return expandUserPath(root)
	}
	if lane == "codex" {
		return CodexStorageRoot()
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		if lane == "claude" {
			return filepath.Join(".claude")
		}
		return filepath.Join(".glass", lane)
	}
	if lane == "claude" {
		return filepath.Join(home, ".claude")
	}
	return filepath.Join(home, ".glass", lane)
}

func liveCaptureDirForLane(lane, root string) string {
	if captureRoot := strings.TrimSpace(os.Getenv(CaptureRootEnv)); captureRoot != "" {
		return expandUserPath(captureRoot)
	}
	if lane == "claude" {
		return filepath.Join(os.TempDir(), "glass-proxy")
	}
	if lane == "codex" || lane == "gemini" || lane == "openai" || lane == "ollama" {
		return filepath.Join(root, "live")
	}
	return filepath.Join(os.TempDir(), "glass-proxy")
}

func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
