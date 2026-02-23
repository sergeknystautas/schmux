package floormanager

import (
	"context"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/signal"
)

func TestShouldInjectSignal(t *testing.T) {
	tests := []struct {
		name     string
		oldState string
		newState string
		want     bool
	}{
		{"working to error", "working", "error", true},
		{"working to needs_input", "working", "needs_input", true},
		{"working to needs_testing", "working", "needs_testing", true},
		{"working to completed", "working", "completed", true},
		{"working to working", "working", "working", false},
		{"empty to working", "", "working", false},
		{"empty to error", "", "error", true},
		{"completed to working", "completed", "working", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldInjectSignal(tt.oldState, tt.newState)
			if got != tt.want {
				t.Errorf("ShouldInjectSignal(%q, %q) = %v, want %v", tt.oldState, tt.newState, got, tt.want)
			}
		})
	}
}

func TestFormatSignalMessage(t *testing.T) {
	sig := signal.Signal{
		State:    "needs_input",
		Message:  "Need auth token format clarification",
		Intent:   "Implementing JWT auth",
		Blockers: "Unknown token expiry",
	}
	msg := FormatSignalMessage("abc-123", "claude-1", "working", sig)

	expected := `[SIGNAL] claude-1 (abc-123) state: working -> needs_input. Summary: "Need auth token format clarification" Intent: "Implementing JWT auth" Blocked: "Unknown token expiry"`
	if msg != expected {
		t.Errorf("unexpected message:\ngot:  %s\nwant: %s", msg, expected)
	}
}

func TestFormatSignalMessageMinimal(t *testing.T) {
	sig := signal.Signal{
		State:   "completed",
		Message: "Done",
	}
	msg := FormatSignalMessage("def-456", "codex-1", "working", sig)

	expected := `[SIGNAL] codex-1 (def-456) state: working -> completed. Summary: "Done"`
	if msg != expected {
		t.Errorf("unexpected message:\ngot:  %s\nwant: %s", msg, expected)
	}
}

func TestFormatSignalMessageNoMessage(t *testing.T) {
	sig := signal.Signal{
		State: "error",
	}
	msg := FormatSignalMessage("ghi-789", "agent-1", "working", sig)

	expected := `[SIGNAL] agent-1 (ghi-789) state: working -> error.`
	if msg != expected {
		t.Errorf("unexpected message:\ngot:  %s\nwant: %s", msg, expected)
	}
}

func TestInjectSkipsNonInjectableSignal(t *testing.T) {
	// working->working should NOT be injected
	inj := NewInjector(context.Background(), nil, 0)

	// First set the previous state to "working" (keyed by session ID)
	inj.mu.Lock()
	inj.previousStates["id-1"] = "working"
	inj.mu.Unlock()

	// Inject working->working (should be skipped)
	inj.Inject("id-1", "agent-1", signal.Signal{State: "working", Message: "still working"})

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 0 {
		t.Errorf("expected 0 pending messages for working->working, got %d", pendingCount)
	}
}

func TestInjectQueuesInjectableSignal(t *testing.T) {
	// working->needs_input SHOULD be injected
	inj := NewInjector(context.Background(), nil, 0)

	// Set up initial state as working (keyed by session ID)
	inj.mu.Lock()
	inj.previousStates["id-1"] = "working"
	inj.mu.Unlock()

	inj.Inject("id-1", "agent-1", signal.Signal{State: "needs_input", Message: "need help"})

	inj.mu.Lock()
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 1 {
		t.Errorf("expected 1 pending message for working->needs_input, got %d", pendingCount)
	}
}

func TestInjectTracksPreviousStates(t *testing.T) {
	inj := NewInjector(context.Background(), nil, 0)

	// First inject: empty->error (injectable, empty old state)
	inj.Inject("id-1", "agent-1", signal.Signal{State: "error", Message: "build failed"})

	inj.mu.Lock()
	if inj.previousStates["id-1"] != "error" {
		t.Errorf("expected previousStates[id-1] = 'error', got %q", inj.previousStates["id-1"])
	}
	pendingCount := len(inj.pending)
	inj.mu.Unlock()

	if pendingCount != 1 {
		t.Errorf("expected 1 pending message after first inject, got %d", pendingCount)
	}

	// Second inject: error->completed (injectable, old state should be "error")
	inj.Inject("id-1", "agent-1", signal.Signal{State: "completed", Message: "done"})

	inj.mu.Lock()
	if inj.previousStates["id-1"] != "completed" {
		t.Errorf("expected previousStates[id-1] = 'completed', got %q", inj.previousStates["id-1"])
	}
	pendingCount = len(inj.pending)
	// Verify the second message references the correct old state
	if pendingCount >= 2 {
		secondMsg := inj.pending[1]
		if secondMsg != `[SIGNAL] agent-1 (id-1) state: error -> completed. Summary: "done"` {
			t.Errorf("unexpected second message:\ngot:  %s\nwant: [SIGNAL] agent-1 (id-1) state: error -> completed. Summary: \"done\"", secondMsg)
		}
	}
	inj.mu.Unlock()

	if pendingCount != 2 {
		t.Errorf("expected 2 pending messages after second inject, got %d", pendingCount)
	}
}

func TestInjectMultipleSessionsIndependent(t *testing.T) {
	inj := NewInjector(context.Background(), nil, 0)

	// Inject from two different sessions
	inj.Inject("id-1", "agent-1", signal.Signal{State: "error", Message: "agent-1 error"})
	inj.Inject("id-2", "agent-2", signal.Signal{State: "needs_input", Message: "agent-2 blocked"})

	inj.mu.Lock()
	defer inj.mu.Unlock()

	if len(inj.pending) != 2 {
		t.Errorf("expected 2 pending messages, got %d", len(inj.pending))
	}

	// Each session should have its own previous state (keyed by session ID)
	if inj.previousStates["id-1"] != "error" {
		t.Errorf("expected id-1 state 'error', got %q", inj.previousStates["id-1"])
	}
	if inj.previousStates["id-2"] != "needs_input" {
		t.Errorf("expected id-2 state 'needs_input', got %q", inj.previousStates["id-2"])
	}
}

func TestInjectUpdatesStateEvenWhenSkipped(t *testing.T) {
	// Even when a signal is not injected (working->working),
	// the previousStates should still be updated
	inj := NewInjector(context.Background(), nil, 0)

	// Inject working state (empty->working is not injectable)
	inj.Inject("id-1", "agent-1", signal.Signal{State: "working", Message: "starting"})

	inj.mu.Lock()
	if inj.previousStates["id-1"] != "working" {
		t.Errorf("expected state 'working' even though injection was skipped, got %q", inj.previousStates["id-1"])
	}
	if len(inj.pending) != 0 {
		t.Errorf("expected 0 pending (working->working not injectable), got %d", len(inj.pending))
	}
	inj.mu.Unlock()
}

func TestFormatShiftMessage(t *testing.T) {
	msg := FormatShiftMessage()

	if !strings.Contains(msg, "[SHIFT]") {
		t.Error("expected [SHIFT] prefix in shift message")
	}
	if !strings.Contains(msg, "memory.md") {
		t.Error("expected memory.md reference in shift message")
	}
	if !strings.Contains(msg, "30s") {
		t.Error("expected timeout duration in shift message")
	}
}

func TestFormatShiftMessageDistinctFromSignal(t *testing.T) {
	msg := FormatShiftMessage()

	if strings.Contains(msg, "[SIGNAL]") {
		t.Error("[SHIFT] message must not contain [SIGNAL] prefix")
	}
}
