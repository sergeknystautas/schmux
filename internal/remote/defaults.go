package remote

import (
	"context"
	"time"

	"github.com/charmbracelet/log"
)

// remoteTmuxClient is the minimal surface applyRemoteTmuxDefaults needs.
// *controlmode.Client satisfies it (SetOption and Execute already exist;
// SetServerOption was added in Group A).
type remoteTmuxClient interface {
	SetOption(ctx context.Context, option, value string) error
	SetServerOption(ctx context.Context, option, value string) error
	Execute(ctx context.Context, cmd string) (string, time.Duration, error)
}

// applyRemoteTmuxDefaults applies all options every remote tmux server should
// have. Called from waitForControlMode (which itself is called from both
// connect() and Reconnect(), so the options are re-applied if the remote tmux
// server is restarted).
//
// Replaces the inline option block at internal/remote/connection.go:736-746
// (replacement happens in Group C). Behavior preservation:
//   - window-size manual: session-scope (matches existing SetOption call, no -g).
//   - DISPLAY :99: raw setenv -g (matches existing Execute).
//   - set-clipboard / terminal-features: NEW; server-scope via SetServerOption.
func applyRemoteTmuxDefaults(ctx context.Context, c remoteTmuxClient, logger *log.Logger) {
	serverOpts := [][2]string{
		{"set-clipboard", "external"},
		{"terminal-features", "*:clipboard"},
	}
	for _, o := range serverOpts {
		if err := c.SetServerOption(ctx, o[0], o[1]); err != nil && logger != nil {
			logger.Warn("applyRemoteTmuxDefaults: server option", "opt", o[0], "err", err)
		}
	}
	if err := c.SetOption(ctx, "window-size", "manual"); err != nil && logger != nil {
		logger.Warn("applyRemoteTmuxDefaults: window-size", "err", err)
	}
	if _, _, err := c.Execute(ctx, "setenv -g DISPLAY :99"); err != nil && logger != nil {
		logger.Warn("applyRemoteTmuxDefaults: DISPLAY", "err", err)
	}
}
