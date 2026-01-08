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
	s := New()

	w := Workspace{
		ID:     "test-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test-001",
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
	if retrieved.Branch != w.Branch {
		t.Errorf("expected Branch %s, got %s", w.Branch, retrieved.Branch)
	}
}

func TestUpdateWorkspace(t *testing.T) {
	s := New()

	w := Workspace{
		ID:     "test-002",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test-002",
	}

	s.AddWorkspace(w)

	// Update workspace branch
	w.Branch = "develop"
	s.UpdateWorkspace(w)

	retrieved, found := s.GetWorkspace("test-002")
	if !found {
		t.Fatal("workspace not found")
	}

	if retrieved.Branch != "develop" {
		t.Errorf("expected Branch to be develop, got %s", retrieved.Branch)
	}
}

func TestAddAndGetSession(t *testing.T) {
	s := New()

	sess := Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Agent:       "claude",
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
	s := New()

	sess := Session{
		ID:          "session-002",
		WorkspaceID: "test-001",
		Agent:       "codex",
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
	s := New()

	// Clear existing sessions
	s.Sessions = []Session{}

	sessions := []Session{
		{ID: "s1", WorkspaceID: "w1", Agent: "a1", Prompt: "p1", TmuxSession: "t1", CreatedAt: time.Now()},
		{ID: "s2", WorkspaceID: "w2", Agent: "a2", Prompt: "p2", TmuxSession: "t2", CreatedAt: time.Now()},
	}

	for _, sess := range sessions {
		s.AddSession(sess)
	}

	retrieved := s.GetSessions()
	if len(retrieved) != len(sessions) {
		t.Errorf("expected %d sessions, got %d", len(sessions), len(retrieved))
	}
}
