package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
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
	// Verifies Shutdown does not panic. Limited assertion possible
	// due to package-level channel state shared across tests.
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

func TestValidateSessionAccess_NoSessions(t *testing.T) {
	// Empty state should pass
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath)

	err := validateSessionAccess(st)
	if err != nil {
		t.Errorf("expected no error with empty state, got: %v", err)
	}
}

func TestValidateSessionAccess_MissingSessionNoUserMismatch(t *testing.T) {
	// State with a session that doesn't exist in tmux should NOT fail
	// if there's no user mismatch (no other user's tmux server running)
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath)

	// Add a fake session
	sess := state.Session{
		ID:          "test-session-123",
		WorkspaceID: "test-workspace",
		Target:      "test-target",
		TmuxSession: "nonexistent-tmux-session-xyz",
		CreatedAt:   time.Now(),
		Pid:         12345,
	}
	if err := st.AddSession(sess); err != nil {
		t.Fatalf("failed to add session: %v", err)
	}

	// This should NOT error because there's no user mismatch
	// (either we have our own tmux server, or there's no tmux server at all)
	err := validateSessionAccess(st)
	// We can't assert error/no-error without controlling the tmux state,
	// but at minimum it should not panic
	_ = err
}

func TestFindOtherTmuxServerOwners(t *testing.T) {
	// This just verifies the function doesn't panic and returns a slice
	currentUID := os.Getuid()
	owners := findOtherTmuxServerOwners(currentUID)
	// Should return empty or owners of other users' tmux servers
	// We can't assert much here without knowing the test environment
	if owners == nil {
		t.Error("expected non-nil slice from findOtherTmuxServerOwners")
	}
}

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

func TestProcessTerminalCapture(t *testing.T) {
	t.Run("strips ANSI and returns plain text", func(t *testing.T) {
		// ESC[32m = green, ESC[0m = reset
		raw := "\x1b[32mHello\x1b[0m world"
		got := processTerminalCapture(raw)
		if got != "Hello world" {
			t.Errorf("got %q, want %q", got, "Hello world")
		}
	})

	t.Run("truncates to last 5000 chars", func(t *testing.T) {
		// Build a string longer than maxTerminalCaptureLen
		long := strings.Repeat("x", 8000)
		got := processTerminalCapture(long)
		if len(got) != maxTerminalCaptureLen {
			t.Errorf("got len %d, want %d", len(got), maxTerminalCaptureLen)
		}
		// Should keep the LAST 5000 chars (all 'x')
		if got != strings.Repeat("x", maxTerminalCaptureLen) {
			t.Error("should keep the last 5000 characters")
		}
	})

	t.Run("truncates after stripping ANSI", func(t *testing.T) {
		// ANSI bloated string: the raw string is long but cleaned is short
		ansiPadding := strings.Repeat("\x1b[31m\x1b[0m", 2000) // lots of ANSI, no visible text
		plainTail := "visible content here"
		raw := ansiPadding + plainTail
		got := processTerminalCapture(raw)
		if got != plainTail {
			t.Errorf("got %q, want %q", got, plainTail)
		}
	})

	t.Run("returns empty for whitespace-only content", func(t *testing.T) {
		got := processTerminalCapture("   \n\t\n  ")
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("returns empty for ANSI-only content", func(t *testing.T) {
		got := processTerminalCapture("\x1b[32m\x1b[0m\x1b[1m")
		if got != "" {
			t.Errorf("got %q, want empty string for ANSI-only input", got)
		}
	})

	t.Run("returns empty for empty input", func(t *testing.T) {
		got := processTerminalCapture("")
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})

	t.Run("preserves content under limit", func(t *testing.T) {
		input := "short output"
		got := processTerminalCapture(input)
		if got != input {
			t.Errorf("got %q, want %q", got, input)
		}
	})

	t.Run("exactly at limit is not truncated", func(t *testing.T) {
		input := strings.Repeat("a", maxTerminalCaptureLen)
		got := processTerminalCapture(input)
		if len(got) != maxTerminalCaptureLen {
			t.Errorf("got len %d, want %d", len(got), maxTerminalCaptureLen)
		}
	})

	t.Run("truncation keeps tail not head", func(t *testing.T) {
		// First 3000 chars are 'a', last 5000 chars are 'b', total 8000
		head := strings.Repeat("a", 3000)
		tail := strings.Repeat("b", 5000)
		got := processTerminalCapture(head + tail)
		if got != tail {
			t.Error("truncation should keep the tail (last 5000 chars), not the head")
		}
	})
}
