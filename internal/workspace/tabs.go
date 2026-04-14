package workspace

import (
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TabCloseHook handles kind-specific cleanup when a tab is closed.
type TabCloseHook interface {
	// CanClose returns whether the tab can be closed right now.
	CanClose(wsID string, tab state.Tab) (bool, error)
	// OnTabClose performs cleanup after the tab is removed from state.
	OnTabClose(wsID string, tab state.Tab) error
}

// RegisterTabCloseHook registers a close hook for a tab kind.
func (m *Manager) RegisterTabCloseHook(kind string, hook TabCloseHook) {
	if m.tabCloseHooks == nil {
		m.tabCloseHooks = make(map[string]TabCloseHook)
	}
	m.tabCloseHooks[kind] = hook
}

// AddWorkspaceWithTabs creates a workspace in state and seeds its system tabs.
// All workspace creation — internal and external — flows through this method.
func (m *Manager) AddWorkspaceWithTabs(ws state.Workspace) error {
	return m.mutateTabsAndSave(func() error {
		if err := m.state.AddWorkspace(ws); err != nil {
			return err
		}
		return m.seedSystemTabsLocked(ws.ID, ws.VCS)
	})
}

// isVCSCapable returns true if the VCS type supports diff/git tabs.
func isVCSCapable(vcs string) bool {
	switch vcs {
	case "", "git", "git-worktree", "git-clone", "sapling":
		return true
	default:
		return false
	}
}

// shortHash returns the first 7 characters of a hash, or the full hash if shorter.
func tabShortHash(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

// SeedSystemTabs creates non-closable system tabs (diff, git) for a workspace.
// Only seeds for VCS-capable workspaces. Safe to call multiple times — dedup prevents duplicates.
func (m *Manager) SeedSystemTabs(wsID string) error {
	ws, ok := m.state.GetWorkspace(wsID)
	if !ok {
		return fmt.Errorf("workspace not found: %s", wsID)
	}
	return m.mutateTabsAndSave(func() error {
		return m.seedSystemTabsLocked(wsID, ws.VCS)
	})
}

// seedSystemTabsLocked adds diff+git tabs for VCS-capable workspaces.
// Caller must be inside mutateTabsAndSave (or hold equivalent guarantees).
func (m *Manager) seedSystemTabsLocked(wsID, vcs string) error {
	if !isVCSCapable(vcs) {
		return nil
	}
	now := time.Now()
	// Reverse order: AddTab prepends, so git first → diff second → [diff, git]
	if err := m.state.AddTab(wsID, state.Tab{
		ID: "sys-git-" + wsID, Kind: "git", Label: "commit graph",
		Route: "/commits/" + wsID, Closable: false, CreatedAt: now,
	}); err != nil {
		return err
	}
	return m.state.AddTab(wsID, state.Tab{
		ID: "sys-diff-" + wsID, Kind: "diff",
		Route: "/diff/" + wsID, Closable: false, CreatedAt: now,
	})
}

// OpenCommitTab opens a commit detail tab for the given hash.
func (m *Manager) OpenCommitTab(wsID, hash string) (*state.Tab, error) {
	short := tabShortHash(hash)
	tab := state.Tab{
		ID:        uuid.NewString(),
		Kind:      "commit",
		Label:     "commit " + short,
		Route:     "/commits/" + wsID + "/" + short,
		Closable:  true,
		Meta:      map[string]string{"hash": hash},
		CreatedAt: time.Now(),
	}
	err := m.mutateTabsAndSave(func() error {
		return m.state.AddTab(wsID, tab)
	})
	if err != nil {
		return nil, err
	}
	return &tab, nil
}

// OpenMarkdownTab opens a markdown preview tab for the given filepath.
func (m *Manager) OpenMarkdownTab(wsID, path string) (*state.Tab, error) {
	label := filepath.Base(path)
	tab := state.Tab{
		ID:        uuid.NewString(),
		Kind:      "markdown",
		Label:     label,
		Route:     "/diff/" + wsID + "/md/" + url.PathEscape(path),
		Closable:  true,
		Meta:      map[string]string{"filepath": path},
		CreatedAt: time.Now(),
	}
	err := m.mutateTabsAndSave(func() error {
		return m.state.AddTab(wsID, tab)
	})
	if err != nil {
		return nil, err
	}
	return &tab, nil
}

// OpenPreviewTab opens a web preview tab.
func (m *Manager) OpenPreviewTab(wsID, previewID string, port int) (*state.Tab, error) {
	tab := state.Tab{
		ID:        "sys-preview-" + previewID,
		Kind:      "preview",
		Label:     fmt.Sprintf("web:%d", port),
		Route:     fmt.Sprintf("/preview/%s/%s", wsID, previewID),
		Closable:  true,
		Meta:      map[string]string{"preview_id": previewID},
		CreatedAt: time.Now(),
	}
	err := m.mutateTabsAndSave(func() error {
		return m.state.AddTab(wsID, tab)
	})
	if err != nil {
		return nil, err
	}
	return &tab, nil
}

// OpenResolveConflictTab opens a resolve-conflict tab for the given hash.
func (m *Manager) OpenResolveConflictTab(wsID, hash string) (*state.Tab, error) {
	short := tabShortHash(hash)
	id := "sys-resolve-conflict-" + short
	tab := state.Tab{
		ID:        id,
		Kind:      "resolve-conflict",
		Route:     fmt.Sprintf("/resolve-conflict/%s/%s", wsID, id),
		Closable:  true,
		Meta:      map[string]string{"hash": short},
		CreatedAt: time.Now(),
	}
	err := m.mutateTabsAndSave(func() error {
		return m.state.AddTab(wsID, tab)
	})
	if err != nil {
		return nil, err
	}
	return &tab, nil
}

// CloseTab closes a tab by ID, running any registered close hook.
func (m *Manager) CloseTab(wsID, tabID string) error {
	tabs := m.state.GetWorkspaceTabs(wsID)
	var found *state.Tab
	for i := range tabs {
		if tabs[i].ID == tabID {
			found = &tabs[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("tab not found: %s", tabID)
	}

	// Check closability
	if hook, ok := m.tabCloseHooks[found.Kind]; ok {
		canClose, err := hook.CanClose(wsID, *found)
		if err != nil {
			return fmt.Errorf("close check failed: %w", err)
		}
		if !canClose {
			return fmt.Errorf("tab is not closable")
		}
	} else if !found.Closable {
		return fmt.Errorf("tab is not closable")
	}

	tab := *found
	return m.mutateTabsAndSave(func() error {
		// Remove tab from state
		if err := m.state.RemoveTab(wsID, tabID); err != nil {
			return err
		}
		// Run hook cleanup
		if hook, ok := m.tabCloseHooks[tab.Kind]; ok {
			if err := hook.OnTabClose(wsID, tab); err != nil {
				// Roll back: re-add the tab
				_ = m.state.AddTab(wsID, tab)
				return fmt.Errorf("close hook failed: %w", err)
			}
		}
		return nil
	})
}
