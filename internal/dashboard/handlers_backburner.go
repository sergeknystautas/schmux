package dashboard

import (
	"encoding/json"
	"net/http"
)

// handleBackburnerWorkspace toggles the backburner state of a workspace.
func (s *Server) handleBackburnerWorkspace(w http.ResponseWriter, r *http.Request) {
	if !s.config.GetBackburnerEnabled() {
		writeJSONError(w, "backburner feature is not enabled", http.StatusNotFound)
		return
	}

	ws, ok := s.requireWorkspace(w, r)
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
	if err := s.state.UpdateWorkspace(ws); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.BroadcastSessions()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
