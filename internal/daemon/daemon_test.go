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
	"github.com/sergeknystautas/schmux/internal/subreddit"
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

func TestNextSubredditGenerationTime_NoCache(t *testing.T) {
	now := time.Date(2026, 2, 25, 12, 0, 0, 0, time.UTC)
	cachePath := filepath.Join(t.TempDir(), "subreddit.json")

	got := nextSubredditGenerationTime(cachePath, now)
	want := now.Add(subredditDigestInterval)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestNextSubredditGenerationTime_UsesCacheDueTimeWhenFresh(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "subreddit.json")

	generatedAt := time.Date(2026, 2, 25, 13, 55, 44, 0, time.FixedZone("PST", -8*3600))
	if err := subreddit.WriteCache(cachePath, subreddit.Cache{
		Content:     "x",
		GeneratedAt: generatedAt,
		Hours:       24,
		CommitCount: 1,
	}); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	now := time.Date(2026, 2, 25, 14, 55, 14, 0, generatedAt.Location())
	got := nextSubredditGenerationTime(cachePath, now)
	want := generatedAt.Add(subredditDigestInterval + 1*time.Second)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}
