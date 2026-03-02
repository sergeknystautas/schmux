package actions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectIntentSignals_Empty(t *testing.T) {
	dir := t.TempDir()
	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals, got %d", len(signals))
	}
}

func TestCollectIntentSignals_WithEvents(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-25T11:00:00Z","type":"status","state":"completed","message":"done"}
{"ts":"2026-02-25T12:00:00Z","type":"status","state":"working","intent":"fix lint errors in src/"}
{"ts":"2026-02-26T09:00:00Z","type":"status","state":"working","intent":"add tests for auth"}
`
	os.WriteFile(filepath.Join(eventsDir, "session1.jsonl"), []byte(data), 0644)

	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(signals))
	}

	// Most frequent first.
	if signals[0].Text != "fix lint errors in src/" {
		t.Errorf("first signal = %q, want 'fix lint errors in src/'", signals[0].Text)
	}
	if signals[0].Count != 2 {
		t.Errorf("first count = %d, want 2", signals[0].Count)
	}
	if signals[1].Text != "add tests for auth" {
		t.Errorf("second signal = %q", signals[1].Text)
	}
	if signals[1].Count != 1 {
		t.Errorf("second count = %d, want 1", signals[1].Count)
	}
}

func TestCollectIntentSignals_Deduplication(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	for _, dir := range []string{dir1, dir2} {
		eventsDir := filepath.Join(dir, ".schmux", "events")
		os.MkdirAll(eventsDir, 0755)
		os.WriteFile(filepath.Join(eventsDir, "s.jsonl"),
			[]byte(`{"ts":"2026-02-25T10:00:00Z","type":"status","state":"working","intent":"shared intent"}`+"\n"), 0644)
	}

	signals, err := CollectIntentSignals([]string{dir1, dir2})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 deduplicated signal, got %d", len(signals))
	}
	if signals[0].Count != 2 {
		t.Errorf("count = %d, want 2", signals[0].Count)
	}
}

func TestCollectIntentSignals_SkipsNonStatus(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)

	data := `{"ts":"2026-02-25T10:00:00Z","type":"failure","tool":"bash","error":"oops"}
{"ts":"2026-02-25T11:00:00Z","type":"reflection","text":"learned"}
`
	os.WriteFile(filepath.Join(eventsDir, "s.jsonl"), []byte(data), 0644)

	signals, err := CollectIntentSignals([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals (no status events with intent), got %d", len(signals))
	}
}
