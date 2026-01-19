package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
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
		Target:       "test-agent",
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
		WorkspacePath: tmpDir,
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

func TestIsQuietSince(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		sessions    []state.Session
		cutoff      string // RFC3339 timestamp
		want        bool
	}{
		{
			name:        "no sessions - is quiet",
			workspaceID: "test-001",
			sessions:    []state.Session{},
			cutoff:      "2024-01-01T00:00:00Z",
			want:        true,
		},
		{
			name:        "session before cutoff - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2023-12-31T23:59:59Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
		{
			name:        "session after cutoff - not quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2024-01-01T00:00:01Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   false,
		},
		{
			name:        "session at cutoff - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2024-01-01T00:00:00Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
		{
			name:        "multiple sessions - one after cutoff - not quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2023-12-31T23:59:59Z")},
				{ID: "sess-002", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2024-01-01T00:00:01Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   false,
		},
		{
			name:        "multiple sessions - all before cutoff - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2023-12-31T23:59:00Z")},
				{ID: "sess-002", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2023-12-31T23:59:59Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
		{
			name:        "sessions for different workspace - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-002", LastOutputAt: parseTime(t, "2024-01-01T00:00:01Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
		{
			name:        "sessions for multiple workspaces - target workspace active - not quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2024-01-01T00:00:01Z")},
				{ID: "sess-002", WorkspaceID: "test-002", LastOutputAt: parseTime(t, "2023-12-31T23:59:59Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   false,
		},
		{
			name:        "sessions for multiple workspaces - other workspace active - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: parseTime(t, "2023-12-31T23:59:59Z")},
				{ID: "sess-002", WorkspaceID: "test-002", LastOutputAt: parseTime(t, "2024-01-01T00:00:01Z")},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
		{
			name:        "zero time - is quiet",
			workspaceID: "test-001",
			sessions: []state.Session{
				{ID: "sess-001", WorkspaceID: "test-001", LastOutputAt: time.Time{}},
			},
			cutoff: "2024-01-01T00:00:00Z",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statePath := t.TempDir() + "/state.json"
			st := state.New(statePath)

			// Add sessions to state
			for _, sess := range tt.sessions {
				st.AddSession(sess)
			}

			cfg := &config.Config{WorkspacePath: "/tmp/workspaces"}
			m := New(cfg, st, statePath)

			cutoff := parseTime(t, tt.cutoff)
			got := m.isQuietSince(tt.workspaceID, cutoff)

			if got != tt.want {
				t.Errorf("isQuietSince() = %v, want %v", got, tt.want)
			}
		})
	}
}

// parseTime parses an RFC3339 timestamp for use in tests.
func parseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("failed to parse time %q: %v", s, err)
	}
	return ts
}

func TestCreateLocalRepo(t *testing.T) {
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

	tests := []struct {
		name        string
		repoName    string
		branch      string
		wantID      string
		wantErr     bool
		errContains string
	}{
		{
			name:    "creates first workspace",
			repoName: "myproject",
			branch:  "main",
			wantID:  "myproject-001",
			wantErr: false,
		},
		{
			name:    "creates second workspace",
			repoName: "myproject",
			branch:  "main",
			wantID:  "myproject-002",
			wantErr: false,
		},
		{
			name:    "creates workspace with different name",
			repoName: "otherproject",
			branch:  "main",
			wantID:  "otherproject-001",
			wantErr: false,
		},
		{
			name:        "empty repo name errors",
			repoName:    "",
			branch:      "main",
			wantErr:     true,
			errContains: "repo name is required",
		},
		{
			name:        "path traversal rejected",
			repoName:    "../etc",
			branch:      "main",
			wantErr:     true,
			errContains: "invalid repo name",
		},
		{
			name:        "slash in name rejected",
			repoName:    "foo/bar",
			branch:      "main",
			wantErr:     true,
			errContains: "invalid repo name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, err := m.CreateLocalRepo(ctx, tt.repoName, tt.branch)

			if tt.wantErr {
				if err == nil {
					t.Errorf("CreateLocalRepo() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("CreateLocalRepo() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("CreateLocalRepo() unexpected error: %v", err)
			}

			// Verify workspace ID
			if w.ID != tt.wantID {
				t.Errorf("CreateLocalRepo() ID = %v, want %v", w.ID, tt.wantID)
			}

			// Verify workspace state
			ws, found := st.GetWorkspace(w.ID)
			if !found {
				t.Fatal("CreateLocalRepo() workspace not found in state")
			}

			if ws.Repo != "local:"+tt.repoName {
				t.Errorf("CreateLocalRepo() Repo = %v, want %v", ws.Repo, "local:"+tt.repoName)
			}

			if ws.Branch != tt.branch {
				t.Errorf("CreateLocalRepo() Branch = %v, want %v", ws.Branch, tt.branch)
			}

			// Verify directory exists
			if _, err := os.Stat(w.Path); os.IsNotExist(err) {
				t.Errorf("CreateLocalRepo() directory does not exist: %s", w.Path)
			}

			// Verify it's a valid git repository
			// Check for .git directory
			gitDir := filepath.Join(w.Path, ".git")
			if _, err := os.Stat(gitDir); os.IsNotExist(err) {
				t.Error("CreateLocalRepo() .git directory does not exist")
			}

			// Verify current branch
			cmd := exec.Command("git", "-C", w.Path, "rev-parse", "--abbrev-ref", "HEAD")
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("failed to get current branch: %v", err)
			}
			actualBranch := strings.TrimSpace(string(output))
			if actualBranch != tt.branch {
				t.Errorf("CreateLocalRepo() branch = %v, want %v", actualBranch, tt.branch)
			}

			// Verify there's an initial commit
			cmd = exec.Command("git", "-C", w.Path, "rev-parse", "HEAD")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("CreateLocalRepo() no initial commit: %v: %s", err, string(output))
			}
		})
	}
}

// TestGitPullRebase_MultipleBranchesConfig reproduces "Cannot rebase onto multiple branches"
// by manually crafting a broken .git/config with multiple merge refs, then verifies
// that schmux's gitPullRebase with explicit origin/<branch> works around it.
func TestGitPullRebase_MultipleBranchesConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a remote repo
	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	// Clone it
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	// Manually break .git/config by adding duplicate merge ref
	gitConfigPath := filepath.Join(cloneDir, ".git", "config")
	configContent, _ := os.ReadFile(gitConfigPath)

	brokenConfig := string(configContent)
	if !strings.Contains(brokenConfig, "[branch \"main\"]") {
		brokenConfig += "\n[branch \"main\"]\n\tremote = origin\n\tmerge = refs/heads/main\n"
	}
	brokenConfig += "\tmerge = refs/heads/feature\n"

	if err := os.WriteFile(gitConfigPath, []byte(brokenConfig), 0644); err != nil {
		t.Fatalf("failed to write broken config: %v", err)
	}

	// Verify raw "git pull --rebase" fails with the error
	cmd := exec.Command("git", "-C", cloneDir, "pull", "--rebase")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("git pull --rebase should have failed with multiple merge refs")
	}
	if !strings.Contains(string(output), "Cannot rebase onto multiple branches") {
		t.Logf("Raw git pull error: %v: %s", err, output)
	} else {
		t.Log("Confirmed: raw 'git pull --rebase' fails with broken config")
	}

	// Now test that schmux's gitPullRebase with explicit branch works
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	// This should work because we explicitly specify origin/main
	err = m.gitPullRebase(ctx, cloneDir, "main")
	if err != nil {
		t.Errorf("gitPullRebase with explicit branch should work: %v", err)
	} else {
		t.Log("SUCCESS: gitPullRebase(origin main) works despite broken upstream config")
	}
}

// TestGitPullRebase_WithBranchParameter tests that gitPullRebase takes
// a branch parameter and explicitly pulls from origin/<branch>.
func TestGitPullRebase_WithBranchParameter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a remote repo
	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	// Clone it
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	// gitPullRebase with explicit origin/<branch> should work
	err := m.gitPullRebase(ctx, cloneDir, "main")
	if err != nil {
		t.Errorf("gitPullRebase(main) failed: %v", err)
	}

	// Switch to feature branch and pull
	runGit(t, cloneDir, "checkout", "feature")
	err = m.gitPullRebase(ctx, cloneDir, "feature")
	if err != nil {
		t.Errorf("gitPullRebase(feature) failed: %v", err)
	}

	t.Log("gitPullRebase() takes branch parameter - explicitly pulls from origin/<branch>")
}

func TestGitRemoteBranchExists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	runGit(t, remoteDir, "checkout", "-b", "feature")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature")
	runGit(t, remoteDir, "checkout", "main")

	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath)
	m := New(cfg, st, statePath)
	ctx := context.Background()

	exists, err := m.gitRemoteBranchExists(ctx, cloneDir, "main")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(main) error: %v", err)
	}
	if !exists {
		t.Error("gitRemoteBranchExists(main) expected true")
	}

	exists, err = m.gitRemoteBranchExists(ctx, cloneDir, "feature")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(feature) error: %v", err)
	}
	if !exists {
		t.Error("gitRemoteBranchExists(feature) expected true")
	}

	exists, err = m.gitRemoteBranchExists(ctx, cloneDir, "missing-branch")
	if err != nil {
		t.Fatalf("gitRemoteBranchExists(missing-branch) error: %v", err)
	}
	if exists {
		t.Error("gitRemoteBranchExists(missing-branch) expected false")
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
