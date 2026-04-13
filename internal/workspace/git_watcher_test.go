package workspace

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
)

func TestResolveGitDir_RegularClone(t *testing.T) {
	t.Parallel()
	// Create a temp directory with a .git/ directory
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}

	resolved, err := resolveGitDir(tmpDir)
	if err != nil {
		t.Fatalf("resolveGitDir() error: %v", err)
	}

	if resolved != gitDir {
		t.Errorf("resolveGitDir() = %s, want %s", resolved, gitDir)
	}
}

func TestResolveGitDir_Worktree(t *testing.T) {
	t.Parallel()
	// Create a temp directory structure simulating a worktree
	tmpDir := t.TempDir()

	// Create the base repo structure
	baseRepo := filepath.Join(tmpDir, "base.git")
	worktreeGitDir := filepath.Join(baseRepo, "worktrees", "my-worktree")
	if err := os.MkdirAll(worktreeGitDir, 0755); err != nil {
		t.Fatalf("failed to create worktree gitdir: %v", err)
	}

	// Create the worktree directory with a .git file
	worktree := filepath.Join(tmpDir, "my-worktree")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Write .git file with gitdir pointer
	gitFile := filepath.Join(worktree, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+worktreeGitDir+"\n"), 0644); err != nil {
		t.Fatalf("failed to write .git file: %v", err)
	}

	resolved, err := resolveGitDir(worktree)
	if err != nil {
		t.Fatalf("resolveGitDir() error: %v", err)
	}

	if resolved != worktreeGitDir {
		t.Errorf("resolveGitDir() = %s, want %s", resolved, worktreeGitDir)
	}
}

func TestResolveGitDir_WorktreeRelativePath(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create base repo structure at tmpDir/repos/base.git/worktrees/wt
	baseRepo := filepath.Join(tmpDir, "repos", "base.git")
	worktreeGitDir := filepath.Join(baseRepo, "worktrees", "wt")
	if err := os.MkdirAll(worktreeGitDir, 0755); err != nil {
		t.Fatalf("failed to create worktree gitdir: %v", err)
	}

	// Create worktree at tmpDir/workspaces/wt
	worktree := filepath.Join(tmpDir, "workspaces", "wt")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Write .git file with relative gitdir pointer
	relPath, err := filepath.Rel(worktree, worktreeGitDir)
	if err != nil {
		t.Fatalf("failed to compute relative path: %v", err)
	}
	gitFile := filepath.Join(worktree, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: "+relPath+"\n"), 0644); err != nil {
		t.Fatalf("failed to write .git file: %v", err)
	}

	resolved, err := resolveGitDir(worktree)
	if err != nil {
		t.Fatalf("resolveGitDir() error: %v", err)
	}

	if resolved != worktreeGitDir {
		t.Errorf("resolveGitDir() = %s, want %s", resolved, worktreeGitDir)
	}
}

func TestResolveSharedBaseRefs(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create base repo with refs/
	baseRepo := filepath.Join(tmpDir, "base.git")
	refsDir := filepath.Join(baseRepo, "refs")
	worktreeDir := filepath.Join(baseRepo, "worktrees", "wt")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("failed to create refs dir: %v", err)
	}
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Should resolve to the base repo's refs/
	got := resolveSharedBaseRefs(worktreeDir)
	if got != refsDir {
		t.Errorf("resolveSharedBaseRefs() = %s, want %s", got, refsDir)
	}

	// Non-worktree path should return empty
	got = resolveSharedBaseRefs(filepath.Join(tmpDir, "regular-clone"))
	if got != "" {
		t.Errorf("resolveSharedBaseRefs() for non-worktree = %s, want empty", got)
	}
}

func TestSuppressionPathsForGitCommandDir_Worktree(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	baseRepo := filepath.Join(tmpDir, "base.git")
	baseRefs := filepath.Join(baseRepo, "refs")
	worktreeGitDir := filepath.Join(baseRepo, "worktrees", "wt")
	if err := os.MkdirAll(baseRefs, 0755); err != nil {
		t.Fatalf("failed to create base refs: %v", err)
	}
	if err := os.MkdirAll(worktreeGitDir, 0755); err != nil {
		t.Fatalf("failed to create worktree gitdir: %v", err)
	}

	worktree := filepath.Join(tmpDir, "wt")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatalf("failed to create worktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+worktreeGitDir+"\n"), 0644); err != nil {
		t.Fatalf("failed to write .git file: %v", err)
	}

	paths := suppressionPathsForGitCommandDir(worktree)
	if len(paths) != 2 {
		t.Fatalf("expected 2 suppression paths, got %d: %v", len(paths), paths)
	}
	if paths[0] != filepath.Clean(worktreeGitDir) && paths[1] != filepath.Clean(worktreeGitDir) {
		t.Errorf("worktree gitdir missing from suppression paths: %v", paths)
	}
	if paths[0] != filepath.Clean(baseRefs) && paths[1] != filepath.Clean(baseRefs) {
		t.Errorf("base refs missing from suppression paths: %v", paths)
	}
}

func TestSuppressionPathsForGitCommandDir_BareRepo(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	bare := filepath.Join(tmpDir, "repo.git")
	if err := os.MkdirAll(filepath.Join(bare, "refs"), 0755); err != nil {
		t.Fatalf("failed to create refs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(bare, "objects"), 0755); err != nil {
		t.Fatalf("failed to create objects: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bare, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to write HEAD: %v", err)
	}

	paths := suppressionPathsForGitCommandDir(bare)
	if len(paths) != 1 || paths[0] != filepath.Clean(bare) {
		t.Fatalf("unexpected suppression paths for bare repo: %v", paths)
	}
}

func TestInternalSuppressionLifecycle(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw == nil {
		t.Fatal("NewGitWatcher() returned nil")
	}
	defer gw.Stop()

	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "refs", "heads"), 0755); err != nil {
		t.Fatalf("failed to create refs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0755); err != nil {
		t.Fatalf("failed to create objects: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to write HEAD: %v", err)
	}

	release := gw.BeginInternalGitSuppressionForDir(gitDir)
	if !gw.isSuppressedPath(filepath.Join(gitDir, "refs", "heads", "main")) {
		t.Fatal("expected path to be suppressed while command is active")
	}

	release()
	if !gw.isSuppressedPath(filepath.Join(gitDir, "FETCH_HEAD")) {
		t.Fatal("expected path to remain suppressed during grace period")
	}

	// Force expiration to avoid sleeping for the grace period in tests.
	gw.suppressedPathsMu.Lock()
	key := filepath.Clean(gitDir)
	state := gw.suppressedPaths[key]
	state.active = 0
	state.until = time.Now().Add(-time.Millisecond)
	gw.suppressedPaths[key] = state
	gw.suppressedPathsMu.Unlock()

	if gw.isSuppressedPath(filepath.Join(gitDir, "refs", "heads", "main")) {
		t.Fatal("expected suppression to expire after grace period")
	}
}

func TestWatcherDisabledByConfig(t *testing.T) {
	t.Parallel()
	disabled := false
	cfg := &config.Config{}
	cfg.Sessions = &config.SessionsConfig{
		GitStatusWatchEnabled: &disabled,
	}

	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw != nil {
		t.Error("NewGitWatcher() should return nil when disabled by config")
	}
}

func TestWatcherEnabledByDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	if !cfg.GetGitStatusWatchEnabled() {
		t.Error("GetGitStatusWatchEnabled() should return true by default")
	}
}

func TestDebounceCollapse(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	refsDir := filepath.Join(gitDir, "refs")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatalf("failed to write HEAD: %v", err)
	}

	var refreshCount atomic.Int32

	cfg := &config.Config{}
	cfg.Sessions = &config.SessionsConfig{
		GitStatusWatchDebounceMs: 200,
	}

	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw == nil {
		t.Fatal("NewGitWatcher() returned nil")
	}

	// Inject test callback to count refreshes without real git operations
	gw.onRefresh = func(workspaceID string) {
		refreshCount.Add(1)
	}

	gw.AddWorkspace("test-001", tmpDir)
	gw.Start()
	defer gw.Stop()

	// Fire 5 rapid events — should collapse into a single refresh
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)
		time.Sleep(20 * time.Millisecond)
	}

	// Poll until the debounce fires (200ms debounce + margin)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if refreshCount.Load() > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	count := refreshCount.Load()
	if count == 0 {
		t.Fatal("expected at least 1 refresh after debounce, got 0")
	}
	if count > 2 {
		// With 200ms debounce and 100ms total event spread, we expect 1 refresh.
		// Allow up to 2 for timing variance, but 5 means no debounce.
		t.Errorf("expected debounce to collapse events into 1-2 refreshes, got %d", count)
	}
}

func TestAddRemoveWorkspace(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	refsDir := filepath.Join(gitDir, "refs", "heads")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}

	cfg := &config.Config{}
	cfg.Sessions = &config.SessionsConfig{
		GitStatusWatchDebounceMs: 60000, // long debounce to prevent timer fires
	}
	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw == nil {
		t.Fatal("NewGitWatcher() returned nil")
	}
	defer gw.Stop()

	// Add workspace
	gw.AddWorkspace("test-001", tmpDir)

	gw.watchedPathsMu.Lock()
	pathCount := len(gw.watchedPaths)
	gw.watchedPathsMu.Unlock()

	if pathCount == 0 {
		t.Error("expected watched paths after AddWorkspace, got 0")
	}

	// Remove workspace
	gw.RemoveWorkspace("test-001")

	gw.watchedPathsMu.Lock()
	pathCount = len(gw.watchedPaths)
	gw.watchedPathsMu.Unlock()

	if pathCount != 0 {
		t.Errorf("expected 0 watched paths after RemoveWorkspace, got %d", pathCount)
	}
}

func TestNewDirsWatched(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	refsDir := filepath.Join(gitDir, "refs")
	logsDir := filepath.Join(gitDir, "logs")
	if err := os.MkdirAll(refsDir, 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatalf("failed to create dirs: %v", err)
	}

	// Use a very long debounce so the timer doesn't fire during the test
	// (we're only testing directory watching, not refresh behavior)
	cfg := &config.Config{}
	cfg.Sessions = &config.SessionsConfig{
		GitStatusWatchDebounceMs: 60000,
	}

	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw == nil {
		t.Fatal("NewGitWatcher() returned nil")
	}
	gw.AddWorkspace("test-001", tmpDir)

	// refs/ should not be watched (high-noise remote/shared ref churn belongs to poller)
	gw.watchedPathsMu.Lock()
	_, refsWatched := gw.watchedPaths[refsDir]
	gw.watchedPathsMu.Unlock()
	if refsWatched {
		t.Fatal("did not expect refs/ directory to be watched")
	}

	gw.Start()
	defer gw.Stop()

	// Create a new subdirectory under logs/ and ensure CREATE handling adds a watch.
	newLogsDir := filepath.Join(logsDir, "refs", "heads")
	if err := os.MkdirAll(newLogsDir, 0755); err != nil {
		t.Fatalf("failed to create logs dir: %v", err)
	}

	// Poll until the new directory (or its parent) appears in watched paths
	intermediateDir := filepath.Join(logsDir, "refs")
	deadline := time.Now().Add(2 * time.Second)
	var watched, intermediateWatched bool
	for time.Now().Before(deadline) {
		gw.watchedPathsMu.Lock()
		_, watched = gw.watchedPaths[newLogsDir]
		_, intermediateWatched = gw.watchedPaths[intermediateDir]
		gw.watchedPathsMu.Unlock()

		if watched || intermediateWatched {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !watched && !intermediateWatched {
		t.Error("expected new subdirectory under logs/ to be watched after CREATE event")
	}
}

func TestStopIdempotent(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{}
	gw := NewGitWatcher(cfg, nil, nil, testLogger())
	if gw == nil {
		t.Fatal("NewGitWatcher() returned nil")
	}
	gw.Start()

	// Calling Stop twice should not panic
	gw.Stop()
	gw.Stop()
}
