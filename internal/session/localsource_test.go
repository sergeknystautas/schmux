package session

import (
	"errors"
	"testing"
)

func TestLocalSource_IsPermanentError_ClosesWithError(t *testing.T) {
	// Verify that isPermanentError correctly identifies permanent errors.
	// LocalSource.run() uses this to decide whether to emit SourceClosed with an error.
	tests := []struct {
		name      string
		err       error
		permanent bool
	}{
		{"can't find session", errors.New("can't find session: test"), true},
		{"no session found", errors.New("no session found: test"), true},
		{"transient", errors.New("connection refused"), false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanentError(tt.err); got != tt.permanent {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, got, tt.permanent)
			}
		})
	}
}

func TestLocalSource_ImplementsControlSource(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	var _ ControlSource = source
}

func TestLocalSource_MethodsFailWhenNotAttached(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)

	if _, err := source.SendKeys("abc"); err == nil {
		t.Error("SendKeys should fail when not attached")
	}
	if _, err := source.CaptureVisible(); err == nil {
		t.Error("CaptureVisible should fail when not attached")
	}
	if _, err := source.CaptureLines(10); err == nil {
		t.Error("CaptureLines should fail when not attached")
	}
	if _, err := source.GetCursorState(); err == nil {
		t.Error("GetCursorState should fail when not attached")
	}
	if err := source.Resize(80, 24); err == nil {
		t.Error("Resize should fail when not attached")
	}
}

func TestLocalSource_IsAttached(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	if source.IsAttached() {
		t.Error("should not be attached before start")
	}
}

func TestLocalSource_SetTmuxSession(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	source.SetTmuxSession("tmux-s2")

	source.mu.RLock()
	got := source.tmuxSession
	source.mu.RUnlock()

	if got != "tmux-s2" {
		t.Errorf("tmuxSession = %q, want %q", got, "tmux-s2")
	}
}
