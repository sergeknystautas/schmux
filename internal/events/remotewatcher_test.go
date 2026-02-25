package events

import (
	"context"
	"sync"
	"testing"
)

func TestRemoteWatcherScript(t *testing.T) {
	script := RemoteWatcherScript("/workspace/.schmux/events/test.jsonl")
	if script == "" {
		t.Fatal("empty script")
	}
	// Must contain tail -f
	if !containsStr(script, "tail -f") {
		t.Error("script should use tail -f")
	}
	// Must contain sentinel markers
	if !containsStr(script, "__SCHMUX_SIGNAL__") {
		t.Error("script should use sentinel markers")
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

type remoteTestHandler struct {
	mu     sync.Mutex
	events []RawEvent
}

func (h *remoteTestHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, raw)
}

func (h *remoteTestHandler) getEvents() []RawEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	cp := make([]RawEvent, len(h.events))
	copy(cp, h.events)
	return cp
}

func TestRemoteEventWatcherProcessOutput(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	w.ProcessOutput(`__SCHMUX_SIGNAL__{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}__END__`)

	events := handler.getEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "status" {
		t.Errorf("type = %v, want status", events[0].Type)
	}
}

func TestRemoteEventWatcherDedup(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	line := `__SCHMUX_SIGNAL__{"ts":"2026-02-18T14:30:00Z","type":"status","state":"working","message":"test"}__END__`
	w.ProcessOutput(line)
	w.ProcessOutput(line) // duplicate

	events := handler.getEvents()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (dedup)", len(events))
	}
}

func TestRemoteEventWatcherNoSentinel(t *testing.T) {
	handler := &remoteTestHandler{}
	w := NewRemoteEventWatcher("test-session", map[string][]EventHandler{
		"status": {handler},
	})

	w.ProcessOutput("some random output without sentinels")

	events := handler.getEvents()
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}
