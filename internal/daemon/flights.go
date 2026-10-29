package daemon

import (
	"context"
	"errors"

	"github.com/charmbracelet/log"

	"github.com/sergeknystautas/schmux/internal/branchsuggest"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/flightname"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/oneshot"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// flightBranchSuggester names a flight's branch from the flight prompt
// through the same branch-suggestion feature the spawn form uses. It fails
// when no branch-suggest target is configured.
type flightBranchSuggester struct{ cfg *config.Config }

func (s flightBranchSuggester) SuggestBranch(ctx context.Context, prompt string) (string, error) {
	result, err := branchsuggest.AskForPrompt(ctx, s.cfg, prompt)
	if err != nil {
		return "", err
	}
	return result.Branch, nil
}

// initFlights builds the flights manager and loads ~/.schmux/flights.json.
// Invalid rows stay inactive and intact; only an unreadable or malformed
// snapshot disables flights. Loading never starts an operation.
//
// The spawn, disposal, commit-settings, chat-runtime, and broadcast
// collaborators exist only once the dashboard server does; the server binds
// them when it starts.
func initFlights(cfg *config.Config, wm *workspace.Manager, logger *log.Logger) *flights.Manager {
	mgr := flights.New(flights.Dependencies{
		Repos:      cfg,
		Workspaces: wm,
		Namer:      flightNamer{cfg: cfg, logger: logger},
		Pilot:      cfg,
		Branches:   flightBranchSuggester{cfg: cfg},
	}, logger)
	if err := mgr.Load(schmuxdir.FlightsPath()); err != nil {
		logger.Error("flights unavailable; continuing without flights and leaving snapshot untouched", "err", err)
		return nil
	}
	return mgr
}

// startMonitor starts the flight monitor. It runs before the dashboard server
// binds chat sessions; the monitor stays idle until that Bind wakes it.
func startMonitor(ctx context.Context, mgr *flights.Manager) {
	mgr.StartMonitoring(ctx)
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
