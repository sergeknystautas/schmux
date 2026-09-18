package dashboard

import (
	"net/http"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// handleUsageGet serves persisted plan quota snapshots. Live records update
// the store; this handler is read-only.
func (s *Server) handleUsageGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, contracts.UsageSnapshotResponse{Providers: s.usageManager.Snapshot()})
}
