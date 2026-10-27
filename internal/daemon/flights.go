package daemon

import (
	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// initFlights builds the flights manager and loads ~/.schmux/flights.json.
// Invalid rows stay inactive and intact; only an unreadable or malformed
// snapshot disables flights. Loading never starts an operation.
//
// The spawn, disposal, commit-settings, and broadcast collaborators exist only
// once the dashboard server does; the server binds them when it starts.
func initFlights(cfg *config.Config, wm *workspace.Manager, logger *log.Logger) *flights.Manager {
	mgr := flights.New(flights.Dependencies{
		Repos:      cfg,
		Workspaces: wm,
	}, logger)
	if err := mgr.Load(schmuxdir.FlightsPath()); err != nil {
		logger.Error("flights unavailable; continuing without flights and leaving snapshot untouched", "err", err)
		return nil
	}
	return mgr
}
