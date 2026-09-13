// Package authcheck runs the harness's own login-status tools and parses
// their answer. It never inspects credential files and never guesses: an
// answer it cannot parse is NoAnswer (docs/specs/chat-signed-out-recovery.md,
// State rules / Correct and clear).
package authcheck

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// Result is the parsed answer of one status-tool run.
type Result int

const (
	NoAnswer Result = iota // timeout, missing binary, unparseable output
	LoggedIn
	LoggedOut
)

// Timeout bounds one status-tool run. The dashboard caller derives its
// context from it.
const Timeout = 10 * time.Second

// Run executes the protocol's status tool and parses the answer. The
// second return is the raw combined output for logging on NoAnswer.
func Run(ctx context.Context, protocol string) (Result, string) {
	var cmd *exec.Cmd
	switch protocol {
	case chat.ProtocolClaude:
		cmd = exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	case chat.ProtocolCodex:
		cmd = exec.CommandContext(ctx, "codex", "login", "status")
	default:
		return NoAnswer, ""
	}
	out, _ := cmd.CombinedOutput()
	raw := string(out)
	switch protocol {
	case chat.ProtocolClaude:
		var v struct {
			LoggedIn *bool `json:"loggedIn"`
		}
		if err := json.Unmarshal(out, &v); err != nil || v.LoggedIn == nil {
			return NoAnswer, raw
		}
		if *v.LoggedIn {
			return LoggedIn, raw
		}
		return LoggedOut, raw
	default:
		if strings.Contains(raw, "Logged in using ChatGPT") {
			return LoggedIn, raw
		}
		if strings.TrimSpace(raw) != "" {
			return LoggedOut, raw
		}
		return NoAnswer, raw
	}
}
