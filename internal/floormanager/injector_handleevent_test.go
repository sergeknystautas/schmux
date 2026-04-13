package floormanager

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/events"
)

// newTestInjector creates an Injector with a minimal Manager (no tmux, no session manager).
// The Manager.TmuxSession() returns "" so flush() returns early without running tmux commands.
func newTestInjector(t *testing.T, debounceMs int) *Injector {
	t.Helper()
	cfg := &config.Config{}
	mgr := New(cfg, nil, nil, t.TempDir(), log.NewWithOptions(io.Discard, log.Options{}))
	logger := log.NewWithOptions(io.Discard, log.Options{})
	return NewInjector(mgr, debounceMs, logger)
}

func makeStatusEvent(state, message string) (events.RawEvent, []byte) {
	raw := events.RawEvent{Type: "status", Ts: "1"}
	evt := events.StatusEvent{
		Type:    "status",
		Ts:      "1",
		State:   state,
		Message: message,
	}
	data, _ := json.Marshal(evt)
	return raw, data
}

func TestHandleEvent_FiltersNonStatusEvents(t *testing.T) {
	inj := newTestInjector(t, 100000) // very long debounce to prevent flush
	defer inj.Stop()

	ctx := context.Background()

	// Send a non-status event — should be ignored
	raw := events.RawEvent{Type: "output", Ts: "1234"}
	data, _ := json.Marshal(raw)
	inj.HandleEvent(ctx, "session-1", raw, data)

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected 0 pending messages for non-status event, got %d", pendingCount)
	}
}

func TestHandleEvent_QueuesStatusEvent(t *testing.T) {
	inj := newTestInjector(t, 100000)
	defer inj.Stop()

	ctx := context.Background()
	raw, data := makeStatusEvent("needs_input", "Waiting for user")
	inj.HandleEvent(ctx, "session-1", raw, data)

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	msg := ""
	if pendingCount > 0 {
		msg = inj.pending[0]
	}
	inj.mu.Unlock()

	if pendingCount != 1 {
		t.Fatalf("expected 1 pending message, got %d", pendingCount)
	}
	if msg == "" {
		t.Fatal("pending message is empty")
	}
}

func TestHandleEvent_SkipsWorkingTransition(t *testing.T) {
	inj := newTestInjector(t, 100000)
	defer inj.Stop()

	ctx := context.Background()

	// First: needs_input (queued because curr != "working")
	raw1, data1 := makeStatusEvent("needs_input", "")
	inj.HandleEvent(ctx, "session-1", raw1, data1)

	// Second: transition to working (should be skipped by shouldInject)
	raw2, data2 := makeStatusEvent("working", "")
	inj.HandleEvent(ctx, "session-1", raw2, data2)

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	// Only the needs_input event should be queued (working was skipped)
	if pendingCount != 1 {
		t.Errorf("expected 1 pending message (working skipped), got %d", pendingCount)
	}
}

func TestHandleEvent_TracksStatePerSession(t *testing.T) {
	inj := newTestInjector(t, 100000)
	defer inj.Stop()

	ctx := context.Background()

	// Session A: error state
	rawA, dataA := makeStatusEvent("error", "build failed")
	inj.HandleEvent(ctx, "session-a", rawA, dataA)

	// Session B: needs_input state
	rawB, dataB := makeStatusEvent("needs_input", "waiting")
	inj.HandleEvent(ctx, "session-b", rawB, dataB)

	inj.mu.Lock()
	stateA := inj.prevState["session-a"]
	stateB := inj.prevState["session-b"]
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if stateA != "error" {
		t.Errorf("prevState[session-a] = %q, want %q", stateA, "error")
	}
	if stateB != "needs_input" {
		t.Errorf("prevState[session-b] = %q, want %q", stateB, "needs_input")
	}
	if pendingCount != 2 {
		t.Errorf("expected 2 pending messages, got %d", pendingCount)
	}
}

func TestHandleEvent_MalformedJSON(t *testing.T) {
	inj := newTestInjector(t, 100000)
	defer inj.Stop()

	ctx := context.Background()
	raw := events.RawEvent{Type: "status", Ts: "1"}

	// Pass malformed JSON — HandleEvent should log a warning and return without queuing
	inj.HandleEvent(ctx, "session-1", raw, []byte(`{not valid json`))

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected 0 pending after malformed JSON, got %d", pendingCount)
	}
}

func TestHandleEvent_RespectsStoppedFlag(t *testing.T) {
	inj := newTestInjector(t, 100000)

	// Stop the injector first
	inj.Stop()

	ctx := context.Background()
	raw, data := makeStatusEvent("error", "should be ignored")
	inj.HandleEvent(ctx, "session-1", raw, data)

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected 0 pending after Stop(), got %d", pendingCount)
	}
}

func TestStop_ClearsPendingMessages(t *testing.T) {
	inj := newTestInjector(t, 100000)

	ctx := context.Background()
	raw, data := makeStatusEvent("error", "")
	inj.HandleEvent(ctx, "session-1", raw, data)

	// Verify there's a pending message
	inj.mu.Lock()
	if len(inj.pending) != 1 {
		inj.mu.Unlock()
		t.Fatal("expected 1 pending message before Stop()")
	}
	inj.mu.Unlock()

	inj.Stop()

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	stopped := inj.stopped
	inj.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected pending cleared after Stop(), got %d", pendingCount)
	}
	if !stopped {
		t.Error("expected stopped=true after Stop()")
	}
}

func TestFlush_RetainsPendingWhenTmuxSessionEmpty(t *testing.T) {
	inj := newTestInjector(t, 50) // short debounce
	defer inj.Stop()

	ctx := context.Background()
	raw, data := makeStatusEvent("error", "build failed")
	inj.HandleEvent(ctx, "session-1", raw, data)

	// Poll for the debounce timer to fire and flush to run.
	// flush returns early since TmuxSession() is "", so messages are
	// preserved in pending for retry once the session comes back.
	deadline := time.Now().Add(2 * time.Second)
	var pendingCount int
	for time.Now().Before(deadline) {
		inj.mu.Lock()
		pendingCount = len(inj.pending)
		inj.mu.Unlock()
		if pendingCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if pendingCount != 1 {
		t.Errorf("expected 1 pending message retained after flush with empty session, got %d", pendingCount)
	}
}
