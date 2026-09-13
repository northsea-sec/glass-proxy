package promptscope

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

const DefaultSystemPrefixChars = 2000

// Signature derives a stable lane key from model + normalized system prefix +
// tools. This is stronger than hashing only the first 500 chars of system, so
// agent lanes that share a common preamble but diverge later do not collapse.
func Signature(body map[string]interface{}, systemPrefixChars int) string {
	if systemPrefixChars <= 0 {
		systemPrefixChars = DefaultSystemPrefixChars
	}

	payload := map[string]interface{}{
		"model":         modelName(body),
		"system_prefix": systemPrefix(body["system"], systemPrefixChars),
		"tools":         body["tools"],
	}
	if payload["system_prefix"] == "" {
		payload["user_prefix"] = firstUserPrefix(body["messages"], 500)
	}

	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:6])
}

func modelName(body map[string]interface{}) string {
	model, _ := body["model"].(string)
	return strings.TrimSpace(model)
}

func systemPrefix(raw interface{}, limit int) string {
	switch sys := raw.(type) {
	case string:
		return truncate(strings.TrimSpace(sys), limit)
	case []interface{}:
		blocks := skipBillingHeader(sys)
		var sb strings.Builder
		for _, rawBlock := range blocks {
			block, ok := rawBlock.(map[string]interface{})
			if !ok {
				continue
			}
			text, _ := block["text"].(string)
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			sb.WriteString(text)
			if sb.Len() >= limit {
				break
			}
			sb.WriteByte('\n')
		}
		return truncate(sb.String(), limit)
	default:
		return ""
	}
}

func firstUserPrefix(raw interface{}, limit int) string {
	msgs, _ := raw.([]interface{})
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		switch content := msg["content"].(type) {
		case string:
			text := strings.TrimSpace(content)
			if text != "" {
				return truncate(text, limit)
			}
		case []interface{}:
			for _, rawBlock := range content {
				block, ok := rawBlock.(map[string]interface{})
				if !ok {
					continue
				}
				if tp, _ := block["type"].(string); tp != "text" {
					continue
				}
				text, _ := block["text"].(string)
				text = strings.TrimSpace(text)
				if text != "" {
					return truncate(text, limit)
				}
			}
		}
	}
	return ""
}

func skipBillingHeader(system []interface{}) []interface{} {
	if len(system) < 2 {
		return system
	}
	first, ok := system[0].(map[string]interface{})
	if !ok {
		return system
	}
	text, _ := first["text"].(string)
	if strings.Contains(text, "billing") || strings.Contains(text, "x-anthropic") {
		return system[1:]
	}
	return system
}

func truncate(text string, limit int) string {
	if limit > 0 && len(text) > limit {
		return text[:limit]
	}
	return text
}
