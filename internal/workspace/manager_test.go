package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestExtractWorkspaceNumber(t *testing.T) {
	tests := []struct {
		id      string
		want    int
		wantErr bool
	}{
		{"test-001", 1, false},
		{"test-002", 2, false},
		{"test-123", 123, false},
		{"myproject-999", 999, false},
		{"invalid", 0, true},
		{"test-abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got, err := extractWorkspaceNumber(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractWorkspaceNumber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractWorkspaceNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindNextWorkspaceNumber(t *testing.T) {
	tests := []struct {
		name       string
		workspaces []state.Workspace
		want       int
	}{
		{
			name:       "no workspaces",
			workspaces: []state.Workspace{},
			want:       1,
		},
		{
			name: "single workspace - returns next",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
			},
			want: 2,
		},
		{
			name: "sequential workspaces - returns next",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
				{ID: "test-002", Repo: "test", Branch: "main", Path: "/tmp/test-002"},
				{ID: "test-003", Repo: "test", Branch: "main", Path: "/tmp/test-003"},
			},
			want: 4,
		},
		{
			name: "gap at start - fills first gap",
			workspaces: []state.Workspace{
				{ID: "test-003", Repo: "test", Branch: "main", Path: "/tmp/test-003"},
			},
			want: 1,
		},
		{
			name: "gap in middle - fills first gap",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
				{ID: "test-003", Repo: "test", Branch: "main", Path: "/tmp/test-003"},
			},
			want: 2,
		},
		{
			name: "multiple gaps - fills first gap",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
				{ID: "test-003", Repo: "test", Branch: "main", Path: "/tmp/test-003"},
				{ID: "test-006", Repo: "test", Branch: "main", Path: "/tmp/test-006"},
			},
			want: 2,
		},
		{
			name: "large gap - fills first gap",
			workspaces: []state.Workspace{
				{ID: "test-100", Repo: "test", Branch: "main", Path: "/tmp/test-100"},
			},
			want: 1,
		},
		{
			name: "non-sequential with existing middle numbers",
			workspaces: []state.Workspace{
				{ID: "test-002", Repo: "test", Branch: "main", Path: "/tmp/test-002"},
				{ID: "test-004", Repo: "test", Branch: "main", Path: "/tmp/test-004"},
			},
			want: 1,
		},
		{
			name: "fills all gaps sequentially",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
				{ID: "test-002", Repo: "test", Branch: "main", Path: "/tmp/test-002"},
			},
			want: 3,
		},
		{
			name: "handles large numbers",
			workspaces: []state.Workspace{
				{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
				{ID: "test-002", Repo: "test", Branch: "main", Path: "/tmp/test-002"},
				{ID: "test-999", Repo: "test", Branch: "main", Path: "/tmp/test-999"},
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findNextWorkspaceNumber(tt.workspaces)
			if got != tt.want {
				t.Errorf("findNextWorkspaceNumber() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	cfg := &config.Config{
		WorkspacePath: "/tmp/workspaces",
	}
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath)

	m := New(cfg, st, statePath)
	if m == nil {
		t.Error("New() returned nil")
	}
	if m.config != cfg {
		t.Error("config not set correctly")
	}
	if m.state != st {
		t.Error("state not set correctly")
	}
}

func TestGetWorkspacesForRepo(t *testing.T) {
	statePath := t.TempDir() + "/state.json"
	st := state.New(statePath)

	// Add some workspaces
	st.Workspaces = []state.Workspace{
		{ID: "test-001", Repo: "test", Branch: "main", Path: "/tmp/test-001"},
		{ID: "test-002", Repo: "test", Branch: "develop", Path: "/tmp/test-002"},
		{ID: "other-001", Repo: "other", Branch: "main", Path: "/tmp/other-001"},
	}

	cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
	m := New(cfg, st, statePath)

	workspaces := m.getWorkspacesForRepo("test")
	if len(workspaces) != 2 {
		t.Errorf("expected 2 workspaces, got %d", len(workspaces))
	}

	workspaces = m.getWorkspacesForRepo("other")
	if len(workspaces) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(workspaces))
	}

	workspaces = m.getWorkspacesForRepo("nonexistent")
	if len(workspaces) != 0 {
		t.Errorf("expected 0 workspaces, got %d", len(workspaces))
	}
}

func TestDispose(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	// Create test workspace directory and state entry
	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("failed to create test workspace directory: %v", err)
	}

	w := state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}
	st.AddWorkspace(w)

	// Initialize git repository to satisfy git safety check
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("failed to initialize git repository: %v", err)
	}
	if err := exec.Command("git", "-C", workspacePath, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git user.email: %v", err)
	}
	if err := exec.Command("git", "-C", workspacePath, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git user.name: %v", err)
	}

	// Dispose the workspace
	err := m.Dispose(workspaceID)
	if err != nil {
		t.Errorf("Dispose() error = %v", err)
	}

	// Verify workspace removed from state
	_, found := st.GetWorkspace(workspaceID)
	if found {
		t.Error("workspace should be removed from state")
	}

	// Verify directory deleted
	if _, err := os.Stat(workspacePath); !os.IsNotExist(err) {
		t.Error("workspace directory should be deleted")
	}
}

func TestDispose_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	// Try to dispose non-existent workspace
	err := m.Dispose("nonexistent")
	if err == nil {
		t.Error("Dispose() should return error for non-existent workspace")
	}
	if err != nil && err.Error() != "workspace not found: nonexistent" {
		t.Errorf("Dispose() error = %v, want 'workspace not found: nonexistent'", err)
	}
}

func TestDispose_ActiveSessions(t *testing.T) {
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	// Create test workspace directory and state entry
	workspaceID := "test-001"
	workspacePath := filepath.Join(tmpDir, workspaceID)
	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		t.Fatalf("failed to create test workspace directory: %v", err)
	}

	w := state.Workspace{
		ID:     workspaceID,
		Repo:   "test",
		Branch: "main",
		Path:   workspacePath,
	}
	st.AddWorkspace(w)

	// Initialize git repository to satisfy git safety check
	if err := exec.Command("git", "init", "-q", workspacePath).Run(); err != nil {
		t.Fatalf("failed to initialize git repository: %v", err)
	}
	if err := exec.Command("git", "-C", workspacePath, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git user.email: %v", err)
	}
	if err := exec.Command("git", "-C", workspacePath, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git user.name: %v", err)
	}

	// Add an active session for this workspace
	sess := state.Session{
		ID:          "sess-001",
		WorkspaceID: workspaceID,
		Target:      "test-agent",
	}
	st.AddSession(sess)

	// Try to dispose workspace with active session
	err := m.Dispose(workspaceID)
	if err == nil {
		t.Error("Dispose() should return error when workspace has active sessions")
	}
	if err != nil && err.Error() != "workspace has active sessions: test-001" {
		t.Errorf("Dispose() error = %v, want 'workspace has active sessions: test-001'", err)
	}

	// Verify workspace still exists in state (not removed)
	_, found := st.GetWorkspace(workspaceID)
	if !found {
		t.Error("workspace should still exist in state after failed dispose")
	}

	// Verify directory still exists
	if _, err := os.Stat(workspacePath); os.IsNotExist(err) {
		t.Error("workspace directory should still exist after failed dispose")
	}
}

// TestDispose_Integration creates a real git workspace and disposes it.
func TestDispose_Integration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath)

	// Create test repo with a branch
	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature-1")

	cfg := &config.Config{
		WorkspacePath:    tmpDir,
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "test", URL: repoDir},
		},
	}
	m := New(cfg, st, statePath)

	// Create workspace via GetOrCreate (real git clone/checkout)
	ws, err := m.GetOrCreate(context.Background(), repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Verify workspace exists
	if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
		t.Fatal("workspace directory should exist after GetOrCreate")
	}

	// Verify state entry
	wsState, found := st.GetWorkspace(ws.ID)
	if !found {
		t.Fatal("workspace should be in state")
	}
	if wsState.Branch != "main" {
		t.Errorf("expected branch main, got %s", wsState.Branch)
	}

	// Dispose the workspace
	if err := m.Dispose(ws.ID); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}

	// Verify directory deleted
	if _, err := os.Stat(ws.Path); !os.IsNotExist(err) {
		t.Error("workspace directory should be deleted after Dispose")
	}

	// Verify state removed
	_, found = st.GetWorkspace(ws.ID)
	if found {
		t.Error("workspace should be removed from state after Dispose")
	}
}

func TestGetOrCreate_LocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = tmpDir
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	ctx := context.Background()

	// Create a local repo via GetOrCreate
	w, err := m.GetOrCreate(ctx, "local:testproject", "main")
	if err != nil {
		t.Fatalf("GetOrCreate() unexpected error: %v", err)
	}

	// Verify workspace ID
	if w.ID != "testproject-001" {
		t.Errorf("GetOrCreate() ID = %v, want %v", w.ID, "testproject-001")
	}

	// Verify directory exists
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		t.Errorf("GetOrCreate() directory does not exist: %s", w.Path)
	}

	// Verify it's a valid git repository
	gitDir := filepath.Join(w.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Error("GetOrCreate() .git directory does not exist")
	}
}

// mockStateStore wraps a state.Store and can simulate failures.
type mockStateStore struct {
	state    *state.State
	failSave bool
}

func (m *mockStateStore) GetWorkspaces() []state.Workspace {
	return m.state.GetWorkspaces()
}

func (m *mockStateStore) GetWorkspace(id string) (state.Workspace, bool) {
	return m.state.GetWorkspace(id)
}

func (m *mockStateStore) AddWorkspace(w state.Workspace) error {
	return m.state.AddWorkspace(w)
}

func (m *mockStateStore) UpdateWorkspace(w state.Workspace) error {
	return m.state.UpdateWorkspace(w)
}

func (m *mockStateStore) RemoveWorkspace(id string) error {
	return m.state.RemoveWorkspace(id)
}

func (m *mockStateStore) GetWorktreeBases() []state.WorktreeBase {
	return m.state.GetWorktreeBases()
}

func (m *mockStateStore) GetWorktreeBaseByURL(repoURL string) (state.WorktreeBase, bool) {
	return m.state.GetWorktreeBaseByURL(repoURL)
}

func (m *mockStateStore) AddWorktreeBase(wb state.WorktreeBase) error {
	return m.state.AddWorktreeBase(wb)
}

func (m *mockStateStore) GetSessions() []state.Session {
	return m.state.GetSessions()
}

func (m *mockStateStore) GetSession(id string) (state.Session, bool) {
	return m.state.GetSession(id)
}

func (m *mockStateStore) AddSession(s state.Session) error {
	return m.state.AddSession(s)
}

func (m *mockStateStore) UpdateSession(s state.Session) error {
	return m.state.UpdateSession(s)
}

func (m *mockStateStore) RemoveSession(id string) error {
	return m.state.RemoveSession(id)
}

func (m *mockStateStore) UpdateSessionLastOutput(sessionID string, t time.Time) {
	m.state.UpdateSessionLastOutput(sessionID, t)
}

func (m *mockStateStore) UpdateSessionLastSignal(sessionID string, t time.Time) {
	m.state.UpdateSessionLastSignal(sessionID, t)
}

func (m *mockStateStore) IncrementNudgeSeq(sessionID string) uint64 {
	return m.state.IncrementNudgeSeq(sessionID)
}

func (m *mockStateStore) GetNudgeSeq(sessionID string) uint64 {
	return m.state.GetNudgeSeq(sessionID)
}

func (m *mockStateStore) GetNeedsRestart() bool {
	return m.state.GetNeedsRestart()
}

func (m *mockStateStore) SetNeedsRestart(needsRestart bool) error {
	return m.state.SetNeedsRestart(needsRestart)
}

func (m *mockStateStore) GetPullRequests() []contracts.PullRequest  { return nil }
func (m *mockStateStore) SetPullRequests(_ []contracts.PullRequest) {}
func (m *mockStateStore) GetPublicRepos() []string                  { return nil }
func (m *mockStateStore) SetPublicRepos(_ []string)                 {}

// Remote host methods
func (m *mockStateStore) GetRemoteHosts() []state.RemoteHost { return nil }
func (m *mockStateStore) GetRemoteHost(id string) (state.RemoteHost, bool) {
	return state.RemoteHost{}, false
}
func (m *mockStateStore) GetRemoteHostByFlavorID(flavorID string) (state.RemoteHost, bool) {
	return state.RemoteHost{}, false
}
func (m *mockStateStore) GetRemoteHostByHostname(hostname string) (state.RemoteHost, bool) {
	return state.RemoteHost{}, false
}
func (m *mockStateStore) AddRemoteHost(rh state.RemoteHost) error        { return nil }
func (m *mockStateStore) UpdateRemoteHost(rh state.RemoteHost) error     { return nil }
func (m *mockStateStore) UpdateRemoteHostStatus(id, status string) error { return nil }
func (m *mockStateStore) RemoveRemoteHost(id string) error               { return nil }
func (m *mockStateStore) GetSessionsByRemoteHostID(hostID string) []state.Session {
	return m.state.GetSessionsByRemoteHostID(hostID)
}
func (m *mockStateStore) GetWorkspacesByRemoteHostID(hostID string) []state.Workspace {
	return m.state.GetWorkspacesByRemoteHostID(hostID)
}

func (m *mockStateStore) Save() error {
	if m.failSave {
		return fmt.Errorf("mock state save failure")
	}
	return m.state.Save()
}

// TestCreateCleanupOnStateSaveFailure verifies that workspace directory is cleaned up
// when clone succeeds but state.Save() fails.
func TestCreateCleanupOnStateSaveFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	workspaceBaseDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspaceBaseDir, 0755); err != nil {
		t.Fatalf("failed to create workspace base dir: %v", err)
	}

	// Create a minimal config
	cfg := &config.Config{
		WorkspacePath: workspaceBaseDir,
		Repos: []config.Repo{
			{Name: "test-repo", URL: "local:test-repo"},
		},
	}

	// Create a mock state store that will fail on Save
	st := state.New("")
	mockSt := &mockStateStore{state: st, failSave: true}

	mgr := New(cfg, mockSt, "")

	ctx := context.Background()

	// Attempt to create a workspace - should fail during state.Save
	_, err := mgr.create(ctx, "local:test-repo", "main")
	if err == nil {
		t.Fatal("expected error from create, got nil")
	}

	// Verify the workspace directory was cleaned up
	entries, err := os.ReadDir(workspaceBaseDir)
	if err != nil {
		t.Fatalf("failed to read workspace base dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected workspace directory to be cleaned up, found %d entries", len(entries))
		for _, e := range entries {
			t.Errorf("  - %s", e.Name())
		}
	}
}

// TestCreateNoCleanupOnSuccess verifies that workspace directory is NOT cleaned up
// when creation succeeds.
func TestCreateNoCleanupOnSuccess(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a real git repo to clone
	repoDir := gitTestWorkTree(t)

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	workspaceBaseDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspaceBaseDir, 0755); err != nil {
		t.Fatalf("failed to create workspace base dir: %v", err)
	}

	// Create a minimal config
	cfg := &config.Config{
		WorkspacePath:    workspaceBaseDir,
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "test-repo", URL: repoDir},
		},
	}

	// Create a mock state store that will succeed
	st := state.New(statePath)
	mockSt := &mockStateStore{state: st, failSave: false}

	mgr := New(cfg, mockSt, statePath)

	ctx := context.Background()

	// Create a workspace - should succeed
	w, err := mgr.create(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Verify the workspace directory still exists
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		t.Errorf("workspace directory was cleaned up on success, path: %s", w.Path)
	}
}

func TestLoadRepoConfig(t *testing.T) {
	t.Run("returns nil when directory does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		nonExistentPath := filepath.Join(tmpDir, "nonexistent")
		cfg, err := LoadRepoConfig(nonExistentPath)
		if err != nil {
			t.Errorf("LoadRepoConfig() returned error for non-existent path: %v", err)
		}
		if cfg != nil {
			t.Errorf("LoadRepoConfig() returned non-nil config for non-existent path")
		}
	})

	t.Run("returns nil when .schmux directory does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg, err := LoadRepoConfig(tmpDir)
		if err != nil {
			t.Errorf("LoadRepoConfig() returned error: %v", err)
		}
		if cfg != nil {
			t.Errorf("LoadRepoConfig() returned non-nil config when no .schmux dir")
		}
	})

	t.Run("returns nil when config.json does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		schmuxDir := filepath.Join(tmpDir, ".schmux")
		if err := os.Mkdir(schmuxDir, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		cfg, err := LoadRepoConfig(tmpDir)
		if err != nil {
			t.Errorf("LoadRepoConfig() returned error for missing config.json: %v", err)
		}
		if cfg != nil {
			t.Errorf("LoadRepoConfig() returned non-nil config for missing config.json")
		}
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		schmuxDir := filepath.Join(tmpDir, ".schmux")
		if err := os.Mkdir(schmuxDir, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath := filepath.Join(schmuxDir, "config.json")
		if err := os.WriteFile(configPath, []byte("{invalid json}"), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}
		cfg, err := LoadRepoConfig(tmpDir)
		if err == nil {
			t.Error("LoadRepoConfig() returned nil error for invalid JSON")
		}
		if cfg != nil {
			t.Errorf("LoadRepoConfig() returned non-nil config for invalid JSON")
		}
	})

	t.Run("parses valid config.json", func(t *testing.T) {
		tmpDir := t.TempDir()
		schmuxDir := filepath.Join(tmpDir, ".schmux")
		if err := os.Mkdir(schmuxDir, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath := filepath.Join(schmuxDir, "config.json")
		configContent := `{
  "quick_launch": [
    {
      "name": "test command",
      "target": "claude",
      "prompt": "test prompt"
    }
  ]
}`
		if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}
		cfg, err := LoadRepoConfig(tmpDir)
		if err != nil {
			t.Errorf("LoadRepoConfig() returned error: %v", err)
		}
		if cfg == nil {
			t.Fatal("LoadRepoConfig() returned nil config for valid JSON")
		}
		if len(cfg.QuickLaunch) != 1 {
			t.Errorf("LoadRepoConfig() returned %d quicklaunch items, want 1", len(cfg.QuickLaunch))
		}
		if cfg.QuickLaunch[0].Name != "test command" {
			t.Errorf("LoadRepoConfig() returned name %s, want 'test command'", cfg.QuickLaunch[0].Name)
		}
	})

	t.Run("returns empty quicklaunch for config with no quick_launch", func(t *testing.T) {
		tmpDir := t.TempDir()
		schmuxDir := filepath.Join(tmpDir, ".schmux")
		if err := os.Mkdir(schmuxDir, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath := filepath.Join(schmuxDir, "config.json")
		if err := os.WriteFile(configPath, []byte(`{}`), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}
		cfg, err := LoadRepoConfig(tmpDir)
		if err != nil {
			t.Errorf("LoadRepoConfig() returned error: %v", err)
		}
		if cfg == nil {
			t.Fatal("LoadRepoConfig() returned nil config")
		}
		if len(cfg.QuickLaunch) != 0 {
			t.Errorf("LoadRepoConfig() returned %d quicklaunch items, want 0", len(cfg.QuickLaunch))
		}
	})
}

func TestRefreshWorkspaceConfig(t *testing.T) {
	t.Run("caches config per workspace without merging", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		configPath := filepath.Join(tmpDir, "config.json")

		cfg := config.CreateDefault(configPath)
		cfg.WorkspacePath = tmpDir
		st := state.New(statePath)

		mgr := New(cfg, st, statePath)

		ws1 := state.Workspace{
			ID:     "repo-001",
			Repo:   "http://example.com/repo",
			Branch: "main",
			Path:   filepath.Join(tmpDir, "repo-001"),
		}
		ws2 := state.Workspace{
			ID:     "repo-002",
			Repo:   "http://example.com/repo",
			Branch: "feature",
			Path:   filepath.Join(tmpDir, "repo-002"),
		}

		schmuxDir1 := filepath.Join(ws1.Path, ".schmux")
		if err := os.MkdirAll(schmuxDir1, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath1 := filepath.Join(schmuxDir1, "config.json")
		configContent1 := `{"quick_launch": [{"name": "command1", "command": "echo one"}]}`
		if err := os.WriteFile(configPath1, []byte(configContent1), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}

		schmuxDir2 := filepath.Join(ws2.Path, ".schmux")
		if err := os.MkdirAll(schmuxDir2, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath2 := filepath.Join(schmuxDir2, "config.json")
		configContent2 := `{"quick_launch": [{"name": "command2", "command": "echo two"}]}`
		if err := os.WriteFile(configPath2, []byte(configContent2), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}

		mgr.RefreshWorkspaceConfig(ws1)
		mgr.RefreshWorkspaceConfig(ws2)

		cfg1 := mgr.GetWorkspaceConfig(ws1.ID)
		if cfg1 == nil || len(cfg1.QuickLaunch) != 1 || cfg1.QuickLaunch[0].Name != "command1" {
			t.Fatalf("expected workspace config for ws1 with command1")
		}
		cfg2 := mgr.GetWorkspaceConfig(ws2.ID)
		if cfg2 == nil || len(cfg2.QuickLaunch) != 1 || cfg2.QuickLaunch[0].Name != "command2" {
			t.Fatalf("expected workspace config for ws2 with command2")
		}
	})

	t.Run("clears cache when config is removed", func(t *testing.T) {
		tmpDir := t.TempDir()
		statePath := filepath.Join(tmpDir, "state.json")
		configPath := filepath.Join(tmpDir, "config.json")

		cfg := config.CreateDefault(configPath)
		cfg.WorkspacePath = tmpDir
		st := state.New(statePath)

		mgr := New(cfg, st, statePath)

		ws := state.Workspace{
			ID:     "repo-001",
			Repo:   "http://example.com/repo",
			Branch: "main",
			Path:   filepath.Join(tmpDir, "repo-001"),
		}

		schmuxDir := filepath.Join(ws.Path, ".schmux")
		if err := os.MkdirAll(schmuxDir, 0755); err != nil {
			t.Fatalf("failed to create .schmux dir: %v", err)
		}
		configPath1 := filepath.Join(schmuxDir, "config.json")
		configContent1 := `{"quick_launch": [{"name": "command1", "command": "echo one"}]}`
		if err := os.WriteFile(configPath1, []byte(configContent1), 0644); err != nil {
			t.Fatalf("failed to write config.json: %v", err)
		}

		mgr.RefreshWorkspaceConfig(ws)
		if mgr.GetWorkspaceConfig(ws.ID) == nil {
			t.Fatalf("expected workspace config to be cached")
		}

		if err := os.Remove(configPath1); err != nil {
			t.Fatalf("failed to remove config.json: %v", err)
		}

		mgr.RefreshWorkspaceConfig(ws)
		if mgr.GetWorkspaceConfig(ws.ID) != nil {
			t.Fatalf("expected workspace config to be cleared after removal")
		}
	})
}
