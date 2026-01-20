package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/tmux"
)

func TestStatus(t *testing.T) {
	// This test requires a running daemon or mocking
	// Skip for now
	t.Skip("requires running daemon")
}

func TestPidFileParsing(t *testing.T) {
	// Test PID file parsing logic
	tmpDir := t.TempDir()
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)

	// Write a test PID
	testPID := 12345
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", testPID)), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	// Read it back
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("failed to read PID file: %v", err)
	}

	var pid int
	if _, err := fmt.Sscanf(string(pidData), "%d", &pid); err != nil {
		t.Fatalf("failed to parse PID: %v", err)
	}

	if pid != testPID {
		t.Errorf("expected PID %d, got %d", testPID, pid)
	}
}

func TestShutdown(t *testing.T) {
	// Just test that Shutdown doesn't panic
	Shutdown()
}

func TestDashboardPort(t *testing.T) {
	if dashboardPort != 7337 {
		t.Errorf("expected dashboard port 7337, got %d", dashboardPort)
	}
}

// mockChecker is a test implementation of tmux.Checker that returns a predefined error.
type mockChecker struct{ err error }

func (m *mockChecker) Check() error { return m.err }

// TestValidateReadyToRun_MissingTmux tests that ValidateReadyToRun fails when tmux is missing.
func TestValidateReadyToRun_MissingTmux(t *testing.T) {
	// Save original checker and restore after test
	original := tmux.TmuxChecker
	defer func() { tmux.TmuxChecker = original }()

	// Mock a checker that returns "tmux not found" error
	tmux.TmuxChecker = &mockChecker{err: errors.New("tmux is not installed or not accessible")}

	err := ValidateReadyToRun()
	if err == nil {
		t.Error("Expected error when tmux is missing, got nil")
	}
	// Error should contain the tmux error message
	expectedMsg := "tmux is not installed"
	if err == nil || !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error containing %q, got %q", expectedMsg, err)
	}
}
