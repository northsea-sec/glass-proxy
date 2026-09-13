// Package mcpcache ensures MCP tools are always present even when servers flap.
// Ported from mitm_itt_addon.py _ensure_mcp_tools_present().
//
// Each conversation has its own tool cache (keyed by convID). Tools observed in
// conversation A are never injected into conversation B. This prevents sessions
// with different MCP server configurations from contaminating each other's
// Anthropic prefix hash.
//
// Use case: if server-memory disconnects mid-session, the per-conv cache
// re-injects its tools on the next request — but only into that conversation.
package mcpcache

import (
	"encoding/json"
	"log"
	"sort"
	"strings"
	"sync"
)

// Cache stores per-conversation highest-watermark MCP tool definitions.
// Tools from conversation A are never injected into conversation B.
type Cache struct {
	mu          sync.Mutex
	convs       map[string]map[string]interface{} // convID -> (name -> canonicalized tool def)
	detectedTTL string
}

// New creates an empty MCP tool cache.
func New() *Cache {
	return &Cache{
		convs: make(map[string]map[string]interface{}),
	}
}

// NewWithPath exists for test compatibility. Path is unused — no disk persistence
// in the per-conv model (each conv seeds itself from its own requests).
func NewWithPath(_ string) *Cache {
	return New()
}

// canonicalize round-trips a tool through JSON to normalize map key order.
// Mirrors mitmproxy's json.loads(json.dumps(t, sort_keys=True)).
func canonicalize(t map[string]interface{}) map[string]interface{} {
	b, err := json.Marshal(t)
	if err != nil {
		return t
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return t
	}
	return out
}

func toolName(raw interface{}) string {
	if tm, ok := raw.(map[string]interface{}); ok {
		name, _ := tm["name"].(string)
		return name
	}
	return ""
}

func canonicalizeTool(raw interface{}) interface{} {
	tm, ok := raw.(map[string]interface{})
	if !ok {
		return raw
	}
	return canonicalize(tm)
}

// Observe records MCP tools from a request into the per-conversation cache.
// convID must be the conversation fingerprint — never "single" or a global key.
func (c *Cache) Observe(convID string, tools []interface{}) {
	if convID == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	conv := c.convs[convID]
	if conv == nil {
		conv = make(map[string]interface{})
		c.convs[convID] = conv
	}

	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		if name == "" || !strings.HasPrefix(name, "mcp__") {
			continue
		}
		if _, exists := conv[name]; !exists {
			conv[name] = canonicalize(tm)
		}
	}
}

// Inject adds any MCP tools previously seen in this conversation but missing from
// the current request. Sorts tools (builtins first, MCP alphabetically) and strips
// cache_control from all tool definitions.
// convID must match the one used in Observe.
func (c *Cache) Inject(convID string, tools []interface{}) ([]interface{}, int) {
	if len(tools) == 0 || convID == "" {
		return tools, 0
	}

	currentNames := make(map[string]bool, len(tools))
	c.mu.Lock()
	conv := c.convs[convID]
	normalized := make([]interface{}, 0, len(tools))
	for _, raw := range tools {
		name := toolName(raw)
		if name != "" {
			currentNames[name] = true
		}
		tool := canonicalizeTool(raw)
		if strings.HasPrefix(name, "mcp__") && conv != nil {
			if cached, ok := conv[name]; ok {
				tool = canonicalizeTool(cached)
			}
		}
		normalized = append(normalized, tool)
	}

	injected := 0
	if conv != nil {
		for name, tool := range conv {
			if !currentNames[name] {
				normalized = append(normalized, canonicalizeTool(tool))
				injected++
				log.Printf("[MCPCACHE] Injected missing tool: %s (conv=%s)", name, convID)
			}
		}
	}
	c.mu.Unlock()

	// Sort: builtins first, then MCP tools alphabetically.
	var builtin, mcp []interface{}
	for _, tool := range normalized {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			builtin = append(builtin, tool)
			continue
		}
		if name, _ := tm["name"].(string); strings.HasPrefix(name, "mcp__") {
			mcp = append(mcp, tool)
		} else {
			builtin = append(builtin, tool)
		}
	}
	sort.SliceStable(builtin, func(i, j int) bool {
		return toolName(builtin[i]) < toolName(builtin[j])
	})
	sort.Slice(mcp, func(i, j int) bool {
		ni, _ := mcp[i].(map[string]interface{})["name"].(string)
		nj, _ := mcp[j].(map[string]interface{})["name"].(string)
		return ni < nj
	})
	tools = append(builtin, mcp...)

	// Strip cache_control from all tool definitions and detect TTL.
	c.mu.Lock()
	c.detectedTTL = ""
	c.mu.Unlock()

	for _, tool := range tools {
		tm, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		if cc, ok := tm["cache_control"].(map[string]interface{}); ok {
			if ttl, _ := cc["ttl"].(string); ttl != "" {
				c.mu.Lock()
				if c.detectedTTL == "" {
					c.detectedTTL = ttl
				}
				c.mu.Unlock()
			}
			delete(tm, "cache_control")
		}
	}

	// Final sort by tool name; tools without a valid name sort to end.
	sort.SliceStable(tools, func(i, j int) bool {
		ni, nj := toolName(tools[i]), toolName(tools[j])
		if (ni == "") != (nj == "") {
			return nj == "" // named before unnamed
		}
		return ni < nj
	})
	return tools, injected
}

// DetectedTTL returns the cache_control TTL detected from tool blocks.
func (c *Cache) DetectedTTL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.detectedTTL
}

// Size returns total cached tool definitions across all conversations.
func (c *Cache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := 0
	for _, conv := range c.convs {
		total += len(conv)
	}
	return total
}

// Names returns all cached tool names across all conversations (for diagnostics).
func (c *Cache) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	seen := make(map[string]bool)
	for _, conv := range c.convs {
		for name := range conv {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names
}
