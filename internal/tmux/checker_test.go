package tmux

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDefaultChecker_Success tests that tmux detection works when tmux is available.
// This test is skipped if tmux is not installed on the test system.
func TestDefaultChecker_Success(t *testing.T) {
	checker := &defaultChecker{}
	if err := checker.Check(); err != nil {
		t.Skipf("tmux not available: %v", err)
	}
	// No error means tmux is installed and working
}

// TestDefaultChecker_ErrorContainsTmux verifies that when tmux is missing,
// the error message is descriptive and mentions "tmux".
func TestDefaultChecker_ErrorFormat(t *testing.T) {
	// Only run this if tmux is NOT installed
	if _, err := exec.LookPath("tmux"); err == nil {
		t.Skip("tmux is installed — cannot test error format for missing tmux")
	}

	checker := &defaultChecker{}
	err := checker.Check()
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

// TestNewDefaultChecker verifies that NewDefaultChecker returns a valid Checker
// that produces the same result as the package-level default.
func TestNewDefaultChecker(t *testing.T) {
	checker := NewDefaultChecker()
	if checker == nil {
		t.Fatal("NewDefaultChecker() returned nil")
	}

	// Both the factory and the default should agree on tmux availability
	defaultErr := TmuxChecker.Check()
	factoryErr := checker.Check()

	if (defaultErr == nil) != (factoryErr == nil) {
		t.Errorf("NewDefaultChecker().Check() = %v, but TmuxChecker.Check() = %v — they should agree",
			factoryErr, defaultErr)
	}
}

// TestCheckerInterface verifies that both implementations satisfy the Checker interface.
func TestCheckerInterface(t *testing.T) {
	var _ Checker = &defaultChecker{}
	var _ Checker = NewDefaultChecker()
}
