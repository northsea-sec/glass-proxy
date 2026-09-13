package proxy

import (
	"testing"
	"time"
)

func TestLaneQuarantineGuardTripsAfterRepeatedToxicPostOverflowRequests(t *testing.T) {
	guard := newLaneQuarantineGuard(laneQuarantineConfig{
		CreateThreshold: 120000,
		ReadThreshold:   15000,
		StreakThreshold: 2,
		Window:          5 * time.Minute,
	})
	now := time.Now()

	reset, streak := guard.Observe("conv", now, true, 147000, 10057, false)
	if reset || streak != 1 {
		t.Fatalf("expected first toxic request to start streak 1, got reset=%t streak=%d", reset, streak)
	}

	reset, streak = guard.Observe("conv", now.Add(30*time.Second), true, 148000, 10057, false)
	if !reset || streak != 2 {
		t.Fatalf("expected second toxic request to trip guard, got reset=%t streak=%d", reset, streak)
	}

	reset, streak = guard.Observe("conv", now.Add(60*time.Second), true, 148000, 10057, false)
	if reset || streak != 1 {
		t.Fatalf("expected streak to restart after quarantine, got reset=%t streak=%d", reset, streak)
	}
}

func TestLaneQuarantineGuardIgnoresHealthyOrNonShadowBackedRequests(t *testing.T) {
	guard := newLaneQuarantineGuard(laneQuarantineConfig{
		CreateThreshold: 120000,
		ReadThreshold:   15000,
		StreakThreshold: 2,
		Window:          5 * time.Minute,
	})
	now := time.Now()

	reset, streak := guard.Observe("conv", now, false, 147000, 10057, false)
	if reset || streak != 0 {
		t.Fatalf("expected non-shadow-backed lane to be ignored, got reset=%t streak=%d", reset, streak)
	}

	reset, streak = guard.Observe("conv", now, true, 147000, 10057, true)
	if reset || streak != 0 {
		t.Fatalf("expected subagent lane to be ignored, got reset=%t streak=%d", reset, streak)
	}

	reset, streak = guard.Observe("conv", now, true, 147000, 10057, false)
	if reset || streak != 1 {
		t.Fatalf("expected toxic request to start streak, got reset=%t streak=%d", reset, streak)
	}

	reset, streak = guard.Observe("conv", now.Add(30*time.Second), true, 359, 136833, false)
	if reset || streak != 0 {
		t.Fatalf("expected healthy recovery to clear streak, got reset=%t streak=%d", reset, streak)
	}
}
