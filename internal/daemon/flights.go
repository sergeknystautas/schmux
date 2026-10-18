package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flightname"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// initFlights builds the flights manager and loads ~/.schmux/flights.json. An
// unreadable or invalid snapshot is a startup error and the file is left
// untouched. Loading never starts a transition or dispose attempt.
//
// The spawn, disposal, commit-settings, and broadcast collaborators exist only
// once the dashboard server does; the server binds them when it starts.
func initFlights(cfg *config.Config, wm *workspace.Manager, logger *log.Logger) (*flights.Manager, error) {
	mgr := flights.New(flights.Dependencies{
		Repos:      cfg,
		Workspaces: wm,
		Namer:      flightNamer{cfg: cfg, logger: logger},
	}, logger)
	if err := mgr.Load(schmuxdir.FlightsPath()); err != nil {
		return nil, fmt.Errorf("load flights: %w", err)
	}
	return mgr, nil
}

// flightNamer adapts the flightname oneshot into the flights.Namer
// interface. It reads the target fresh from config on each call so a target
// change picks up on the next pass. Empty target returns flights.ErrNamingOff
// without calling the LLM; oneshot.ErrDisabled is mapped to the same.
type flightNamer struct {
	cfg    *config.Config
	logger *log.Logger
}

func (n flightNamer) Name(ctx context.Context, prompt string) (string, error) {
	target := n.cfg.GetFlightNameTarget()
	if target == "" {
		return "", flights.ErrNamingOff
	}
	n.logger.Info("flight name attempt", "target", target)
	result, err := flightname.AskForPrompt(ctx, n.cfg, prompt)
	if errors.Is(err, oneshot.ErrDisabled) {
		return "", flights.ErrNamingOff
	}
	if err != nil {
		return "", err
	}
	return result.Name, nil
}
