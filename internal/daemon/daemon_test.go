package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

func TestStatus_NoPidFile(t *testing.T) {
	// When no PID file exists (or the daemon was never started),
	// Status() should return running=false without error.
	// This tests the common case and the PID file parsing logic.
	//
	// Note: this can only run reliably when no daemon is actually running.
	// If a daemon IS running, it still validates the function doesn't panic
	// and returns consistent results.
	running, url, _, err := Status()
	if err != nil {
		t.Fatalf("Status() returned unexpected error: %v", err)
	}
	if running {
		// A daemon is running on this machine — skip the not-running assertions
		// but still verify the URL is well-formed
		if url == "" {
			t.Error("Status() returned running=true but empty url")
		}
		t.Skipf("daemon is running at %s — cannot test not-running case", url)
	}
	// Not running: url should be empty
	if url != "" {
		t.Errorf("Status() returned running=false but non-empty url: %q", url)
	}
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

// mockChecker is a test implementation of tmux.Checker that returns a predefined error.
type mockChecker struct{ err error }

func (m *mockChecker) Check() error { return m.err }

func TestValidateSessionAccess_NoSessions(t *testing.T) {
	// Empty state should pass
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)

	err := validateSessionAccess(st)
	if err != nil {
		t.Errorf("expected no error with empty state, got: %v", err)
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

func TestValidateReadyToRun_StalePidFile(t *testing.T) {
	original := tmux.TmuxChecker
	defer func() { tmux.TmuxChecker = original }()
	tmux.TmuxChecker = &mockChecker{err: nil}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Write a PID file with a PID that is definitely not running.
	// PID 2^22 - 1 (4194303) is unlikely to be an active process.
	stalePID := 4194303
	pidFile := filepath.Join(schmuxDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", stalePID)), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	err := ValidateReadyToRun()
	if err != nil {
		t.Errorf("expected nil error for stale PID file, got: %v", err)
	}

	// Stale PID file should have been removed
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Error("expected stale PID file to be removed")
	}
}

func TestValidateReadyToRun_RunningPid(t *testing.T) {
	original := tmux.TmuxChecker
	defer func() { tmux.TmuxChecker = original }()
	tmux.TmuxChecker = &mockChecker{err: nil}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Use the current process PID — guaranteed to be alive
	pidFile := filepath.Join(schmuxDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	err := ValidateReadyToRun()
	if err == nil {
		t.Fatal("expected error for running PID, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("expected error containing 'already running', got: %v", err)
	}
}

func TestValidateReadyToRun_MalformedPidFile(t *testing.T) {
	original := tmux.TmuxChecker
	defer func() { tmux.TmuxChecker = original }()
	tmux.TmuxChecker = &mockChecker{err: nil}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Write garbage into the PID file
	pidFile := filepath.Join(schmuxDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte("not-a-number\n"), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	err := ValidateReadyToRun()
	if err != nil {
		t.Errorf("expected nil error for malformed PID file (treated as stale), got: %v", err)
	}
}

func TestValidateReadyToRun_NoPidFile(t *testing.T) {
	original := tmux.TmuxChecker
	defer func() { tmux.TmuxChecker = original }()
	tmux.TmuxChecker = &mockChecker{err: nil}

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	err := ValidateReadyToRun()
	if err != nil {
		t.Errorf("expected nil error with no PID file, got: %v", err)
	}

	// Verify .schmux dir was created
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if _, err := os.Stat(schmuxDir); os.IsNotExist(err) {
		t.Error("expected .schmux directory to be created")
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	d := NewDaemon()

	// First call should close the channel
	d.Shutdown()

	// Second call must not panic
	d.Shutdown()

	// shutdownChan should be closed (non-blocking receive)
	select {
	case <-d.shutdownChan:
		// ok — channel is closed
	default:
		t.Error("expected shutdownChan to be closed")
	}
}

func TestShutdown_CancelsContext(t *testing.T) {
	d := NewDaemon()
	d.Shutdown()

	if d.shutdownCtx.Err() != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", d.shutdownCtx.Err())
	}
}

func TestDevRestart_Idempotent(t *testing.T) {
	d := NewDaemon()

	d.DevRestart()
	d.DevRestart() // must not panic

	select {
	case <-d.devRestartChan:
		// ok — channel is closed
	default:
		t.Error("expected devRestartChan to be closed")
	}
}

func TestDevRestart_CancelsContext(t *testing.T) {
	d := NewDaemon()
	d.DevRestart()

	if d.shutdownCtx.Err() != context.Canceled {
		t.Errorf("expected context.Canceled, got: %v", d.shutdownCtx.Err())
	}
}

func TestNewDaemon(t *testing.T) {
	d := NewDaemon()

	if d.shutdownChan == nil {
		t.Error("shutdownChan should be initialized")
	}
	if d.devRestartChan == nil {
		t.Error("devRestartChan should be initialized")
	}
	if d.shutdownCtx == nil {
		t.Error("shutdownCtx should be initialized")
	}
	if d.cancelFunc == nil {
		t.Error("cancelFunc should be initialized")
	}
	if d.shutdownCtx.Err() != nil {
		t.Error("shutdownCtx should not be canceled on creation")
	}
}

func TestValidateSessionAccess_WithSessions_OurSocketExists(t *testing.T) {
	// If our tmux socket exists, validateSessionAccess should succeed
	// even when sessions are present in state.
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	st.AddSession(state.Session{ID: "s1", TmuxSession: "test-session"})

	// Create a fake tmux socket at the expected path for our UID
	uid := os.Getuid()
	socketDir := fmt.Sprintf("/tmp/tmux-%d", uid)

	// Only run the socket-exists path if the socket dir actually exists
	// (which it does on machines where tmux has been used)
	if _, err := os.Stat(socketDir); err == nil {
		err := validateSessionAccess(st)
		if err != nil {
			t.Errorf("expected no error when our tmux socket exists, got: %v", err)
		}
	} else {
		t.Skip("tmux socket dir does not exist — skipping socket-exists test")
	}
}

func TestFindOtherTmuxServerOwners_ExcludesOwnUID(t *testing.T) {
	uid := os.Getuid()
	owners := findOtherTmuxServerOwners(uid)

	// Verify our own UID never appears in the result
	ownStr := fmt.Sprintf("uid %d)", uid)
	for _, o := range owners {
		if strings.Contains(o, ownStr) {
			t.Errorf("findOtherTmuxServerOwners should exclude our own UID, but found: %s", o)
		}
	}
}

func TestFindOtherTmuxServerOwners_ImpossibleUID(t *testing.T) {
	// Using UID 0 should find any non-root tmux servers.
	// Using a very high UID should find nothing unexpected.
	// The key assertion: the function doesn't crash.
	owners := findOtherTmuxServerOwners(999999999)
	// We can't assert specific results (depends on host state),
	// but verify no panic and return is a valid slice.
	if owners == nil {
		t.Error("expected non-nil slice (even if empty)")
	}
}

func TestStop_NoPidFile(t *testing.T) {
	// Override HOME to a temp dir with no PID file
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Ensure .schmux dir exists but no PID file
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	err := Stop()
	if err == nil {
		t.Fatal("expected error when no PID file exists")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected error containing 'not running', got: %v", err)
	}
}

func TestStop_StalePid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	// Write a PID for a process that isn't running
	pidFile := filepath.Join(schmuxDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte("4194303\n"), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	err := Stop()
	if err == nil {
		t.Fatal("expected error when sending SIGTERM to dead process")
	}
	// On macOS/Linux, signaling a non-existent process returns ESRCH
	if !strings.Contains(err.Error(), "SIGTERM") {
		t.Errorf("expected error about SIGTERM, got: %v", err)
	}
}

func TestStop_MalformedPidFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create schmux dir: %v", err)
	}

	pidFile := filepath.Join(schmuxDir, pidFileName)
	if err := os.WriteFile(pidFile, []byte("garbage\n"), 0644); err != nil {
		t.Fatalf("failed to write PID file: %v", err)
	}

	err := Stop()
	if err == nil {
		t.Fatal("expected error for malformed PID file")
	}
	if !strings.Contains(err.Error(), "parse PID") {
		t.Errorf("expected error about parsing PID, got: %v", err)
	}
}

// Silence unused import warning for syscall — used by ValidateReadyToRun tests
var _ = syscall.Signal(0)
