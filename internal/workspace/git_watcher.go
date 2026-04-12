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

	"github.com/charmbracelet/log"
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
	logger    *log.Logger

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

	// suppressedPaths tracks short-lived path-prefix suppressions for fsnotify
	// events caused by schmux's own git commands. Keys are cleaned absolute-ish
	// path prefixes (e.g. a gitdir or shared refs dir).
	suppressedPaths   map[string]suppressionState
	suppressedPathsMu sync.Mutex

	// stopCh signals the event loop to exit.
	stopCh   chan struct{}
	stopOnce sync.Once
}

type suppressionState struct {
	active int
	until  time.Time
}

const internalGitEventSuppressGrace = 750 * time.Millisecond

// NewGitWatcher creates a new git watcher. Returns nil if watching is disabled
// in config.
func NewGitWatcher(cfg *config.Config, mgr *Manager, broadcast func(), logger *log.Logger) *GitWatcher {
	if !cfg.GetGitStatusWatchEnabled() {
		logger.Info("disabled by config")
		return nil
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error("failed to create watcher", "err", err)
		return nil
	}

	return &GitWatcher{
		watcher:         w,
		cfg:             cfg,
		mgr:             mgr,
		broadcast:       broadcast,
		logger:          logger,
		watchedPaths:    make(map[string][]string),
		debounceTimers:  make(map[string]*time.Timer),
		lastStatusHash:  make(map[string]string),
		suppressedPaths: make(map[string]suppressionState),
		stopCh:          make(chan struct{}),
	}
}

// Start launches the event loop goroutine.
func (gw *GitWatcher) Start() {
	go gw.eventLoop()
	gw.logger.Info("started")
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

		gw.logger.Info("stopped")
	})
}

// AddWorkspace adds filesystem watches for a workspace's git metadata.
func (gw *GitWatcher) AddWorkspace(workspaceID, workspacePath string) {
	gitDir, err := resolveGitDir(workspacePath)
	if err != nil {
		gw.logger.Warn("failed to resolve git dir", "workspace_id", workspaceID, "err", err)
		return
	}

	// Watch the gitdir itself (catches HEAD, index, packed-refs, FETCH_HEAD changes)
	gw.addWatch(gitDir, workspaceID)

	// Watch logs/ directory (captures HEAD reflog and related local branch movement).
	//
	// Intentionally do NOT watch refs/ (including shared worktree-base refs/):
	// ref updates are high-noise (especially fetch/remotes) and are handled by the
	// poller. The watcher is for fast local workspace feedback.
	logsDir := filepath.Join(gitDir, "logs")
	gw.watchRecursive(logsDir, workspaceID)

	gw.logger.Debug("watching", "workspace_id", workspaceID, "gitdir", gitDir)
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

	gw.logger.Info("unwatched", "workspace_id", workspaceID)
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
			gw.logger.Error("watcher error", "err", err)
		case <-gw.stopCh:
			return
		}
	}
}

// handleEvent maps an fsnotify event to workspace IDs and resets debounce timers.
func (gw *GitWatcher) handleEvent(event fsnotify.Event) {
	// On CREATE events for directories, add a watch (handles new logs/ subdirs, etc.)
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

	// Ignore short-lived git metadata churn caused by schmux's own git commands.
	// Keep this check after CREATE handling so we still add watches for new dirs.
	if gw.isSuppressedPath(event.Name) {
		return
	}

	// Map the event path to workspace IDs
	workspaceIDs := gw.findWorkspaceIDs(event.Name)
	for _, id := range workspaceIDs {
		gw.resetDebounce(id)
	}
}

// BeginInternalGitSuppressionForDir suppresses watcher refreshes for git metadata
// events under the git paths associated with dir while a schmux-run git command is
// in flight, plus a small grace period after completion. It returns a release func.
func (gw *GitWatcher) BeginInternalGitSuppressionForDir(dir string) func() {
	if gw == nil || dir == "" {
		return func() {}
	}

	paths := suppressionPathsForGitCommandDir(dir)
	if len(paths) == 0 {
		return func() {}
	}

	gw.suppressedPathsMu.Lock()
	for _, p := range paths {
		state := gw.suppressedPaths[p]
		state.active++
		gw.suppressedPaths[p] = state
	}
	gw.suppressedPathsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			now := time.Now()
			until := now.Add(internalGitEventSuppressGrace)

			gw.suppressedPathsMu.Lock()
			defer gw.suppressedPathsMu.Unlock()

			for _, p := range paths {
				state, ok := gw.suppressedPaths[p]
				if !ok {
					continue
				}
				if state.active > 0 {
					state.active--
				}
				if state.until.Before(until) {
					state.until = until
				}
				if state.active == 0 && !now.Before(state.until) {
					delete(gw.suppressedPaths, p)
				} else {
					gw.suppressedPaths[p] = state
				}
			}
		})
	}
}

func (gw *GitWatcher) isSuppressedPath(path string) bool {
	if gw == nil || path == "" {
		return false
	}
	cleanPath := filepath.Clean(path)
	now := time.Now()

	gw.suppressedPathsMu.Lock()
	defer gw.suppressedPathsMu.Unlock()

	suppressed := false
	for prefix, state := range gw.suppressedPaths {
		// Opportunistic cleanup of expired entries.
		if state.active == 0 && !now.Before(state.until) {
			delete(gw.suppressedPaths, prefix)
			continue
		}
		if state.active > 0 || now.Before(state.until) {
			if pathHasPrefix(cleanPath, prefix) {
				suppressed = true
			}
		}
	}
	return suppressed
}

func pathHasPrefix(path, prefix string) bool {
	path = filepath.Clean(path)
	prefix = filepath.Clean(prefix)
	if path == prefix {
		return true
	}
	if prefix == "." || prefix == string(filepath.Separator) {
		return strings.HasPrefix(path, prefix)
	}
	return strings.HasPrefix(path, prefix+string(filepath.Separator))
}

func suppressionPathsForGitCommandDir(dir string) []string {
	if dir == "" {
		return nil
	}

	seen := make(map[string]struct{})
	add := func(out *[]string, p string) {
		if p == "" {
			return
		}
		cp := filepath.Clean(p)
		if _, ok := seen[cp]; ok {
			return
		}
		seen[cp] = struct{}{}
		*out = append(*out, cp)
	}

	var out []string

	// Workspace path (regular clone or worktree path)
	if gitDir, err := resolveGitDir(dir); err == nil {
		add(&out, gitDir)
		add(&out, resolveSharedBaseRefs(gitDir))
		return out
	}

	// Bare repo / git metadata dir path (e.g. shared worktree base or query repo)
	if looksLikeGitMetadataDir(dir) {
		add(&out, dir)
	}

	return out
}

func looksLikeGitMetadataDir(dir string) bool {
	if dir == "" {
		return false
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return false
	}
	// Heuristic: git metadata dirs (bare repos or .git dirs) have HEAD and refs/ (or objects/)
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "refs")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "objects")); err == nil {
		return true
	}
	return false
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
	// Skip if watcher has been stopped (timer may fire after Stop)
	select {
	case <-gw.stopCh:
		return
	default:
	}

	if gw.onRefresh != nil {
		gw.onRefresh(workspaceID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gw.cfg.GitStatusTimeout())
	defer cancel()

	w, err := gw.mgr.updateGitStatusWithTrigger(ctx, workspaceID, RefreshTriggerWatcher)
	if err != nil {
		if errors.Is(err, ErrWorkspaceLocked) {
			return
		}
		gw.logger.Warn("failed to update status", "workspace_id", workspaceID, "err", err)
		return
	}

	// Hash the git status fields
	newHash := fmt.Sprintf("%v|%v|%v|%v|%v|%v|%v|%v|%v|%v|%v",
		w.Dirty, w.Ahead, w.Behind,
		w.LinesAdded, w.LinesRemoved, w.FilesChanged, w.Branch,
		w.CommitsSyncedWithRemote, w.RemoteBranchExists, w.LocalUniqueCommits, w.RemoteUniqueCommits)

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
			gw.logger.Warn("failed to watch", "path", path, "err", err)
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
