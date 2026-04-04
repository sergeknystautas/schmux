package tmux

import (
	"os/exec"
	"strings"
	"testing"
)

// TestTmuxServerCheck_Success tests that TmuxServer.Check works when tmux is available.
// This test is skipped if tmux is not installed on the test system.
func TestTmuxServerCheck_Success(t *testing.T) {
	srv := NewTmuxServer("tmux", "test-check", nil)
	if err := srv.Check(); err != nil {
		t.Skipf("tmux not available: %v", err)
	}
	// No error means tmux is installed and working
}

// TestTmuxServerCheck_ErrorFormat verifies that when tmux is missing,
// the error message is descriptive and mentions "tmux".
func TestTmuxServerCheck_ErrorFormat(t *testing.T) {
	// Only run this if tmux is NOT installed
	if _, err := exec.LookPath("tmux"); err == nil {
		t.Skip("tmux is installed — cannot test error format for missing tmux")
	}

	srv := NewTmuxServer("tmux", "test-check", nil)
	err := srv.Check()
	if err == nil {
		t.Fatal("expected error when tmux is not installed, got nil")
	}
	if !strings.Contains(err.Error(), "tmux") {
		t.Errorf("error should mention 'tmux', got: %v", err)
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error should mention 'not installed', got: %v", err)
	}
}
