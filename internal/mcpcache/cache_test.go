package mcpcache

import (
	"sort"
	"testing"
)

func makeTool(name string) map[string]interface{} {
	return map[string]interface{}{
		"name":        name,
		"description": "test tool " + name,
		"input_schema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func makeTools(names ...string) []interface{} {
	out := make([]interface{}, len(names))
	for i, n := range names {
		out[i] = makeTool(n)
	}
	return out
}

func toolNames(tools []interface{}) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		if tm, ok := t.(map[string]interface{}); ok {
			if n, _ := tm["name"].(string); n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

// THE CORE REGRESSION TEST.
// Conv 67185 has 7 tools. Conv 67821 has 42 tools.
// 67821's tools must NEVER be injected into 67185's requests.
func TestConvAToolsNeverInjectedIntoConvB(t *testing.T) {
	c := New()

	conv67821_tools := makeTools(
		"mcp__chrome-devtools__click",
		"mcp__chrome-devtools__navigate_page",
		"mcp__chrome-devtools__take_screenshot",
		"mcp__server-memory__create_entities",
		"mcp__server-memory__read_graph",
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	)
	conv67185_tools := makeTools(
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	)

	// 67821 populates its per-conv cache with 7 tools
	c.Observe("conv_67821", conv67821_tools)

	// 67185 sends only 2 tools — must get back ONLY 2, not 7
	result, injected := c.Inject("conv_67821_tools_never_cross_to_67185", conv67185_tools)
	if injected != 0 {
		t.Errorf("CROSS-CONV CONTAMINATION: %d tools from unknown conv injected into 67185", injected)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 tools for fresh conv, got %d: %v", len(result), toolNames(result))
	}
}

// Conv isolation: 67821's tools must not appear in 67185's Inject result.
func TestPerConvIsolation(t *testing.T) {
	c := New()

	// 67821 observes chrome-devtools + server-memory
	c.Observe("conv_67821", makeTools(
		"mcp__chrome-devtools__click",
		"mcp__chrome-devtools__navigate_page",
		"mcp__server-memory__create_entities",
		"mcp__server-memory__read_graph",
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	))

	// 67185 observes only brave-search + sequential-thinking
	c.Observe("conv_67185", makeTools(
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	))

	// 67185 sends its 2 tools — must get back exactly 2, not 6
	result, injected := c.Inject("conv_67185", makeTools(
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	))
	if injected != 0 {
		t.Errorf("CROSS-CONV CONTAMINATION: injected %d tools from 67821 into 67185: %v",
			injected, toolNames(result))
	}
	if len(result) != 2 {
		t.Errorf("expected 2 tools for conv_67185, got %d: %v", len(result), toolNames(result))
	}

	// Verify none of 67821's exclusive tools leaked into 67185
	names := toolNames(result)
	for _, n := range names {
		if n == "mcp__chrome-devtools__click" || n == "mcp__server-memory__create_entities" {
			t.Errorf("67821 tool %q leaked into 67185's result", n)
		}
	}
}

// MCP flap scenario: server disconnects mid-session for ONE conversation.
// The per-conv cache re-injects its own tools — not another conv's tools.
func TestMCPFlapWithinSameConv(t *testing.T) {
	c := New()

	// Conv 67185 previously sent 4 tools (before flap)
	c.Observe("conv_67185", makeTools(
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
		"mcp__chrome-devtools__click",
		"mcp__chrome-devtools__navigate_page",
	))

	// After MCP flap: CC sends only 2 tools (chrome-devtools server is down)
	result, injected := c.Inject("conv_67185", makeTools(
		"mcp__brave-search__brave_web_search",
		"mcp__sequential-thinking__sequentialthinking",
	))

	if injected != 2 {
		t.Errorf("expected 2 tools re-injected after flap, got %d", injected)
	}
	if len(result) != 4 {
		t.Errorf("expected 4 total tools after flap recovery, got %d: %v", len(result), toolNames(result))
	}

	// Verify the re-injected tools are the right ones
	names := toolNames(result)
	required := []string{
		"mcp__brave-search__brave_web_search",
		"mcp__chrome-devtools__click",
		"mcp__chrome-devtools__navigate_page",
		"mcp__sequential-thinking__sequentialthinking",
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	for _, req := range required {
		if !nameSet[req] {
			t.Errorf("expected tool %q after flap recovery, not present", req)
		}
	}
}

// A fresh conversation with no prior Observe gets zero injection.
func TestFreshConvGetsNoInjection(t *testing.T) {
	c := New()

	// Some other conv populates the cache
	c.Observe("conv_other", makeTools(
		"mcp__chrome-devtools__click",
		"mcp__server-memory__create_entities",
	))

	// Fresh conv — never seen before — must get exactly what it sent
	result, injected := c.Inject("conv_fresh", makeTools(
		"mcp__brave-search__brave_web_search",
	))
	if injected != 0 {
		t.Errorf("fresh conv got %d tools injected from other conv: %v", injected, toolNames(result))
	}
	if len(result) != 1 {
		t.Errorf("expected 1 tool for fresh conv, got %d", len(result))
	}
}

// MCP tools must be sorted alphabetically after builtins — deterministic on every call.
func TestDeterministicOrdering(t *testing.T) {
	c := New()

	c.Observe("conv_a", makeTools(
		"mcp__server-memory__search_nodes",
		"mcp__chrome-devtools__take_screenshot",
		"mcp__brave-search__brave_web_search",
		"mcp__chrome-devtools__click",
		"mcp__sequential-thinking__sequentialthinking",
		"mcp__server-memory__create_entities",
	))

	builtin := []interface{}{makeTool("Read"), makeTool("Write"), makeTool("Bash")}

	result1, _ := c.Inject("conv_a", append([]interface{}{}, builtin...))
	result2, _ := c.Inject("conv_a", append([]interface{}{}, builtin...))

	n1 := toolNames(result1)
	n2 := toolNames(result2)
	if len(n1) != len(n2) {
		t.Fatalf("different lengths: %d vs %d", len(n1), len(n2))
	}
	for i := range n1 {
		if n1[i] != n2[i] {
			t.Errorf("position %d differs: %q vs %q", i, n1[i], n2[i])
		}
	}

	// Builtins before MCP
	seenMCP := false
	for _, name := range n1 {
		isMCP := len(name) >= 5 && name[:5] == "mcp__"
		if seenMCP && !isMCP {
			t.Errorf("builtin %q appeared after MCP tools", name)
		}
		if isMCP {
			seenMCP = true
		}
	}

	// MCP tools sorted
	var mcpNames []string
	for _, name := range n1 {
		if len(name) >= 5 && name[:5] == "mcp__" {
			mcpNames = append(mcpNames, name)
		}
	}
	sorted := make([]string, len(mcpNames))
	copy(sorted, mcpNames)
	sort.Strings(sorted)
	for i := range mcpNames {
		if mcpNames[i] != sorted[i] {
			t.Errorf("MCP not sorted at %d: got %q want %q", i, mcpNames[i], sorted[i])
		}
	}
}

// cache_control must be stripped from all tools after Inject.
func TestCacheControlStripped(t *testing.T) {
	c := New()

	toolWithCC := makeTool("mcp__chrome-devtools__click")
	toolWithCC["cache_control"] = map[string]interface{}{"type": "ephemeral", "ttl": "1h"}
	c.Observe("conv_a", []interface{}{toolWithCC})

	reqTool := makeTool("mcp__sequential-thinking__sequentialthinking")
	reqTool["cache_control"] = map[string]interface{}{"type": "ephemeral"}

	result, _ := c.Inject("conv_a", []interface{}{reqTool})
	for _, t2 := range result {
		tm := t2.(map[string]interface{})
		if _, has := tm["cache_control"]; has {
			t.Errorf("tool %q still has cache_control after Inject", tm["name"])
		}
	}
}

// Empty tool list is never touched.
func TestEmptyToolListNotTouched(t *testing.T) {
	c := New()
	c.Observe("conv_a", makeTools("mcp__chrome-devtools__click"))
	result, injected := c.Inject("conv_a", []interface{}{})
	if injected != 0 || len(result) != 0 {
		t.Errorf("empty list was modified: injected=%d len=%d", injected, len(result))
	}
}

func TestInjectReusesCachedCanonicalMCPDefinitions(t *testing.T) {
	c := New()

	cached := makeTool("mcp__server-memory__search_nodes")
	cached["description"] = "cached definition"
	c.Observe("conv_a", []interface{}{cached})

	current := makeTool("mcp__server-memory__search_nodes")
	current["description"] = "drifted definition"

	result, injected := c.Inject("conv_a", []interface{}{current})
	if injected != 0 {
		t.Fatalf("expected no injection for existing tool, got %d", injected)
	}
	if got := result[0].(map[string]interface{})["description"]; got != "cached definition" {
		t.Fatalf("expected cached MCP definition to win, got %v", got)
	}
}

func TestDeterministicOrderingSortsBuiltinsByName(t *testing.T) {
	c := New()
	builtin := []interface{}{makeTool("Write"), makeTool("Bash"), makeTool("Read")}

	result, _ := c.Inject("conv_a", builtin)
	got := toolNames(result)
	want := []string{"Bash", "Read", "Write"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("builtin order mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
