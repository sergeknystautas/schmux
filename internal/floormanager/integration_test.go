package floormanager

import (
	"context"
	"testing"
	"time"
)

func TestShiftRotationEndShift(t *testing.T) {
	// Test that EndShift() unblocks the shift rotation wait
	m := &Manager{
		shiftDone: make(chan struct{}, 1),
	}

	done := make(chan struct{})
	go func() {
		select {
		case <-m.shiftDone:
			close(done)
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for end-shift")
		}
	}()

	m.EndShift()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Error("EndShift did not signal shiftDone")
	}
}

func TestInjectorFilteringIntegration(t *testing.T) {
	// Test full filtering pipeline with multiple events
	ctx := context.Background()
	_ = ctx

	// Verify that working->working is skipped
	if shouldInject("working", "working") {
		t.Error("should skip working->working")
	}

	// Verify that idle->working is skipped
	if shouldInject("idle", "working") {
		t.Error("should skip idle->working")
	}

	// Verify that working->error is injected
	if !shouldInject("working", "error") {
		t.Error("should inject working->error")
	}

	// Verify that working->needs_input is injected
	if !shouldInject("working", "needs_input") {
		t.Error("should inject working->needs_input")
	}

	// Verify that ""->error is injected (first event)
	if !shouldInject("", "error") {
		t.Error("should inject initial error state")
	}

	// Verify formatting includes all fields when present
	msg := FormatSignalMessage("test-session", "working", "needs_input", "help me", "doing OAuth", "token format unknown")
	expected := `[SIGNAL] test-session: working -> needs_input "help me" intent="doing OAuth" blocked="token format unknown"`
	if msg != expected {
		t.Errorf("unexpected format:\n  got:  %s\n  want: %s", msg, expected)
	}

	// Verify formatting with empty optional fields
	msg2 := FormatSignalMessage("agent-1", "", "idle", "", "", "")
	expected2 := "[SIGNAL] agent-1: -> idle"
	if msg2 != expected2 {
		t.Errorf("unexpected format:\n  got:  %s\n  want: %s", msg2, expected2)
	}
}

func TestEndShiftNoChannel(t *testing.T) {
	// EndShift with nil channel should not panic
	m := &Manager{}
	m.EndShift() // should be a no-op
}

func TestInjectionCountThreshold(t *testing.T) {
	// Test that injection count increments correctly
	m := &Manager{
		stopCh: make(chan struct{}),
	}

	m.IncrementInjectionCount(5)
	if got := m.InjectionCount(); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}

	m.IncrementInjectionCount(3)
	if got := m.InjectionCount(); got != 8 {
		t.Errorf("expected 8, got %d", got)
	}

	m.ResetInjectionCount()
	if got := m.InjectionCount(); got != 0 {
		t.Errorf("expected 0 after reset, got %d", got)
	}
}
