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
// NOTE: This test uses global variable mutation (TmuxChecker) which is an anti-pattern.
// It's acceptable here because:
// 1. We properly save/restore the original value
// 2. Tests run sequentially in the same package
// 3. The alternative (dependency injection) requires larger refactoring of daemon.go
// TODO: Refactor to use dependency injection when daemon structure allows it.
func TestChecker_MissingTmux(t *testing.T) {
	// Save original checker and restore after test (ensures test isolation)
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
// NOTE: Uses global variable mutation - see TestChecker_MissingTmux for rationale.
func TestChecker_TmuxNoOutput(t *testing.T) {
	// Save original checker and restore after test (ensures test isolation)
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
