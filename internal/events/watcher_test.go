package events

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testHandler struct {
	mu     sync.Mutex
	events []RawEvent
	data   [][]byte
}

func (h *testHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, raw)
	cp := make([]byte, len(data))
	copy(cp, data)
	h.data = append(h.data, cp)
}

func (h *testHandler) getEvents() []RawEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]RawEvent, len(h.events))
	copy(cp, h.events)
	return cp
}

func TestEventWatcherDispatch(t *testing.T) {
	dir := t.TempDir()
	eventsDir := filepath.Join(dir, ".schmux", "events")
	os.MkdirAll(eventsDir, 0755)
	path := filepath.Join(eventsDir, "test-session.jsonl")

	statusHandler := &testHandler{}
	failureHandler := &testHandler{}

	w, err := NewEventWatcher(path, "test-session", map[string][]EventHandler{
		"status":  {statusHandler},
		"failure": {failureHandler},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Write a status event
	AppendEvent(path, StatusEvent{
		Ts: "2026-02-18T14:30:00Z", Type: "status",
		State: "working", Message: "test",
	})

	// Wait for dispatch
	deadline := time.After(2 * time.Second)
	for {
		if len(statusHandler.getEvents()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for status event dispatch")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	if len(failureHandler.getEvents()) != 0 {
		t.Error("failure handler should not have received events")
	}
}

