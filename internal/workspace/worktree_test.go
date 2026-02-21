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
	t.Parallel()
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
	t.Parallel()
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

	// Add repo to config with BarePath
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", repoDir)}

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
	t.Parallel()
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

	// Add repo to config with BarePath
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", repoDir)}

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
	t.Parallel()
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
	t.Parallel()
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

// TestAddWorktree_StaleLocalBranchAfterForcesPush verifies that addWorktree
// does NOT use a stale local refs/heads/main when origin/main has been
// force-pushed to an unrelated history. The worktree should get the latest
// origin/main, not the orphaned local ref.
func TestAddWorktree_StaleLocalBranchAfterForcePush(t *testing.T) {
	t.Parallel()
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

	// Create "remote" repo with initial commit on main
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone
	bareRepoPath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}
	m.setDefaultBranch(remoteDir, "main")

	// Record the initial local main commit
	initialHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")

	// Force-push an orphan commit to main on the remote
	runGit(t, remoteDir, "checkout", "--orphan", "orphan-temp")
	writeFile(t, remoteDir, "orphan.txt", "orphan content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "orphan commit - new history")
	runGit(t, remoteDir, "branch", "-f", "main")
	runGit(t, remoteDir, "checkout", "main")

	newRemoteHash := gitCommitHash(t, remoteDir, "HEAD")

	// Fetch to update origin/main in the bare clone
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Sanity: local refs/heads/main should still be at the old commit (stale)
	localMainHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")
	if localMainHash != initialHash {
		t.Fatalf("local main should still be stale: got %s, want %s", localMainHash, initialHash)
	}

	// Sanity: origin/main should be at the new orphan commit
	originMainHash := gitCommitHash(t, bareRepoPath, "refs/remotes/origin/main")
	if originMainHash != newRemoteHash {
		t.Fatalf("origin/main should be at new commit: got %s, want %s", originMainHash, newRemoteHash)
	}

	// Create a worktree on main — should get origin/main, NOT the stale local ref
	worktreePath := filepath.Join(tmpDir, "wt-main")
	if err := m.addWorktree(ctx, bareRepoPath, worktreePath, "main", remoteDir); err != nil {
		t.Fatalf("addWorktree() failed: %v", err)
	}

	// Verify the worktree HEAD matches origin/main (new history)
	worktreeHash := gitCommitHash(t, worktreePath, "HEAD")
	if worktreeHash != newRemoteHash {
		t.Errorf("addWorktree() used stale local main: got %s, want %s (origin/main)", worktreeHash, newRemoteHash)
	}

	// Verify the orphan file exists (proves we got the new history)
	orphanFilePath := filepath.Join(worktreePath, "orphan.txt")
	if _, err := os.Stat(orphanFilePath); os.IsNotExist(err) {
		t.Error("worktree is missing orphan.txt — got stale local main instead of origin/main")
	}
}

// TestAddWorktree_DivergedLocalBranchPreferOrigin verifies that when local
// refs/heads/main has diverged from origin/main (e.g., local-only commits
// plus remote force-push), addWorktree still creates the worktree from
// origin/main so the workspace stays connected to the upstream history.
func TestAddWorktree_DivergedLocalBranchPrefersOrigin(t *testing.T) {
	t.Parallel()
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

	// Create "remote" repo with initial commit
	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	// Create bare clone
	bareRepoPath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}
	m.setDefaultBranch(remoteDir, "main")

	// Make a local-only commit on refs/heads/main in the bare clone (via temp worktree)
	divergeWorktree := filepath.Join(tmpDir, "diverge-wt")
	runGit(t, bareRepoPath, "worktree", "add", divergeWorktree, "main")
	writeFile(t, divergeWorktree, "local-only.txt", "local divergent commit")
	runGit(t, divergeWorktree, "add", ".")
	runGit(t, divergeWorktree, "config", "user.email", "test@test.com")
	runGit(t, divergeWorktree, "config", "user.name", "Test")
	runGit(t, divergeWorktree, "commit", "-m", "local divergent commit")
	runGit(t, bareRepoPath, "worktree", "remove", divergeWorktree)

	// Add different commits to the remote
	writeFile(t, remoteDir, "remote-new.txt", "remote new content")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "remote divergent commit")

	remoteMainHash := gitCommitHash(t, remoteDir, "HEAD")

	// Fetch to update origin/main
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Sanity: local main and origin/main should have diverged
	localMainHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")
	originMainHash := gitCommitHash(t, bareRepoPath, "refs/remotes/origin/main")
	if localMainHash == originMainHash {
		t.Fatal("test setup broken: local and origin main should have diverged")
	}

	// Create a worktree on main — should prefer origin/main over diverged local
	worktreePath := filepath.Join(tmpDir, "wt-main")
	if err := m.addWorktree(ctx, bareRepoPath, worktreePath, "main", remoteDir); err != nil {
		t.Fatalf("addWorktree() failed: %v", err)
	}

	// Verify the worktree HEAD matches origin/main
	worktreeHash := gitCommitHash(t, worktreePath, "HEAD")
	if worktreeHash != remoteMainHash {
		t.Errorf("addWorktree() used diverged local main: got %s, want %s (origin/main)", worktreeHash, remoteMainHash)
	}

	// Verify the remote file exists
	remoteFilePath := filepath.Join(worktreePath, "remote-new.txt")
	if _, err := os.Stat(remoteFilePath); os.IsNotExist(err) {
		t.Error("worktree is missing remote-new.txt — got stale local main instead of origin/main")
	}
}
