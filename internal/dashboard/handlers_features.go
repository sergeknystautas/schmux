package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/tunnel"
)

// handleGetFeatures handles GET /api/features — reports which optional modules are available.
func (s *Server) handleGetFeatures(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.Features{
		Tunnel: tunnel.IsAvailable(),
		GitHub: github.IsAvailable(),
	})
}
