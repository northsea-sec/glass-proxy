package proxy

import (
	"sync"
	"time"

	"proxy.local/app/internal/subagent"
)

const (
	agentToolRateWindow = 120 * time.Second
	agentToolRateMax    = 15
)

// agentToolRateLimiter tracks per-conversation tool-bearing subagent call
// frequency. When a conversation exceeds agentToolRateMax calls within
// agentToolRateWindow, new loop-prone subagent calls are blocked with a fake
// response.
type agentToolRateLimiter struct {
	mu    sync.Mutex
	calls map[string][]time.Time // convID -> timestamps
}

func newAgentToolRateLimiter() *agentToolRateLimiter {
	return &agentToolRateLimiter{calls: make(map[string][]time.Time)}
}

func isRateLimitedToolSubagent(info subagent.Classification) bool {
	if !info.HasTools {
		return false
	}
	return info.Type == subagent.TypeAgentTool || info.Type == subagent.TypeSmallSystem
}

// Record adds a call timestamp and returns true if the call should be blocked.
func (rl *agentToolRateLimiter) Record(convID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-agentToolRateWindow)

	// Prune old entries
	existing := rl.calls[convID]
	pruned := existing[:0]
	for _, t := range existing {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}

	if len(pruned) >= agentToolRateMax {
		rl.calls[convID] = pruned
		return true // blocked
	}

	rl.calls[convID] = append(pruned, now)
	return false
}

// ResetConversation clears the rate limit state for a conversation (on new user message).
func (rl *agentToolRateLimiter) ResetConversation(convID string) {
	rl.mu.Lock()
	delete(rl.calls, convID)
	rl.mu.Unlock()
}
