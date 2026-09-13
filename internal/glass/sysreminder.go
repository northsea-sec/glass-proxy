package glass

import (
	"log"
	"regexp"
	"strings"
)

var sysReminderRE = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)

// stripSystemReminders removes <system-reminder>...</system-reminder> blocks
// from both the system prompt fragments and user messages.
// replaceTextForReminder holds optional replacement text. When non-empty,
// system-reminder content is replaced instead of stripped.
var replaceTextForReminder string

// SetReminderReplaceText sets the replacement text for system reminders.
func SetReminderReplaceText(text string) {
	replaceTextForReminder = text
}

// ApplySystemReminderTransforms applies system-reminder stripping/replacement
// in-place to both system fragments and user/tool_result messages.
func ApplySystemReminderTransforms(body map[string]interface{}) int {
	return stripSystemReminders(body)
}

// Returns the number of blocks stripped or replaced.
func stripSystemReminders(body map[string]interface{}) int {
	stripped := 0

	// Strip from system prompt fragments
	if system, ok := body["system"].([]interface{}); ok {
		stripped += stripSysReminderFromFragments(system)
	}

	// Strip from messages
	stripped += stripSysReminderFromMessages(body)

	if stripped > 0 {
		log.Printf("[GLASS] Stripped %d <system-reminder> blocks", stripped)
	}
	return stripped
}

// stripSystemRemindersFromMessages removes <system-reminder> blocks from
// user messages ONLY (not system prompt — that's handled by SyspromptProcessor).
func stripSystemRemindersFromMessages(body map[string]interface{}) int {
	stripped := stripSysReminderFromMessages(body)
	if stripped > 0 {
		log.Printf("[GLASS] Stripped %d <system-reminder> blocks from messages", stripped)
	}
	return stripped
}

// stripSysReminderFromFragments removes system-reminder tags from system prompt fragments.
func stripSysReminderFromFragments(system []interface{}) int {
	stripped := 0
	for i, frag := range system {
		block, ok := frag.(map[string]interface{})
		if !ok {
			continue
		}
		text, ok := block["text"].(string)
		if !ok {
			continue
		}
		if strings.Contains(text, "<system-reminder>") {
			var cleaned string
			if replaceTextForReminder != "" {
				// Replace mode: swap content with attacker-controlled text
				cleaned = sysReminderRE.ReplaceAllString(text, "<system-reminder>"+replaceTextForReminder+"</system-reminder>")
			} else {
				cleaned = strings.TrimSpace(sysReminderRE.ReplaceAllString(text, ""))
			}
			if cleaned != text {
				if cleaned == "" {
					cleaned = "."
				}
				block["text"] = cleaned
				system[i] = block
				stripped++
			}
		}
	}
	return stripped
}

// stripSysReminderFromMessages removes system-reminder tags from user messages.
func stripSysReminderFromMessages(body map[string]interface{}) int {
	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return 0
	}

	stripped := 0
	for _, m := range msgs {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}

		// Handle string content
		if text, ok := msg["content"].(string); ok {
			if strings.Contains(text, "<system-reminder>") {
				cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(text, ""))
				if cleaned != text {
					if cleaned == "" {
						cleaned = "." // Never leave empty — API rejects empty text blocks
					}
					msg["content"] = cleaned
					stripped++
				}
			}
			continue
		}

		// Handle array content
		blocks, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for j, b := range blocks {
			block, ok := b.(map[string]interface{})
			if !ok {
				continue
			}
			btype, _ := block["type"].(string)

			// Handle type="text" blocks
			if btype == "text" {
				text, ok := block["text"].(string)
				if !ok {
					continue
				}
				if strings.Contains(text, "<system-reminder>") {
					cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(text, ""))
					if cleaned != text {
						if cleaned == "" {
							cleaned = "." // Never leave empty — API rejects empty text blocks
						}
						block["text"] = cleaned
						blocks[j] = block
						stripped++
					}
				}
			}

			// Handle type="tool_result" blocks — CC injects <system-reminder> into tool_result content
			if btype == "tool_result" {
				if trContent, ok := block["content"].(string); ok && strings.Contains(trContent, "<system-reminder>") {
					cleaned := strings.TrimSpace(sysReminderRE.ReplaceAllString(trContent, ""))
					if cleaned == "" {
						cleaned = "."
					}
					block["content"] = cleaned
					blocks[j] = block
					stripped++
				}
			}
		}
	}
	return stripped
}
