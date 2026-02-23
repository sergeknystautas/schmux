package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/bus"
)

// floorManagerStatusResponse is the response for GET /api/floor-manager.
type floorManagerStatusResponse struct {
	Enabled           bool    `json:"enabled"`
	SessionID         *string `json:"session_id"`
	Running           bool    `json:"running"`
	InjectionCount    int     `json:"injection_count"`
	RotationThreshold int     `json:"rotation_threshold"`
}

// handleFloorManager returns floor manager status.
func (s *Server) handleFloorManager(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := floorManagerStatusResponse{
		Enabled: s.config.GetFloorManagerEnabled(),
	}

	if sess, found := s.state.GetFloorManagerSession(); found {
		resp.SessionID = &sess.ID
		resp.Running = s.session.IsRunning(r.Context(), sess.ID)
	}

	if s.floorManager != nil {
		resp.InjectionCount = s.floorManager.GetInjectionCount()
		resp.RotationThreshold = s.config.GetFloorManagerRotationThreshold()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleEscalate handles POST /api/escalate (set) and DELETE /api/escalate (clear).
func (s *Server) handleEscalate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodySize)).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}
		if len(req.Message) > 500 {
			http.Error(w, "message must be 500 characters or less", http.StatusBadRequest)
			return
		}

		sess, found := s.state.GetFloorManagerSession()
		if !found {
			http.Error(w, "floor manager session not found", http.StatusNotFound)
			return
		}

		if s.eventBus != nil {
			s.eventBus.Publish(bus.Event{
				Type:      "escalation.set",
				SessionID: sess.ID,
				Payload:   bus.EscalationPayload{Message: req.Message},
			})
		}
		w.WriteHeader(http.StatusOK)

	case http.MethodDelete:
		sess, found := s.state.GetFloorManagerSession()
		if !found {
			http.Error(w, "floor manager session not found", http.StatusNotFound)
			return
		}

		if s.eventBus != nil {
			s.eventBus.Publish(bus.Event{
				Type:      "escalation.cleared",
				SessionID: sess.ID,
				Payload:   bus.EscalationPayload{},
			})
		}
		w.WriteHeader(http.StatusOK)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
