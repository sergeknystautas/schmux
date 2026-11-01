package flighthistory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testReconciliation(id string) ReconciliationEvent {
	return ReconciliationEvent{
		EventID:      id,
		RecordedAt:   time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
		Kind:         ReconcileResourceMissing,
		FlightID:     "fl-1",
		ResourceKind: "session",
		ResourceID:   "s-1",
		LastKnown:    json.RawMessage(`{"role":"planner"}`),
		Source:       "unknown",
	}
}

func TestReconciliationWriterAppendsCompleteJSONLWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history", "reconciliation.jsonl")
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	w := NewReconciliationWriter(path, func() time.Time { return now }, func() string { return "re-gen" })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("NewReconciliationWriter created the file before the first append: %v", err)
	}

	first := testReconciliation("")
	first.RecordedAt = time.Time{}
	if err := w.Append([]ReconciliationEvent{first, testReconciliation("re-2")}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.Count(data, []byte("\n")) != 2 {
		t.Fatalf("want two newline-terminated lines, got %q", data)
	}
	events, malformed, err := ReadReconciliation(path)
	if err != nil || len(malformed) != 0 || len(events) != 2 {
		t.Fatalf("events=%d malformed=%v err=%v", len(events), malformed, err)
	}
	if events[0].EventID != "re-gen" || !events[0].RecordedAt.Equal(now) || events[0].SchemaVersion != SchemaVersion {
		t.Errorf("identity not filled: %+v", events[0])
	}
	if events[1].EventID != "re-2" {
		t.Errorf("explicit identity overwritten: %+v", events[1])
	}
}

func TestReadReconciliationRetainsMalformedLinesWithoutModifyingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reconciliation.jsonl")
	good, _ := json.Marshal(testReconciliation("re-1"))
	content := append(append(good, '\n'), []byte("garbage\n")...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	events, malformed, err := ReadReconciliation(path)

	if err != nil || len(events) != 1 || len(malformed) != 1 || malformed[0].Line != 2 || string(malformed[0].Bytes) != "garbage\n" {
		t.Fatalf("events=%d malformed=%+v err=%v", len(events), malformed, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, content) {
		t.Errorf("file modified: %q", after)
	}
}

func TestReadReconciliationMissingFileReturnsEmpty(t *testing.T) {
	events, malformed, err := ReadReconciliation(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || len(events) != 0 || len(malformed) != 0 {
		t.Fatalf("events=%v malformed=%v err=%v", events, malformed, err)
	}
}
