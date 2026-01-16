package state

import (
	"os"
	"path/filepath"
	"strings"
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
	s := New("")

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
	s := New("")

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
	s := New("")

	sess := Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Target:       "claude",
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
	if retrieved.Target != sess.Target {
		t.Errorf("expected Target %s, got %s", sess.Target, retrieved.Target)
	}
}

func TestRemoveSession(t *testing.T) {
	s := New("")

	sess := Session{
		ID:          "session-002",
		WorkspaceID: "test-001",
		Target:       "codex",
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
	s := New("")

	// Clear existing sessions
	s.Sessions = []Session{}

	sessions := []Session{
		{ID: "s1", WorkspaceID: "w1", Target: "a1", TmuxSession: "t1", CreatedAt: time.Now()},
		{ID: "s2", WorkspaceID: "w2", Target: "a2", TmuxSession: "t2", CreatedAt: time.Now()},
	}

	for _, sess := range sessions {
		s.AddSession(sess)
	}

	retrieved := s.GetSessions()
	if len(retrieved) != len(sessions) {
		t.Errorf("expected %d sessions, got %d", len(sessions), len(retrieved))
	}
}

// Error path tests

func TestUpdateWorkspaceNotFound(t *testing.T) {
	s := New("")

	w := Workspace{
		ID:     "nonexistent",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	}

	err := s.UpdateWorkspace(w)
	if err == nil {
		t.Fatal("expected error when updating nonexistent workspace, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestUpdateSessionNotFound(t *testing.T) {
	s := New("")

	sess := Session{
		ID:          "nonexistent",
		WorkspaceID: "test-001",
		Target:       "claude",
		TmuxSession: "test",
		CreatedAt:   time.Now(),
	}

	err := s.UpdateSession(sess)
	if err == nil {
		t.Fatal("expected error when updating nonexistent session, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestSaveEmptyPath(t *testing.T) {
	s := New("")

	err := s.Save()
	if err == nil {
		t.Fatal("expected error when saving with empty path, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected 'empty' error, got: %v", err)
	}
}

func TestSaveValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := tmpDir + "/state.json"
	s := New(statePath)

	w := Workspace{
		ID:     "test-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	}
	s.AddWorkspace(w)

	err := s.Save()
	if err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	// Verify the file was created
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("state file is empty")
	}
}

func TestUpdateWorkspaceThenSave(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := tmpDir + "/state.json"
	s := New(statePath)

	w := Workspace{
		ID:     "test-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	}
	s.AddWorkspace(w)

	// Update the workspace
	w.Branch = "develop"
	err := s.UpdateWorkspace(w)
	if err != nil {
		t.Fatalf("failed to update workspace: %v", err)
	}

	// Save and reload
	err = s.Save()
	if err != nil {
		t.Fatalf("failed to save state: %v", err)
	}

	s2, err := Load(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	retrieved, found := s2.GetWorkspace("test-001")
	if !found {
		t.Fatal("workspace not found after reload")
	}
	if retrieved.Branch != "develop" {
		t.Errorf("expected branch 'develop', got '%s'", retrieved.Branch)
	}
}
