package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestGitStatus_WatcherSkipsFetch verifies that watcher-triggered refreshes
// do NOT run git fetch, while poller-triggered refreshes DO.
func TestGitStatus_WatcherSkipsFetch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create a "remote" repo and clone it
	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Attach telemetry to count git commands
	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Run gitStatus with watcher trigger
	m.gitStatus(ctx, "ws-test", RefreshTriggerWatcher, cloneDir, remoteDir)

	snap := tel.Snapshot(true)
	if fetchCount := snap.Counters["git_fetch"]; fetchCount != 0 {
		t.Errorf("watcher trigger: expected 0 git fetch calls, got %d", fetchCount)
	}

	// Run gitStatus with poller trigger — should fetch
	m.gitStatus(ctx, "ws-test", RefreshTriggerPoller, cloneDir, remoteDir)

	snap = tel.Snapshot(true)
	if fetchCount := snap.Counters["git_fetch"]; fetchCount != 1 {
		t.Errorf("poller trigger: expected 1 git fetch call, got %d", fetchCount)
	}

	// Run gitStatus with explicit trigger — should also fetch
	m.gitStatus(ctx, "ws-test", RefreshTriggerExplicit, cloneDir, remoteDir)

	snap = tel.Snapshot(true)
	if fetchCount := snap.Counters["git_fetch"]; fetchCount != 1 {
		t.Errorf("explicit trigger: expected 1 git fetch call, got %d", fetchCount)
	}
}

// TestGitStatus_WatcherStillReturnsAccurateLocalState verifies that even without
// fetching, watcher-triggered refreshes still return accurate local state
// (dirty files, current branch, line counts).
func TestGitStatus_WatcherStillReturnsAccurateLocalState(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create a dirty file
	writeFile(t, cloneDir, "dirty.txt", "uncommitted change")

	dirty, _, _, _, _, _, _, _, _, _, currentBranch := m.gitStatus(ctx, "ws-test", RefreshTriggerWatcher, cloneDir, remoteDir)

	if !dirty {
		t.Error("watcher trigger: expected dirty=true with uncommitted file")
	}
	if currentBranch != "main" {
		t.Errorf("watcher trigger: expected currentBranch=main, got %q", currentBranch)
	}
}

// TestGitStatus_ReturnsCurrentBranch verifies that gitStatus returns the current
// branch name, eliminating the need for a separate gitCurrentBranch call.
func TestGitStatus_ReturnsCurrentBranch(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)

	// Create a feature branch on the remote
	runGit(t, remoteDir, "checkout", "-b", "feature/test")
	writeFile(t, remoteDir, "feature.txt", "feature")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "feature commit")
	runGit(t, remoteDir, "checkout", "main")

	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// On main branch
	_, _, _, _, _, _, _, _, _, _, branch := m.gitStatus(ctx, "ws-test", RefreshTriggerExplicit, cloneDir, remoteDir)
	if branch != "main" {
		t.Errorf("expected currentBranch=main, got %q", branch)
	}

	// Switch to feature branch
	runGit(t, cloneDir, "checkout", "feature/test")
	_, _, _, _, _, _, _, _, _, _, branch = m.gitStatus(ctx, "ws-test", RefreshTriggerExplicit, cloneDir, remoteDir)
	if branch != "feature/test" {
		t.Errorf("expected currentBranch=feature/test, got %q", branch)
	}
}

// TestEnsuredQueryRepos_CachePreventsRevalidation verifies that once a repo URL
// is cached in ensuredQueryRepos, ensureOriginQueryRepo skips the expensive
// ensureCorrectOriginURL and originQueryRepoNeedsRepair checks.
func TestEnsuredQueryRepos_CachePreventsRevalidation(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Manually create the query repo directory that ensureOriginQueryRepo would find
	queryRepoPath := m.getQueryRepoPath(remoteDir)
	if queryRepoPath == "" {
		t.Fatal("getQueryRepoPath returned empty — repo not in config")
	}

	// Clone bare into the query repo path
	runGit(t, "", "clone", "--bare", remoteDir, queryRepoPath)
	runGit(t, queryRepoPath, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")

	// First call — repo exists but not yet cached, should run validation
	path1, err := m.ensureOriginQueryRepo(ctx, remoteDir)
	if err != nil {
		t.Fatalf("first ensureOriginQueryRepo() failed: %v", err)
	}
	if path1 == "" {
		t.Fatal("first ensureOriginQueryRepo() returned empty path")
	}

	snap1 := tel.Snapshot(true)
	if snap1.TotalCommands == 0 {
		t.Fatal("first call should have run git commands for validation")
	}
	// Should have run git config --get remote.origin.url (ensureCorrectOriginURL)
	configGetCount := int64(0)
	for _, cmd := range snap1.AllCommands {
		if cmd.Command == "git config --get remote.origin.url" {
			configGetCount++
		}
	}
	if configGetCount == 0 {
		t.Error("first call should have run 'git config --get remote.origin.url' for validation")
	}

	// Verify the cache is now set
	m.ensuredQueryReposMu.RLock()
	cached := m.ensuredQueryRepos[remoteDir]
	m.ensuredQueryReposMu.RUnlock()
	if !cached {
		t.Fatal("ensuredQueryRepos should be true after first successful call")
	}

	// Second call — should skip validation entirely
	path2, err := m.ensureOriginQueryRepo(ctx, remoteDir)
	if err != nil {
		t.Fatalf("second ensureOriginQueryRepo() failed: %v", err)
	}
	if path2 != path1 {
		t.Errorf("paths differ: first=%q second=%q", path1, path2)
	}

	snap2 := tel.Snapshot(true)
	if snap2.TotalCommands != 0 {
		t.Errorf("second call should run 0 git commands (cached), got %d", snap2.TotalCommands)
	}
}

// TestEnsuredQueryRepos_InitializedInNew verifies that the ensuredQueryRepos map
// is properly initialized when New() creates a Manager.
func TestEnsuredQueryRepos_InitializedInNew(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	if m.ensuredQueryRepos == nil {
		t.Fatal("ensuredQueryRepos should be initialized by New()")
	}
	if len(m.ensuredQueryRepos) != 0 {
		t.Errorf("ensuredQueryRepos should start empty, has %d entries", len(m.ensuredQueryRepos))
	}
}

// TestUpdateGitStatus_NoDuplicateBranchQueries verifies that
// updateGitStatusWithTriggerAndRound does NOT run a separate git rev-parse
// for the current branch, since gitStatus already returns it.
func TestUpdateGitStatus_NoDuplicateBranchQueries(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)

	// Add workspace to state
	w := state.Workspace{
		ID:     "test-001",
		Repo:   remoteDir,
		Branch: "main",
		Path:   cloneDir,
	}
	st.AddWorkspace(w)

	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Run UpdateGitStatus
	updated, err := m.UpdateGitStatus(ctx, "test-001")
	if err != nil {
		t.Fatalf("UpdateGitStatus() failed: %v", err)
	}

	if updated.Branch != "main" {
		t.Errorf("expected branch=main, got %q", updated.Branch)
	}

	snap := tel.Snapshot(false)

	// Count rev-parse calls — gitStatusWithRound runs one for the current branch.
	// Before the optimization, updateGitStatusWithTriggerAndRound would run a
	// second rev-parse. Verify only one exists.
	revParseCount := snap.Counters["git_rev-parse"]
	if revParseCount > 1 {
		t.Errorf("expected at most 1 rev-parse call (branch detection), got %d — duplicate query not eliminated", revParseCount)
	}
}

// TestUpdateGitStatus_NoDuplicateRemoteBranchCheck verifies that
// updateGitStatusWithTriggerAndRound does NOT run a separate show-ref
// to check remote branch existence, since gitStatus already computes it.
func TestUpdateGitStatus_NoDuplicateRemoteBranchCheck(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()
	cloneDir := filepath.Join(tmpDir, "clone")
	runGit(t, tmpDir, "clone", remoteDir, "clone")
	runGit(t, cloneDir, "config", "user.email", "test@test.com")
	runGit(t, cloneDir, "config", "user.name", "Test")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)

	w := state.Workspace{
		ID:     "test-001",
		Repo:   remoteDir,
		Branch: "main",
		Path:   cloneDir,
	}
	st.AddWorkspace(w)

	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	updated, err := m.UpdateGitStatus(ctx, "test-001")
	if err != nil {
		t.Fatalf("UpdateGitStatus() failed: %v", err)
	}

	// Remote branch "main" should exist
	if !updated.RemoteBranchExists {
		t.Error("expected RemoteBranchExists=true for main branch")
	}

	snap := tel.Snapshot(false)

	// Count show-ref calls. gitStatusWithRound calls show-ref once to check
	// if origin/<branch> exists. Before the optimization, updateGitStatusWithTriggerAndRound
	// would call it again via GetWorktreeBaseByURL. Count should be exactly 1.
	showRefCount := snap.Counters["git_show-ref"]
	if showRefCount > 1 {
		t.Errorf("expected at most 1 show-ref call (remote branch check), got %d — duplicate query not eliminated", showRefCount)
	}
}
