package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	// Create a temporary state directory
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// This test would require mocking the home directory
	// For now, we'll skip the actual load test
	t.Skip("requires home directory mocking")
}

func TestAddAndGetWorkspace(t *testing.T) {
	s := Get()

	w := Workspace{
		ID:     "test-001",
		Repo:   "test",
		Path:   "/tmp/test-001",
		InUse:  false,
		Usable: true,
	}

	s.AddWorkspace(w)

	retrieved, found := s.GetWorkspace("test-001")
	if !found {
		t.Fatal("workspace not found")
	}

	if retrieved.ID != w.ID {
		t.Errorf("expected ID %s, got %s", w.ID, retrieved.ID)
	}
	if retrieved.Repo != w.Repo {
		t.Errorf("expected Repo %s, got %s", w.Repo, retrieved.Repo)
	}
}

func TestUpdateWorkspace(t *testing.T) {
	s := Get()

	w := Workspace{
		ID:     "test-002",
		Repo:   "test",
		Path:   "/tmp/test-002",
		InUse:  false,
		Usable: true,
	}

	s.AddWorkspace(w)

	// Update workspace
	w.InUse = true
	w.SessionID = "session-123"
	s.UpdateWorkspace(w)

	retrieved, found := s.GetWorkspace("test-002")
	if !found {
		t.Fatal("workspace not found")
	}

	if !retrieved.InUse {
		t.Error("expected InUse to be true")
	}
	if retrieved.SessionID != "session-123" {
		t.Errorf("expected SessionID session-123, got %s", retrieved.SessionID)
	}
}

func TestFindAvailableWorkspace(t *testing.T) {
	s := Get()

	s.Workspaces = []Workspace{
		{ID: "test-001", Repo: "test", Path: "/tmp/test-001", InUse: true, Usable: true},
		{ID: "test-002", Repo: "test", Path: "/tmp/test-002", InUse: false, Usable: false},
		{ID: "test-003", Repo: "test", Path: "/tmp/test-003", InUse: false, Usable: true},
		{ID: "other-001", Repo: "other", Path: "/tmp/other-001", InUse: false, Usable: true},
	}

	// Should find test-003 (available and usable)
	w, found := s.FindAvailableWorkspace("test")
	if !found {
		t.Fatal("available workspace not found")
	}
	if w.ID != "test-003" {
		t.Errorf("expected ID test-003, got %s", w.ID)
	}

	// Should not find any for "other" if we're looking for "test"
	_, found = s.FindAvailableWorkspace("nonexistent")
	if found {
		t.Error("expected not to find workspace for nonexistent repo")
	}
}

func TestAddAndGetSession(t *testing.T) {
	s := Get()

	sess := Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Agent:       "claude",
		Branch:      "main",
		Prompt:      "fix the bug",
		TmuxSession: "schmux-test-001-abc123",
		CreatedAt:   time.Now(),
	}

	s.AddSession(sess)

	retrieved, found := s.GetSession("session-001")
	if !found {
		t.Fatal("session not found")
	}

	if retrieved.ID != sess.ID {
		t.Errorf("expected ID %s, got %s", sess.ID, retrieved.ID)
	}
	if retrieved.Agent != sess.Agent {
		t.Errorf("expected Agent %s, got %s", sess.Agent, retrieved.Agent)
	}
}

func TestRemoveSession(t *testing.T) {
	s := Get()

	sess := Session{
		ID:          "session-002",
		WorkspaceID: "test-001",
		Agent:       "codex",
		Branch:      "main",
		Prompt:      "add feature",
		TmuxSession: "schmux-test-001-def456",
		CreatedAt:   time.Now(),
	}

	s.AddSession(sess)

	// Remove session
	s.RemoveSession("session-002")

	_, found := s.GetSession("session-002")
	if found {
		t.Error("session should have been removed")
	}
}

func TestGetSessions(t *testing.T) {
	s := Get()

	// Clear existing sessions
	s.Sessions = []Session{}

	sessions := []Session{
		{ID: "s1", WorkspaceID: "w1", Agent: "a1", Branch: "main", Prompt: "p1", TmuxSession: "t1", CreatedAt: time.Now()},
		{ID: "s2", WorkspaceID: "w2", Agent: "a2", Branch: "main", Prompt: "p2", TmuxSession: "t2", CreatedAt: time.Now()},
	}

	for _, sess := range sessions {
		s.AddSession(sess)
	}

	retrieved := s.GetSessions()
	if len(retrieved) != len(sessions) {
		t.Errorf("expected %d sessions, got %d", len(sessions), len(retrieved))
	}
}
