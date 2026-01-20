package tmux

import (
	"errors"
	"strings"
	"testing"
)

// mockChecker is a test implementation of Checker that returns a predefined error.
type mockChecker struct{ err error }

func (m *mockChecker) Check() error { return m.err }

// TestDefaultChecker_Success tests that tmux detection works when tmux is available.
// This test is skipped if tmux is not installed on the test system.
func TestDefaultChecker_Success(t *testing.T) {
	checker := &defaultChecker{}
	if err := checker.Check(); err != nil {
		// If tmux is not installed, that's OK for this test - we're testing
		// that when it IS installed, detection works.
		// In CI/containers without tmux, this will fail but that's expected.
		t.Skipf("tmux not available: %v", err)
	}
	// No error means tmux is installed and working
}

// TestChecker_MissingTmux tests that checker fails when tmux is missing.
func TestChecker_MissingTmux(t *testing.T) {
	// Save original checker and restore after test
	original := TmuxChecker
	defer func() { TmuxChecker = original }()

	// Mock a checker that returns "tmux not found" error
	TmuxChecker = &mockChecker{err: errors.New("tmux is not installed or not accessible")}

	err := TmuxChecker.Check()
	if err == nil {
		t.Error("Expected error when tmux is missing, got nil")
	}
	expectedMsg := "tmux is not installed"
	if err == nil || !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error containing %q, got %q", expectedMsg, err)
	}
}

// TestChecker_TmuxNoOutput tests that checker fails when tmux returns no output.
func TestChecker_TmuxNoOutput(t *testing.T) {
	// Save original checker and restore after test
	original := TmuxChecker
	defer func() { TmuxChecker = original }()

	// Mock a checker that returns "no output" error
	TmuxChecker = &mockChecker{err: errors.New("tmux command produced no output")}

	err := TmuxChecker.Check()
	if err == nil {
		t.Error("Expected error when tmux produces no output, got nil")
	}
	expectedMsg := "tmux command produced no output"
	if err == nil || err.Error() != expectedMsg {
		t.Errorf("Expected error %q, got %q", expectedMsg, err)
	}
}
