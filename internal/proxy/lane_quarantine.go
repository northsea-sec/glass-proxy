package proxy

import (
	"sync"
	"time"
)

type laneQuarantineConfig struct {
	CreateThreshold int
	ReadThreshold   int
	StreakThreshold int
	Window          time.Duration
}

type laneQuarantineState struct {
	streak      int
	lastToxicAt time.Time
}

type laneQuarantineGuard struct {
	mu     sync.Mutex
	cfg    laneQuarantineConfig
	states map[string]laneQuarantineState
}

func newLaneQuarantineGuard(cfg laneQuarantineConfig) *laneQuarantineGuard {
	if cfg.CreateThreshold <= 0 || cfg.ReadThreshold < 0 || cfg.StreakThreshold <= 0 || cfg.Window <= 0 {
		return nil
	}
	return &laneQuarantineGuard{
		cfg:    cfg,
		states: make(map[string]laneQuarantineState),
	}
}

func (g *laneQuarantineGuard) Observe(convID string, at time.Time, shadowBacked bool, cacheCreate int, cacheRead int, isSubagent bool) (bool, int) {
	if g == nil || convID == "" {
		return false, 0
	}
	if at.IsZero() {
		at = time.Now()
	}

	toxic := shadowBacked && !isSubagent && cacheCreate >= g.cfg.CreateThreshold && cacheRead <= g.cfg.ReadThreshold

	g.mu.Lock()
	defer g.mu.Unlock()

	if !toxic {
		delete(g.states, convID)
		return false, 0
	}

	state := g.states[convID]
	if state.lastToxicAt.IsZero() || at.Sub(state.lastToxicAt) > g.cfg.Window {
		state.streak = 1
	} else {
		state.streak++
	}
	state.lastToxicAt = at

	if state.streak >= g.cfg.StreakThreshold {
		delete(g.states, convID)
		return true, state.streak
	}

	g.states[convID] = state
	return false, state.streak
}
