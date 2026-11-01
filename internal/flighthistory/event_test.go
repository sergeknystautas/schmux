package flighthistory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testEvent(id string) Event {
	return Event{
		SchemaVersion:  SchemaVersion,
		EventID:        id,
		RecordedAt:     time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC),
		Change:         ChangeUpdated,
		Actor:          ActorUser,
		Cause:          "edit_flight",
		SnapshotSHA256: "abc",
		FlightID:       "fl-1",
		RowSHA256:      "row",
		State:          RowState{WorkflowState: "planning", PhaseVisit: 2},
	}
}

// A line from another schema version is evidence the review must see, not an
// event the manager may act on: it is retained and reported as malformed.
func TestReadEventsReportsOtherSchemaVersionsAsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	old := testEvent("fe-old")
	old.SchemaVersion = 1
	oldLine, _ := json.Marshal(old)
	current, _ := json.Marshal(testEvent("fe-new"))
	content := append(append(oldLine, '\n'), append(current, '\n')...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	events, malformed, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 1 || events[0].EventID != "fe-new" {
		t.Fatalf("events = %+v, want only the current-schema event", events)
	}
	if len(malformed) != 1 || malformed[0].Line != 1 || !bytes.Equal(malformed[0].Bytes, append(oldLine, '\n')) {
		t.Fatalf("malformed = %+v, want line 1 retained verbatim", malformed)
	}
}

func TestLedgerAppendWritesCompleteJSONLWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history", "events.jsonl")
	l := NewLedger(path, time.Now, func() string { return "fe-x" })

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("NewLedger created the file before the first append: %v", err)
	}

	if err := l.Append([]Event{testEvent("fe-1"), testEvent("fe-2")}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Append([]Event{testEvent("fe-3")}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(data, []byte("\n")) || bytes.Count(data, []byte("\n")) != 3 {
		t.Fatalf("want three newline-terminated lines, got %q", data)
	}
	events, malformed, err := ReadEvents(path)
	if err != nil || len(malformed) != 0 {
		t.Fatalf("ReadEvents err=%v malformed=%+v", err, malformed)
	}
	if len(events) != 3 || events[2].EventID != "fe-3" {
		t.Fatalf("events = %+v", events)
	}
}

func TestLedgerAppendFillsMissingIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	l := NewLedger(path, func() time.Time { return now }, func() string { return "fe-gen" })

	ev := testEvent("")
	ev.RecordedAt = time.Time{}
	ev.SchemaVersion = 0
	if err := l.Append([]Event{ev}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events, _, err := ReadEvents(path)
	if err != nil || len(events) != 1 {
		t.Fatalf("ReadEvents err=%v events=%+v", err, events)
	}
	got := events[0]
	if got.EventID != "fe-gen" || !got.RecordedAt.Equal(now) || got.SchemaVersion != SchemaVersion {
		t.Errorf("identity not filled: %+v", got)
	}
}

func TestReadEventsRetainsMalformedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	good1, _ := json.Marshal(testEvent("fe-1"))
	good2, _ := json.Marshal(testEvent("fe-2"))
	content := append(append(append(good1, '\n'), []byte("not-json\n")...), append(good2, '\n')...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	events, malformed, err := ReadEvents(path)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 || len(malformed) != 1 || malformed[0].Line != 2 {
		t.Fatalf("events=%d malformed=%+v", len(events), malformed)
	}
	if string(malformed[0].Bytes) != "not-json\n" {
		t.Fatalf("malformed bytes=%q", malformed[0].Bytes)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, content) {
		t.Fatalf("ReadEvents modified the file:\nbefore %q\nafter  %q", content, after)
	}
}

func TestReadEventsKeepsTrailingPartialLineAsMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	good, _ := json.Marshal(testEvent("fe-1"))
	content := append(append(good, '\n'), []byte(`{"event_id":"fe-2"`)...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	events, malformed, err := ReadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(malformed) != 1 || malformed[0].Line != 2 {
		t.Fatalf("events=%d malformed=%+v", len(events), malformed)
	}
	if string(malformed[0].Bytes) != `{"event_id":"fe-2"` {
		t.Errorf("malformed bytes=%q", malformed[0].Bytes)
	}
}

func TestReadEventsMissingFileReturnsEmpty(t *testing.T) {
	events, malformed, err := ReadEvents(filepath.Join(t.TempDir(), "nope", "events.jsonl"))
	if err != nil || len(events) != 0 || len(malformed) != 0 {
		t.Fatalf("events=%v malformed=%v err=%v", events, malformed, err)
	}
}

func TestLedgerAppendAfterPartialLineStartsFreshLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	partial := []byte(`{"event_id":"fe-torn"`)
	if err := os.WriteFile(path, partial, 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(path, time.Now, func() string { return "fe-x" })
	if err := l.Append([]Event{testEvent("fe-1")}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	events, malformed, err := ReadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID != "fe-1" {
		t.Fatalf("new event was swallowed by the torn line: events=%+v", events)
	}
	if len(malformed) != 1 || malformed[0].Line != 1 {
		t.Fatalf("torn line not retained: %+v", malformed)
	}
	data, _ := os.ReadFile(path)
	if !bytes.HasPrefix(data, partial) {
		t.Fatalf("torn bytes were modified: %q", data)
	}
}

func TestJSONLinesHelpersAppendAndReadAnyRecordShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "records.jsonl")
	type record struct {
		N int `json:"n"`
	}
	if err := AppendJSONLines(path, []any{record{1}, record{2}}); err != nil {
		t.Fatalf("AppendJSONLines: %v", err)
	}
	if mode := modeOf(t, path); mode != 0o600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
	appendTorn := []byte("{\"n\":")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(appendTorn); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := AppendJSONLines(path, []any{record{3}}); err != nil {
		t.Fatal(err)
	}

	var got []record
	malformed, err := ReadJSONLines(path, func(line []byte) error {
		var r record
		if err := json.Unmarshal(line, &r); err != nil {
			return err
		}
		got = append(got, r)
		return nil
	})

	if err != nil || len(got) != 3 || got[2].N != 3 || len(malformed) != 1 || malformed[0].Line != 3 {
		t.Fatalf("got=%v malformed=%+v err=%v", got, malformed, err)
	}
}
