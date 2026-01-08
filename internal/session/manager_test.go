package session

import (
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
	"github.com/sergek/schmux/internal/workspace"
)

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
		Agents: []config.Agent{
			{Name: "test", Command: "test"},
		},
	}
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)
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
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Agent:       "test",
		Prompt:      "test prompt",
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
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)

	_, err := m.GetAttachCommand("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestGetAllSessions(t *testing.T) {
	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)

	// Clear existing sessions
	st.Sessions = []state.Session{}

	// Add test sessions
	sessions := []state.Session{
		{ID: "s1", WorkspaceID: "w1", Agent: "a1", Prompt: "p1", TmuxSession: "t1"},
		{ID: "s2", WorkspaceID: "w2", Agent: "a2", Prompt: "p2", TmuxSession: "t2"},
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
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-002",
		WorkspaceID: "test-002",
		Agent:       "test",
		Prompt:      "test prompt",
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
	st := state.New()
	wm := workspace.New(cfg, st)

	m := New(cfg, st, wm)

	// Add a test session
	sess := state.Session{
		ID:          "session-003",
		WorkspaceID: "test-003",
		Agent:       "test",
		Prompt:      "test prompt",
		TmuxSession: "schmux-test-003-ghi789",
	}

	st.AddSession(sess)

	// This will fail if tmux is not installed or session doesn't exist
	// which is expected in a test environment
	_ = m.IsRunning("session-003")
}
