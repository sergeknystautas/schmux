package workspace

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
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

// TestEnsureOriginQueries_SkipsDefaultBranchWhenCached verifies that
// EnsureOriginQueries skips the getDefaultBranch call (git symbolic-ref)
// when the default branch is already cached from a previous call.
func TestEnsureOriginQueries_SkipsDefaultBranchWhenCached(t *testing.T) {
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

	// First call — creates the query repo and detects default branch
	if err := m.EnsureOriginQueries(ctx); err != nil {
		t.Fatalf("first EnsureOriginQueries() failed: %v", err)
	}

	// Verify default branch was cached
	defaultBranch, err := m.GetDefaultBranch(ctx, remoteDir)
	if err != nil {
		t.Fatalf("GetDefaultBranch() failed after first call: %v", err)
	}
	if defaultBranch != "main" {
		t.Errorf("expected cached default branch=main, got %q", defaultBranch)
	}

	// Reset telemetry to measure only the second call
	tel.Snapshot(true)

	// Second call — default branch is cached, should skip symbolic-ref
	if err := m.EnsureOriginQueries(ctx); err != nil {
		t.Fatalf("second EnsureOriginQueries() failed: %v", err)
	}

	snap2 := tel.Snapshot(true)
	symbolicRefCount2 := snap2.Counters["git_symbolic-ref"]
	if symbolicRefCount2 != 0 {
		t.Errorf("second call should run 0 symbolic-ref calls (cached), got %d", symbolicRefCount2)
	}
}

// TestUpdateLocalDefaultBranch_ShortCircuitsWhenRefsMatch verifies that
// updateLocalDefaultBranch skips the worktree-check, merge-base, and update-ref
// commands when refs/heads/main and refs/remotes/origin/main already point to
// the same commit (the common steady-state case).
func TestUpdateLocalDefaultBranch_ShortCircuitsWhenRefsMatch(t *testing.T) {
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

	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Create bare clone
	bareRepoPath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	// Fetch so origin/main is up to date
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Fast-forward local main to match origin/main (ensure they're equal)
	m.setDefaultBranch(remoteDir, "main")
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	// Reset telemetry to measure only the second call
	tel.Snapshot(true)

	// Call updateLocalDefaultBranch again — refs should already match
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	snap := tel.Snapshot(false)

	// Should have run exactly 1 rev-parse (to compare SHAs) and nothing else
	if revParseCount := snap.Counters["git_rev-parse"]; revParseCount != 1 {
		t.Errorf("expected 1 rev-parse call (SHA comparison), got %d", revParseCount)
	}
	if mergeBaseCount := snap.Counters["git_merge-base"]; mergeBaseCount != 0 {
		t.Errorf("expected 0 merge-base calls (short-circuited), got %d", mergeBaseCount)
	}
	if updateRefCount := snap.Counters["git_update-ref"]; updateRefCount != 0 {
		t.Errorf("expected 0 update-ref calls (short-circuited), got %d", updateRefCount)
	}
	if worktreeCount := snap.Counters["git_worktree"]; worktreeCount != 0 {
		t.Errorf("expected 0 worktree list calls (short-circuited), got %d", worktreeCount)
	}
}

// TestUpdateLocalDefaultBranch_StillUpdatesWhenBehind verifies that
// updateLocalDefaultBranch still performs the update when local main is behind
// origin/main (the rev-parse short-circuit only triggers when SHAs match).
func TestUpdateLocalDefaultBranch_StillUpdatesWhenBehind(t *testing.T) {
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

	remoteDir := gitTestWorkTree(t)
	cfg.Repos = []config.Repo{testRepoWithBarePath(t, "test", remoteDir)}

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())
	ctx := context.Background()

	// Create bare clone
	bareRepoPath, err := m.ensureWorktreeBase(ctx, remoteDir)
	if err != nil {
		t.Fatalf("ensureWorktreeBase() failed: %v", err)
	}

	m.setDefaultBranch(remoteDir, "main")
	initialHash := gitCommitHash(t, bareRepoPath, "refs/heads/main")

	// Push a new commit to the remote
	writeFile(t, remoteDir, "new.txt", "new")
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-m", "new commit")
	remoteHash := gitCommitHash(t, remoteDir, "HEAD")

	// Fetch to update origin/main
	if err := m.gitFetch(ctx, bareRepoPath); err != nil {
		t.Fatalf("gitFetch() failed: %v", err)
	}

	// Verify local main is still at initial
	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != initialHash {
		t.Fatalf("local main should be stale: got %s, want %s", got, initialHash)
	}

	// Call updateLocalDefaultBranch — should advance local main
	m.updateLocalDefaultBranch(ctx, "", RefreshTriggerExplicit, bareRepoPath, remoteDir, nil)

	if got := gitCommitHash(t, bareRepoPath, "refs/heads/main"); got != remoteHash {
		t.Errorf("updateLocalDefaultBranch() did not advance local main: got %s, want %s", got, remoteHash)
	}
}

// TestUpdateAllGitStatus_ParallelExecution verifies that UpdateAllGitStatus
// processes multiple workspaces concurrently.
func TestUpdateAllGitStatus_ParallelExecution(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	remoteDir := gitTestWorkTree(t)
	tmpDir := t.TempDir()

	// Create two cloned workspaces
	clone1 := filepath.Join(tmpDir, "ws-001")
	clone2 := filepath.Join(tmpDir, "ws-002")
	runGit(t, tmpDir, "clone", remoteDir, "ws-001")
	runGit(t, tmpDir, "clone", remoteDir, "ws-002")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)

	st.AddWorkspace(state.Workspace{ID: "ws-001", Repo: remoteDir, Branch: "main", Path: clone1})
	st.AddWorkspace(state.Workspace{ID: "ws-002", Repo: remoteDir, Branch: "main", Path: clone2})

	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Run UpdateAllGitStatus — should complete without error
	m.UpdateAllGitStatus(ctx)

	// Verify both workspaces were updated
	w1, _ := st.GetWorkspace("ws-001")
	w2, _ := st.GetWorkspace("ws-002")
	if w1.Branch != "main" {
		t.Errorf("ws-001 branch should be main, got %q", w1.Branch)
	}
	if w2.Branch != "main" {
		t.Errorf("ws-002 branch should be main, got %q", w2.Branch)
	}

	// Verify fetch deduplication: both workspaces are separate clones (not
	// worktrees sharing a bare clone), so they need separate fetches.
	snap := tel.Snapshot(false)
	if snap.Counters["git_fetch"] < 2 {
		t.Errorf("expected at least 2 fetches (one per workspace), got %d", snap.Counters["git_fetch"])
	}
}

// TestWorktreeListCache_DeduplicatesCalls verifies that the worktreeListCache
// caches results and only runs git worktree list once per key.
func TestWorktreeListCache_DeduplicatesCalls(t *testing.T) {
	t.Parallel()

	cache := newWorktreeListCache()
	var calls atomic.Int32

	fn := func() ([]byte, error) {
		calls.Add(1)
		return []byte("worktree /path\nbranch refs/heads/main\n"), nil
	}

	ctx := context.Background()

	// First call — should execute fn
	out1, err := cache.Get(ctx, "/repo/path", fn)
	if err != nil {
		t.Fatalf("first Get() error: %v", err)
	}
	if string(out1) != "worktree /path\nbranch refs/heads/main\n" {
		t.Errorf("unexpected output: %q", string(out1))
	}

	// Second call with same key — should return cached result
	out2, err := cache.Get(ctx, "/repo/path", fn)
	if err != nil {
		t.Fatalf("second Get() error: %v", err)
	}
	if string(out2) != string(out1) {
		t.Errorf("cached output mismatch: %q vs %q", string(out2), string(out1))
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 underlying call, got %d", got)
	}
}

// TestWorktreeListCache_DifferentKeys verifies that the cache stores results
// independently per key.
func TestWorktreeListCache_DifferentKeys(t *testing.T) {
	t.Parallel()

	cache := newWorktreeListCache()
	var calls atomic.Int32

	fn := func() ([]byte, error) {
		calls.Add(1)
		return []byte("result"), nil
	}

	ctx := context.Background()

	cache.Get(ctx, "key-a", fn)
	cache.Get(ctx, "key-b", fn)
	cache.Get(ctx, "key-a", fn) // cached
	cache.Get(ctx, "key-b", fn) // cached

	if got := calls.Load(); got != 2 {
		t.Errorf("expected 2 underlying calls (one per unique key), got %d", got)
	}
}

// TestWorktreeListCache_ConcurrentWaiters verifies that concurrent Get calls
// for the same key wait for the first caller's result.
func TestWorktreeListCache_ConcurrentWaiters(t *testing.T) {
	t.Parallel()

	cache := newWorktreeListCache()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	fn := func() ([]byte, error) {
		calls.Add(1)
		close(started)
		<-release
		return []byte("done"), nil
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = cache.Get(ctx, "key", fn)
	}()

	<-started

	go func() {
		defer wg.Done()
		_, errs[1] = cache.Get(ctx, "key", fn)
	}()

	close(release)
	wg.Wait()

	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 underlying call, got %d", got)
	}
}

// TestWorktreeListCache_NilCachePassesThrough verifies that a nil cache
// falls through to the function (used when not in a poll round).
func TestWorktreeListCache_NilCachePassesThrough(t *testing.T) {
	t.Parallel()

	var cache *worktreeListCache // nil
	var calls atomic.Int32

	fn := func() ([]byte, error) {
		calls.Add(1)
		return []byte("output"), nil
	}

	ctx := context.Background()
	out, err := cache.Get(ctx, "key", fn)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if string(out) != "output" {
		t.Errorf("unexpected output: %q", string(out))
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
}

// TestPollRound_BundlesBothCaches verifies that newPollRound creates both
// sub-caches and that they work independently.
func TestPollRound_BundlesBothCaches(t *testing.T) {
	t.Parallel()

	round := newPollRound()
	if round.fetch == nil {
		t.Error("pollRound.fetch should not be nil")
	}
	if round.worktree == nil {
		t.Error("pollRound.worktree should not be nil")
	}
}

// TestFetchOriginQueries_ParallelExecution verifies that FetchOriginQueries
// fetches multiple origin query repos concurrently rather than sequentially.
func TestFetchOriginQueries_ParallelExecution(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Create two separate remote repos
	remote1 := gitTestWorkTree(t)
	remote2 := gitTestWorkTree(t)

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	configPath := filepath.Join(tmpDir, "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	cfg.Repos = []config.Repo{
		testRepoWithBarePath(t, "repo1", remote1),
		testRepoWithBarePath(t, "repo2", remote2),
	}

	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Create the origin query repos
	if err := m.EnsureOriginQueries(ctx); err != nil {
		t.Fatalf("EnsureOriginQueries() failed: %v", err)
	}

	tel.Snapshot(true) // reset

	// Run FetchOriginQueries — should fetch both repos
	m.FetchOriginQueries(ctx)

	snap := tel.Snapshot(false)
	fetchCount := snap.Counters["git_fetch"]
	if fetchCount < 2 {
		t.Errorf("expected at least 2 fetches (one per origin query repo), got %d", fetchCount)
	}
}

// TestCommitsSyncedWithRemote_DerivedFromRevList verifies that commitsSyncedWithRemote
// is derived from rev-list counts rather than two separate merge-base --is-ancestor calls.
func TestCommitsSyncedWithRemote_DerivedFromRevList(t *testing.T) {
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

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// When on main and synced with origin/main, commitsSynced should be true
	_, _, _, _, _, _, commitsSynced, remoteBranchExists, _, _, _ := m.gitStatus(ctx, "ws-test", RefreshTriggerExplicit, cloneDir, remoteDir)
	if !remoteBranchExists {
		t.Error("expected remoteBranchExists=true for main branch")
	}
	if !commitsSynced {
		t.Error("expected commitsSyncedWithRemote=true when HEAD matches origin/main")
	}

	snap := tel.Snapshot(false)

	// Verify no merge-base --is-ancestor calls were made for the synced check.
	// The only merge-base calls should be for the orphan check or common ancestor,
	// NOT for commitsSyncedWithRemote (which is now derived from rev-list).
	// Count all commands to verify rev-list was used.
	var mergeBaseIsAncestorCount int
	for _, cmd := range snap.AllCommands {
		if cmd.Command == "git merge-base --is-ancestor HEAD origin/main" ||
			cmd.Command == "git merge-base --is-ancestor origin/main HEAD" {
			mergeBaseIsAncestorCount++
		}
	}
	if mergeBaseIsAncestorCount != 0 {
		t.Errorf("expected 0 merge-base --is-ancestor calls (replaced by rev-list derivation), got %d", mergeBaseIsAncestorCount)
	}
}

// TestCommitsSyncedWithRemote_FalseWhenAhead verifies that commitsSyncedWithRemote
// is false when local has commits not pushed to remote.
func TestCommitsSyncedWithRemote_FalseWhenAhead(t *testing.T) {
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

	// Make a local commit that's not pushed
	writeFile(t, cloneDir, "local.txt", "local change")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "local commit")

	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{WorkspacePath: tmpDir}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	ctx := context.Background()

	_, _, _, _, _, _, commitsSynced, _, localUnique, _, _ := m.gitStatus(ctx, "ws-test", RefreshTriggerExplicit, cloneDir, remoteDir)
	if commitsSynced {
		t.Error("expected commitsSyncedWithRemote=false when local has unpushed commits")
	}
	if localUnique != 1 {
		t.Errorf("expected localUnique=1, got %d", localUnique)
	}
}

// TestOrphanCheck_SkippedWhenSynced verifies that the orphan detection check
// (merge-base HEAD origin/main) is skipped when ahead=0 and behind=0.
func TestOrphanCheck_SkippedWhenSynced(t *testing.T) {
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
	m.setDefaultBranch(remoteDir, "main")

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	// Run UpdateGitStatus — on main branch, synced with origin, ahead=0, behind=0
	updated, err := m.UpdateGitStatus(ctx, "test-001")
	if err != nil {
		t.Fatalf("UpdateGitStatus() failed: %v", err)
	}

	if updated.GitAhead != 0 || updated.GitBehind != 0 {
		t.Fatalf("expected ahead=0, behind=0; got ahead=%d, behind=%d", updated.GitAhead, updated.GitBehind)
	}

	snap := tel.Snapshot(false)

	// Count merge-base calls — should be 0 since ahead=behind=0 skips the orphan check
	// (and optimization H eliminated the --is-ancestor calls)
	mergeBaseCount := snap.Counters["git_merge-base"]
	if mergeBaseCount != 0 {
		t.Errorf("expected 0 merge-base calls when ahead=behind=0 (orphan check skipped), got %d", mergeBaseCount)
	}

	if updated.GitDefaultBranchOrphaned {
		t.Error("workspace should not be marked as orphaned when ahead=behind=0")
	}
}

// TestOrphanCheck_StillRunsWhenAhead verifies that the orphan detection check
// still runs when the workspace has commits ahead of default.
func TestOrphanCheck_StillRunsWhenAhead(t *testing.T) {
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

	// Create a local commit to make workspace ahead
	writeFile(t, cloneDir, "ahead.txt", "ahead")
	runGit(t, cloneDir, "add", ".")
	runGit(t, cloneDir, "commit", "-m", "ahead commit")

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
	m.setDefaultBranch(remoteDir, "main")

	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)

	ctx := context.Background()

	updated, err := m.UpdateGitStatus(ctx, "test-001")
	if err != nil {
		t.Fatalf("UpdateGitStatus() failed: %v", err)
	}

	if updated.GitAhead == 0 {
		t.Fatal("expected ahead > 0 with local commit")
	}

	snap := tel.Snapshot(false)

	// Should have at least 1 merge-base call for orphan check
	mergeBaseCount := snap.Counters["git_merge-base"]
	if mergeBaseCount == 0 {
		t.Error("expected merge-base call for orphan check when ahead > 0")
	}

	// Should NOT be orphaned (shares ancestry with origin/main)
	if updated.GitDefaultBranchOrphaned {
		t.Error("workspace should not be orphaned — it shares ancestry with origin/main")
	}
}

// TestRefreshDefaultBranchThrottled_SkipsWithinInterval verifies that
// refreshDefaultBranchThrottled skips the git symbolic-ref call when called
// within the refresh interval.
func TestRefreshDefaultBranchThrottled_SkipsWithinInterval(t *testing.T) {
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

	// Create the origin query repo
	if err := m.EnsureOriginQueries(ctx); err != nil {
		t.Fatalf("EnsureOriginQueries() failed: %v", err)
	}

	queryRepoPath := m.getQueryRepoPath(remoteDir)

	// Reset telemetry
	tel.Snapshot(true)

	// First throttled call — should run symbolic-ref (no recent refresh)
	// Clear the refresh time to simulate fresh state
	m.defaultBranchCacheMu.Lock()
	delete(m.defaultBranchRefreshAt, remoteDir)
	m.defaultBranchCacheMu.Unlock()

	m.refreshDefaultBranchThrottled(ctx, queryRepoPath, remoteDir)

	snap1 := tel.Snapshot(true)
	if snap1.Counters["git_symbolic-ref"] == 0 {
		t.Error("first call should have run symbolic-ref")
	}

	// Second call immediately after — should be throttled
	m.refreshDefaultBranchThrottled(ctx, queryRepoPath, remoteDir)

	snap2 := tel.Snapshot(true)
	if snap2.Counters["git_symbolic-ref"] != 0 {
		t.Errorf("second call should skip symbolic-ref (throttled), got %d calls", snap2.Counters["git_symbolic-ref"])
	}
}
