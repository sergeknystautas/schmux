package compound

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PropagateFunc is called to propagate an overlay change to sibling workspaces.
type PropagateFunc func(sourceWorkspaceID, repoURL, relPath string, content []byte)

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
	watcher   *Watcher
	executor  LLMExecutor
	propagate PropagateFunc

	workspaces map[string]*workspaceInfo // workspaceID → info
	mu         sync.RWMutex
}

// NewCompounder creates a new Compounder.
func NewCompounder(debounceMs int, executor LLMExecutor, propagate PropagateFunc) (*Compounder, error) {
	c := &Compounder{
		executor:   executor,
		propagate:  propagate,
		workspaces: make(map[string]*workspaceInfo),
	}

	watcher, err := NewWatcher(debounceMs, c.onFileChange)
	if err != nil {
		return nil, err
	}
	c.watcher = watcher

	return c, nil
}

// AddWorkspace registers a workspace for overlay compounding.
func (c *Compounder) AddWorkspace(workspaceID, workspacePath, overlayDir, repoURL string, manifest map[string]string) {
	c.mu.Lock()
	c.workspaces[workspaceID] = &workspaceInfo{
		Path:       workspacePath,
		OverlayDir: overlayDir,
		RepoURL:    repoURL,
		Manifest:   manifest,
	}
	c.mu.Unlock()

	if err := c.watcher.AddWorkspace(workspaceID, workspacePath, manifest); err != nil {
		fmt.Printf("[compound] warning: failed to add workspace watch: %v\n", err)
	}
}

// RemoveWorkspace stops watching a workspace.
func (c *Compounder) RemoveWorkspace(workspaceID string) {
	c.watcher.RemoveWorkspace(workspaceID)
	c.mu.Lock()
	delete(c.workspaces, workspaceID)
	c.mu.Unlock()
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
func (c *Compounder) Reconcile(workspaceID string) {
	c.mu.RLock()
	info, ok := c.workspaces[workspaceID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	manifest := info.Manifest
	c.mu.RUnlock()

	for relPath := range manifest {
		c.onFileChange(workspaceID, relPath)
	}
}

// onFileChange is the callback from the watcher when an overlay-managed file changes.
func (c *Compounder) onFileChange(workspaceID, relPath string) {
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
		fmt.Printf("[compound] failed to determine merge action for %s in %s: %v\n", relPath, workspaceID, err)
		return
	}

	if action == MergeActionSkip {
		return
	}

	fmt.Printf("[compound] syncing %s from %s (action=%d)\n", relPath, workspaceID, action)

	// Ensure overlay parent directory exists
	if err := os.MkdirAll(filepath.Dir(overlayPath), 0755); err != nil {
		fmt.Printf("[compound] failed to create overlay directory: %v\n", err)
		return
	}

	// Execute merge
	mergedContent, err := ExecuteMerge(context.Background(), action, wsPath, overlayPath, c.executor)
	if err != nil {
		fmt.Printf("[compound] merge failed for %s in %s: %v\n", relPath, workspaceID, err)
		return
	}

	// Update the manifest hash for this workspace
	newHash := HashBytes(mergedContent)
	c.mu.Lock()
	if info, ok := c.workspaces[workspaceID]; ok {
		info.Manifest[relPath] = newHash
	}
	c.mu.Unlock()

	// Propagate to sibling workspaces
	if c.propagate != nil && mergedContent != nil {
		c.propagate(workspaceID, repoURL, relPath, mergedContent)
	}
}
