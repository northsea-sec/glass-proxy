package glass

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"proxy.local/app/internal/subagent"
	"proxy.local/app/internal/sysprompt"
)

func TestCutSection_Basic(t *testing.T) {
	text := `# System
System content here.

# Doing tasks
Task content line 1.
Task content line 2.

# Professional objectivity
Objectivity content here.`

	result := cutSection(text, "# Doing tasks")
	if strings.Contains(result, "# Doing tasks") {
		t.Errorf("section heading still present")
	}
	if strings.Contains(result, "Task content") {
		t.Errorf("section body still present")
	}
	if !strings.Contains(result, "# System") {
		t.Errorf("preceding section removed")
	}
	if !strings.Contains(result, "# Professional objectivity") {
		t.Errorf("following section removed")
	}
	if !strings.Contains(result, "Objectivity content") {
		t.Errorf("following section body removed")
	}
}

func TestCutSection_NotFound(t *testing.T) {
	text := "# System\nContent here.\n# Environment\nEnv stuff."
	result := cutSection(text, "# Doing tasks")
	if result != text {
		t.Errorf("text was modified when heading not found")
	}
}

func TestCutSection_LastSection(t *testing.T) {
	text := "# System\nContent here.\n# Tone and style\nTone content.\nMore tone."
	result := cutSection(text, "# Tone and style")
	if strings.Contains(result, "Tone content") {
		t.Errorf("last section body still present")
	}
	if !strings.Contains(result, "# System") {
		t.Errorf("preceding section removed")
	}
}

func TestStripBoilerplateSections_AllThree(t *testing.T) {
	sysText := `# System
System content.

# Doing tasks
Do tasks content here.
More task lines.

# Professional objectivity
Objectivity rules.

# Epistemological operating principles
Epistemic rules.

# User frustration protocol
Frustration rules.

# Research disposition
Research rules.

# Self-diagnosis
Self diag rules.

# Using your tools
Tool usage content.
More tool lines.

# Tone and style
Tone content.
More tone.

# auto memory
Memory instructions.

# Environment
Env details.`

	system := []interface{}{
		map[string]interface{}{"type": "text", "text": sysText},
	}

	result := stripBoilerplateSections(system)
	if len(result) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result))
	}

	block := result[0].(map[string]interface{})
	text := block["text"].(string)

	// Should be GONE
	if strings.Contains(text, "# Doing tasks") {
		t.Error("# Doing tasks not removed")
	}
	if strings.Contains(text, "Do tasks content") {
		t.Error("Doing tasks body not removed")
	}
	if strings.Contains(text, "# Using your tools") {
		t.Error("# Using your tools not removed")
	}
	if strings.Contains(text, "Tool usage content") {
		t.Error("Using your tools body not removed")
	}
	if strings.Contains(text, "# Tone and style") {
		t.Error("# Tone and style not removed")
	}
	if strings.Contains(text, "Tone content") {
		t.Error("Tone and style body not removed")
	}

	// Must SURVIVE
	mustSurvive := []string{
		"# System", "System content",
		"# Professional objectivity", "Objectivity rules",
		"# Epistemological operating principles", "Epistemic rules",
		"# User frustration protocol", "Frustration rules",
		"# Research disposition", "Research rules",
		"# Self-diagnosis", "Self diag rules",
		"# auto memory", "Memory instructions",
		"# Environment", "Env details",
	}
	for _, s := range mustSurvive {
		if !strings.Contains(text, s) {
			t.Errorf("MISSING (should survive): %q", s)
		}
	}

	// Verify token savings
	origLen := len(sysText)
	newLen := len(text)
	saved := origLen - newLen
	if saved < 100 {
		t.Errorf("expected significant savings, only saved %d chars", saved)
	}
	t.Logf("Stripped %d chars (%.0f%% savings from boilerplate)", saved, float64(saved)/float64(origLen)*100)
}

func TestInjectFacts_Empty(t *testing.T) {
	system := []interface{}{
		map[string]interface{}{"type": "text", "text": "# System\nContent."},
	}
	sp := &SyspromptProcessor{}
	result := sp.InjectFacts(system, "")
	if len(result) != 1 {
		t.Errorf("empty facts should not modify system, got %d fragments", len(result))
	}
}

func TestInjectFacts_WithFacts(t *testing.T) {
	sysText := `# System
System content.

# Doing tasks
Task content.

# Professional objectivity
Objectivity.

# auto memory
Memory.`

	system := []interface{}{
		map[string]interface{}{"type": "text", "text": sysText},
	}
	sp := &SyspromptProcessor{}
	result := sp.InjectFacts(system, "# Operational context\nFact 1.\nFact 2.")

	// Should have 2 fragments: stripped system + facts
	if len(result) != 2 {
		t.Fatalf("expected 2 fragments, got %d", len(result))
	}

	// First fragment: system without boilerplate
	block0 := result[0].(map[string]interface{})
	text0 := block0["text"].(string)
	if strings.Contains(text0, "# Doing tasks") {
		t.Error("boilerplate not stripped")
	}
	if !strings.Contains(text0, "# Professional objectivity") {
		t.Error("custom section removed")
	}

	// Second fragment: facts
	block1 := result[1].(map[string]interface{})
	text1 := block1["text"].(string)
	if !strings.Contains(text1, "Fact 1") {
		t.Error("facts not injected")
	}
}

func TestProcess_WithFactsUsesCanonicalCache(t *testing.T) {
	system := []interface{}{
		map[string]interface{}{"type": "text", "text": `# System
System content.

# Doing tasks
Task content.

# Professional objectivity
Objectivity.

# auto memory
Memory.`},
	}

	info := subagent.Classification{}
	sp := NewSyspromptProcessor(sysprompt.NewPipeline(false), t.TempDir())

	result1, mods1 := sp.Process(system, info, "# Operational context\nFact 1.")
	if mods1 != 0 {
		t.Fatalf("expected no pipeline mods with disabled pipeline, got %d", mods1)
	}
	if len(result1) != 2 {
		t.Fatalf("expected 2 fragments, got %d", len(result1))
	}
	if got := len(sp.canonicalCache); got != 1 {
		t.Fatalf("expected 1 canonical cache entry, got %d", got)
	}

	block0 := result1[0].(map[string]interface{})
	text0 := block0["text"].(string)
	if strings.Contains(text0, "# Doing tasks") {
		t.Fatal("boilerplate not stripped in canonical path")
	}
	if !strings.Contains(text0, "# Professional objectivity") {
		t.Fatal("custom section removed in canonical path")
	}

	result2, mods2 := sp.Process(deepCopyFragments(system), info, "# Operational context\nFact 1.")
	if mods2 != 0 {
		t.Fatalf("expected cache hit to report 0 mods, got %d", mods2)
	}
	if got := len(sp.canonicalCache); got != 1 {
		t.Fatalf("expected same fact overlay to reuse canonical cache, got %d entries", got)
	}

	block1a := result1[1].(map[string]interface{})
	block1b := result2[1].(map[string]interface{})
	if block1a["text"] != block1b["text"] {
		t.Fatal("canonical cache returned different fact fragment for identical facts")
	}

	result3, _ := sp.Process(system, info, "# Operational context\nFact 2.")
	if got := len(sp.canonicalCache); got != 2 {
		t.Fatalf("expected changed facts to create a new canonical cache entry, got %d", got)
	}
	block3 := result3[1].(map[string]interface{})
	if !strings.Contains(block3["text"].(string), "Fact 2") {
		t.Fatal("updated facts not reflected in canonical output")
	}
}

func TestCutSection_RealWorldHeadings(t *testing.T) {
	// Simulate the actual CC system prompt heading order
	text := `# System
 - All text you output outside of tool use is displayed to the user.

# Doing tasks
 - The user will primarily request you to perform software engineering tasks.
 - You are highly capable and often allow users to complete ambitious tasks.
 - In general, do not propose changes to code you haven't read.
 - If the user asks for help or wants to give feedback inform them of the following:
  - /help: Get help with using Claude Code
  - To give feedback, users should report the issue at https://github.com/anthropics/claude-code/issues

# Professional objectivity
Prioritize technical accuracy and truthfulness over validating the user's beliefs.

# Epistemological operating principles
Apply empirical epistemology to all claims, including your own.

# User frustration protocol
When the user expresses frustration, anger, or says STOP: halt all current work.

# Research disposition
This environment is a private security research lab.

# Self-diagnosis
After completing any multi-step task, briefly review your own work.

# Using your tools
 - Do NOT use the Bash to run commands when a relevant dedicated tool is provided.
 - Use the Agent tool with specialized agents when the task at hand matches.
 - For simple, directed codebase searches use the Glob or Grep directly.

# Tone and style
 - Only use emojis if the user explicitly requests it.
 - Your responses should be short and concise.

# auto memory
You have a persistent auto memory directory.

# Environment
You have been invoked in the following environment.`

	for _, target := range factReplacementTargets {
		result := cutSection(text, target)
		if strings.Contains(result, target) {
			t.Errorf("%q not removed", target)
		}
	}

	// Apply all three cuts
	result := text
	for _, target := range factReplacementTargets {
		result = cutSection(result, target)
	}

	// Verify custom sections survived
	customs := []string{
		"# Professional objectivity",
		"# Epistemological operating principles",
		"# User frustration protocol",
		"# Research disposition",
		"# Self-diagnosis",
		"# auto memory",
		"# Environment",
	}
	for _, c := range customs {
		if !strings.Contains(result, c) {
			t.Errorf("custom section %q was removed!", c)
		}
	}

	saved := len(text) - len(result)
	t.Logf("Saved %d chars from real-world prompt", saved)
}

func TestProcess_CanonicalCacheTracksSyspromptEditorPatches(t *testing.T) {
	patchPath := filepath.Join(t.TempDir(), "patches.json")
	writePatchFile := func(replace string, ts time.Time) {
		t.Helper()
		data := `[{"find":"Original system fragment","replace":"` + replace + `"}]`
		if err := os.WriteFile(patchPath, []byte(data), 0644); err != nil {
			t.Fatalf("write patch file: %v", err)
		}
		if err := os.Chtimes(patchPath, ts, ts); err != nil {
			t.Fatalf("chtimes patch file: %v", err)
		}
	}

	writePatchFile("Patched fragment v1", time.Now())

	pipe := sysprompt.NewPipeline(false)
	pipe.SyncConfig(true, patchPath, "")
	sp := NewSyspromptProcessor(pipe, t.TempDir())

	system := []interface{}{
		map[string]interface{}{"type": "text", "text": "Original system fragment"},
	}
	info := subagent.Classification{}

	result1, mods1 := sp.Process(system, info, "")
	if mods1 != 1 {
		t.Fatalf("expected cache miss with patch to report 1 modification, got %d", mods1)
	}
	text1 := result1[0].(map[string]interface{})["text"].(string)
	if text1 != "Patched fragment v1" {
		t.Fatalf("unexpected first patched text %q", text1)
	}
	if got := len(sp.canonicalCache); got != 1 {
		t.Fatalf("expected 1 canonical cache entry after first patch, got %d", got)
	}
	if !canonicalCacheContainsText(sp.canonicalCache, "Patched fragment v1") {
		t.Fatal("canonical cache did not store the substituted v1 prompt")
	}

	result2, mods2 := sp.Process(deepCopyFragments(system), info, "")
	if mods2 != 0 {
		t.Fatalf("expected identical second request to hit canonical cache, got %d modifications", mods2)
	}
	text2 := result2[0].(map[string]interface{})["text"].(string)
	if text2 != "Patched fragment v1" {
		t.Fatalf("unexpected cached patched text %q", text2)
	}

	writePatchFile("Patched fragment v2", time.Now().Add(time.Second))

	result3, mods3 := sp.Process(deepCopyFragments(system), info, "")
	if mods3 != 1 {
		t.Fatalf("expected patch-file change to force a fresh canonical cache entry, got %d modifications", mods3)
	}
	text3 := result3[0].(map[string]interface{})["text"].(string)
	if text3 != "Patched fragment v2" {
		t.Fatalf("unexpected refreshed patched text %q", text3)
	}
	if got := len(sp.canonicalCache); got != 2 {
		t.Fatalf("expected patch change to create a second canonical cache entry, got %d", got)
	}
	if !canonicalCacheContainsText(sp.canonicalCache, "Patched fragment v2") {
		t.Fatal("canonical cache did not store the substituted v2 prompt")
	}

	result4, mods4 := sp.Process(deepCopyFragments(system), info, "")
	if mods4 != 0 {
		t.Fatalf("expected refreshed patch to hit canonical cache on second read, got %d modifications", mods4)
	}
	text4 := result4[0].(map[string]interface{})["text"].(string)
	if text4 != "Patched fragment v2" {
		t.Fatalf("unexpected cached v2 text %q", text4)
	}
}

func canonicalCacheContainsText(cache map[string][]interface{}, want string) bool {
	for _, fragments := range cache {
		for _, fragment := range fragments {
			block, ok := fragment.(map[string]interface{})
			if !ok {
				continue
			}
			if text, _ := block["text"].(string); text == want {
				return true
			}
		}
	}
	return false
}
