package dashboard

import (
	"encoding/json"
	"net/http"
)

// handleBackburnerWorkspace toggles the backburner state of a workspace.
func (h *WorkspaceHandlers) handleBackburnerWorkspace(w http.ResponseWriter, r *http.Request) {
	if !h.config.GetBackburnerEnabled() {
		writeJSONError(w, "backburner feature is not enabled", http.StatusNotFound)
		return
	}

	ws, ok := h.requireWorkspace(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Backburner bool `json:"backburner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ws.Backburner = req.Backburner
	if req.Backburner && ws.IntentShared {
		ws.IntentShared = false
	}
	if err := h.state.UpdateWorkspace(ws); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.broadcastSessions()

	if req.Backburner && h.triggerRepofeedPublish != nil {
		h.triggerRepofeedPublish()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
