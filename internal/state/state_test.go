package state

import (
	"encoding/json"
	"fmt"
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

func TestSavePreservesUnknownTopLevelFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"future_state":{"keep":true},"workspaces":[],"sessions":[]}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Future struct {
			Keep bool `json:"keep"`
		} `json:"future_state"`
	}
	if err := json.Unmarshal(data, &saved); err != nil || !saved.Future.Keep {
		t.Fatalf("unknown state field lost: %s (err %v)", data, err)
	}
}

func TestLoadMigratesLegacyNestedTabs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Old format: tabs nested on workspace
	data := []byte(`{
		"workspaces": [{
			"id": "ws-1",
			"repo": "repo",
			"branch": "main",
			"path": "/tmp/ws-1",
			"tabs": [
				{"id": "sys-diff-ws-1", "kind": "diff", "route": "/diff/ws-1", "closable": false, "created_at": "2026-01-01T00:00:00Z"},
				{"id": "sys-git-ws-1", "kind": "git", "label": "commit graph", "route": "/commits/ws-1", "closable": false, "created_at": "2026-01-01T00:00:00Z"}
			]
		}],
		"sessions": []
	}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Tabs should be at top level
	tabs := st.GetWorkspaceTabs("ws-1")
	if len(tabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d", len(tabs))
	}

	// WorkspaceID should be stamped
	st.mu.RLock()
	for _, tab := range st.Tabs {
		if tab.WorkspaceID != "ws-1" {
			t.Errorf("tab %s WorkspaceID = %q, want ws-1", tab.ID, tab.WorkspaceID)
		}
	}
	st.mu.RUnlock()
}

func TestLoadNewFormatTabs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// New format: tabs at top level
	data := []byte(`{
		"workspaces": [{
			"id": "ws-1",
			"repo": "repo",
			"branch": "main",
			"path": "/tmp/ws-1"
		}],
		"sessions": [],
		"tabs": [
			{"id": "sys-diff-ws-1", "workspace_id": "ws-1", "kind": "diff", "route": "/diff/ws-1", "closable": false, "created_at": "2026-01-01T00:00:00Z"}
		]
	}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws-1")
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab, got %d", len(tabs))
	}
	if tabs[0].ID != "sys-diff-ws-1" {
		t.Errorf("tab ID = %q, want sys-diff-ws-1", tabs[0].ID)
	}
}

func TestLoadMixedFormatDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	// Mixed format: same tab in both locations (crash scenario)
	data := []byte(`{
		"workspaces": [{
			"id": "ws-1",
			"repo": "repo",
			"branch": "main",
			"path": "/tmp/ws-1",
			"tabs": [
				{"id": "sys-diff-ws-1", "kind": "diff", "label": "stale", "route": "/diff/ws-1", "closable": false, "created_at": "2026-01-01T00:00:00Z"}
			]
		}],
		"sessions": [],
		"tabs": [
			{"id": "sys-diff-ws-1", "workspace_id": "ws-1", "kind": "diff", "label": "fresh", "route": "/diff/ws-1", "closable": false, "created_at": "2026-01-01T00:00:00Z"}
		]
	}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	tabs := st.GetWorkspaceTabs("ws-1")
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab after dedup, got %d", len(tabs))
	}
	// Top-level (new format) wins
	if tabs[0].Label != "fresh" {
		t.Errorf("Label = %q, want 'fresh' (top-level should win)", tabs[0].Label)
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

func TestLoadDropsRemoteRecords(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "project")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(shared, "wip.txt")
	if err := os.WriteFile(keep, []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "state.json")
	raw := fmt.Sprintf(`{
		"workspaces": [
			{"id":"local-1","repo":"r","branch":"main","path":%[1]q},
			{"id":"remote-1","repo":"Cloud","branch":"host","path":%[1]q,"remote_host_id":"host-1","remote_path":%[1]q,
			 "tabs":[{"id":"nested","kind":"diff","route":"/diff/remote-1"}]}
		],
		"sessions": [
			{"id":"s-local","workspace_id":"local-1","target":"claude","tmux_session":"a"},
			{"id":"s-remote","workspace_id":"remote-1","target":"claude","tmux_session":"b","remote_host_id":"host-1"},
			{"id":"s-orphan","workspace_id":"remote-1","target":"claude","tmux_session":"c"}
		],
		"tabs": [
			{"id":"t-local","workspace_id":"local-1","kind":"git","route":"/commits/local-1"},
			{"id":"t-remote","workspace_id":"remote-1","kind":"git","route":"/commits/remote-1"}
		],
		"remote_hosts": [{"id":"host-1","profile_id":"p","status":"connected"}],
		"future_state": {"keep":true}
	}`, shared)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path, nil)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if got := st.GetWorkspaces(); len(got) != 1 || got[0].ID != "local-1" || got[0].Path != shared {
		t.Errorf("workspaces = %+v, want only local-1 at %s", got, shared)
	}
	if got := st.GetSessions(); len(got) != 1 || got[0].ID != "s-local" {
		t.Errorf("sessions = %+v, want only s-local", got)
	}
	if got := st.GetWorkspaceTabs("remote-1"); len(got) != 0 {
		t.Errorf("remote-1 tabs = %+v, want none", got)
	}
	if got := st.GetWorkspaceTabs("local-1"); len(got) != 1 {
		t.Errorf("local-1 tabs = %+v, want 1", got)
	}
	if b, err := os.ReadFile(keep); err != nil || string(b) != "uncommitted" {
		t.Errorf("file at dropped record's path = %q, %v", b, err)
	}

	// Load persisted the result, and remote_hosts does not ride along as an unknown field.
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(saved, &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["remote_hosts"]; ok {
		t.Errorf("remote_hosts survived save: %s", saved)
	}
	if _, ok := top["future_state"]; !ok {
		t.Errorf("unrelated unknown field lost: %s", saved)
	}
	if strings.Contains(string(saved), "remote-1") {
		t.Errorf("remote-1 still on disk: %s", saved)
	}
}

func TestLoadRejectsMalformedRemoteHostID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"workspaces":[{"id":"w","path":"/x","remote_host_id":7}],"sessions":[]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, nil); err == nil {
		t.Fatal("Load() = nil, want an error: a skipped remote record would become a local one")
	}
}

func TestLoadLeavesLocalOnlyStateUnsaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	raw := `{"workspaces":[{"id":"w","path":"/x"}],"sessions":[]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != raw {
		t.Errorf("Load rewrote a state file with nothing to drop: %s", got)
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

	// Make 10 rapid changes with SaveBatched()
	for i := 0; i < 10; i++ {
		s.AddWorkspace(Workspace{
			ID:   "ws-" + string(rune('a'+i)),
			Repo: "repo",
			Path: "/path",
		})
		s.SaveBatched()
	}

	// Flush synchronously instead of sleeping for the debounce window
	s.FlushPending()

	loaded, err := Load(statePath, nil)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}

	if len(loaded.Workspaces) != 10 {
		t.Errorf("expected 10 workspaces, got %d", len(loaded.Workspaces))
	}
}

// TestSaveBatchedDebounce verifies that the debounce timer resets on rapid calls
func TestSaveBatchedDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	s := New(statePath, nil)

	// Add workspace and trigger batched save
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo1", Path: "/path1"})
	s.SaveBatched()

	// Add another workspace — second SaveBatched should reset the timer
	s.AddWorkspace(Workspace{ID: "ws-2", Repo: "repo2", Path: "/path2"})
	s.SaveBatched()

	// Flush synchronously — both workspaces should be persisted
	s.FlushPending()

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

	// FlushPending forces the batched save synchronously
	s2.FlushPending()

	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("SaveBatched() should persist after FlushPending: %v", err)
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

func TestUpdateSessionResumeID(t *testing.T) {
	st := New("", nil)
	st.Sessions = []Session{{ID: "s1"}}

	if changed := st.UpdateSessionResumeID("s1", "conv-abc"); !changed {
		t.Fatal("first set should report changed")
	}
	sess, _ := st.GetSession("s1")
	if sess.ResumeID != "conv-abc" {
		t.Fatalf("ResumeID = %q, want conv-abc", sess.ResumeID)
	}
	if st.UpdateSessionResumeID("s1", "conv-abc") {
		t.Fatal("setting the same value should report no change")
	}
	if st.UpdateSessionResumeID("missing", "x") {
		t.Fatal("unknown session should report no change")
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

func TestRemoveWorkspaceRemovesTabs(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: "/tmp/ws-1"})
	s.AddWorkspace(Workspace{ID: "ws-2", Repo: "repo", Branch: "dev", Path: "/tmp/ws-2"})

	_ = s.AddTab("ws-1", Tab{ID: "tab-a", Kind: "diff", Route: "/diff/ws-1", CreatedAt: time.Now()})
	_ = s.AddTab("ws-1", Tab{ID: "tab-b", Kind: "git", Route: "/commits/ws-1", CreatedAt: time.Now()})
	_ = s.AddTab("ws-2", Tab{ID: "tab-c", Kind: "diff", Route: "/diff/ws-2", CreatedAt: time.Now()})

	if err := s.RemoveWorkspace("ws-1"); err != nil {
		t.Fatalf("RemoveWorkspace() failed: %v", err)
	}

	if tabs := s.GetWorkspaceTabs("ws-1"); len(tabs) != 0 {
		t.Errorf("expected 0 tabs for removed workspace, got %d", len(tabs))
	}
	if tabs := s.GetWorkspaceTabs("ws-2"); len(tabs) != 1 {
		t.Errorf("expected 1 tab for remaining workspace, got %d", len(tabs))
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

	t.Run("RemoveRepoBase removes only matching URL", func(t *testing.T) {
		otherURL := "git@github.com:user/other.git"
		if err := s.AddRepoBase(RepoBase{RepoURL: otherURL, Path: "/other/path"}); err != nil {
			t.Fatalf("AddRepoBase() error: %v", err)
		}
		if err := s.RemoveRepoBase("git@github.com:user/repo.git"); err != nil {
			t.Fatalf("RemoveRepoBase() error: %v", err)
		}
		if _, found := s.GetRepoBaseByURL("git@github.com:user/repo.git"); found {
			t.Error("removed repo base is still present")
		}
		if _, found := s.GetRepoBaseByURL(otherURL); !found {
			t.Error("RemoveRepoBase removed a different repo base")
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

func TestTabCRUD(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()})

	tab := Tab{
		ID:        "tab-1",
		Kind:      "markdown",
		Label:     "README",
		Route:     "/notes/ws-1/readme",
		Closable:  true,
		Meta:      map[string]string{"filepath": "/README.md"},
		CreatedAt: time.Now(),
	}

	t.Run("AddTab inserts a new tab", func(t *testing.T) {
		if err := s.AddTab("ws-1", tab); err != nil {
			t.Fatalf("AddTab() failed: %v", err)
		}
		tabs := s.GetWorkspaceTabs("ws-1")
		if len(tabs) != 1 {
			t.Fatalf("expected 1 tab, got %d", len(tabs))
		}
		if tabs[0].ID != "tab-1" {
			t.Errorf("tab ID = %q, want 'tab-1'", tabs[0].ID)
		}
	})

	t.Run("RemoveTab deletes tab by ID", func(t *testing.T) {
		if err := s.RemoveTab("ws-1", "tab-1"); err != nil {
			t.Fatalf("RemoveTab() failed: %v", err)
		}
		tabs := s.GetWorkspaceTabs("ws-1")
		if len(tabs) != 0 {
			t.Errorf("expected 0 tabs after remove, got %d", len(tabs))
		}
	})

	t.Run("RemoveTab is idempotent for missing tab", func(t *testing.T) {
		// Removing a tab that does not exist should not error
		if err := s.RemoveTab("ws-1", "tab-1"); err != nil {
			t.Errorf("RemoveTab for missing tab should not error: %v", err)
		}
	})

	t.Run("AddTab returns error for unknown workspace", func(t *testing.T) {
		err := s.AddTab("nonexistent-ws", tab)
		if err == nil {
			t.Error("expected error for unknown workspace")
		}
	})

	t.Run("RemoveTab returns error for unknown workspace", func(t *testing.T) {
		err := s.RemoveTab("nonexistent-ws", "tab-1")
		if err == nil {
			t.Error("expected error for unknown workspace")
		}
	})

	t.Run("GetWorkspaceTabs returns empty slice for unknown workspace", func(t *testing.T) {
		tabs := s.GetWorkspaceTabs("nonexistent-ws")
		if tabs == nil {
			t.Error("expected non-nil empty slice, got nil")
		}
		if len(tabs) != 0 {
			t.Errorf("expected 0 tabs, got %d", len(tabs))
		}
	})
}

func TestTabIdempotency(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-1", Repo: "repo", Branch: "main", Path: t.TempDir()})

	tab := Tab{
		ID:        "tab-a",
		Kind:      "diff",
		Label:     "Diff",
		Route:     "/commits/ws-1",
		Closable:  false,
		CreatedAt: time.Now(),
	}

	// Add once
	if err := s.AddTab("ws-1", tab); err != nil {
		t.Fatalf("first AddTab failed: %v", err)
	}

	// Add again with same kind (same dedup key) but different label
	tab2 := tab
	tab2.ID = "tab-b"
	tab2.Label = "Updated Diff"
	if err := s.AddTab("ws-1", tab2); err != nil {
		t.Fatalf("second AddTab failed: %v", err)
	}

	tabs := s.GetWorkspaceTabs("ws-1")
	if len(tabs) != 1 {
		t.Fatalf("expected 1 tab after idempotent add, got %d", len(tabs))
	}
	if tabs[0].Label != "Updated Diff" {
		t.Errorf("Label = %q, want 'Updated Diff' (should be upserted)", tabs[0].Label)
	}

	// Verify preview tabs deduplicate by preview_id
	previewTab1 := Tab{
		ID:       "prev-tab-1",
		Kind:     "preview",
		Label:    "Preview 1",
		Route:    "/preview/ws-1/prev-1",
		Closable: true,
		Meta:     map[string]string{"preview_id": "prev-1"},
	}
	previewTab2 := Tab{
		ID:       "prev-tab-2",
		Kind:     "preview",
		Label:    "Preview 1 Updated",
		Route:    "/preview/ws-1/prev-1",
		Closable: true,
		Meta:     map[string]string{"preview_id": "prev-1"},
	}
	s.AddTab("ws-1", previewTab1)
	s.AddTab("ws-1", previewTab2)

	tabs = s.GetWorkspaceTabs("ws-1")
	previewCount := 0
	for _, tab := range tabs {
		if tab.Kind == "preview" {
			previewCount++
			if tab.Label != "Preview 1 Updated" {
				t.Errorf("preview tab Label = %q, want 'Preview 1 Updated'", tab.Label)
			}
		}
	}
	if previewCount != 1 {
		t.Errorf("expected 1 preview tab, got %d", previewCount)
	}
}

func TestTabWorkspaceIsolation(t *testing.T) {
	s := New("", nil)
	s.AddWorkspace(Workspace{ID: "ws-a", Repo: "repo", Branch: "main", Path: t.TempDir()})
	s.AddWorkspace(Workspace{ID: "ws-b", Repo: "repo", Branch: "dev", Path: t.TempDir()})

	tabA := Tab{
		ID: "tab-1", Kind: "markdown", Label: "README",
		Route: "/diff/ws-a/md/README.md", Closable: true,
		Meta: map[string]string{"filepath": "README.md"}, CreatedAt: time.Now(),
	}
	tabB := Tab{
		ID: "tab-2", Kind: "markdown", Label: "CHANGELOG",
		Route: "/diff/ws-b/md/CHANGELOG.md", Closable: true,
		Meta: map[string]string{"filepath": "CHANGELOG.md"}, CreatedAt: time.Now(),
	}

	if err := s.AddTab("ws-a", tabA); err != nil {
		t.Fatalf("AddTab ws-a: %v", err)
	}
	if err := s.AddTab("ws-b", tabB); err != nil {
		t.Fatalf("AddTab ws-b: %v", err)
	}

	t.Run("tabs are isolated by workspace", func(t *testing.T) {
		aTabs := s.GetWorkspaceTabs("ws-a")
		if len(aTabs) != 1 || aTabs[0].ID != "tab-1" {
			t.Errorf("ws-a tabs = %+v, want [tab-1]", aTabs)
		}
		bTabs := s.GetWorkspaceTabs("ws-b")
		if len(bTabs) != 1 || bTabs[0].ID != "tab-2" {
			t.Errorf("ws-b tabs = %+v, want [tab-2]", bTabs)
		}
	})

	t.Run("WorkspaceID is set on stored tabs", func(t *testing.T) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, tab := range s.Tabs {
			if tab.ID == "tab-1" && tab.WorkspaceID != "ws-a" {
				t.Errorf("tab-1 WorkspaceID = %q, want ws-a", tab.WorkspaceID)
			}
			if tab.ID == "tab-2" && tab.WorkspaceID != "ws-b" {
				t.Errorf("tab-2 WorkspaceID = %q, want ws-b", tab.WorkspaceID)
			}
		}
	})

	t.Run("RemoveTab only affects target workspace", func(t *testing.T) {
		if err := s.RemoveTab("ws-a", "tab-1"); err != nil {
			t.Fatalf("RemoveTab: %v", err)
		}
		if len(s.GetWorkspaceTabs("ws-a")) != 0 {
			t.Error("ws-a should have 0 tabs after remove")
		}
		if len(s.GetWorkspaceTabs("ws-b")) != 1 {
			t.Error("ws-b should still have 1 tab")
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

func TestSchmuxDataDir(t *testing.T) {
	t.Run("git repo uses .schmux", func(t *testing.T) {
		dir := t.TempDir()
		// No .sl directory — treated as git
		got := SchmuxDataDir(dir)
		want := filepath.Join(dir, ".schmux")
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("sapling repo uses .sl/schmux", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".sl"), 0755)
		got := SchmuxDataDir(dir)
		want := filepath.Join(dir, ".sl", "schmux")
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("sapling repo with .hg control dir (mercurial-compat mode) uses .sl/schmux", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, ".hg"), 0755)
		got := SchmuxDataDir(dir)
		want := filepath.Join(dir, ".sl", "schmux")
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})

	t.Run("sapling repo with .hg as a symlink (virtual-filesystem-backed) uses .sl/schmux", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "hg-target")
		os.MkdirAll(target, 0755)
		if err := os.Symlink(target, filepath.Join(dir, ".hg")); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		got := SchmuxDataDir(dir)
		want := filepath.Join(dir, ".sl", "schmux")
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
}

func TestSchmuxDataDirRelative(t *testing.T) {
	if got := SchmuxDataDirRelative("sapling"); got != filepath.Join(".sl", "schmux") {
		t.Errorf("sapling: got %s", got)
	}
	if got := SchmuxDataDirRelative("git"); got != ".schmux" {
		t.Errorf("git: got %s", got)
	}
}

func TestWorkspace_LabelRoundTrip(t *testing.T) {
	ws := Workspace{
		ID:     "myrepo-007",
		Repo:   "git@github.com:foo/myrepo",
		Branch: "",
		Path:   "/tmp/myrepo-007",
		VCS:    "sapling",
		Label:  "Login bug fix",
	}
	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"label":"Login bug fix"`) {
		t.Fatalf("label not serialized: %s", data)
	}
	var got Workspace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Label != "Login bug fix" {
		t.Fatalf("expected Label=%q, got %q", "Login bug fix", got.Label)
	}

	// Empty Label must omit from JSON
	wsEmpty := Workspace{ID: "x", Repo: "r", Path: "/p"}
	data2, _ := json.Marshal(wsEmpty)
	if strings.Contains(string(data2), `"label"`) {
		t.Fatalf("empty label should be omitted: %s", data2)
	}
}

func TestWorkspaceOwnerFlightIDRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := New(p, nil)
	if err := s.AddWorkspace(Workspace{ID: "repo-001", Repo: "r", Branch: "b", Path: "/x"}); err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetWorkspace("repo-001")
	w.OwnerFlightID = "fl-abc123"
	if err := s.UpdateWorkspace(w); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s2.GetWorkspace("repo-001")
	if got.OwnerFlightID != "fl-abc123" {
		t.Fatalf("OwnerFlightID after reload = %q", got.OwnerFlightID)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), `"owner_flight_id": "fl-abc123"`) {
		t.Errorf("state.json lacks owner_flight_id: %s", raw)
	}
}

func TestWorkspaceDedicatedRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := New(p, nil)
	if err := s.AddWorkspace(Workspace{ID: "repo-001", Repo: "r", Branch: "b", Path: "/x", Dedicated: "client-performance"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s2, err := Load(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s2.GetWorkspace("repo-001")
	if got.Dedicated != "client-performance" {
		t.Fatalf("Dedicated after reload = %q", got.Dedicated)
	}
	raw, _ := os.ReadFile(p)
	if !strings.Contains(string(raw), `"dedicated": "client-performance"`) {
		t.Errorf("state.json lacks dedicated: %s", raw)
	}

	// An unstamped workspace must omit the field entirely.
	wsEmpty := Workspace{ID: "x", Repo: "r", Path: "/p"}
	data, _ := json.Marshal(wsEmpty)
	if strings.Contains(string(data), `"dedicated"`) {
		t.Fatalf("empty dedicated should be omitted: %s", data)
	}
}

func TestFindDedicatedWorkspace(t *testing.T) {
	ws := []Workspace{
		{ID: "repo-001"},
		{ID: "repo-002", Dedicated: "client-performance"},
		{ID: "repo-003", Dedicated: "build-monitor/repo/ci"},
	}
	if got, ok := FindDedicatedWorkspace(ws, "client-performance"); !ok || got.ID != "repo-002" {
		t.Errorf("client-performance = %+v, %v", got, ok)
	}
	if got, ok := FindDedicatedWorkspace(ws, "build-monitor/repo/ci"); !ok || got.ID != "repo-003" {
		t.Errorf("build-monitor/repo/ci = %+v, %v", got, ok)
	}
	if _, ok := FindDedicatedWorkspace(ws, "nope"); ok {
		t.Error("unknown name found a workspace")
	}
	if _, ok := FindDedicatedWorkspace(ws, ""); ok {
		t.Error("empty name matched an unstamped workspace")
	}
	if _, ok := FindDedicatedWorkspace(nil, "client-performance"); ok {
		t.Error("nil list found a workspace")
	}
}

func TestFindOwnedWorkspace(t *testing.T) {
	ws := []Workspace{
		{ID: "repo-001"},
		{ID: "repo-002", OwnerFlightID: "fl-a"},
		{ID: "repo-003", OwnerFlightID: "fl-b"},
	}
	if got, ok := FindOwnedWorkspace(ws, "fl-b"); !ok || got.ID != "repo-003" {
		t.Errorf("fl-b = %+v, %v", got, ok)
	}
	if _, ok := FindOwnedWorkspace(ws, "fl-zzz"); ok {
		t.Error("unknown flight found a workspace")
	}
	if _, ok := FindOwnedWorkspace(ws, ""); ok {
		t.Error("empty flight id matched an unstamped workspace")
	}
	if _, ok := FindOwnedWorkspace(nil, "fl-a"); ok {
		t.Error("nil list found a workspace")
	}
}
