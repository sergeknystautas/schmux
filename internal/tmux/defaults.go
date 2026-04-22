package tmux

import (
	"context"

	"github.com/charmbracelet/log"
)

// tmuxServerOptionSetter is the minimal surface ApplyTmuxServerDefaults needs.
// *TmuxServer satisfies it. Carved out as an interface so daemon-startup code
// can be tested with a fake recorder.
type tmuxServerOptionSetter interface {
	SetServerOption(ctx context.Context, option, value string) error
}

// ApplyTmuxServerDefaults sets server-scope options every schmux-owned tmux
// server should have:
//   - set-clipboard external: forward OSC 52 from inner panes out to the daemon
//     PTY without keeping a tmux-internal copy of every yanked secret.
//   - terminal-features '*:clipboard': bypass tmux's outer-terminal Ms-capability
//     check so OSC 52 forwarding works regardless of the daemon's TERM.
//
// Errors are logged and swallowed — these options are belt-and-braces; failure
// must not prevent server startup. Pre-existing tmux servers schmux did not
// start will still receive these options for as long as they live; they may
// drop the options if killed and restarted outside schmux's control.
func ApplyTmuxServerDefaults(ctx context.Context, srv tmuxServerOptionSetter, logger *log.Logger) {
	options := [][2]string{
		{"set-clipboard", "external"},
		{"terminal-features", "*:clipboard"},
	}
	for _, opt := range options {
		if err := srv.SetServerOption(ctx, opt[0], opt[1]); err != nil {
			if logger != nil {
				logger.Warn("ApplyTmuxServerDefaults: failed to set option",
					"option", opt[0], "value", opt[1], "err", err)
			}
		}
	}
}
