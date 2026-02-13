package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sergeknystautas/schmux/internal/config"
)

// GitWatcher watches .git metadata directories for changes and triggers
// debounced git status refreshes for affected workspaces.
type GitWatcher struct {
	watcher   *fsnotify.Watcher
	cfg       *config.Config
	mgr       *Manager
	broadcast func()

	// onRefresh is called instead of the default refreshWorkspace logic when set.
	// Used for testing to avoid real git operations.
	onRefresh func(workspaceID string)

	// watchedPaths maps watched filesystem paths to workspace IDs.
	// Multiple workspaces can map to the same path (shared base repo refs/).
	watchedPaths   map[string][]string
	watchedPathsMu sync.Mutex

	// debounceTimers holds per-workspace debounce timers.
	debounceTimers   map[string]*time.Timer
	debounceTimersMu sync.Mutex

	// lastStatusHash stores the last known git status hash per workspace
	lastStatusHash   map[string]string
	lastStatusHashMu sync.Mutex

	// stopCh signals the event loop to exit.
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewGitWatcher creates a new git watcher. Returns nil if watching is disabled
// in config.
func NewGitWatcher(cfg *config.Config, mgr *Manager, broadcast func()) *GitWatcher {
	if !cfg.GetGitStatusWatchEnabled() {
		fmt.Println("[git-watcher] disabled by config")
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("[git-watcher] failed to create watcher: %v\n", err)
		return nil
	}

	return &GitWatcher{
		watcher:        w,
		cfg:            cfg,
		mgr:            mgr,
		broadcast:      broadcast,
		watchedPaths:   make(map[string][]string),
		debounceTimers: make(map[string]*time.Timer),
		lastStatusHash: make(map[string]string),
		stopCh:         make(chan struct{}),
	}
}

// Start launches the event loop goroutine.
func (gw *GitWatcher) Start() {
	go gw.eventLoop()
	fmt.Println("[git-watcher] started")
}

// Stop closes the watcher and cancels all pending timers.
// Safe to call multiple times.
func (gw *GitWatcher) Stop() {
	gw.stopOnce.Do(func() {
		close(gw.stopCh)
		gw.watcher.Close()

		gw.debounceTimersMu.Lock()
		for _, t := range gw.debounceTimers {
			t.Stop()
		}
		gw.debounceTimersMu.Unlock()

		fmt.Println("[git-watcher] stopped")
	})
}

// AddWorkspace adds filesystem watches for a workspace's git metadata.
func (gw *GitWatcher) AddWorkspace(workspaceID, workspacePath string) {
	gitDir, err := resolveGitDir(workspacePath)
	if err != nil {
		fmt.Printf("[git-watcher] failed to resolve git dir for %s: %v\n", workspaceID, err)
		return
	}

	// Watch the gitdir itself (catches HEAD, index, packed-refs, FETCH_HEAD changes)
	gw.addWatch(gitDir, workspaceID)

	// Watch refs/ tree
	refsDir := filepath.Join(gitDir, "refs")
	gw.watchRecursive(refsDir, workspaceID)

	// Watch logs/ directory
	logsDir := filepath.Join(gitDir, "logs")
	gw.watchRecursive(logsDir, workspaceID)

	// For worktrees, also watch the shared base repo's refs/
	baseRefsDir := resolveSharedBaseRefs(gitDir)
	if baseRefsDir != "" && baseRefsDir != refsDir {
		gw.watchRecursive(baseRefsDir, workspaceID)
	}

	fmt.Printf("[git-watcher] watching %s (gitdir=%s)\n", workspaceID, gitDir)
}

// RemoveWorkspace removes all watches for a workspace and cancels its debounce timer.
func (gw *GitWatcher) RemoveWorkspace(workspaceID string) {
	gw.watchedPathsMu.Lock()
	var pathsToRemove []string
	for path, ids := range gw.watchedPaths {
		filtered := removeFromSlice(ids, workspaceID)
		if len(filtered) == 0 {
			pathsToRemove = append(pathsToRemove, path)
			delete(gw.watchedPaths, path)
		} else {
			gw.watchedPaths[path] = filtered
		}
	}
	gw.watchedPathsMu.Unlock()

	for _, path := range pathsToRemove {
		gw.watcher.Remove(path)
	}

	gw.debounceTimersMu.Lock()
	if t, ok := gw.debounceTimers[workspaceID]; ok {
		t.Stop()
		delete(gw.debounceTimers, workspaceID)
	}
	gw.debounceTimersMu.Unlock()

	fmt.Printf("[git-watcher] unwatched %s\n", workspaceID)
}

// eventLoop processes fsnotify events and errors.
func (gw *GitWatcher) eventLoop() {
	for {
		select {
		case event, ok := <-gw.watcher.Events:
			if !ok {
				return
			}
			gw.handleEvent(event)
		case err, ok := <-gw.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[git-watcher] error: %v\n", err)
		case <-gw.stopCh:
			return
		}
	}
}

// handleEvent maps an fsnotify event to workspace IDs and resets debounce timers.
func (gw *GitWatcher) handleEvent(event fsnotify.Event) {
	// On CREATE events for directories, add a watch (handles new refs subdirs)
	if event.Has(fsnotify.Create) {
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			gw.watchedPathsMu.Lock()
			// Find workspace IDs from the parent directory
			parentDir := filepath.Dir(event.Name)
			ids := gw.watchedPaths[parentDir]
			gw.watchedPathsMu.Unlock()

			for _, id := range ids {
				gw.addWatch(event.Name, id)
			}
		}
	}

	// Map the event path to workspace IDs
	workspaceIDs := gw.findWorkspaceIDs(event.Name)
	for _, id := range workspaceIDs {
		gw.resetDebounce(id)
	}
}

// findWorkspaceIDs returns workspace IDs associated with the given path.
// Checks the exact path and all parent directories.
func (gw *GitWatcher) findWorkspaceIDs(path string) []string {
	gw.watchedPathsMu.Lock()
	defer gw.watchedPathsMu.Unlock()

	// Check the exact path
	if ids, ok := gw.watchedPaths[path]; ok {
		return ids
	}

	// Check parent directories (event may be for a file inside a watched dir)
	dir := filepath.Dir(path)
	for dir != "/" && dir != "." {
		if ids, ok := gw.watchedPaths[dir]; ok {
			return ids
		}
		dir = filepath.Dir(dir)
	}

	return nil
}

// resetDebounce resets or creates a debounce timer for the workspace.
func (gw *GitWatcher) resetDebounce(workspaceID string) {
	debounce := gw.cfg.GitStatusWatchDebounce()

	gw.debounceTimersMu.Lock()
	defer gw.debounceTimersMu.Unlock()

	if t, ok := gw.debounceTimers[workspaceID]; ok {
		t.Reset(debounce)
		return
	}

	gw.debounceTimers[workspaceID] = time.AfterFunc(debounce, func() {
		gw.refreshWorkspace(workspaceID)
	})
}

// refreshWorkspace runs a git status update for the workspace and broadcasts
// only if the git status actually changed.
func (gw *GitWatcher) refreshWorkspace(workspaceID string) {
	if gw.onRefresh != nil {
		gw.onRefresh(workspaceID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gw.cfg.GitStatusTimeout())
	defer cancel()

	w, err := gw.mgr.UpdateGitStatus(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, ErrWorkspaceLocked) {
			return
		}
		fmt.Printf("[git-watcher] failed to update status for %s: %v\n", workspaceID, err)
		return
	}

	// Hash the git status fields
	newHash := fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v",
		w.GitDirty, w.GitAhead, w.GitBehind,
		w.GitLinesAdded, w.GitLinesRemoved, w.GitFilesChanged, w.Branch)

	// Check if changed
	gw.lastStatusHashMu.Lock()
	oldHash := gw.lastStatusHash[workspaceID]
	changed := oldHash != newHash
	if changed {
		gw.lastStatusHash[workspaceID] = newHash
	}
	gw.lastStatusHashMu.Unlock()

	if changed {
		if gw.broadcast != nil {
			gw.broadcast()
		}
	}
}

// addWatch adds a filesystem watch and maps the path to a workspace ID.
func (gw *GitWatcher) addWatch(path string, workspaceID string) {
	if _, err := os.Stat(path); err != nil {
		return // path doesn't exist, skip silently
	}

	gw.watchedPathsMu.Lock()
	ids := gw.watchedPaths[path]
	if !containsString(ids, workspaceID) {
		gw.watchedPaths[path] = append(ids, workspaceID)
	}
	needsAdd := len(gw.watchedPaths[path]) == 1 || len(ids) == 0
	gw.watchedPathsMu.Unlock()

	if needsAdd {
		if err := gw.watcher.Add(path); err != nil {
			fmt.Printf("[git-watcher] failed to watch %s: %v\n", path, err)
		}
	}
}

// watchRecursive watches a directory and all its subdirectories.
func (gw *GitWatcher) watchRecursive(dir string, workspaceID string) {
	if _, err := os.Stat(dir); err != nil {
		return // directory doesn't exist, skip
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			gw.addWatch(path, workspaceID)
		}
		return nil
	})
}

// resolveGitDir returns the actual .git directory for a workspace path.
// For regular clones, this is <path>/.git.
// For worktrees, .git is a file containing "gitdir: <path>", and we resolve that.
func resolveGitDir(workspacePath string) (string, error) {
	dotGit := filepath.Join(workspacePath, ".git")

	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", fmt.Errorf("no .git found: %w", err)
	}

	// Regular clone: .git is a directory
	if info.IsDir() {
		return dotGit, nil
	}

	// Worktree: .git is a file with "gitdir: <path>"
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return "", fmt.Errorf("failed to read .git file: %w", err)
	}

	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return "", fmt.Errorf("unexpected .git file content: %s", content)
	}

	gitDir := strings.TrimPrefix(content, "gitdir: ")

	// Resolve relative paths against the workspace directory
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(workspacePath, gitDir)
	}

	// Clean the path to resolve .. components
	gitDir = filepath.Clean(gitDir)

	if _, err := os.Stat(gitDir); err != nil {
		return "", fmt.Errorf("resolved gitdir does not exist: %s: %w", gitDir, err)
	}

	return gitDir, nil
}

// resolveSharedBaseRefs returns the shared base repo's refs/ directory
// for a worktree gitdir. Returns empty string if not a worktree or if
// the path can't be resolved.
//
// Worktree gitdirs look like: <base-repo>/worktrees/<name>/
// The shared refs are at: <base-repo>/refs/
func resolveSharedBaseRefs(gitDir string) string {
	// Check if this looks like a worktree gitdir
	// Pattern: .../worktrees/<name>
	dir := filepath.Dir(gitDir)
	if filepath.Base(dir) != "worktrees" {
		return ""
	}

	baseRepo := filepath.Dir(dir)
	refsDir := filepath.Join(baseRepo, "refs")
	if _, err := os.Stat(refsDir); err != nil {
		return ""
	}

	return refsDir
}

// containsString checks if a string slice contains a value.
func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// removeFromSlice removes a value from a string slice, returning the new slice.
func removeFromSlice(slice []string, val string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != val {
			result = append(result, s)
		}
	}
	return result
}
