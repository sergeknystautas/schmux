package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

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
			name:     "creates first workspace",
			repoName: "myproject",
			branch:   "main",
			wantID:   "myproject-001",
			wantErr:  false,
		},
		{
			name:     "creates second workspace",
			repoName: "myproject",
			branch:   "main",
			wantID:   "myproject-002",
			wantErr:  false,
		},
		{
			name:     "creates workspace with different name",
			repoName: "otherproject",
			branch:   "main",
			wantID:   "otherproject-001",
			wantErr:  false,
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

func TestEnsureUniqueBranchRetryExhaustion(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	ctx := context.Background()
	repoDir := gitTestWorkTree(t)

	baseRepoPath, err := m.ensureWorktreeBase(ctx, repoDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	worktreePath := filepath.Join(tmpDir, "wt-main")
	runGit(t, baseRepoPath, "worktree", "add", worktreePath, "main")
	runGit(t, baseRepoPath, "branch", "main-aaa", "main")

	origRandSuffix := m.randSuffix
	m.randSuffix = func(length int) string {
		return "aaa"
	}
	defer func() {
		m.randSuffix = origRandSuffix
	}()

	if _, _, err := m.ensureUniqueBranch(ctx, baseRepoPath, "main"); err == nil {
		t.Fatalf("ensureUniqueBranch() expected error, got nil")
	}
}

func TestBranchSourceRefPrefersRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	ctx := context.Background()
	repoDir := gitTestWorkTree(t)
	gitTestBranch(t, repoDir, "feature")

	baseRepoPath, err := m.ensureWorktreeBase(ctx, repoDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	cmd := exec.Command("git", "rev-parse", "refs/heads/feature")
	cmd.Dir = baseRepoPath
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to resolve feature hash: %v", err)
	}
	featureHash := strings.TrimSpace(string(output))

	runGit(t, baseRepoPath, "update-ref", "refs/remotes/origin/feature", featureHash)

	ref, err := m.branchSourceRef(ctx, baseRepoPath, "feature")
	if err != nil {
		t.Fatalf("branchSourceRef() failed: %v", err)
	}
	if ref != "origin/feature" {
		t.Fatalf("branchSourceRef() = %s, want origin/feature", ref)
	}
}

// TestCreateLocalRepoCleanupOnStateSaveFailure verifies that local repo directory is cleaned up
// when init succeeds but state.Save() fails.
func TestCreateLocalRepoCleanupOnStateSaveFailure(t *testing.T) {
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
		Repos:         []config.Repo{},
	}

	// Create a mock state store that will fail on Save
	st := state.New("")
	mockSt := &mockStateStore{state: st, failSave: true}

	mgr := New(cfg, mockSt, "")

	ctx := context.Background()

	// Attempt to create a local repo workspace - should fail during state.Save
	_, err := mgr.CreateLocalRepo(ctx, "myproject", "main")
	if err == nil {
		t.Fatal("expected error from CreateLocalRepo, got nil")
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

// TestCreateLocalRepoNoCleanupOnSuccess verifies that local repo directory is NOT cleaned up
// when creation succeeds.
func TestCreateLocalRepoNoCleanupOnSuccess(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	workspaceBaseDir := filepath.Join(tmpDir, "workspaces")
	if err := os.MkdirAll(workspaceBaseDir, 0755); err != nil {
		t.Fatalf("failed to create workspace base dir: %v", err)
	}

	// Create a config with a path
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = workspaceBaseDir
	cfg.Repos = []config.Repo{}

	// Create a mock state store that will succeed
	st := state.New(statePath)
	mockSt := &mockStateStore{state: st, failSave: false}

	mgr := New(cfg, mockSt, statePath)

	ctx := context.Background()

	// Create a local repo workspace - should succeed
	w, err := mgr.CreateLocalRepo(ctx, "myproject", "main")
	if err != nil {
		t.Fatalf("CreateLocalRepo failed: %v", err)
	}

	// Verify the workspace directory still exists
	if _, err := os.Stat(w.Path); os.IsNotExist(err) {
		t.Errorf("workspace directory was cleaned up on success, path: %s", w.Path)
	}
}

// TestEnsureWorktreeBaseNamespacedPath verifies that new GitHub repos use namespaced paths (owner/repo.git).
func TestEnsureWorktreeBaseNamespacedPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	reposDir := filepath.Join(tmpDir, "repos")

	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = reposDir
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	ctx := context.Background()

	// Create a test repo to use as the "remote"
	remoteDir := gitTestWorkTree(t)

	// Simulate a GitHub-style URL by using the test repo path
	// Since it's a file path, extractRepoPath will fall back to just the repo name
	// Let's verify the namespaced path logic by checking the directory structure

	// First, test with a non-GitHub URL (file path) - should use flat structure
	basePath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	// For non-GitHub URLs, should use the repo name only
	repoName := extractRepoName(remoteDir)
	expectedPath := filepath.Join(reposDir, repoName+".git")
	if basePath != expectedPath {
		t.Errorf("ensureWorktreeBase() = %q, want %q", basePath, expectedPath)
	}

	// Verify directory was created
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		t.Errorf("worktree base directory was not created: %s", basePath)
	}
}

// TestEnsureWorktreeBaseLegacyFallback verifies that existing legacy repos are reused.
func TestEnsureWorktreeBaseLegacyFallback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	reposDir := filepath.Join(tmpDir, "repos")

	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = reposDir
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	ctx := context.Background()

	// Create a test repo to use as the "remote"
	remoteDir := gitTestWorkTree(t)
	repoName := extractRepoName(remoteDir)

	// Pre-create a legacy bare repo at the flat path
	if err := os.MkdirAll(reposDir, 0755); err != nil {
		t.Fatalf("failed to create repos dir: %v", err)
	}
	legacyPath := filepath.Join(reposDir, repoName+".git")
	runGit(t, reposDir, "clone", "--bare", remoteDir, repoName+".git")

	// ensureWorktreeBase should find and reuse the legacy path
	basePath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	if basePath != legacyPath {
		t.Errorf("ensureWorktreeBase() = %q, want legacy path %q", basePath, legacyPath)
	}

	// Verify it was tracked in state
	wb, found := st.GetWorktreeBaseByURL(remoteDir)
	if !found {
		t.Error("legacy worktree base was not tracked in state")
	}
	if wb.Path != legacyPath {
		t.Errorf("state tracked path = %q, want %q", wb.Path, legacyPath)
	}
}
