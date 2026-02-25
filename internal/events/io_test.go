package events

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	err := AppendEvent(path, StatusEvent{
		Ts: "2026-02-18T14:30:00Z", Type: "status",
		State: "working", Message: "doing stuff",
	})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("file is empty")
	}
	// Must end with newline
	if data[len(data)-1] != '\n' {
		t.Error("event line does not end with newline")
	}
}

func TestAppendMultipleEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:31:00Z", Type: "status", State: "completed"})

	events, err := ReadEvents(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestReadLastByType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working", Message: "first"})
	AppendEvent(path, FailureEvent{Ts: "2026-02-18T14:30:30Z", Type: "failure", Tool: "Bash"})
	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:31:00Z", Type: "status", State: "completed", Message: "done"})

	raw, data, err := ReadLastByType(path, "status")
	if err != nil {
		t.Fatal(err)
	}
	if raw.Ts != "2026-02-18T14:31:00Z" {
		t.Errorf("ts = %v, want 2026-02-18T14:31:00Z", raw.Ts)
	}
	if data == nil {
		t.Fatal("data is nil")
	}
}

func TestReadLastByTypeNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})

	_, _, err := ReadLastByType(path, "reflection")
	if err == nil {
		t.Error("expected error for missing type")
	}
}

func TestReadEventsWithFilter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	AppendEvent(path, StatusEvent{Ts: "2026-02-18T14:30:00Z", Type: "status", State: "working"})
	AppendEvent(path, FailureEvent{Ts: "2026-02-18T14:30:30Z", Type: "failure", Tool: "Bash"})
	AppendEvent(path, ReflectionEvent{Ts: "2026-02-18T14:31:00Z", Type: "reflection", Text: "test"})

	loreTypes := map[string]bool{"failure": true, "reflection": true, "friction": true}
	events, err := ReadEvents(path, func(raw RawEvent) bool {
		return loreTypes[raw.Type]
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2 (failure + reflection)", len(events))
	}
}

func TestReadEventsNonexistentFile(t *testing.T) {
	events, err := ReadEvents("/nonexistent/path.jsonl", nil)
	if err != nil {
		t.Fatal("should not error for missing file")
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}
