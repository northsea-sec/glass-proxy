package glass

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLogFinalPrefixDiagnosticPersistsLatestAndHistory(t *testing.T) {
	t.Parallel()

	cfg := DefaultGlassConfig()
	cfg.ShadowDir = t.TempDir()
	engine := NewEngine(cfg)

	body1 := map[string]interface{}{
		"system":   mainSessionSystem(),
		"tools":    sharedTools(),
		"messages": []interface{}{userTextMessage("hello"), assistantTextMessage("world"), userTextMessage("next")},
	}
	engine.logFinalPrefixDiagnostic("conv", body1, 2, outboundPrefixDiagnosticMeta{
		RequestKey:         "req-1",
		PrevAnchor:         -1,
		InjectedReferences: true,
		ReferenceInsertAt:  1,
		CacheMessages:      3,
		EvictedCount:       2,
		BatchCount:         1,
	})

	body2 := map[string]interface{}{
		"system":   mainSessionSystem(),
		"tools":    sharedTools(),
		"messages": []interface{}{userTextMessage("hello changed"), assistantTextMessage("world"), userTextMessage("next")},
	}
	engine.logFinalPrefixDiagnostic("conv", body2, 2, outboundPrefixDiagnosticMeta{
		RequestKey:         "req-2",
		PrevAnchor:         1,
		InjectedReferences: true,
		ReferenceInsertAt:  1,
		CacheMessages:      3,
		EvictedCount:       2,
		BatchCount:         1,
	})

	latestPath, eventsPath := outboundPrefixDiagnosticPaths(cfg.ShadowDir, "conv")
	latestData, err := os.ReadFile(latestPath)
	if err != nil {
		t.Fatalf("read latest snapshot: %v", err)
	}

	var latest outboundPrefixDiagnosticEvent
	if err := json.Unmarshal(latestData, &latest); err != nil {
		t.Fatalf("unmarshal latest snapshot: %v", err)
	}
	if got, want := latest.ChangeKind, "changed"; got != want {
		t.Fatalf("expected latest change kind %q, got %q", want, got)
	}
	if got, want := latest.Divergence, "msg[0]"; got != want {
		t.Fatalf("expected divergence %q, got %q", want, got)
	}
	if latest.PrevHash == "" || latest.PrevHash == latest.Snapshot.Hash {
		t.Fatalf("expected previous hash to differ, got prev=%q current=%q", latest.PrevHash, latest.Snapshot.Hash)
	}
	if got, want := latest.Meta.RequestKey, "req-2"; got != want {
		t.Fatalf("expected request key %q, got %q", want, got)
	}
	if got, want := latest.Meta.VisibleMessages, 3; got != want {
		t.Fatalf("expected visible messages %d, got %d", want, got)
	}
	if got, want := latest.TailChangeKind, "same"; got != want {
		t.Fatalf("expected latest tail change kind %q, got %q", want, got)
	}
	if !latest.Meta.InjectedReferences || latest.Meta.ReferenceInsertAt != 1 {
		t.Fatalf("expected injected reference metadata to persist, got %+v", latest.Meta)
	}

	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatalf("open event history: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var events []outboundPrefixDiagnosticEvent
	for scanner.Scan() {
		var event outboundPrefixDiagnosticEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal event line: %v", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan event history: %v", err)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("expected %d events, got %d", want, got)
	}
	if got, want := events[0].ChangeKind, "init"; got != want {
		t.Fatalf("expected first event kind %q, got %q", want, got)
	}
	if got, want := events[0].TailChangeKind, "init"; got != want {
		t.Fatalf("expected first tail event kind %q, got %q", want, got)
	}
	if got, want := events[1].ChangeKind, "changed"; got != want {
		t.Fatalf("expected second event kind %q, got %q", want, got)
	}
	if got, want := filepath.Base(latestPath), outboundPrefixLatestFile; got != want {
		t.Fatalf("expected latest file name %q, got %q", want, got)
	}
	if got, want := filepath.Base(eventsPath), outboundPrefixEventsFile; got != want {
		t.Fatalf("expected events file name %q, got %q", want, got)
	}
}
