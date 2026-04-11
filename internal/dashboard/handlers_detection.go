package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
)

// handleDetectionSummary returns what tools were detected at daemon startup.
func (s *Server) handleDetectionSummary(w http.ResponseWriter, r *http.Request) {
	// Map detected agents from the model manager
	detectedTools := s.models.GetDetectedTools()
	agents := make([]contracts.DetectionAgent, len(detectedTools))
	for i, dt := range detectedTools {
		agents[i] = contracts.DetectionAgent{
			Name:    dt.Name,
			Command: dt.Command,
			Source:  dt.Source,
		}
	}

	// Map detected VCS tools
	vcs := make([]contracts.DetectionVCS, len(s.detectedVCS))
	for i, v := range s.detectedVCS {
		vcs[i] = contracts.DetectionVCS{
			Name: v.Name,
			Path: v.Path,
		}
	}

	// Map detected tmux status
	tmuxStatus := contracts.DetectionTmux{
		Available: s.detectedTmux.Available,
		Path:      s.detectedTmux.Path,
	}

	resp := contracts.DetectionSummaryResponse{
		Status: "ready",
		Agents: agents,
		VCS:    vcs,
		Tmux:   tmuxStatus,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("failed to encode response", "handler", "detection-summary", "err", err)
	}
}
