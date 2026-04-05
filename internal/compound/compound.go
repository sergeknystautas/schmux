package compound

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// PropagateFunc is called to propagate an overlay change to sibling workspaces.
type PropagateFunc func(sourceWorkspaceID, repoURL, relPath string, content []byte)

// ManifestUpdateFunc is called to persist an updated manifest hash to state.
type ManifestUpdateFunc func(workspaceID, relPath, hash string)

// workspaceInfo holds metadata about a watched workspace.
type workspaceInfo struct {
	Path       string
	OverlayDir string
	RepoURL    string
	Manifest   map[string]string // relPath → hash at copy time
}

// Compounder orchestrates the overlay compounding loop:
// watch → merge → propagate.
type Compounder struct {
	watcher        *Watcher
	executor       LLMExecutor
	propagate      PropagateFunc
	manifestUpdate ManifestUpdateFunc
	logger         *log.Logger

	workspaces       map[string]*workspaceInfo     // workspaceID → info
	reconcileCancels map[string]context.CancelFunc // workspaceID → cancel func for background reconcile
	mu               sync.RWMutex
}

// NewCompounder creates a new Compounder.
func NewCompounder(debounceMs int, suppressionTTL time.Duration, executor LLMExecutor, propagate PropagateFunc, manifestUpdate ManifestUpdateFunc, logger *log.Logger) (*Compounder, error) {
	c := &Compounder{
		executor:         executor,
		propagate:        propagate,
		manifestUpdate:   manifestUpdate,
		logger:           logger,
		workspaces:       make(map[string]*workspaceInfo),
		reconcileCancels: make(map[string]context.CancelFunc),
	}

	watcher, err := NewWatcher(debounceMs, suppressionTTL, c.onFileChange, logger)
	if err != nil {
		return nil, err
	}
	c.watcher = watcher

	return c, nil
}

// AddWorkspace registers a workspace for overlay compounding.
func (c *Compounder) AddWorkspace(workspaceID, workspacePath, overlayDir, repoURL string, manifest map[string]string, declaredPaths []string) {
	// Defensive copy to prevent shared reference bugs
	manifestCopy := make(map[string]string, len(manifest))
	for k, v := range manifest {
		manifestCopy[k] = v
	}

	c.mu.Lock()
	c.workspaces[workspaceID] = &workspaceInfo{
		Path:       workspacePath,
		OverlayDir: overlayDir,
		RepoURL:    repoURL,
		Manifest:   manifestCopy,
	}
	c.mu.Unlock()

	if err := c.watcher.AddWorkspaceWithDeclaredPaths(workspaceID, workspacePath, manifestCopy, declaredPaths); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to add workspace watch", "err", err)
		}
	}
}

// RemoveWorkspace stops watching a workspace. This is the hard safety gate:
// after it returns, no watches, debounce timers, or reconcile cancels exist
// for this workspace. Idempotent — safe to call on an already-removed workspace.
func (c *Compounder) RemoveWorkspace(workspaceID string) {
	c.watcher.RemoveWorkspace(workspaceID)
	c.mu.Lock()
	delete(c.workspaces, workspaceID)
	if cancel, ok := c.reconcileCancels[workspaceID]; ok {
		cancel()
		delete(c.reconcileCancels, workspaceID)
	}
	c.mu.Unlock()
}

// SetReconcileCancel stores a cancel function for a workspace's background reconcile goroutine.
// Calling cancel() is idempotent per the Go spec — safe to call from both CancelReconcile
// and the goroutine's defer.
func (c *Compounder) SetReconcileCancel(workspaceID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconcileCancels[workspaceID] = cancel
}

// CancelReconcile cancels any in-flight background reconcile for the workspace and removes
// the cancel func. This is best-effort — with few overlay files, the reconcile may complete
// before the cancellation is observed. The real safety guarantee is RemoveWorkspace, which
// unconditionally stops all watches and removes the workspace from the map.
func (c *Compounder) CancelReconcile(workspaceID string) {
	c.mu.Lock()
	cancel := c.reconcileCancels[workspaceID]
	delete(c.reconcileCancels, workspaceID)
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Start begins the file watching event loop.
func (c *Compounder) Start() {
	c.watcher.Start()
}

// Stop shuts down the compounder.
func (c *Compounder) Stop() {
	c.watcher.Stop()
}

// Suppress temporarily prevents a file change from triggering the compounding loop.
func (c *Compounder) Suppress(workspaceID, relPath string) {
	c.watcher.Suppress(workspaceID, relPath)
}

// Reconcile checks all overlay-managed files in a workspace and syncs any that have changed.
// The context allows callers to set a timeout for the reconciliation pass.
func (c *Compounder) Reconcile(ctx context.Context, workspaceID string) {
	c.mu.RLock()
	info, ok := c.workspaces[workspaceID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	// Copy manifest keys to avoid holding lock during file I/O
	relPaths := make([]string, 0, len(info.Manifest))
	for relPath := range info.Manifest {
		relPaths = append(relPaths, relPath)
	}
	c.mu.RUnlock()

	for _, relPath := range relPaths {
		if ctx.Err() != nil {
			if c.logger != nil {
				c.logger.Warn("reconciliation cancelled", "workspace_id", workspaceID, "err", ctx.Err())
			}
			return
		}
		c.processFileChange(ctx, workspaceID, relPath)
	}
}

// onFileChange is the callback from the watcher when an overlay-managed file changes.
func (c *Compounder) onFileChange(workspaceID, relPath string) {
	c.processFileChange(context.Background(), workspaceID, relPath)
}

// processFileChange handles a file change with a context for cancellation/timeout support.
func (c *Compounder) processFileChange(ctx context.Context, workspaceID, relPath string) {
	// Early exit if context is cancelled (e.g., workspace being disposed).
	// This provides finer-grained cancellation than the between-files check in Reconcile.
	if ctx.Err() != nil {
		return
	}

	// Validate relPath to prevent path traversal
	if err := ValidateRelPath(relPath); err != nil {
		if c.logger != nil {
			c.logger.Error("rejecting unsafe relPath", "path", relPath, "err", err)
		}
		return
	}

	c.mu.RLock()
	info, ok := c.workspaces[workspaceID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	manifestHash := info.Manifest[relPath]
	wsPath := filepath.Join(info.Path, relPath)
	overlayPath := filepath.Join(info.OverlayDir, relPath)
	repoURL := info.RepoURL
	c.mu.RUnlock()

	// Determine merge action
	action, err := DetermineMergeAction(wsPath, overlayPath, manifestHash)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("failed to determine merge action", "path", relPath, "workspace_id", workspaceID, "err", err)
		}
		return
	}

	if action == MergeActionSkip {
		return
	}

	if c.logger != nil {
		c.logger.Info("syncing overlay", "path", relPath, "workspace_id", workspaceID, "action", action)
	}

	// Ensure overlay parent directory exists
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0755); err != nil {
		if c.logger != nil {
			c.logger.Error("failed to create overlay directory", "err", err)
		}
		return
	}

	// Execute merge
	mergedContent, err := ExecuteMerge(ctx, action, wsPath, overlayPath, c.executor)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("merge failed", "path", relPath, "workspace_id", workspaceID, "err", err)
		}
		return
	}

	// Update the manifest hash for this workspace
	newHash := HashBytes(mergedContent)
	c.mu.Lock()
	if info, ok := c.workspaces[workspaceID]; ok {
		info.Manifest[relPath] = newHash
	}
	c.mu.Unlock()

	// Persist the updated hash to state
	if c.manifestUpdate != nil {
		c.manifestUpdate(workspaceID, relPath, newHash)
	}

	// Propagate to sibling workspaces
	if c.propagate != nil && mergedContent != nil {
		if c.logger != nil {
			c.logger.Info("propagating to siblings", "path", relPath, "workspace_id", workspaceID)
		}
		c.propagate(workspaceID, repoURL, relPath, mergedContent)
	}
}
