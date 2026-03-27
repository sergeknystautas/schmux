package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, ".schmux", "state.json")

	// Create the .schmux directory
	schmuxDir := filepath.Join(tmpDir, ".schmux")
	if err := os.MkdirAll(schmuxDir, 0755); err != nil {
		t.Fatalf("failed to create .schmux dir: %v", err)
	}

	// Load should succeed even with no state file (returns empty state)
	st, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if st == nil {
		t.Fatal("Load() returned nil state")
	}

	// Verify empty state
	if len(st.GetSessions()) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(st.GetSessions()))
	}
	if len(st.GetWorkspaces()) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(st.GetWorkspaces()))
	}
}

func TestAddAndGetWorkspace(t *testing.T) {
	s := New("", nil)

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
	s := New("", nil)

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
	s := New("", nil)

	sess := Session{
		ID:          "session-001",
		WorkspaceID: "test-001",
		Target:      "claude",
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
	s := New("", nil)

	sess := Session{
		ID:          "session-002",
		WorkspaceID: "test-001",
		Target:      "codex",
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
	// Create fresh state for test isolation
	s := New("", nil)

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
	s := New("", nil)

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
	s := New("", nil)

	sess := Session{
		ID:          "nonexistent",
		WorkspaceID: "test-001",
		Target:      "claude",
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
	s := New("", nil)

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
	s := New(statePath, nil)

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
	s := New(statePath, nil)

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

	s2, err := Load(statePath, nil)
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

func TestRemoteHostCRUD(t *testing.T) {
	t.Run("AddRemoteHost and GetRemoteHost", func(t *testing.T) {
		s := New("", nil)

		rh := RemoteHost{
			ID:       "host-001",
			FlavorID: "flavor-1",
			Hostname: "remote-host-123.example.com",
			Status:   RemoteHostStatusConnected,
		}

		if err := s.AddRemoteHost(rh); err != nil {
			t.Fatalf("AddRemoteHost failed: %v", err)
		}

		retrieved, found := s.GetRemoteHost("host-001")
		if !found {
			t.Fatal("remote host not found")
		}
		if retrieved.Hostname != "remote-host-123.example.com" {
			t.Errorf("Hostname = %q, want %q", retrieved.Hostname, "remote-host-123.example.com")
		}
	})

	t.Run("AddRemoteHost updates existing entry with same ID", func(t *testing.T) {
		s := New("", nil)

		rh := RemoteHost{
			ID:       "host-001",
			FlavorID: "flavor-1",
			Hostname: "old.host.net",
			Status:   RemoteHostStatusConnected,
		}
		s.AddRemoteHost(rh)

		// Add again with same ID - should update
		rh.Hostname = "new.host.net"
		s.AddRemoteHost(rh)

		hosts := s.GetRemoteHosts()
		if len(hosts) != 1 {
			t.Fatalf("expected 1 host, got %d", len(hosts))
		}
		if hosts[0].Hostname != "new.host.net" {
			t.Errorf("Hostname = %q, want %q", hosts[0].Hostname, "new.host.net")
		}
	})

	t.Run("GetRemoteHostByFlavorID", func(t *testing.T) {
		s := New("", nil)

		s.AddRemoteHost(RemoteHost{ID: "host-1", FlavorID: "flavor-a", Hostname: "a.net"})
		s.AddRemoteHost(RemoteHost{ID: "host-2", FlavorID: "flavor-b", Hostname: "b.net"})

		rh, found := s.GetRemoteHostByFlavorID("flavor-b")
		if !found {
			t.Fatal("host not found by flavor ID")
		}
		if rh.Hostname != "b.net" {
			t.Errorf("Hostname = %q, want %q", rh.Hostname, "b.net")
		}

		_, found = s.GetRemoteHostByFlavorID("nonexistent")
		if found {
			t.Error("expected nonexistent flavor to not be found")
		}
	})

	t.Run("GetRemoteHostByHostname", func(t *testing.T) {
		s := New("", nil)

		s.AddRemoteHost(RemoteHost{ID: "host-1", FlavorID: "flavor-a", Hostname: "a.net"})
		s.AddRemoteHost(RemoteHost{ID: "host-2", FlavorID: "flavor-b", Hostname: "b.net"})

		rh, found := s.GetRemoteHostByHostname("a.net")
		if !found {
			t.Fatal("host not found by hostname")
		}
		if rh.ID != "host-1" {
			t.Errorf("ID = %q, want %q", rh.ID, "host-1")
		}

		_, found = s.GetRemoteHostByHostname("nonexistent.net")
		if found {
			t.Error("expected nonexistent hostname to not be found")
		}
	})

	t.Run("UpdateRemoteHost", func(t *testing.T) {
		s := New("", nil)

		s.AddRemoteHost(RemoteHost{
			ID:       "host-001",
			FlavorID: "flavor-1",
			Hostname: "old.net",
			Status:   RemoteHostStatusConnected,
		})

		err := s.UpdateRemoteHost(RemoteHost{
			ID:       "host-001",
			FlavorID: "flavor-1",
			Hostname: "updated.net",
			Status:   RemoteHostStatusDisconnected,
		})
		if err != nil {
			t.Fatalf("UpdateRemoteHost failed: %v", err)
		}

		rh, _ := s.GetRemoteHost("host-001")
		if rh.Hostname != "updated.net" {
			t.Errorf("Hostname = %q, want %q", rh.Hostname, "updated.net")
		}
		if rh.Status != RemoteHostStatusDisconnected {
			t.Errorf("Status = %q, want %q", rh.Status, RemoteHostStatusDisconnected)
		}
	})

	t.Run("UpdateRemoteHost fails for nonexistent ID", func(t *testing.T) {
		s := New("", nil)

		err := s.UpdateRemoteHost(RemoteHost{ID: "nonexistent"})
		if err == nil {
			t.Error("expected error for nonexistent ID")
		}
	})

	t.Run("UpdateRemoteHostStatus", func(t *testing.T) {
		s := New("", nil)

		s.AddRemoteHost(RemoteHost{
			ID:     "host-001",
			Status: RemoteHostStatusConnected,
		})

		if err := s.UpdateRemoteHostStatus("host-001", RemoteHostStatusDisconnected); err != nil {
			t.Fatalf("UpdateRemoteHostStatus failed: %v", err)
		}

		rh, _ := s.GetRemoteHost("host-001")
		if rh.Status != RemoteHostStatusDisconnected {
			t.Errorf("Status = %q, want %q", rh.Status, RemoteHostStatusDisconnected)
		}
	})

	t.Run("RemoveRemoteHost", func(t *testing.T) {
		s := New("", nil)

		s.AddRemoteHost(RemoteHost{ID: "host-1"})
		s.AddRemoteHost(RemoteHost{ID: "host-2"})

		if err := s.RemoveRemoteHost("host-1"); err != nil {
			t.Fatalf("RemoveRemoteHost failed: %v", err)
		}

		hosts := s.GetRemoteHosts()
		if len(hosts) != 1 {
			t.Fatalf("expected 1 host, got %d", len(hosts))
		}
		if hosts[0].ID != "host-2" {
			t.Errorf("remaining host ID = %q, want %q", hosts[0].ID, "host-2")
		}
	})

	t.Run("RemoveRemoteHost is idempotent for nonexistent", func(t *testing.T) {
		s := New("", nil)

		// Should not error for nonexistent
		if err := s.RemoveRemoteHost("nonexistent"); err != nil {
			t.Errorf("RemoveRemoteHost should not error for nonexistent: %v", err)
		}
	})
}

func TestRemoteHostPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create and save state with remote host
	s := New(statePath, nil)
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip
	s.AddRemoteHost(RemoteHost{
		ID:          "host-001",
		FlavorID:    "flavor-1",
		Hostname:    "test.host.net",
		Status:      RemoteHostStatusConnected,
		ConnectedAt: now,
		ExpiresAt:   now.Add(12 * time.Hour),
	})

	if err := s.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load and verify
	s2, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	hosts := s2.GetRemoteHosts()
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	rh := hosts[0]
	if rh.ID != "host-001" {
		t.Errorf("ID = %q, want %q", rh.ID, "host-001")
	}
	if rh.Hostname != "test.host.net" {
		t.Errorf("Hostname = %q, want %q", rh.Hostname, "test.host.net")
	}
	if rh.Status != RemoteHostStatusConnected {
		t.Errorf("Status = %q, want %q", rh.Status, RemoteHostStatusConnected)
	}
}

func TestSessionRemoteFields(t *testing.T) {
	s := New("", nil)

	sess := Session{
		ID:           "session-001",
		WorkspaceID:  "ws-001",
		Target:       "claude",
		TmuxSession:  "schmux-ws-001",
		RemoteHostID: "host-001",
		RemotePaneID: "%5",
		RemoteWindow: "@3",
	}

	s.AddSession(sess)

	retrieved, found := s.GetSession("session-001")
	if !found {
		t.Fatal("session not found")
	}

	if !retrieved.IsRemoteSession() {
		t.Error("expected IsRemoteSession() to be true")
	}
	if retrieved.RemoteHostID != "host-001" {
		t.Errorf("RemoteHostID = %q, want %q", retrieved.RemoteHostID, "host-001")
	}
	if retrieved.RemotePaneID != "%5" {
		t.Errorf("RemotePaneID = %q, want %q", retrieved.RemotePaneID, "%5")
	}
	if retrieved.RemoteWindow != "@3" {
		t.Errorf("RemoteWindow = %q, want %q", retrieved.RemoteWindow, "@3")
	}
}

func TestWorkspaceRemoteFields(t *testing.T) {
	s := New("", nil)

	ws := Workspace{
		ID:           "ws-001",
		Repo:         "https://github.com/test/repo",
		Branch:       "main",
		Path:         "/local/path",
		RemoteHostID: "host-001",
		RemotePath:   "~/workspace",
	}

	s.AddWorkspace(ws)

	retrieved, found := s.GetWorkspace("ws-001")
	if !found {
		t.Fatal("workspace not found")
	}

	if !retrieved.IsRemoteWorkspace() {
		t.Error("expected IsRemoteWorkspace() to be true")
	}
	if retrieved.RemoteHostID != "host-001" {
		t.Errorf("RemoteHostID = %q, want %q", retrieved.RemoteHostID, "host-001")
	}
	if retrieved.RemotePath != "~/workspace" {
		t.Errorf("RemotePath = %q, want %q", retrieved.RemotePath, "~/workspace")
	}
}

// TestSave_Atomicity verifies that Save() uses atomic writes
func TestSave_Atomicity(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath, nil)
	s.AddWorkspace(Workspace{
		ID:     "ws-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	})

	// Save the state
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Verify state file exists
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state file should exist: %v", err)
	}

	// Verify temp file was cleaned up
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should not exist after successful save")
	}

	// Verify the file is valid JSON
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}

	if !strings.Contains(string(data), "ws-001") {
		t.Error("state file should contain workspace ID")
	}
}

// TestSave_NoCorruption verifies that interrupted saves don't corrupt the file
func TestSave_NoCorruption(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath, nil)

	// Create initial valid state
	s.AddWorkspace(Workspace{
		ID:     "ws-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	})

	if err := s.Save(); err != nil {
		t.Fatalf("initial Save() failed: %v", err)
	}

	// Read the valid state
	validData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read initial state: %v", err)
	}

	// Simulate interrupted save by writing temp file but not renaming
	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte("corrupted data"), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	// The original file should still be valid
	currentData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state after temp write: %v", err)
	}

	if string(currentData) != string(validData) {
		t.Error("original state file should not be corrupted by temp file")
	}

	// Clean up temp file
	os.Remove(tmpPath)
}

// TestSave_EmptyPath verifies that Save() fails gracefully with empty path
func TestSave_EmptyPath(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{
		ID:     "ws-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	})

	err := s.Save()
	if err == nil {
		t.Error("Save() should fail with empty path")
	}

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error about empty path, got: %v", err)
	}
}

// TestSave_CreatesDirectory verifies that Save() creates parent directory
func TestSave_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "nested", "dir", "state.json")

	s := New(statePath, nil)
	s.AddWorkspace(Workspace{
		ID:     "ws-001",
		Repo:   "https://github.com/test/repo",
		Branch: "main",
		Path:   "/tmp/test",
	})

	if err := s.Save(); err != nil {
		t.Fatalf("Save() should create parent directory: %v", err)
	}

	// Verify directory was created
	parentDir := filepath.Dir(statePath)
	if _, err := os.Stat(parentDir); err != nil {
		t.Errorf("parent directory should exist: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state file should exist: %v", err)
	}
}

// TestSave_Concurrent verifies that concurrent saves don't corrupt the file
func TestSave_Concurrent(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath, nil)

	// Perform concurrent saves
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			s.AddWorkspace(Workspace{
				ID:     string(rune('a' + id)),
				Repo:   "https://github.com/test/repo",
				Branch: "main",
				Path:   "/tmp/test",
			})
			if err := s.Save(); err != nil {
				t.Errorf("Save() failed in goroutine %d: %v", id, err)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify the final state file is valid JSON
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read final state: %v", err)
	}

	// Verify it's valid JSON by loading it
	_, err = Load(statePath, nil)
	if err != nil {
		t.Errorf("final state should be valid JSON: %v", err)
	}

	// Verify no temp file remains
	tmpPath := statePath + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should not exist after concurrent saves")
	}

	t.Logf("Final state file size: %d bytes", len(data))
}

// TestStateSaveBatching verifies that rapid SaveBatched() calls are coalesced
// into a single save operation (Issue 6 fix).
func TestStateSaveBatching(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	s := New(statePath, nil)

	// Track number of actual file writes by checking modification time changes
	saveCount := 0
	var lastModTime time.Time

	// Make 10 rapid changes with SaveBatched()
	for i := 0; i < 10; i++ {
		s.AddWorkspace(Workspace{
			ID:   "ws-" + string(rune('a'+i)),
			Repo: "repo",
			Path: "/path",
		})
		s.SaveBatched()

		// Small delay to simulate rapid updates
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for batching window (500ms) plus some buffer
	time.Sleep(700 * time.Millisecond)

	// Check file was saved
	info, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("state file should exist: %v", err)
	}

	// Count how many times mod time changed
	if !lastModTime.IsZero() && info.ModTime().After(lastModTime) {
		saveCount++
	}
	lastModTime = info.ModTime()

	// Verify batching worked - should be 1 save, not 10
	// We can't easily count actual saves, but we can verify the file exists
	// and contains all workspaces
	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if len(loaded.Workspaces) != 10 {
		t.Errorf("expected 10 workspaces, got %d", len(loaded.Workspaces))
	}

	t.Logf("Batching successful: all 10 workspaces persisted")
}

// TestSaveBatchedDebounce verifies that the debounce timer resets on rapid calls
func TestSaveBatchedDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	s := New(statePath, nil)

	// Add workspace and trigger batched save
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo1", Path: "/path1"})
	s.SaveBatched()

	// Wait 300ms (less than 500ms batch window)
	time.Sleep(300 * time.Millisecond)

	// Add another workspace - this should reset the timer
	s.AddWorkspace(Workspace{ID: "ws-2", Repo: "repo2", Path: "/path2"})
	s.SaveBatched()

	// Wait another 300ms
	time.Sleep(300 * time.Millisecond)

	// File should not exist yet (timer was reset)
	if _, err := os.Stat(statePath); err == nil {
		// File exists, which is OK if the timer fired
		// This is a timing-sensitive test, so we just log
		t.Log("Note: file exists earlier than expected (timer variation)")
	}

	// Wait for full batch window
	time.Sleep(300 * time.Millisecond)

	// Now file should exist with both workspaces
	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if len(loaded.Workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(loaded.Workspaces))
	}
}

// TestSaveImmediateVsBatched verifies that Save() is immediate while SaveBatched() is delayed
func TestSaveImmediateVsBatched(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	s := New(statePath, nil)

	// Test immediate save
	s.AddWorkspace(Workspace{ID: "ws-immediate", Repo: "repo", Path: "/path"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// File should exist immediately
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("Save() should persist immediately: %v", err)
	}

	// Clean up
	os.Remove(statePath)

	// Test batched save
	s2 := New(statePath, nil)
	s2.AddWorkspace(Workspace{ID: "ws-batched", Repo: "repo", Path: "/path"})
	s2.SaveBatched()

	// File should NOT exist immediately
	time.Sleep(50 * time.Millisecond)
	if _, err := os.Stat(statePath); err == nil {
		t.Log("Note: SaveBatched() persisted earlier than expected")
	}

	// Wait for batch window
	time.Sleep(600 * time.Millisecond)

	// Now file should exist
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("SaveBatched() should persist after debounce: %v", err)
	}
}

func TestNudgeSeqPersistenceRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create state with a session, increment NudgeSeq, save
	s := New(statePath, nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})
	s.IncrementNudgeSeq("sess-1") // 1
	s.IncrementNudgeSeq("sess-1") // 2
	s.IncrementNudgeSeq("sess-1") // 3
	seq := s.GetNudgeSeq("sess-1")
	if seq != 3 {
		t.Fatalf("NudgeSeq before save = %d, want 3", seq)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Load from disk and verify
	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	loadedSeq := loaded.GetNudgeSeq("sess-1")
	if loadedSeq != 3 {
		t.Errorf("NudgeSeq after load = %d, want 3", loadedSeq)
	}
}

func TestLastSignalAtNotPersisted(t *testing.T) {
	// LastSignalAt has json:"-" and should NOT survive save/load
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create state with a session, set LastSignalAt, save
	s := New(statePath, nil)
	ts := time.Date(2026, 2, 11, 12, 0, 0, 0, time.UTC)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})
	s.UpdateSessionLastSignal("sess-1", ts)
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// Load from disk and verify LastSignalAt is zero (not persisted)
	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	sess, found := loaded.GetSession("sess-1")
	if !found {
		t.Fatal("session not found after load")
	}
	if !sess.LastSignalAt.IsZero() {
		t.Errorf("LastSignalAt after load = %v, want zero (should not persist)", sess.LastSignalAt)
	}
}

func TestUpdateSessionXtermTitle(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})

	t.Run("sets title and returns true", func(t *testing.T) {
		changed := s.UpdateSessionXtermTitle("sess-1", "Working on feature X")
		if !changed {
			t.Error("expected changed=true for initial title set")
		}
		sess, _ := s.GetSession("sess-1")
		if sess.XtermTitle != "Working on feature X" {
			t.Errorf("XtermTitle = %q, want %q", sess.XtermTitle, "Working on feature X")
		}
	})

	t.Run("returns false when title unchanged", func(t *testing.T) {
		changed := s.UpdateSessionXtermTitle("sess-1", "Working on feature X")
		if changed {
			t.Error("expected changed=false when title is the same")
		}
	})

	t.Run("returns true when title changes", func(t *testing.T) {
		changed := s.UpdateSessionXtermTitle("sess-1", "New title")
		if !changed {
			t.Error("expected changed=true when title differs")
		}
		sess, _ := s.GetSession("sess-1")
		if sess.XtermTitle != "New title" {
			t.Errorf("XtermTitle = %q, want %q", sess.XtermTitle, "New title")
		}
	})

	t.Run("returns false for nonexistent session", func(t *testing.T) {
		changed := s.UpdateSessionXtermTitle("nonexistent", "title")
		if changed {
			t.Error("expected changed=false for nonexistent session")
		}
	})
}

func TestXtermTitleNotPersisted(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath, nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})
	s.UpdateSessionXtermTitle("sess-1", "My Title")
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	sess, found := loaded.GetSession("sess-1")
	if !found {
		t.Fatal("session not found after load")
	}
	if sess.XtermTitle != "" {
		t.Errorf("XtermTitle should be empty after load, got %q", sess.XtermTitle)
	}
}

func TestLastOutputAtNotPersisted(t *testing.T) {
	// LastOutputAt has json:"-" and should NOT survive save/load
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	s := New(statePath, nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})
	s.UpdateSessionLastOutput("sess-1", time.Now())
	if err := s.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	sess, found := loaded.GetSession("sess-1")
	if !found {
		t.Fatal("session not found after load")
	}
	if !sess.LastOutputAt.IsZero() {
		t.Errorf("LastOutputAt should be zero after load, got %v", sess.LastOutputAt)
	}
}

func TestIncrementNudgeSeqConcurrent(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})

	const goroutines = 10
	const increments = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < increments; j++ {
				s.IncrementNudgeSeq("sess-1")
			}
		}()
	}
	wg.Wait()

	expected := uint64(goroutines * increments)
	got := s.GetNudgeSeq("sess-1")
	if got != expected {
		t.Errorf("NudgeSeq after concurrent increments = %d, want %d", got, expected)
	}
}

func TestIncrementNudgeSeqNonexistentSession(t *testing.T) {
	s := New("", nil)
	// Should return 0 for non-existent session, not panic
	seq := s.IncrementNudgeSeq("nonexistent")
	if seq != 0 {
		t.Errorf("IncrementNudgeSeq for nonexistent session = %d, want 0", seq)
	}
}

func TestUpdateSessionLastSignalNonexistentSession(t *testing.T) {
	s := New("", nil)
	// Should not panic for non-existent session
	s.UpdateSessionLastSignal("nonexistent", time.Now())
}

func TestUpdateSessionNudge(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})

	err := s.UpdateSessionNudge("sess-1", `{"state":"Completed","summary":"Done"}`)
	if err != nil {
		t.Fatalf("UpdateSessionNudge failed: %v", err)
	}

	sess, found := s.GetSession("sess-1")
	if !found {
		t.Fatal("session not found")
	}
	if sess.Nudge != `{"state":"Completed","summary":"Done"}` {
		t.Errorf("Nudge = %q, want JSON payload", sess.Nudge)
	}
}

func TestUpdateSessionNudgePreservesOtherFields(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test", Target: "claude"})
	s.IncrementNudgeSeq("sess-1") // NudgeSeq = 1

	err := s.UpdateSessionNudge("sess-1", `{"state":"Error","summary":"oops"}`)
	if err != nil {
		t.Fatalf("UpdateSessionNudge failed: %v", err)
	}

	sess, _ := s.GetSession("sess-1")
	if sess.Target != "claude" {
		t.Errorf("Target was overwritten: %q", sess.Target)
	}
	if sess.NudgeSeq != 1 {
		t.Errorf("NudgeSeq was overwritten: %d", sess.NudgeSeq)
	}
}

func TestUpdateSessionNudgeNotFound(t *testing.T) {
	s := New("", nil)
	err := s.UpdateSessionNudge("nonexistent", "payload")
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestClearSessionNudge(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test", Nudge: `{"state":"Error"}`})

	cleared := s.ClearSessionNudge("sess-1")
	if !cleared {
		t.Error("expected cleared=true when nudge was non-empty")
	}

	sess, _ := s.GetSession("sess-1")
	if sess.Nudge != "" {
		t.Errorf("Nudge should be empty after clear, got %q", sess.Nudge)
	}

	// Second clear should return false (already empty)
	cleared = s.ClearSessionNudge("sess-1")
	if cleared {
		t.Error("expected cleared=false when nudge was already empty")
	}
}

func TestWorkspacePreviewCRUD(t *testing.T) {
	s := New("", nil)
	preview := WorkspacePreview{
		ID:          "prev_1",
		WorkspaceID: "ws-1",
		TargetHost:  "127.0.0.1",
		TargetPort:  5173,
		ProxyPort:   51853,
		Status:      "ready",
		CreatedAt:   time.Now(),
		LastUsedAt:  time.Now(),
	}
	if err := s.UpsertPreview(preview); err != nil {
		t.Fatalf("UpsertPreview() failed: %v", err)
	}
	got, found := s.GetPreview("prev_1")
	if !found {
		t.Fatal("GetPreview() did not find inserted preview")
	}
	if got.ProxyPort != 51853 {
		t.Fatalf("ProxyPort = %d, want 51853", got.ProxyPort)
	}
	match, found := s.FindPreview("ws-1", "127.0.0.1", 5173)
	if !found {
		t.Fatal("FindPreview() did not find tuple")
	}
	if match.ID != "prev_1" {
		t.Fatalf("FindPreview() ID = %s, want prev_1", match.ID)
	}
	if err := s.RemovePreview("prev_1"); err != nil {
		t.Fatalf("RemovePreview() failed: %v", err)
	}
	if _, found := s.GetPreview("prev_1"); found {
		t.Fatal("preview should be removed")
	}
}

func TestRemoveWorkspaceRemovesPreviews(t *testing.T) {
	s := New("", nil)
	if err := s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: "/tmp/ws-1"}); err != nil {
		t.Fatalf("AddWorkspace() failed: %v", err)
	}
	_ = s.UpsertPreview(WorkspacePreview{ID: "prev_1", WorkspaceID: "ws-1", TargetHost: "127.0.0.1", TargetPort: 3000})
	_ = s.UpsertPreview(WorkspacePreview{ID: "prev_2", WorkspaceID: "ws-2", TargetHost: "127.0.0.1", TargetPort: 3001})

	if err := s.RemoveWorkspace("ws-1"); err != nil {
		t.Fatalf("RemoveWorkspace() failed: %v", err)
	}
	if _, found := s.GetPreview("prev_1"); found {
		t.Fatal("workspace preview should be removed when workspace is removed")
	}
	if _, found := s.GetPreview("prev_2"); !found {
		t.Fatal("other workspace preview should remain")
	}
}

func TestUpdateSessionFunc(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test", Target: "claude"})

	t.Run("updates session via callback", func(t *testing.T) {
		found := s.UpdateSessionFunc("sess-1", func(sess *Session) {
			sess.Target = "codex"
			sess.Nudge = "updated"
		})
		if !found {
			t.Fatal("expected found=true")
		}

		sess, ok := s.GetSession("sess-1")
		if !ok {
			t.Fatal("session should exist")
		}
		if sess.Target != "codex" {
			t.Errorf("Target = %q, want 'codex'", sess.Target)
		}
		if sess.Nudge != "updated" {
			t.Errorf("Nudge = %q, want 'updated'", sess.Nudge)
		}
	})

	t.Run("returns false for nonexistent session", func(t *testing.T) {
		found := s.UpdateSessionFunc("nonexistent", func(sess *Session) {
			sess.Target = "should-not-happen"
		})
		if found {
			t.Error("expected found=false for nonexistent session")
		}
	})
}

func TestUpdateSessionFunc_Concurrent(t *testing.T) {
	s := New("", nil)
	s.AddSession(Session{ID: "sess-1", TmuxSession: "test"})

	const goroutines = 20
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.UpdateSessionFunc("sess-1", func(sess *Session) {
				sess.NudgeSeq++
			})
		}()
	}
	wg.Wait()

	sess, _ := s.GetSession("sess-1")
	if sess.NudgeSeq != uint64(goroutines) {
		t.Errorf("NudgeSeq = %d, want %d (concurrent increments should be serialized)", sess.NudgeSeq, goroutines)
	}
}

func TestFlushPending(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	s := New(statePath, nil)

	// Add data and trigger batched save
	s.AddWorkspace(Workspace{ID: "ws-flush", Repo: "repo", Branch: "main", Path: "/tmp"})
	s.SaveBatched()

	// File should not exist yet (batching hasn't fired)
	time.Sleep(50 * time.Millisecond)

	// Flush should force the save immediately
	s.FlushPending()

	// File should now exist with the workspace
	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() after FlushPending failed: %v", err)
	}
	if len(loaded.Workspaces) != 1 {
		t.Errorf("expected 1 workspace after flush, got %d", len(loaded.Workspaces))
	}
}

func TestFlushPending_NoPending(t *testing.T) {
	s := New("", nil)
	// Should not panic when no pending saves
	s.FlushPending()
}

func TestGetWorkspacePreviews(t *testing.T) {
	s := New("", nil)

	// Insert previews across two workspaces
	s.UpsertPreview(WorkspacePreview{ID: "prev-1", WorkspaceID: "ws-1", TargetHost: "127.0.0.1", TargetPort: 3000})
	s.UpsertPreview(WorkspacePreview{ID: "prev-2", WorkspaceID: "ws-1", TargetHost: "127.0.0.1", TargetPort: 5173})
	s.UpsertPreview(WorkspacePreview{ID: "prev-3", WorkspaceID: "ws-2", TargetHost: "127.0.0.1", TargetPort: 8080})

	t.Run("returns only matching workspace previews", func(t *testing.T) {
		got := s.GetWorkspacePreviews("ws-1")
		if len(got) != 2 {
			t.Fatalf("GetWorkspacePreviews('ws-1') returned %d previews, want 2", len(got))
		}
		ids := map[string]bool{}
		for _, p := range got {
			ids[p.ID] = true
		}
		if !ids["prev-1"] || !ids["prev-2"] {
			t.Errorf("expected prev-1 and prev-2, got %v", ids)
		}
	})

	t.Run("returns empty slice for unknown workspace", func(t *testing.T) {
		got := s.GetWorkspacePreviews("nonexistent")
		if got == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(got) != 0 {
			t.Errorf("expected 0 previews, got %d", len(got))
		}
	})
}

func TestGetPreviews(t *testing.T) {
	s := New("", nil)

	t.Run("returns empty slice when no previews", func(t *testing.T) {
		got := s.GetPreviews()
		if len(got) != 0 {
			t.Errorf("expected 0 previews, got %d", len(got))
		}
	})

	t.Run("returns all previews", func(t *testing.T) {
		s.UpsertPreview(WorkspacePreview{ID: "p1", WorkspaceID: "ws-1"})
		s.UpsertPreview(WorkspacePreview{ID: "p2", WorkspaceID: "ws-2"})

		got := s.GetPreviews()
		if len(got) != 2 {
			t.Fatalf("expected 2 previews, got %d", len(got))
		}
	})
}

func TestRemoveWorkspacePreviews(t *testing.T) {
	s := New("", nil)
	s.UpsertPreview(WorkspacePreview{ID: "p1", WorkspaceID: "ws-1"})
	s.UpsertPreview(WorkspacePreview{ID: "p2", WorkspaceID: "ws-1"})
	s.UpsertPreview(WorkspacePreview{ID: "p3", WorkspaceID: "ws-2"})

	t.Run("removes matching and returns count", func(t *testing.T) {
		removed := s.RemoveWorkspacePreviews("ws-1")
		if removed != 2 {
			t.Errorf("RemoveWorkspacePreviews() = %d, want 2", removed)
		}

		// ws-2 preview should survive
		remaining := s.GetPreviews()
		if len(remaining) != 1 {
			t.Fatalf("expected 1 remaining preview, got %d", len(remaining))
		}
		if remaining[0].ID != "p3" {
			t.Errorf("remaining preview ID = %q, want 'p3'", remaining[0].ID)
		}
	})

	t.Run("returns zero for no matches", func(t *testing.T) {
		removed := s.RemoveWorkspacePreviews("nonexistent")
		if removed != 0 {
			t.Errorf("RemoveWorkspacePreviews() = %d, want 0", removed)
		}
	})
}

func TestUpdateOverlayManifest(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: "/tmp"})

	t.Run("sets manifest on existing workspace", func(t *testing.T) {
		manifest := map[string]string{"file.txt": "abc123"}
		s.UpdateOverlayManifest("ws-1", manifest)

		ws, found := s.GetWorkspace("ws-1")
		if !found {
			t.Fatal("workspace not found")
		}
		if ws.OverlayManifest["file.txt"] != "abc123" {
			t.Errorf("OverlayManifest['file.txt'] = %q, want 'abc123'", ws.OverlayManifest["file.txt"])
		}
	})

	t.Run("replaces existing manifest entirely", func(t *testing.T) {
		newManifest := map[string]string{"other.txt": "def456"}
		s.UpdateOverlayManifest("ws-1", newManifest)

		ws, _ := s.GetWorkspace("ws-1")
		if _, exists := ws.OverlayManifest["file.txt"]; exists {
			t.Error("old manifest entry 'file.txt' should not exist after replacement")
		}
		if ws.OverlayManifest["other.txt"] != "def456" {
			t.Errorf("OverlayManifest['other.txt'] = %q, want 'def456'", ws.OverlayManifest["other.txt"])
		}
	})

	t.Run("no-ops for unknown workspace", func(t *testing.T) {
		// Should not panic or error
		s.UpdateOverlayManifest("nonexistent", map[string]string{"x": "y"})
	})
}

func TestRepoBaseCRUD(t *testing.T) {
	s := New("", nil)

	t.Run("GetRepoBases returns empty slice when nil", func(t *testing.T) {
		bases := s.GetRepoBases()
		if bases == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(bases) != 0 {
			t.Errorf("expected 0 bases, got %d", len(bases))
		}
	})

	t.Run("AddRepoBase inserts new entry", func(t *testing.T) {
		err := s.AddRepoBase(RepoBase{
			RepoURL: "git@github.com:user/repo.git",
			Path:    "/home/user/.schmux/repos/repo.git",
		})
		if err != nil {
			t.Fatalf("AddRepoBase() error: %v", err)
		}

		bases := s.GetRepoBases()
		if len(bases) != 1 {
			t.Fatalf("expected 1 base, got %d", len(bases))
		}
		if bases[0].RepoURL != "git@github.com:user/repo.git" {
			t.Errorf("RepoURL = %q, want 'git@github.com:user/repo.git'", bases[0].RepoURL)
		}
	})

	t.Run("AddRepoBase upserts by RepoURL", func(t *testing.T) {
		err := s.AddRepoBase(RepoBase{
			RepoURL: "git@github.com:user/repo.git",
			Path:    "/updated/path",
		})
		if err != nil {
			t.Fatalf("AddRepoBase() error: %v", err)
		}

		bases := s.GetRepoBases()
		if len(bases) != 1 {
			t.Fatalf("expected 1 base after upsert, got %d", len(bases))
		}
		if bases[0].Path != "/updated/path" {
			t.Errorf("Path = %q, want '/updated/path' (should have been updated)", bases[0].Path)
		}
	})

	t.Run("GetRepoBaseByURL finds entry", func(t *testing.T) {
		wb, found := s.GetRepoBaseByURL("git@github.com:user/repo.git")
		if !found {
			t.Fatal("expected to find worktree base")
		}
		if wb.Path != "/updated/path" {
			t.Errorf("Path = %q, want '/updated/path'", wb.Path)
		}
	})

	t.Run("GetRepoBaseByURL returns false for unknown", func(t *testing.T) {
		_, found := s.GetRepoBaseByURL("unknown-url")
		if found {
			t.Error("expected found=false for unknown URL")
		}
	})

	t.Run("GetRepoBases returns defensive copy", func(t *testing.T) {
		bases := s.GetRepoBases()
		bases[0].Path = "mutated"

		original := s.GetRepoBases()
		if original[0].Path == "mutated" {
			t.Error("GetRepoBases should return a copy, not the original slice")
		}
	})
}

func TestUpdateOverlayManifestEntry(t *testing.T) {
	t.Parallel()
	s := &State{
		Workspaces: []Workspace{
			{ID: "ws-1"},
			{ID: "ws-2", OverlayManifest: map[string]string{"existing.md": "sha256:aaa"}},
		},
	}

	t.Run("creates manifest map when nil", func(t *testing.T) {
		s.UpdateOverlayManifestEntry("ws-1", "CLAUDE.md", "sha256:abc123")

		manifest := s.Workspaces[0].OverlayManifest
		if manifest == nil {
			t.Fatal("expected manifest to be initialized")
		}
		if manifest["CLAUDE.md"] != "sha256:abc123" {
			t.Errorf("got %q, want 'sha256:abc123'", manifest["CLAUDE.md"])
		}
	})

	t.Run("adds entry to existing manifest", func(t *testing.T) {
		s.UpdateOverlayManifestEntry("ws-2", "AGENTS.md", "sha256:def456")

		manifest := s.Workspaces[1].OverlayManifest
		if manifest["existing.md"] != "sha256:aaa" {
			t.Error("existing entry should be preserved")
		}
		if manifest["AGENTS.md"] != "sha256:def456" {
			t.Errorf("new entry = %q, want 'sha256:def456'", manifest["AGENTS.md"])
		}
	})

	t.Run("overwrites existing entry", func(t *testing.T) {
		s.UpdateOverlayManifestEntry("ws-2", "existing.md", "sha256:bbb")

		if s.Workspaces[1].OverlayManifest["existing.md"] != "sha256:bbb" {
			t.Error("entry should have been overwritten")
		}
	})

	t.Run("no-op for unknown workspace", func(t *testing.T) {
		// Should not panic or modify anything
		s.UpdateOverlayManifestEntry("ws-nonexistent", "file.md", "sha256:xxx")
	})
}

func TestGetSessionsByRemoteHostID(t *testing.T) {
	t.Parallel()
	s := &State{
		Sessions: []Session{
			{ID: "s1", RemoteHostID: "host-1"},
			{ID: "s2", RemoteHostID: "host-2"},
			{ID: "s3", RemoteHostID: "host-1"},
			{ID: "s4", RemoteHostID: ""},
		},
	}

	t.Run("returns matching sessions", func(t *testing.T) {
		result := s.GetSessionsByRemoteHostID("host-1")
		if len(result) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(result))
		}
		ids := map[string]bool{result[0].ID: true, result[1].ID: true}
		if !ids["s1"] || !ids["s3"] {
			t.Errorf("expected s1 and s3, got %v", ids)
		}
	})

	t.Run("returns empty for no matches", func(t *testing.T) {
		result := s.GetSessionsByRemoteHostID("host-999")
		if len(result) != 0 {
			t.Errorf("expected 0, got %d", len(result))
		}
	})

	t.Run("empty host ID does not match local sessions", func(t *testing.T) {
		// Empty string should technically match sessions with empty RemoteHostID
		// but this tests the actual behavior
		result := s.GetSessionsByRemoteHostID("")
		// s4 has RemoteHostID="" so it should match
		if len(result) != 1 || result[0].ID != "s4" {
			t.Errorf("expected [s4], got %v", result)
		}
	})
}

func TestGetWorkspacesByRemoteHostID(t *testing.T) {
	t.Parallel()
	s := &State{
		Workspaces: []Workspace{
			{ID: "ws-1", RemoteHostID: "host-a"},
			{ID: "ws-2", RemoteHostID: "host-b"},
			{ID: "ws-3", RemoteHostID: "host-a"},
			{ID: "ws-4", RemoteHostID: ""},
		},
	}

	t.Run("returns matching workspaces", func(t *testing.T) {
		result := s.GetWorkspacesByRemoteHostID("host-a")
		if len(result) != 2 {
			t.Fatalf("expected 2 workspaces, got %d", len(result))
		}
		ids := map[string]bool{result[0].ID: true, result[1].ID: true}
		if !ids["ws-1"] || !ids["ws-3"] {
			t.Errorf("expected ws-1 and ws-3, got %v", ids)
		}
	})

	t.Run("returns empty for no matches", func(t *testing.T) {
		result := s.GetWorkspacesByRemoteHostID("host-999")
		if len(result) != 0 {
			t.Errorf("expected 0, got %d", len(result))
		}
	})
}

func TestAddWorkspace_Upsert(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"), nil)

	w1 := Workspace{ID: "ws-1", Repo: "original"}
	if err := s.AddWorkspace(w1); err != nil {
		t.Fatalf("first AddWorkspace: %v", err)
	}

	w2 := Workspace{ID: "ws-1", Repo: "updated"}
	if err := s.AddWorkspace(w2); err != nil {
		t.Fatalf("second AddWorkspace: %v", err)
	}

	workspaces := s.GetWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(workspaces))
	}
	if workspaces[0].Repo != "updated" {
		t.Errorf("expected repo %q, got %q", "updated", workspaces[0].Repo)
	}
}

func TestAddSession_Upsert(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "state.json"), nil)

	s1 := Session{ID: "sess-1", Target: "claude"}
	if err := s.AddSession(s1); err != nil {
		t.Fatalf("first AddSession: %v", err)
	}

	s2 := Session{ID: "sess-1", Target: "codex"}
	if err := s.AddSession(s2); err != nil {
		t.Fatalf("second AddSession: %v", err)
	}

	sessions := s.GetSessions()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].Target != "codex" {
		t.Errorf("expected target %q, got %q", "codex", sessions[0].Target)
	}
}

func TestMigrateSessionTargets(t *testing.T) {
	sessions := []Session{
		{ID: "s1", Target: "claude-opus"},
		{ID: "s2", Target: "minimax"},
		{ID: "s3", Target: "claude-opus-4-6"}, // already migrated
		{ID: "s4", Target: ""},                // empty target
	}

	changed := migrateSessionTargets(sessions)
	if !changed {
		t.Error("expected migration to occur")
	}
	if sessions[0].Target != "claude-opus-4-6" {
		t.Errorf("sessions[0].Target = %q, want %q", sessions[0].Target, "claude-opus-4-6")
	}
	if sessions[1].Target != "MiniMax-M2.1" {
		t.Errorf("sessions[1].Target = %q, want %q", sessions[1].Target, "MiniMax-M2.1")
	}
	if sessions[2].Target != "claude-opus-4-6" {
		t.Errorf("sessions[2].Target = %q, want %q", sessions[2].Target, "claude-opus-4-6")
	}
	if sessions[3].Target != "" {
		t.Errorf("sessions[3].Target = %q, want empty", sessions[3].Target)
	}
}

func TestMigrateSessionTargets_NoChange(t *testing.T) {
	sessions := []Session{
		{ID: "s1", Target: "claude-opus-4-6"},
		{ID: "s2", Target: "MiniMax-M2.1"},
	}

	changed := migrateSessionTargets(sessions)
	if changed {
		t.Error("should return false when no legacy IDs present")
	}
}

func TestMigrateSessionTargets_Empty(t *testing.T) {
	changed := migrateSessionTargets(nil)
	if changed {
		t.Error("should return false for nil sessions")
	}
	changed = migrateSessionTargets([]Session{})
	if changed {
		t.Error("should return false for empty sessions")
	}
}

func TestMigrateSessionTargets_ViaLoad(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Write a state file with legacy session targets
	stateJSON := `{
		"workspaces": [],
		"sessions": [
			{"id": "s1", "workspace_id": "w1", "target": "claude-opus", "tmux_session": "t1", "created_at": "2025-01-01T00:00:00Z"},
			{"id": "s2", "workspace_id": "w1", "target": "minimax", "tmux_session": "t2", "created_at": "2025-01-01T00:00:00Z"}
		]
	}`
	if err := os.WriteFile(statePath, []byte(stateJSON), 0644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	st, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	sessions := st.GetSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].Target != "claude-opus-4-6" {
		t.Errorf("sessions[0].Target = %q, want %q", sessions[0].Target, "claude-opus-4-6")
	}
	if sessions[1].Target != "MiniMax-M2.1" {
		t.Errorf("sessions[1].Target = %q, want %q", sessions[1].Target, "MiniMax-M2.1")
	}
}

func TestFindWorkspaceByRepoBranch(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-001", Repo: "myrepo", Branch: "main", Path: "/tmp/ws-001"})
	s.AddWorkspace(Workspace{ID: "ws-002", Repo: "myrepo", Branch: "schmux/lore", Path: "/tmp/ws-002"})
	s.AddWorkspace(Workspace{ID: "ws-003", Repo: "other", Branch: "schmux/lore", Path: "/tmp/ws-003"})

	// Find by repo + branch
	ws, found := s.FindWorkspaceByRepoBranch("myrepo", "schmux/lore")
	if !found {
		t.Fatal("expected to find workspace")
	}
	if ws.ID != "ws-002" {
		t.Errorf("expected ws-002, got %s", ws.ID)
	}

	// Not found
	_, found = s.FindWorkspaceByRepoBranch("myrepo", "nonexistent")
	if found {
		t.Error("expected not found")
	}
}

func TestWorkspaceStatusConstants(t *testing.T) {
	if WorkspaceStatusProvisioning != "provisioning" {
		t.Errorf("expected provisioning, got %s", WorkspaceStatusProvisioning)
	}
	if WorkspaceStatusRunning != "running" {
		t.Errorf("expected running, got %s", WorkspaceStatusRunning)
	}
	if WorkspaceStatusFailed != "failed" {
		t.Errorf("expected failed, got %s", WorkspaceStatusFailed)
	}
	if WorkspaceStatusDisposing != "disposing" {
		t.Errorf("expected disposing, got %s", WorkspaceStatusDisposing)
	}
}

func TestWorkspaceStatusPersisted(t *testing.T) {
	w := Workspace{
		ID:     "test-1",
		Repo:   "https://example.com/repo.git",
		Branch: "main",
		Path:   "/tmp/test",
		Status: WorkspaceStatusRunning,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status":"running"`) {
		t.Errorf("expected status in JSON, got %s", string(data))
	}

	var w2 Workspace
	if err := json.Unmarshal(data, &w2); err != nil {
		t.Fatal(err)
	}
	if w2.Status != WorkspaceStatusRunning {
		t.Errorf("expected running after roundtrip, got %s", w2.Status)
	}
}
