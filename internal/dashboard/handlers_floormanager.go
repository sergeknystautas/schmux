package dashboard

import (
	"net/http"
)

type floorManagerStatusResponse struct {
	Enabled           bool   `json:"enabled"`
	TmuxSession       string `json:"tmux_session"`
	Running           bool   `json:"running"`
	InjectionCount    int    `json:"injection_count"`
	RotationThreshold int    `json:"rotation_threshold"`
}

func (s *Server) handleGetFloorManager(w http.ResponseWriter, r *http.Request) {
	resp := floorManagerStatusResponse{
		Enabled:           s.config.GetFloorManagerEnabled(),
		RotationThreshold: s.config.GetFloorManagerRotationThreshold(),
	}

	if s.floorManager != nil {
		resp.TmuxSession = s.floorManager.TmuxSession()
		resp.Running = s.floorManager.Running()
		resp.InjectionCount = s.floorManager.InjectionCount()
	}

	writeJSON(w, resp)
}

func (s *Server) handleEndShift(w http.ResponseWriter, r *http.Request) {
	if s.floorManager == nil {
		writeJSONError(w, "floor manager not active", http.StatusNotFound)
		return
	}

	s.floorManager.EndShift()
	writeJSON(w, map[string]string{"status": "ok"})
}
