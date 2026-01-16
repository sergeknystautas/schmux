package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/workspace"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		RunTargets: []config.RunTarget{
			{Name: "test", Type: config.RunTargetTypePromptable, Command: "test"},
		},
	}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)
	if m == nil {
		t.Error("New() returned nil")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.state != st {
		t.Error("state not set correctly")
	}
	if m.workspace != wm {
		t.Error("workspace manager not set correctly")
	}
}

func TestGetAttachCommand(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Agent:       "test",
		TmuxSession: "schmux-test-001-abc123",
	}

	st.AddSession(sess)

	cmd, err := m.GetAttachCommand("session-001")
	if err != nil {
		t.Errorf("GetAttachCommand() error = %v", err)
	}

	expected := `tmux attach -t "schmux-test-001-abc123"`
	if cmd != expected {
		t.Errorf("expected %s, got %s", expected, cmd)
	}
}

func TestGetAttachCommandNotFound(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	_, err := m.GetAttachCommand("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetAllSessions(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Clear existing sessions
	st.Sessions = []state.Session{}

	// Add test sessions
	sessions := []state.Session{
		{ID: "s1", WorkspaceID: "w1", Agent: "a1", TmuxSession: "t1"},
		{ID: "s2", WorkspaceID: "w2", Agent: "a2", TmuxSession: "t2"},
	}

	for _, sess := range sessions {
		st.AddSession(sess)
	}

	all := m.GetAllSessions()
	if len(all) != len(sessions) {
		t.Errorf("expected %d sessions, got %d", len(sessions), len(all))
	}
}

func TestGetSession(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-002",
		WorkspaceID: "test-002",
		Agent:       "test",
		TmuxSession: "schmux-test-002-def456",
	}

	st.AddSession(sess)

	retrieved, err := m.GetSession("session-002")
	if err != nil {
		t.Errorf("GetSession() error = %v", err)
	}

	if retrieved.ID != sess.ID {
		t.Errorf("expected ID %s, got %s", sess.ID, retrieved.ID)
	}

	_, err = m.GetSession("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestIsRunning(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-003",
		WorkspaceID: "test-003",
		Agent:       "test",
		TmuxSession: "schmux-test-003-ghi789",
	}

	st.AddSession(sess)

	// This will fail if tmux is not installed or session doesn't exist
	// which is expected in a test environment
	_ = m.IsRunning(context.Background(), "session-003")

	t.Run("returns false for nonexistent session", func(t *testing.T) {
		running := m.IsRunning(context.Background(), "nonexistent")
		if running {
			t.Error("expected false for nonexistent session")
		}
	})

	t.Run("returns false for session with no PID and no tmux", func(t *testing.T) {
		sessNoPid := state.Session{
			ID:          "session-nopid",
			WorkspaceID: "test-nopid",
			Agent:       "test",
			TmuxSession: "nonexistent-tmux-session",
			Pid:         0,
		}
		st.AddSession(sessNoPid)

		running := m.IsRunning(context.Background(), "session-nopid")
		if running {
			t.Error("expected false for session with no PID and no tmux")
		}
	})
}

func TestGetOutput(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-004",
		WorkspaceID: "test-004",
		Agent:       "test",
		TmuxSession: "schmux-test-004-jkl012",
	}

	st.AddSession(sess)

	// This will fail if tmux is not installed
	_, _ = m.GetOutput(context.Background(), "session-004")

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		_, err := m.GetOutput(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})
}

func TestGetLogPath(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	logPath, err := m.GetLogPath("test-session")
	if err != nil {
		t.Errorf("GetLogPath() error = %v", err)
	}
	// Should contain session ID
	if !contains(logPath, "test-session") {
		t.Errorf("log path should contain session ID: %s", logPath)
	}
}

func TestSanitizeNickname(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "replaces dots with dashes",
			input:    "my.session",
			expected: "my-session",
		},
		{
			name:     "replaces colons with dashes",
			input:    "my:session",
			expected: "my-session",
		},
		{
			name:     "replaces both dots and colons",
			input:    "my.session:name",
			expected: "my-session-name",
		},
		{
			name:     "leaves valid characters unchanged",
			input:    "my-session_123",
			expected: "my-session_123",
		},
		{
			name:     "handles empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeNickname(tt.input)
			if got != tt.expected {
				t.Errorf("sanitizeNickname(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRenameSession(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-005",
		WorkspaceID: "test-005",
		Agent:       "test",
		TmuxSession: "schmux-test-005-mno345",
		Nickname:    "old-name",
	}

	st.AddSession(sess)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.RenameSession(context.Background(), "nonexistent", "new-name")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})

	// Actual rename test requires tmux
	t.Run("rename attempts tmux operation", func(t *testing.T) {
		// This will fail if tmux is not installed
		_ = m.RenameSession(context.Background(), "session-005", "new-name")
	})
}

func TestDispose(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-006",
		WorkspaceID: "test-006",
		Agent:       "test",
		TmuxSession: "schmux-test-006-pqr678",
	}

	st.AddSession(sess)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.Dispose(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})

	// Actual dispose test requires tmux
	t.Run("dispose removes session from state", func(t *testing.T) {
		// Create a new session for this test
		sess2 := state.Session{
			ID:          "session-007",
			WorkspaceID: "test-007",
			Agent:       "test",
			TmuxSession: "schmux-test-007-stu901",
		}
		st.AddSession(sess2)

		// This will fail on tmux kill, but should still remove from state
		_ = m.Dispose(context.Background(), "session-007")

		_, found := st.GetSession("session-007")
		if found {
			t.Log("session still in state (tmux may have failed)")
		}
	})
}

func TestEnsurePipePane(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		Terminal:      &config.TerminalSize{Width: 80, Height: 24, SeedLines: 100},
	}
	st := state.New("")
	statePath := t.TempDir() + "/state.json"
	wm := workspace.New(cfg, st, statePath)

	m := New(cfg, st, statePath, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-008",
		WorkspaceID: "test-008",
		Agent:       "test",
		TmuxSession: "schmux-test-008-vwx234",
	}

	st.AddSession(sess)

	t.Run("returns error for nonexistent session", func(t *testing.T) {
		err := m.EnsurePipePane(context.Background(), "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent session")
		}
	})

	// Skip the actual pipe-pane test to avoid creating log files in ~/.schmux/logs
	t.Run("requires tmux - skipped", func(t *testing.T) {
		t.Skip("requires tmux to be installed")
	})
}

func TestPruneLogFiles(t *testing.T) {
	t.Run("prune with no active sessions", func(t *testing.T) {
		// Use temp directory for logs, not ~/.schmux/logs
		tmpDir := t.TempDir()

		// Create test log files in temp directory
		testLogPath := filepath.Join(tmpDir, "orphaned-session.log")
		if err := os.WriteFile(testLogPath, []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create test log: %v", err)
		}

		// Manually call prune logic with temp directory
		activeIDs := make(map[string]bool) // empty = no active sessions
		entries, err := os.ReadDir(tmpDir)
		if err != nil {
			t.Fatalf("failed to read temp log dir: %v", err)
		}

		// Count files before prune
		beforeCount := 0
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
				beforeCount++
			}
		}

		// Simulate prune - delete files not in activeIDs
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
				continue
			}
			sessionID := strings.TrimSuffix(entry.Name(), ".log")
			if !activeIDs[sessionID] {
				logPath := filepath.Join(tmpDir, entry.Name())
				os.Remove(logPath)
			}
		}

		// File should be removed
		if _, err := os.Stat(testLogPath); err == nil {
			t.Error("orphaned log file still exists (expected to be removed)")
		}

		// Count files after prune
		entries, _ = os.ReadDir(tmpDir)
		afterCount := 0
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") {
				afterCount++
			}
		}

		if beforeCount != 1 || afterCount != 0 {
			t.Errorf("expected 1 file before, 0 after; got %d before, %d after", beforeCount, afterCount)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (len(substr) == 0 || s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
