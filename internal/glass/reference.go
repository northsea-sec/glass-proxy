package glass

import (
	"fmt"
)

const referenceAssistantText = "Understood. I have access to the archived conversation history and will reread it when exact prior details are needed."
const referenceUserTemplate = "CONTEXT NOTE: Earlier conversation history has been evicted from the visible window.\nDurable facts from evicted history may appear in the system prompt under '# Durable operational context'. A recent 'Operational Context' note may also appear later in the conversation.\nRelevant prior history is saved to: %s\nDo not infer missing details from bookmarks alone. Use the Read tool to inspect that file before resuming older work."

// ensureReferencePair freezes the injected reference pair for an evicted
// session. Once a conversation has crossed into shadow-backed mode, the
// injected prefix bytes must remain unchanged across later eviction batches or
// Anthropic will see a brand-new prefix again.
func ensureReferencePair(state *SessionState, shadowDir string) {
	if state == nil || state.EvictedCount == 0 {
		return
	}
	archivePath := referenceArchivePath(state, shadowDir)
	sanitizedUser := fmt.Sprintf(referenceUserTemplate, archivePath)
	if state.ReferenceUser != "" && state.ReferenceAsst != "" {
		if state.ReferenceBatch == 0 {
			state.ReferenceBatch = state.BatchCount
		}
		if referenceTextNeedsRefresh(state.ReferenceUser) && sanitizedUser != "" {
			state.ReferenceUser = sanitizedUser
		}
		return
	}

	state.ReferenceUser = sanitizedUser
	state.ReferenceAsst = referenceAssistantText
	state.ReferenceBatch = state.BatchCount
}

// buildReferenceMessages creates the frozen user+assistant pair injected at the
// start of retained messages when evictions have occurred.
func buildReferenceMessages(state *SessionState, shadowDir string) (user, assistant map[string]interface{}) {
	if state == nil || state.EvictedCount == 0 {
		return nil, nil
	}
	ensureReferencePair(state, shadowDir)
	if state.ReferenceUser == "" || state.ReferenceAsst == "" {
		return nil, nil
	}

	user = map[string]interface{}{
		"role": "user",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": state.ReferenceUser,
			},
		},
	}

	assistant = map[string]interface{}{
		"role": "assistant",
		"content": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": state.ReferenceAsst,
			},
		},
	}

	return user, assistant
}

func referenceTextNeedsRefresh(text string) bool {
	return shouldSuppressBootstrapText("", text)
}

func referenceArchivePath(state *SessionState, shadowDir string) string {
	if state != nil && state.ChapterMDPath != "" {
		return state.ChapterMDPath
	}
	return fmt.Sprintf("%s/%s/shadow.md", shadowDir, state.ConvID)
}

// injectReferenceMessages inserts the reference pair at the beginning of messages
// (after any anchor messages). When the outbound request must still end with a
// user message, the injected user+assistant pair is never allowed to append at
// the tail because that would force an assistant-final request.
func injectReferenceMessages(body map[string]interface{}, state *SessionState, cfg GlassConfig, requireUserFinal bool) bool {
	injected, _ := injectReferenceMessagesAt(body, state, cfg, requireUserFinal, cfg.AnchorKeepMsgs)
	return injected
}

// injectReferenceMessagesAt inserts the reference pair at a preferred boundary
// and returns the actual insertion point after structural safety adjustments.
func injectReferenceMessagesAt(body map[string]interface{}, state *SessionState, cfg GlassConfig, requireUserFinal bool, preferredInsertAt int) (bool, int) {
	if state.EvictedCount == 0 {
		return false, -1
	}

	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return false, -1
	}

	user, assistant := buildReferenceMessages(state, cfg.ShadowDir)
	if user == nil {
		return false, -1
	}

	// Insert after anchor messages
	insertAt := preferredInsertAt
	if insertAt < 0 {
		insertAt = cfg.AnchorKeepMsgs
	}
	if insertAt > len(msgs) {
		insertAt = 0
	}
	insertAt = adjustReferenceInsertAt(msgs, insertAt, requireUserFinal)

	for _, candidate := range candidateReferenceInsertPositions(msgs, insertAt, requireUserFinal) {
		newMsgs := buildReferenceInsertedMessages(msgs, candidate, user, assistant)
		if issues := validateRequestMessages(newMsgs, requireUserFinal); len(issues) == 0 {
			body["messages"] = newMsgs
			return true, candidate
		}
	}

	body["messages"] = buildReferenceInsertedMessages(msgs, insertAt, user, assistant)
	return true, insertAt
}

func buildReferenceInsertedMessages(msgs []interface{}, insertAt int, user, assistant map[string]interface{}) []interface{} {
	newMsgs := make([]interface{}, 0, len(msgs)+2)
	newMsgs = append(newMsgs, msgs[:insertAt]...)
	newMsgs = append(newMsgs, user, assistant)
	newMsgs = append(newMsgs, msgs[insertAt:]...)
	return newMsgs
}

func candidateReferenceInsertPositions(msgs []interface{}, preferredInsertAt int, requireUserFinal bool) []int {
	if preferredInsertAt < 0 {
		preferredInsertAt = 0
	}
	if preferredInsertAt > len(msgs) {
		preferredInsertAt = len(msgs)
	}

	candidates := make([]int, 0, len(msgs)+1)
	seen := make(map[int]bool)
	appendCandidate := func(pos int) {
		if pos < 0 || pos > len(msgs) || seen[pos] || !isSafeReferenceInsertBoundary(msgs, pos, requireUserFinal) {
			return
		}
		seen[pos] = true
		candidates = append(candidates, pos)
	}

	appendCandidate(preferredInsertAt)
	for delta := 1; delta <= len(msgs); delta++ {
		appendCandidate(preferredInsertAt + delta)
		appendCandidate(preferredInsertAt - delta)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, adjustReferenceInsertAt(msgs, preferredInsertAt, requireUserFinal))
	}
	return candidates
}

func adjustReferenceInsertAt(msgs []interface{}, insertAt int, requireUserFinal bool) int {
	if insertAt <= 0 || insertAt >= len(msgs) {
		return insertAt
	}

	if isSafeReferenceInsertBoundary(msgs, insertAt, requireUserFinal) {
		return insertAt
	}

	for i := insertAt + 1; i <= len(msgs); i++ {
		if isSafeReferenceInsertBoundary(msgs, i, requireUserFinal) {
			return i
		}
	}
	for i := insertAt - 1; i >= 0; i-- {
		if isSafeReferenceInsertBoundary(msgs, i, requireUserFinal) {
			return i
		}
	}
	return 0
}

func isSafeReferenceInsertBoundary(msgs []interface{}, insertAt int, requireUserFinal bool) bool {
	if insertAt < 0 || insertAt > len(msgs) {
		return false
	}
	if insertAt == 0 {
		return true
	}
	if insertAt == len(msgs) {
		return !requireUserFinal
	}

	prevMsg, okPrev := msgs[insertAt-1].(map[string]interface{})
	currMsg, okCurr := msgs[insertAt].(map[string]interface{})
	if !okPrev || !okCurr {
		return false
	}

	prevRole, _ := prevMsg["role"].(string)
	currRole, _ := currMsg["role"].(string)

	// Injected pair is user+assistant, so the surrounding boundary must be
	// assistant | [user, assistant] | user to preserve alternation.
	if prevRole != "assistant" || currRole != "user" {
		return false
	}

	// Never split an assistant tool_use from the user tool_result that satisfies it.
	prevUses := collectToolUseSet(prevMsg["content"])
	currResults := collectToolResultIDs(currMsg["content"])
	if len(prevUses) == 0 || len(currResults) == 0 {
		return true
	}
	for id := range currResults {
		if prevUses[id] {
			return false
		}
	}
	return true
}

// adjustBreakpointForReferenceInjection translates a pre-injection Build() anchor
// to the final request slice that includes the injected reference pair.
func adjustBreakpointForReferenceInjection(anchorIdx int, injected bool, insertAt int, finalMsgCount int) int {
	if !injected || anchorIdx < 0 {
		return anchorIdx
	}

	originalCount := finalMsgCount - 2
	if insertAt > originalCount {
		insertAt = 0
	}
	if anchorIdx >= insertAt {
		return anchorIdx + 2
	}
	return anchorIdx
}
