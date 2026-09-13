package glass

const (
	// ExtendedCacheTTL requests Anthropic's 1-hour prompt cache TTL.
	ExtendedCacheTTL = "1h"
	// ExtendedCacheTTLBeta must be sent with 1-hour prompt cache TTL requests.
	ExtendedCacheTTLBeta = "extended-cache-ttl-2025-04-11"
	// ContextEditingBeta enables Anthropic's server-side Context Editing API.
	ContextEditingBeta = "context-management-2025-06-27"
)

func extendedCacheControl() map[string]interface{} {
	return map[string]interface{}{
		"type": "ephemeral",
		"ttl":  ExtendedCacheTTL,
	}
}

func normalizeCacheControlTTL(body map[string]interface{}, ttl string) {
	if body == nil || ttl == "" {
		return
	}
	normalizeCacheControlTTLInValue(body["system"], ttl)
	normalizeCacheControlTTLInValue(body["tools"], ttl)
}

func normalizeCacheControlTTLInValue(v interface{}, ttl string) {
	arr, ok := v.([]interface{})
	if !ok {
		return
	}
	for _, raw := range arr {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		normalizeCacheControlTTLMap(item, ttl)
		if content, ok := item["content"].([]interface{}); ok {
			for _, blockRaw := range content {
				block, ok := blockRaw.(map[string]interface{})
				if !ok {
					continue
				}
				normalizeCacheControlTTLMap(block, ttl)
			}
		}
	}
}

func normalizeCacheControlTTLMap(m map[string]interface{}, ttl string) {
	cc, ok := m["cache_control"].(map[string]interface{})
	if !ok {
		return
	}
	cc["ttl"] = ttl
}

// StripAllCacheControl removes request-level automatic caching plus any explicit
// cache_control markers from system, tools, and message blocks. Returns the
// number of removed markers.
func StripAllCacheControl(body map[string]interface{}) int {
	stripped := 0
	if body == nil {
		return 0
	}

	if _, ok := body["cache_control"]; ok {
		delete(body, "cache_control")
		stripped++
	}

	stripped += stripCacheControlFromArray(body["system"])
	stripped += stripCacheControlFromArray(body["tools"])

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return stripped
	}
	for _, raw := range msgs {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := msg["cache_control"]; ok {
			delete(msg, "cache_control")
			stripped++
		}
		if content, ok := msg["content"].([]interface{}); ok {
			stripped += stripCacheControlFromBlocks(content)
		}
	}

	return stripped
}

func stripCacheControlFromArray(v interface{}) int {
	arr, ok := v.([]interface{})
	if !ok {
		return 0
	}
	stripped := 0
	for _, raw := range arr {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := item["cache_control"]; ok {
			delete(item, "cache_control")
			stripped++
		}
		if content, ok := item["content"].([]interface{}); ok {
			stripped += stripCacheControlFromBlocks(content)
		}
	}
	return stripped
}

func stripCacheControlFromBlocks(blocks []interface{}) int {
	stripped := 0
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := block["cache_control"]; ok {
			delete(block, "cache_control")
			stripped++
		}
	}
	return stripped
}
