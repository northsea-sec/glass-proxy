package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"proxy.local/app/internal/config"
	"proxy.local/app/internal/guard"
)

var sensitiveCommandPatterns = map[string]*regexp.Regexp{
	"sensitive file access": regexp.MustCompile(`(?i)(\.env|\.ssh|id_rsa|authorized_keys|/etc/passwd|/etc/shadow|aws/credentials|kube/config)`),
	"environment dump":      regexp.MustCompile(`(?i)\b(printenv|env|set)\b`),
	"network exfiltration":  regexp.MustCompile(`(?i)\b(curl|wget|scp|rsync|nc|netcat|python\s+-c|node\s+-e)\b`),
}

type anthropicToolStreamGuard struct {
	proxy             *Proxy
	ctx               context.Context
	cfg               config.Config
	active            bool
	bufferedEvents    [][]string
	index             int
	toolName          string
	partialInput      strings.Builder
	rewriteStopReason bool
}

type openAIToolStreamGuard struct {
	proxy          *Proxy
	ctx            context.Context
	cfg            config.Config
	active         bool
	bufferedEvents [][]string
	toolCalls      map[int]*openAIBufferedToolCall
	lastID         string
	lastModel      string
	lastCreated    int64
}

type openAIBufferedToolCall struct {
	index int
	id    string
	name  string
	args  strings.Builder
}

func (p *Proxy) guardClient(cfg config.Config) *guard.Client {
	return guard.NewClientWithHTTPClient(cfg.SecurityGuardBaseURL, p.guardHTTPClient)
}

func (p *Proxy) guardEnabled(cfg config.Config) bool {
	return cfg.SecurityGuardEnabled && strings.TrimSpace(cfg.SecurityGuardBaseURL) != ""
}

func (p *Proxy) sanitizeAnthropicRequestBody(ctx context.Context, body map[string]interface{}, cfg config.Config) error {
	if !p.guardEnabled(cfg) {
		return nil
	}

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return nil
	}

	client := p.guardClient(cfg)
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if !ok {
				continue
			}
			if blockType, _ := block["type"].(string); blockType != "tool_result" {
				continue
			}
			text, ok := extractTextContent(block["content"])
			if !ok || strings.TrimSpace(text) == "" {
				continue
			}
			urls := guard.ExtractURLs(text)
			sourceURL := ""
			if len(urls) > 0 {
				sourceURL = urls[0]
			}
			checked, err := client.CheckContent(ctx, guard.ContentCheckRequest{
				SourceURL: sourceURL,
				Content:   text,
			})
			if err != nil {
				return err
			}
			if checked.Allowed {
				continue
			}
			block["content"] = []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": checked.SanitizedText,
				},
			}
			block["is_error"] = true
		}
	}
	return nil
}

func (p *Proxy) sanitizeOpenAIRequestBody(ctx context.Context, body map[string]interface{}, cfg config.Config) error {
	if !p.guardEnabled(cfg) {
		return nil
	}

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return nil
	}

	client := p.guardClient(cfg)
	for _, rawMsg := range msgs {
		msg, ok := rawMsg.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		text, ok := extractTextContent(msg["content"])
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		urls := guard.ExtractURLs(text)
		sourceURL := ""
		if len(urls) > 0 {
			sourceURL = urls[0]
		}
		checked, err := client.CheckContent(ctx, guard.ContentCheckRequest{
			SourceURL: sourceURL,
			Content:   text,
		})
		if err != nil {
			return err
		}
		if checked.Allowed {
			continue
		}
		msg["content"] = checked.SanitizedText
	}
	return nil
}

func (p *Proxy) secureAnthropicResponseBody(ctx context.Context, body []byte, cfg config.Config) ([]byte, error) {
	if !p.guardEnabled(cfg) {
		return body, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}

	content, ok := payload["content"].([]interface{})
	if !ok {
		return body, nil
	}

	blockedAny := false
	for i, rawBlock := range content {
		block, ok := rawBlock.(map[string]interface{})
		if !ok {
			continue
		}
		if blockType, _ := block["type"].(string); blockType != "tool_use" {
			continue
		}
		name, _ := block["name"].(string)
		input, _ := block["input"].(map[string]interface{})
		allowed, reasons, err := p.evaluateToolCall(ctx, name, input, cfg)
		if err != nil {
			return nil, err
		}
		if allowed {
			continue
		}
		content[i] = map[string]interface{}{
			"type": "text",
			"text": buildBlockedToolText(name, reasons),
		}
		blockedAny = true
	}

	if blockedAny {
		payload["content"] = content
		if stopReason, _ := payload["stop_reason"].(string); stopReason == "tool_use" {
			payload["stop_reason"] = "end_turn"
		}
		updated, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return updated, nil
	}
	return body, nil
}

func (p *Proxy) secureOpenAINonStreamingResponse(ctx context.Context, body []byte, cfg config.Config) ([]byte, error) {
	if !p.guardEnabled(cfg) {
		return body, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok {
		return body, nil
	}

	changed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		msg, ok := choice["message"].(map[string]interface{})
		if !ok {
			continue
		}
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}

		var reasons []string
		allowAll := true
		for _, rawCall := range toolCalls {
			call, ok := rawCall.(map[string]interface{})
			if !ok {
				continue
			}
			functionObj, _ := call["function"].(map[string]interface{})
			name, _ := functionObj["name"].(string)
			input := map[string]interface{}{}
			if argsRaw, _ := functionObj["arguments"].(string); strings.TrimSpace(argsRaw) != "" {
				_ = json.Unmarshal([]byte(argsRaw), &input)
			}
			allowed, toolReasons, err := p.evaluateToolCall(ctx, name, input, cfg)
			if err != nil {
				return nil, err
			}
			if !allowed {
				allowAll = false
				reasons = append(reasons, toolReasons...)
			}
		}
		if allowAll {
			continue
		}

		msg["tool_calls"] = nil
		msg["content"] = buildBlockedToolText("tool_call", reasons)
		choice["finish_reason"] = "stop"
		changed = true
	}

	if !changed {
		return body, nil
	}

	updated, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (p *Proxy) evaluateToolCall(ctx context.Context, toolName string, input map[string]interface{}, cfg config.Config) (bool, []string, error) {
	if !p.guardEnabled(cfg) {
		return true, nil, nil
	}

	client := p.guardClient(cfg)
	var reasons []string

	for _, rawURL := range collectURLs(input, "") {
		resp, err := client.CheckURL(ctx, guard.URLCheckRequest{
			URL:        rawURL,
			AllowHosts: cfg.SecurityGuardAllowedHosts,
		})
		if err != nil {
			return false, nil, err
		}
		if !resp.Allowed {
			reasons = append(reasons, fmt.Sprintf("blocked URL %s: %s", rawURL, strings.Join(resp.Reasons, ", ")))
		}
	}

	command := findCommand(input)
	if command != "" {
		reasons = append(reasons, detectSensitiveCommand(command)...)
		pkgResp, err := client.CheckPackage(ctx, guard.PackageCheckRequest{
			Command:                    command,
			AllowPackages:              cfg.SecurityGuardAllowedPackages,
			MinAgeHours:                cfg.SecurityGuardPackageMinAgeHours,
			RequireVerifiedAttestation: cfg.SecurityGuardRequireVerifiedAttestation,
		})
		if err != nil {
			return false, nil, err
		}
		if !pkgResp.Allowed {
			reasons = append(reasons, pkgResp.Reasons...)
		}
	}

	reasons = uniqueStringSlice(reasons)
	return len(reasons) == 0, reasons, nil
}

func newAnthropicToolStreamGuard(p *Proxy, ctx context.Context, cfg config.Config) *anthropicToolStreamGuard {
	return &anthropicToolStreamGuard{
		proxy: p,
		ctx:   ctx,
		cfg:   cfg,
	}
}

func newOpenAIToolStreamGuard(p *Proxy, ctx context.Context, cfg config.Config) *openAIToolStreamGuard {
	return &openAIToolStreamGuard{
		proxy:     p,
		ctx:       ctx,
		cfg:       cfg,
		toolCalls: make(map[int]*openAIBufferedToolCall),
	}
}

func (g *anthropicToolStreamGuard) Process(lines []string) ([][]string, error) {
	payload, eventType, err := parseSSEPayload(lines)
	if err != nil {
		if g.active {
			g.bufferedEvents = append(g.bufferedEvents, cloneEventLines(lines))
			return nil, nil
		}
		return [][]string{cloneEventLines(lines)}, nil
	}

	parsedType, _ := payload["type"].(string)

	if g.active {
		g.bufferedEvents = append(g.bufferedEvents, cloneEventLines(lines))
		if parsedType == "content_block_delta" {
			if delta, _ := payload["delta"].(map[string]interface{}); delta != nil {
				if deltaType, _ := delta["type"].(string); deltaType == "input_json_delta" {
					if partial, _ := delta["partial_json"].(string); partial != "" {
						g.partialInput.WriteString(partial)
					}
				}
			}
		}
		if parsedType != "content_block_stop" {
			return nil, nil
		}

		input := map[string]interface{}{}
		if raw := strings.TrimSpace(g.partialInput.String()); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		allowed, reasons, evalErr := g.proxy.evaluateToolCall(g.ctx, g.toolName, input, g.cfg)
		out := make([][]string, 0, len(g.bufferedEvents)+1)
		if evalErr != nil {
			return nil, evalErr
		}
		if allowed {
			out = append(out, g.bufferedEvents...)
		} else {
			out = append(out, buildAnthropicBlockedToolEvents(g.index, buildBlockedToolText(g.toolName, reasons))...)
			g.rewriteStopReason = true
		}
		g.active = false
		g.bufferedEvents = nil
		g.index = 0
		g.toolName = ""
		g.partialInput.Reset()
		return out, nil
	}

	if g.rewriteStopReason && parsedType == "message_delta" {
		if delta, _ := payload["delta"].(map[string]interface{}); delta != nil {
			if stopReason, _ := delta["stop_reason"].(string); stopReason == "tool_use" {
				delta["stop_reason"] = "end_turn"
				rewritten, mErr := marshalSSEEvent(eventType, payload)
				if mErr == nil {
					g.rewriteStopReason = false
					return [][]string{rewritten}, nil
				}
			}
		}
	}

	if parsedType == "content_block_start" {
		contentBlock, _ := payload["content_block"].(map[string]interface{})
		if blockType, _ := contentBlock["type"].(string); blockType == "tool_use" {
			g.active = true
			g.bufferedEvents = [][]string{cloneEventLines(lines)}
			g.index = intFromValue(payload["index"])
			g.toolName, _ = contentBlock["name"].(string)
			if input, _ := contentBlock["input"].(map[string]interface{}); len(input) > 0 {
				raw, _ := json.Marshal(input)
				g.partialInput.Write(raw)
			}
			return nil, nil
		}
	}

	return [][]string{cloneEventLines(lines)}, nil
}

func extractTextContent(v interface{}) (string, bool) {
	switch typed := v.(type) {
	case string:
		return typed, true
	case []interface{}:
		var parts []string
		for _, item := range typed {
			if block, ok := item.(map[string]interface{}); ok {
				for _, key := range []string{"text", "content"} {
					if text, ok := block[key].(string); ok && strings.TrimSpace(text) != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		if len(parts) == 0 {
			return "", false
		}
		return strings.Join(parts, "\n"), true
	default:
		return "", false
	}
}

func collectURLs(v interface{}, key string) []string {
	seen := make(map[string]struct{})
	var out []string
	var walk func(interface{}, string)
	walk = func(node interface{}, currentKey string) {
		switch typed := node.(type) {
		case string:
			if isURLKey(currentKey) {
				candidate := strings.TrimSpace(typed)
				if candidate != "" {
					if _, ok := seen[candidate]; !ok {
						seen[candidate] = struct{}{}
						out = append(out, candidate)
					}
				}
			}
			for _, match := range guard.ExtractURLs(typed) {
				if _, ok := seen[match]; ok {
					continue
				}
				seen[match] = struct{}{}
				out = append(out, match)
			}
		case []interface{}:
			for _, item := range typed {
				walk(item, currentKey)
			}
		case map[string]interface{}:
			for childKey, child := range typed {
				walk(child, childKey)
			}
		}
	}
	walk(v, key)
	return out
}

func isURLKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "url", "uri", "href", "link", "address":
		return true
	default:
		return false
	}
}

func findCommand(input map[string]interface{}) string {
	for _, key := range []string{"command", "cmd", "shell_command", "bash_command"} {
		if val, ok := input[key].(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

func detectSensitiveCommand(command string) []string {
	var reasons []string
	lower := strings.ToLower(command)
	networkMatch := sensitiveCommandPatterns["network exfiltration"].MatchString(lower)
	if sensitiveCommandPatterns["sensitive file access"].MatchString(lower) {
		reasons = append(reasons, "command accesses sensitive local files")
	}
	if networkMatch && sensitiveCommandPatterns["environment dump"].MatchString(lower) {
		reasons = append(reasons, "command combines environment dumping with network egress")
	}
	return uniqueStringSlice(reasons)
}

func buildBlockedToolText(toolName string, reasons []string) string {
	if strings.TrimSpace(toolName) == "" {
		toolName = "tool"
	}
	reasons = uniqueStringSlice(reasons)
	if len(reasons) == 0 {
		return fmt.Sprintf("glass-guard blocked unsafe %s execution.", toolName)
	}
	return fmt.Sprintf("glass-guard blocked unsafe %s execution: %s.", toolName, strings.Join(reasons, "; "))
}

func parseSSEPayload(lines []string) (map[string]interface{}, string, error) {
	eventType := ""
	dataLines := sseDataLines(lines)
	for _, line := range lines {
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
	}
	raw := strings.Join(dataLines, "\n")
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, eventType, err
	}
	return payload, eventType, nil
}

func sseDataLines(lines []string) []string {
	var dataLines []string
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ")
			dataLines = append(dataLines, data)
		}
	}
	return dataLines
}

func marshalSSEEvent(eventType string, payload map[string]interface{}) ([]string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, 2)
	if eventType != "" {
		lines = append(lines, "event: "+eventType)
	}
	lines = append(lines, "data: "+string(data))
	return lines, nil
}

func buildAnthropicBlockedToolEvents(index int, text string) [][]string {
	start := map[string]interface{}{
		"type":  "content_block_start",
		"index": index,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	delta := map[string]interface{}{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": text,
		},
	}
	stop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": index,
	}

	startLines, _ := marshalSSEEvent("content_block_start", start)
	deltaLines, _ := marshalSSEEvent("content_block_delta", delta)
	stopLines, _ := marshalSSEEvent("content_block_stop", stop)
	return [][]string{startLines, deltaLines, stopLines}
}

func cloneEventLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

func intFromValue(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}

func uniqueStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func (g *openAIToolStreamGuard) Process(lines []string) ([][]string, error) {
	dataLines := sseDataLines(lines)
	raw := strings.Join(dataLines, "\n")
	if strings.TrimSpace(raw) == "[DONE]" {
		if g.active {
			emitted, err := g.finalize()
			if err != nil {
				return nil, err
			}
			emitted = append(emitted, cloneEventLines(lines))
			return emitted, nil
		}
		return [][]string{cloneEventLines(lines)}, nil
	}

	payload, _, err := parseSSEPayload(lines)
	if err != nil {
		if g.active {
			g.bufferedEvents = append(g.bufferedEvents, cloneEventLines(lines))
			return nil, nil
		}
		return [][]string{cloneEventLines(lines)}, nil
	}

	if g.active {
		g.bufferedEvents = append(g.bufferedEvents, cloneEventLines(lines))
		g.update(payload)
		if g.shouldFinalize(payload) {
			return g.finalize()
		}
		return nil, nil
	}

	if g.hasToolCalls(payload) {
		g.active = true
		g.bufferedEvents = [][]string{cloneEventLines(lines)}
		g.update(payload)
		if g.shouldFinalize(payload) {
			return g.finalize()
		}
		return nil, nil
	}

	return [][]string{cloneEventLines(lines)}, nil
}

func (g *openAIToolStreamGuard) hasToolCalls(payload map[string]interface{}) bool {
	choices, _ := payload["choices"].([]interface{})
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		delta, _ := choice["delta"].(map[string]interface{})
		if toolCalls, _ := delta["tool_calls"].([]interface{}); len(toolCalls) > 0 {
			return true
		}
	}
	return false
}

func (g *openAIToolStreamGuard) shouldFinalize(payload map[string]interface{}) bool {
	choices, _ := payload["choices"].([]interface{})
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		if finishReason, _ := choice["finish_reason"].(string); finishReason == "tool_calls" || finishReason == "function_call" {
			return true
		}
	}
	return false
}

func (g *openAIToolStreamGuard) update(payload map[string]interface{}) {
	if id, _ := payload["id"].(string); id != "" {
		g.lastID = id
	}
	if model, _ := payload["model"].(string); model != "" {
		g.lastModel = model
	}
	if created, ok := payload["created"].(float64); ok {
		g.lastCreated = int64(created)
	}

	choices, _ := payload["choices"].([]interface{})
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]interface{})
		if !ok {
			continue
		}
		delta, _ := choice["delta"].(map[string]interface{})
		toolCalls, _ := delta["tool_calls"].([]interface{})
		for _, rawCall := range toolCalls {
			call, ok := rawCall.(map[string]interface{})
			if !ok {
				continue
			}
			index := intFromValue(call["index"])
			buf, ok := g.toolCalls[index]
			if !ok {
				buf = &openAIBufferedToolCall{index: index}
				g.toolCalls[index] = buf
			}
			if id, _ := call["id"].(string); id != "" {
				buf.id = id
			}
			functionObj, _ := call["function"].(map[string]interface{})
			if functionObj == nil {
				continue
			}
			if name, _ := functionObj["name"].(string); name != "" {
				buf.name = name
			}
			if args, _ := functionObj["arguments"].(string); args != "" {
				buf.args.WriteString(args)
			}
		}
	}
}

func (g *openAIToolStreamGuard) finalize() ([][]string, error) {
	keys := make([]int, 0, len(g.toolCalls))
	for index := range g.toolCalls {
		keys = append(keys, index)
	}
	sort.Ints(keys)

	var reasons []string
	allowAll := true
	for _, index := range keys {
		call := g.toolCalls[index]
		input := map[string]interface{}{}
		if raw := strings.TrimSpace(call.args.String()); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		allowed, toolReasons, err := g.proxy.evaluateToolCall(g.ctx, call.name, input, g.cfg)
		if err != nil {
			return nil, err
		}
		if !allowed {
			allowAll = false
			reasons = append(reasons, toolReasons...)
		}
	}

	var out [][]string
	if allowAll {
		out = append(out, g.bufferedEvents...)
	} else {
		out = append(out, buildOpenAIBlockedToolEvent(g.lastID, g.lastModel, g.lastCreated, reasons))
	}

	g.active = false
	g.bufferedEvents = nil
	g.toolCalls = make(map[int]*openAIBufferedToolCall)
	g.lastID = ""
	g.lastModel = ""
	g.lastCreated = 0
	return out, nil
}

func buildOpenAIBlockedToolEvent(id string, model string, created int64, reasons []string) []string {
	if id == "" {
		id = fmt.Sprintf("chatcmpl-guard-%d", time.Now().UnixNano())
	}
	if created == 0 {
		created = time.Now().Unix()
	}
	payload := map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"delta": map[string]interface{}{
					"role":    "assistant",
					"content": buildBlockedToolText("tool_call", reasons),
				},
				"finish_reason": "stop",
			},
		},
	}
	lines, _ := marshalSSEEvent("", payload)
	return lines
}
