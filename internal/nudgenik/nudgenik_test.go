package nudgenik

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/state"
)

func setupFakeTmux(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "tmux")
	script := "#!/bin/sh\nprintf \"%s\" \"${TMUX_FAKE_OUTPUT}\"\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}

	pathEnv := dir + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", pathEnv)
}

func TestAskForSessionNoResponse(t *testing.T) {
	setupFakeTmux(t)
	t.Setenv("TMUX_FAKE_OUTPUT", "❯\n")

	cfg := &config.Config{
		Nudgenik: &config.NudgenikConfig{Target: "claude"},
	}
	sess := state.Session{ID: "sess-1", TmuxSession: "sess-1"}

	_, err := AskForSession(context.Background(), cfg, sess)
	if !errors.Is(err, ErrNoResponse) {
		t.Fatalf("expected ErrNoResponse, got %v", err)
	}
}

func TestAskForSessionAgentMissing(t *testing.T) {
	setupFakeTmux(t)
	t.Setenv("TMUX_FAKE_OUTPUT", "hello\n❯\n")

	if _, found, err := detect.FindDetectedTool(context.Background(), "claude"); err == nil && found {
		t.Skip("claude detected; skipping missing agent test")
	}

	cfg := &config.Config{
		Nudgenik: &config.NudgenikConfig{Target: "claude"},
	}
	sess := state.Session{ID: "sess-2", TmuxSession: "sess-2"}

	_, err := AskForSession(context.Background(), cfg, sess)
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("expected ErrTargetNotFound, got %v", err)
	}
}

func TestAskForSessionDisabled(t *testing.T) {
	setupFakeTmux(t)
	t.Setenv("TMUX_FAKE_OUTPUT", "hello\n❯\n")

	cfg := &config.Config{}
	sess := state.Session{ID: "sess-3", TmuxSession: "sess-3"}

	_, err := AskForSession(context.Background(), cfg, sess)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}
