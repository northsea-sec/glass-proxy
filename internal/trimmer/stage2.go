// stage2.go — Stage 2 message dropping algorithm.
// When context exceeds trigger threshold, drops messages to target threshold.
// Uses actual API input_tokens from previous response for accurate decisions.
//
// Key improvements over naive sequential dropping:
//   - Token-weighted pair dropping: drops highest-cost pairs first (fewer drops, less orphan damage)
//   - Persistent drop tracking: re-drops previously dropped messages (CC re-sends full history)
//   - Cooldown: prevents rapid-fire triggers (30s default, emergency bypass at 190K)
//   - Cycle detector: purges stale hashes if re-drops fire N consecutive calls
package trimmer

import (
	"log"
	"sort"
	"time"

	"proxy.local/app/internal/config"
)

const (
	defaultTrigger       = 180000
	defaultTarget        = 140000
	defaultKeepMin       = 6
	defaultCooldownSec   = 30
	defaultEmergencyOver = 10000 // trigger + this = emergency threshold
)

// lastFireTimes tracks per-conversation Stage 2 fire timestamps for cooldown.
var lastFireTimes = make(map[string]time.Time)

// DropOldMessages is Stage 2: drop oldest messages entirely when context grows too large.
func (t *Trimmer) DropOldMessages(body map[string]interface{}, cfg config.Config) int {
	return t.dropOldMessages(body, cfg)
}

func (t *Trimmer) dropOldMessages(body map[string]interface{}, cfg config.Config) int {
	msgs, ok := body["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		return 0
	}

	convID := ConvFingerprint(body)

	// --- Phase 0: Persistent re-drops (re-apply previously dropped messages) ---
	kr := cfg.TrimKeepRecent
	if kr == 0 {
		kr = 20
	}
	redropIndices := t.pDrops.RedropsFor(convID, msgs, kr)
	if len(redropIndices) > 0 {
		// Remove in reverse order to preserve indices
		sort.Sort(sort.Reverse(sort.IntSlice(redropIndices)))
		for _, idx := range redropIndices {
			if idx < len(msgs) {
				msgs = append(msgs[:idx], msgs[idx+1:]...)
			}
		}
		// Ensure msg[0] is user role after re-drop
		for len(msgs) > 0 {
			if first, ok := msgs[0].(map[string]interface{}); ok {
				if role, _ := first["role"].(string); role == "user" {
					break
				}
			}
			msgs = msgs[1:]
		}
		body["messages"] = msgs
		log.Printf("[TRIM] Persistent re-drop: removed %d messages (conv=%s)", len(redropIndices), convID[:12])
	}

	// --- Phase 1: Decide whether to trigger new drops ---
	t.mu.Lock()
	lastInput := t.lastInput[convID]
	t.mu.Unlock()

	trigger := cfg.DropTriggerTokens
	if trigger == 0 {
		trigger = defaultTrigger
	}

	var currentTokens int
	if lastInput > 0 {
		currentTokens = lastInput
	} else {
		currentTokens = estimateTokens(body)
	}

	if currentTokens < trigger {
		return len(redropIndices) * 100 // estimated savings from re-drops only
	}

	// --- Cooldown check ---
	cooldownSec := cfg.Stage2CooldownSec
	if cooldownSec == 0 {
		cooldownSec = defaultCooldownSec
	}
	emergencyTokens := cfg.Stage2EmergencyTokens
	if emergencyTokens == 0 {
		emergencyTokens = trigger + defaultEmergencyOver
	}

	lastFire, hasFired := lastFireTimes[convID]
	if hasFired && time.Since(lastFire) < time.Duration(cooldownSec)*time.Second {
		if currentTokens < emergencyTokens {
			log.Printf("[TRIM] Stage 2 COOLDOWN: conv=%s last_fire=%vs ago < %ds, skipping",
				convID[:12], time.Since(lastFire).Seconds(), cooldownSec)
			return len(redropIndices) * 100
		}
		log.Printf("[TRIM] Stage 2 EMERGENCY: conv=%s tokens=%d >= emergency=%d, bypassing cooldown",
			convID[:12], currentTokens, emergencyTokens)
	}
	lastFireTimes[convID] = time.Now()

	// --- Phase 2: Token-weighted pair dropping ---
	target := cfg.DropTargetTokens
	if target == 0 {
		target = defaultTarget
	}
	keepMin := cfg.DropKeepMinMessages
	if keepMin == 0 {
		keepMin = defaultKeepMin
	}

	overshoot := currentTokens - target
	minRemaining := keepMin
	if kr+5 > minRemaining {
		minRemaining = kr + 5
	}

	droppableEnd := len(msgs) - minRemaining
	if droppableEnd <= 0 {
		return len(redropIndices) * 100
	}

	// Build list of droppable pairs with their estimated token cost
	type pairCost struct {
		Index int
		Cost  int
		Size  int // 1 or 2 messages
	}
	var pairs []pairCost
	i := 0
	for i < droppableEnd && i+1 < len(msgs) {
		m0, _ := msgs[i].(map[string]interface{})
		m1, _ := msgs[i+1].(map[string]interface{})
		r0, _ := m0["role"].(string)
		r1, _ := m1["role"].(string)
		if r0 != r1 && i+1 < droppableEnd {
			cost := estimateTokens(m0) + estimateTokens(m1)
			pairs = append(pairs, pairCost{Index: i, Cost: cost, Size: 2})
			i += 2
		} else {
			cost := estimateTokens(m0)
			pairs = append(pairs, pairCost{Index: i, Cost: cost, Size: 1})
			i++
		}
	}

	// Sort by token cost descending — drop largest pairs first
	sort.Slice(pairs, func(a, b int) bool {
		return pairs[a].Cost > pairs[b].Cost
	})

	// Collect indices to drop
	indexSet := make(map[int]bool)
	tokensFreed := 0
	for _, p := range pairs {
		if tokensFreed >= overshoot {
			break
		}
		if len(msgs)-len(indexSet) <= minRemaining {
			break
		}
		for offset := 0; offset < p.Size; offset++ {
			indexSet[p.Index+offset] = true
		}
		tokensFreed += p.Cost
	}

	if len(indexSet) == 0 {
		return len(redropIndices) * 100
	}

	// Record hashes BEFORE removal (messages still in slice)
	var dropIndices []int
	for idx := range indexSet {
		dropIndices = append(dropIndices, idx)
	}
	sort.Ints(dropIndices)
	t.pDrops.RecordDrops(convID, msgs, dropIndices)

	// Remove in reverse order
	sort.Sort(sort.Reverse(sort.IntSlice(dropIndices)))
	for _, idx := range dropIndices {
		if idx < len(msgs) {
			msgs = append(msgs[:idx], msgs[idx+1:]...)
		}
	}

	// Ensure msg[0] is user role
	for len(msgs) > keepMin {
		if first, ok := msgs[0].(map[string]interface{}); ok {
			if role, _ := first["role"].(string); role == "user" {
				break
			}
		}
		msgs = msgs[1:]
	}

	body["messages"] = msgs

	// Reset watermark after drop
	t.mu.Lock()
	delete(t.watermarks, convID)
	t.mu.Unlock()

	log.Printf("[TRIM] Stage 2: dropped %d messages (~%d tokens freed, conv=%s)",
		len(indexSet), tokensFreed, convID[:12])
	t.stats.TotalDropped += len(indexSet)

	return tokensFreed
}
