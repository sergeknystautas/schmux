package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/commstyles"
	"github.com/sergeknystautas/schmux/internal/dashboardsx"
	"github.com/sergeknystautas/schmux/internal/floormanager"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/lore"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/personas"
	"github.com/sergeknystautas/schmux/internal/repofeed"
	"github.com/sergeknystautas/schmux/internal/subreddit"
	"github.com/sergeknystautas/schmux/internal/telemetry"
	"github.com/sergeknystautas/schmux/internal/timelapse"
	"github.com/sergeknystautas/schmux/internal/tunnel"
	"github.com/sergeknystautas/schmux/internal/update"
)

// handleGetFeatures handles GET /api/features — reports which optional modules are available.
func (s *Server) handleGetFeatures(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(contracts.Features{
		Tunnel:        tunnel.IsAvailable(),
		GitHub:        github.IsAvailable(),
		Telemetry:     telemetry.IsPostHogAvailable(),
		Update:        update.IsAvailable(),
		DashboardSX:   dashboardsx.IsAvailable(),
		ModelRegistry: models.IsAvailable(),
		Repofeed:      repofeed.IsAvailable(),
		Subreddit:     subreddit.IsAvailable(),
		Personas:      personas.IsAvailable(),
		CommStyles:    commstyles.IsAvailable(),
		Lore:          lore.IsAvailable(),
		FloorManager:  floormanager.IsAvailable(),
		Timelapse:     timelapse.IsAvailable(),
	})
}
