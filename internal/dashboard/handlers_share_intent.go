package dashboard

import (
	"encoding/json"
	"net/http"
)

// handleShareIntent toggles the intent sharing state of a workspace.
func (h *WorkspaceHandlers) handleShareIntent(w http.ResponseWriter, r *http.Request) {
	if !h.config.GetRepofeedEnabled() {
		writeJSONError(w, "repofeed feature is not enabled", http.StatusNotFound)
		return
	}

	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Share bool `json:"share"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ws.IntentShared = req.Share
	if err := h.state.UpdateWorkspace(ws); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.broadcastSessions()

	// Trigger immediate repofeed publish so the change appears right away
	if h.triggerRepofeedPublish != nil {
		h.triggerRepofeedPublish()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
