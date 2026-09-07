package daemon

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// chatNudgeTestSetup builds a minimal state/session manager pair with
// a configurable list of sessions. We do not run a daemon, just the
// NudgeNik eligibility check directly.
func chatNudgeTestSetup(t *testing.T) (*config.Config, *state.State, *session.Manager) {
	t.Helper()
	schmuxDir := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, log.NewWithOptions(io.Discard, log.Options{}))
	cfg := &config.Config{
		ConfigData: config.ConfigData{
			WorkspacePath: schmuxDir,
			RunTargets:    []config.RunTarget{{Name: "command", Command: "true"}},
		},
	}
	wm := workspace.New(cfg, st, statePath, log.NewWithOptions(io.Discard, log.Options{}))
	sm := session.New(cfg, st, statePath, wm, nil, log.NewWithOptions(io.Discard, log.Options{}))
	t.Cleanup(func() {
		sm.Stop()
	})
	return cfg, st, sm
}

// TestCheckInactiveSessionsForNudge_SkipsChat verifies that the
// NudgeNik eligibility check explicitly skips chat sessions, even
// when they have an empty Nudge, a zero activity timestamp, and the
// inactivity window is satisfied. Chat sessions reach classification
// only via the headless runtime; NudgeNik must not even attempt
// classification (which would fail anyway, because GetTracker returns
// ErrChatSession).
func TestCheckInactiveSessionsForNudge_SkipsChat(t *testing.T) {
	cfg, st, _ := chatNudgeTestSetup(t)
	cfg.Nudgenik = &config.NudgenikConfig{Targets: []string{"command"}}
	now := time.Now()
	// Chat session: empty Nudge, zero activity timestamps.
	chatSess := state.Session{
		ID:           "chat-1",
		WorkspaceID:  "ws-1",
		Target:       "claude",
		TmuxSession:  "chat-1",
		Kind:         state.SessionKindChat,
		ChatProtocol: "claude-stream-json",
		CreatedAt:    now,
	}
	if err := st.AddSession(chatSess); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// With NudgeNik enabled, the chat guard must return before any manager
	// lookup. A nil manager makes a regression fail at that first lookup,
	// without starting a terminal tracker or invoking an LLM.
	checkInactiveSessionsForNudge(ctx, cfg, st, nil, func() { t.Error("unexpected broadcast") }, log.NewWithOptions(io.Discard, log.Options{}))
	sess, _ := st.GetSession("chat-1")
	if sess.Nudge != "" {
		t.Fatalf("chat session was classified: %s", sess.Nudge)
	}
}

// Disabled NudgeNik still leaves terminal state alone.
func TestCheckInactiveSessionsForNudge_Disabled(t *testing.T) {
	cfg, st, sm := chatNudgeTestSetup(t)
	st.AddSession(state.Session{
		ID: "term-1", WorkspaceID: "ws-1", Target: "command", TmuxSession: "term-1",
		CreatedAt: time.Now(),
	})
	// No nudgenik targets → function returns before session iteration.
	checkInactiveSessionsForNudge(context.Background(), cfg, st, sm, func() {}, log.NewWithOptions(io.Discard, log.Options{}))
	sess, _ := st.GetSession("term-1")
	if sess.Nudge != "" {
		t.Fatalf("terminal session touched: %s", sess.Nudge)
	}
}
