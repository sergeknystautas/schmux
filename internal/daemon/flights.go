package daemon

import (
	"fmt"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// initFlights builds the flights manager and loads ~/.schmux/flights.json. An
// unreadable or invalid snapshot is a startup error and the file is left
// untouched. Loading never starts a transition or terminate attempt.
//
// The spawn, disposal, commit-settings, and broadcast collaborators exist only
// once the dashboard server does; the server binds them when it starts.
func initFlights(cfg *config.Config, wm *workspace.Manager, logger *log.Logger) (*flights.Manager, error) {
	mgr := flights.New(flights.Dependencies{
		Repos:      cfg,
		Workspaces: wm,
	}, logger)
	if err := mgr.Load(schmuxdir.FlightsPath()); err != nil {
		return nil, fmt.Errorf("load flights: %w", err)
	}
	return mgr, nil
}
