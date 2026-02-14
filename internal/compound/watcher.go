package compound

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// OnChangeFunc is called when an overlay-managed file changes in a workspace.
type OnChangeFunc func(workspaceID, relPath string)

// Watcher watches overlay-managed files in workspaces for changes.
type Watcher struct {
	watcher    *fsnotify.Watcher
	debounceMs int
	onChange   OnChangeFunc

	// workspaceID → workspacePath
	workspacePaths map[string]string
	// workspaceID → (relPath → true)
	workspaceFiles map[string]map[string]bool
	// absolute dir path → []workspaceID
	watchedDirs map[string][]string
	// debounce key (workspaceID:relPath) → timer
	debounceTimers map[string]*time.Timer
	// suppress key (workspaceID:relPath) → expiry
	suppressed map[string]time.Time
	// workspace root path → []absolute dir paths that couldn't be watched yet
	pendingDirs map[string][]string

	mu       sync.Mutex
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewWatcher creates a new file watcher for overlay-managed files.
func NewWatcher(debounceMs int, onChange OnChangeFunc) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		watcher:        fsw,
		debounceMs:     debounceMs,
		onChange:       onChange,
		workspacePaths: make(map[string]string),
		workspaceFiles: make(map[string]map[string]bool),
		watchedDirs:    make(map[string][]string),
		debounceTimers: make(map[string]*time.Timer),
		suppressed:     make(map[string]time.Time),
		pendingDirs:    make(map[string][]string),
		stopCh:         make(chan struct{}),
	}, nil
}

// AddWorkspace registers a workspace's overlay-managed files for watching.
func (w *Watcher) AddWorkspace(workspaceID, workspacePath string, manifest map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.workspacePaths[workspaceID] = workspacePath
	w.workspaceFiles[workspaceID] = make(map[string]bool)

	dirsToWatch := make(map[string]bool)
	for relPath := range manifest {
		w.workspaceFiles[workspaceID][relPath] = true
		absDir := filepath.Join(workspacePath, filepath.Dir(relPath))
		dirsToWatch[absDir] = true
	}

	for dir := range dirsToWatch {
		if err := w.watcher.Add(dir); err != nil {
			fmt.Printf("[compound] warning: failed to watch directory %s: %v\n", dir, err)
			continue
		}
		w.watchedDirs[dir] = append(w.watchedDirs[dir], workspaceID)
	}

	return nil
}

// AddWorkspaceWithDeclaredPaths registers a workspace's overlay-managed files for watching,
// including declared paths that may not exist yet (e.g., agent-generated config files).
// When a directory for a declared path doesn't exist, it is tracked as pending and the
// workspace root is watched instead to detect directory creation.
func (w *Watcher) AddWorkspaceWithDeclaredPaths(workspaceID, workspacePath string, manifest map[string]string, declaredPaths []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.workspacePaths[workspaceID] = workspacePath
	w.workspaceFiles[workspaceID] = make(map[string]bool)

	dirsToWatch := make(map[string]bool)

	// Register manifest files (existing)
	for relPath := range manifest {
		w.workspaceFiles[workspaceID][relPath] = true
		absDir := filepath.Join(workspacePath, filepath.Dir(relPath))
		dirsToWatch[absDir] = true
	}

	// Register declared paths (may not exist yet)
	for _, relPath := range declaredPaths {
		w.workspaceFiles[workspaceID][relPath] = true
		absDir := filepath.Join(workspacePath, filepath.Dir(relPath))
		dirsToWatch[absDir] = true
	}

	for dir := range dirsToWatch {
		if err := w.watcher.Add(dir); err != nil {
			// Directory doesn't exist yet — track as pending
			w.pendingDirs[workspacePath] = append(w.pendingDirs[workspacePath], dir)
			// Watch workspace root to detect new directory creation
			w.watcher.Add(workspacePath)
			continue
		}
		w.watchedDirs[dir] = append(w.watchedDirs[dir], workspaceID)
	}

	return nil
}

// RemoveWorkspace stops watching a workspace's files.
func (w *Watcher) RemoveWorkspace(workspaceID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	wsPath := w.workspacePaths[workspaceID]
	files := w.workspaceFiles[workspaceID]

	dirsToCheck := make(map[string]bool)
	for relPath := range files {
		absDir := filepath.Join(wsPath, filepath.Dir(relPath))
		dirsToCheck[absDir] = true
	}

	for dir := range dirsToCheck {
		ids := w.watchedDirs[dir]
		var remaining []string
		for _, id := range ids {
			if id != workspaceID {
				remaining = append(remaining, id)
			}
		}
		if len(remaining) == 0 {
			w.watcher.Remove(dir)
			delete(w.watchedDirs, dir)
		} else {
			w.watchedDirs[dir] = remaining
		}
	}

	// Cancel debounce timers for this workspace
	prefix := workspaceID + ":"
	for key, timer := range w.debounceTimers {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			timer.Stop()
			delete(w.debounceTimers, key)
		}
	}

	// Clean up suppression entries for this workspace
	for key := range w.suppressed {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(w.suppressed, key)
		}
	}

	// Clean up pending dirs for this workspace
	if wsPath != "" {
		delete(w.pendingDirs, wsPath)
	}

	delete(w.workspacePaths, workspaceID)
	delete(w.workspaceFiles, workspaceID)
}

// Suppress temporarily prevents a file from triggering the onChange callback.
func (w *Watcher) Suppress(workspaceID, relPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := workspaceID + ":" + relPath
	w.suppressed[key] = time.Now().Add(5 * time.Second)
}

// Start begins the event processing loop.
func (w *Watcher) Start() {
	go w.eventLoop()
}

// Stop shuts down the watcher.
func (w *Watcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
		w.watcher.Close()
		w.mu.Lock()
		for _, timer := range w.debounceTimers {
			timer.Stop()
		}
		w.mu.Unlock()
	})
}

func (w *Watcher) eventLoop() {
	for {
		select {
		case <-w.stopCh:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Create != 0 {
				// Check if a directory was created — may resolve pending watches
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					w.retryPendingDirs(event.Name)
				}
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				w.handleEvent(event)
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			fmt.Printf("[compound] watcher error: %v\n", err)
		}
	}
}

func (w *Watcher) handleEvent(event fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()

	absPath := event.Name
	dir := filepath.Dir(absPath)

	workspaceIDs, ok := w.watchedDirs[dir]
	if !ok {
		return
	}

	for _, wsID := range workspaceIDs {
		wsPath := w.workspacePaths[wsID]
		files := w.workspaceFiles[wsID]

		relPath, err := filepath.Rel(wsPath, absPath)
		if err != nil {
			continue
		}

		if !files[relPath] {
			continue
		}

		// Check suppression
		key := wsID + ":" + relPath
		if expiry, suppressed := w.suppressed[key]; suppressed {
			if time.Now().Before(expiry) {
				continue
			}
			delete(w.suppressed, key)
		}

		w.resetDebounce(wsID, relPath)
	}
}

// retryPendingDirs attempts to watch directories that were previously pending
// because they didn't exist when AddWorkspaceWithDeclaredPaths was called.
// After successfully watching a directory, it scans for files that were created
// before the watch was established (closing the race between mkdir and file write).
func (w *Watcher) retryPendingDirs(createdDir string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for wsRoot, pendingDirs := range w.pendingDirs {
		var remaining []string
		for _, dir := range pendingDirs {
			if dir == createdDir || filepath.Dir(dir) == createdDir {
				if err := w.watcher.Add(dir); err != nil {
					remaining = append(remaining, dir)
					continue
				}
				// Find workspace IDs for this root and scan for existing files
				for wsID, wsPath := range w.workspacePaths {
					if wsPath == wsRoot {
						w.watchedDirs[dir] = append(w.watchedDirs[dir], wsID)
						// Scan for files that were created before the watch was established
						w.scanDirForExistingFiles(wsID, wsPath, dir)
					}
				}
			} else {
				remaining = append(remaining, dir)
			}
		}
		if len(remaining) == 0 {
			delete(w.pendingDirs, wsRoot)
		} else {
			w.pendingDirs[wsRoot] = remaining
		}
	}
}

// scanDirForExistingFiles checks a newly-watched directory for files that already exist
// and match the workspace's overlay file set. This closes the race between directory
// creation and watch establishment — files written before the watch starts would otherwise
// be missed.
// Must be called with w.mu held.
func (w *Watcher) scanDirForExistingFiles(workspaceID, workspacePath, dir string) {
	files := w.workspaceFiles[workspaceID]
	if files == nil {
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		absPath := filepath.Join(dir, entry.Name())
		relPath, err := filepath.Rel(workspacePath, absPath)
		if err != nil {
			continue
		}
		if files[relPath] {
			w.resetDebounce(workspaceID, relPath)
		}
	}
}

func (w *Watcher) resetDebounce(workspaceID, relPath string) {
	key := workspaceID + ":" + relPath
	if timer, ok := w.debounceTimers[key]; ok {
		timer.Stop()
	}
	w.debounceTimers[key] = time.AfterFunc(time.Duration(w.debounceMs)*time.Millisecond, func() {
		w.onChange(workspaceID, relPath)
	})
}
