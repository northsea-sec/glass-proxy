package runtimepaths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePreservesLegacyClaudeAndCodexDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(RuntimeRootEnv, "")
	t.Setenv(CaptureRootEnv, "")

	claude := Resolve("anthropic")
	if claude.Lane != "claude" {
		t.Fatalf("claude lane = %q, want claude", claude.Lane)
	}
	if claude.RootDir != filepath.Join(home, ".claude") {
		t.Fatalf("claude root = %q", claude.RootDir)
	}
	if claude.LiveCaptureDir != filepath.Join(os.TempDir(), "glass-proxy") {
		t.Fatalf("claude live capture = %q", claude.LiveCaptureDir)
	}

	codex := Resolve("codex")
	if codex.RootDir != filepath.Join(home, ".codex", "glass-proxy") {
		t.Fatalf("codex root = %q", codex.RootDir)
	}
	if codex.LiveCaptureDir != filepath.Join(codex.RootDir, "live") {
		t.Fatalf("codex live capture = %q", codex.LiveCaptureDir)
	}
}

func TestResolveSupportsExplicitNonClaudeLanes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(RuntimeRootEnv, "")
	t.Setenv(CaptureRootEnv, "")

	cases := map[string]string{
		"gemini": filepath.Join(home, ".glass", "gemini"),
		"openai": filepath.Join(home, ".glass", "openai"),
		"ollama": filepath.Join(home, ".glass", "ollama"),
	}
	for lane, wantRoot := range cases {
		paths := Resolve(lane)
		if paths.Lane != lane {
			t.Fatalf("%s normalized lane = %q", lane, paths.Lane)
		}
		if paths.RootDir != wantRoot {
			t.Fatalf("%s root = %q, want %q", lane, paths.RootDir, wantRoot)
		}
		if paths.LiveCaptureDir != filepath.Join(wantRoot, "live") {
			t.Fatalf("%s live capture = %q", lane, paths.LiveCaptureDir)
		}
	}
}
