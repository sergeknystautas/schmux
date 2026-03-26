package dashboard

import (
	"time"

	"github.com/sergeknystautas/schmux/internal/preview"
)

func (s *Server) previewReconcileLoop() {
	if s.previewManager == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cache := preview.BuildPortOwnerCache()
			changed := false
			for _, ws := range s.state.GetWorkspaces() {
				if ws.RemoteHostID != "" {
					continue
				}
				updated, err := s.previewManager.ReconcileWorkspaceWithCache(ws.ID, cache)
				if err != nil {
					continue
				}
				if updated {
					changed = true
				}
			}
			if changed {
				go s.BroadcastSessions()
			}
		case <-s.shutdownCtx.Done():
			return
		}
	}
}
