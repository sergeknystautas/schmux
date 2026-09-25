package dashboard

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func readChatPerformanceEvents(t *testing.T) []chatPerformanceEvent {
	t.Helper()
	path := filepath.Join(schmuxdir.Get(), "diagnostics", "chat-performance.jsonl")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []chatPerformanceEvent
	for _, line := range bytes.Split(bytes.TrimSpace(content), []byte{'\n'}) {
		var event chatPerformanceEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestChatPerformanceAppendsAcrossWrites(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.recordChatPerformance("history", []any{"session", "chat-1", "load_id", "load-1"})
	srv.recordChatPerformance("browser_load", []any{"session", "chat-1", "load_id", "load-1"})
	events := readChatPerformanceEvents(t)
	if len(events) != 2 || events[0].Kind != "history" || events[1].Kind != "browser_load" {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Data["load_id"] != "load-1" || events[1].Data["load_id"] != "load-1" {
		t.Errorf("load IDs = %v, %v", events[0].Data["load_id"], events[1].Data["load_id"])
	}
}
