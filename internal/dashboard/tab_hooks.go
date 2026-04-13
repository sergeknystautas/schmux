package dashboard

import (
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// resolveConflictCloseHook implements workspace.TabCloseHook for resolve-conflict tabs.
type resolveConflictCloseHook struct {
	server *Server
}

func (h *resolveConflictCloseHook) CanClose(wsID string, tab state.Tab) (bool, error) {
	hash := tab.Meta["hash"]
	if hash == "" {
		return tab.Closable, nil
	}
	if crState := h.server.getLinearSyncResolveConflictState(wsID); crState != nil {
		snapshot := crState.Snapshot()
		if snapshot.Hash == hash {
			return snapshot.Status != "in_progress", nil
		}
	}
	return true, nil
}

func (h *resolveConflictCloseHook) OnTabClose(wsID string, tab state.Tab) error {
	hash := tab.Meta["hash"]
	if hash == "" {
		return nil
	}
	if err := h.server.state.RemoveResolveConflict(wsID, hash); err != nil {
		return err
	}
	if current := h.server.getLinearSyncResolveConflictState(wsID); current != nil &&
		current.Hash == hash &&
		current.Status != "in_progress" {
		h.server.deleteLinearSyncResolveConflictState(wsID)
	}
	return nil
}

// RegisterTabCloseHooks registers all tab close hooks with the workspace manager.
func (s *Server) RegisterTabCloseHooks(wm workspace.WorkspaceManager, previewMgr PreviewDeleter) {
	wm.RegisterTabCloseHook("resolve-conflict", &resolveConflictCloseHook{server: s})
	if previewMgr != nil {
		wm.RegisterTabCloseHook("preview", &previewCloseHook{previewManager: previewMgr})
	}
}

// PreviewDeleter is an interface for deleting previews without touching tabs.
type PreviewDeleter interface {
	DeletePreviewOnly(workspaceID, previewID string) error
}

// previewCloseHook implements workspace.TabCloseHook for preview tabs.
type previewCloseHook struct {
	previewManager PreviewDeleter
}

func (h *previewCloseHook) CanClose(_ string, tab state.Tab) (bool, error) {
	return tab.Closable, nil
}

func (h *previewCloseHook) OnTabClose(wsID string, tab state.Tab) error {
	previewID := tab.Meta["preview_id"]
	if previewID == "" {
		return nil
	}
	return h.previewManager.DeletePreviewOnly(wsID, previewID)
}
