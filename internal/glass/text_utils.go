package glass

import (
	"strings"
	"unicode"
)

// Text utility functions used by shadow.go, reference.go, and chapter.go.

var syntheticMemoryNeedles = []string{
	"[request interrupted by user",
	"thoughtnumber",
	"totalthoughts",
	"ctrl+o to expand",
	"#### message",
	"sequential-thinking",
	"tool_result",
	"tool_use_id",
	"toolu_",
}

var authorizationNeedles = []string{
	"go ahead",
	"i approve",
	"approved",
	"please proceed",
	"please do",
	"you can",
	"do it",
	"proceed",
	"restart it",
	"restart the",
	"kill it",
	"stop it",
	"run it",
	"implement it",
}

var authorizationAckValues = map[string]bool{
	"ok":    true,
	"okay":  true,
	"yes":   true,
	"sure":  true,
	"yep":   true,
	"yeah":  true,
	"fine":  true,
	"do it": true,
}

var trivialAckValues = map[string]bool{
	"ok":         true,
	"okay":       true,
	"yes":        true,
	"sure":       true,
	"yep":        true,
	"yeah":       true,
	"fine":       true,
	"got it":     true,
	"understood": true,
	"thanks":     true,
	"continue":   true,
	"go on":      true,
	"go":         true,
	"next":       true,
	"proceed":    true,
	"resume":     true,
	"keep going": true,
	"carry on":   true,
}

func normalizeSnippet(text string) string {
	if text == "" {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(text))
	for len(fields) > 0 && isNoiseOnlyToken(fields[0]) {
		fields = fields[1:]
	}
	return strings.Join(fields, " ")
}

func clipFactText(text string, maxChars int) string {
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return strings.TrimSpace(text[:maxChars-3]) + "..."
}

func isNoiseOnlyToken(token string) bool {
	if token == "" || len(token) > 3 {
		return false
	}
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
		if !unicode.IsPunct(r) && !unicode.IsSymbol(r) {
			return false
		}
	}
	return true
}

func normalizeFactContent(content string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(content))), " ")
}

func shouldSuppressBootstrapText(role, text string) bool {
	normalized := normalizeFactContent(text)
	if normalized == "" {
		return true
	}
	for _, needle := range syntheticMemoryNeedles {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	if role == "assistant" && looksLikeStructuredAssistantReport(text) {
		return true
	}
	return false
}

func looksLikeStructuredAssistantReport(text string) bool {
	trimmed := strings.TrimSpace(text)
	lowered := strings.ToLower(trimmed)
	markers := 0
	for _, needle := range []string{"## ", "### ", "---", "| ", "**root cause**", "**current state**", "full regression", "what's outstanding"} {
		if strings.Contains(lowered, needle) {
			markers++
		}
	}
	if markers >= 2 {
		return true
	}
	return markers > 0 && len(trimmed) >= 80
}

func isAuthorizationText(text string) bool {
	normalized := normalizeFactContent(text)
	if normalized == "" {
		return false
	}
	if shouldSuppressBootstrapText("user", text) {
		return false
	}
	if authorizationAckValues[normalized] {
		return true
	}
	for _, needle := range authorizationNeedles {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func isTrivialAck(text string) bool {
	normalized := normalizeFactContent(text)
	if normalized == "" {
		return true
	}
	if trivialAckValues[normalized] {
		return true
	}
	return len(strings.Fields(normalized)) <= 3
}
