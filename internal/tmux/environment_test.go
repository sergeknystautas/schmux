package tmux

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestShowEnvironment(t *testing.T) {
	server := NewTmuxServer("tmux", "default", nil)
	if err := server.Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	sessName := fmt.Sprintf("schmux-env-test-%d", os.Getpid())

	_ = server.KillSession(ctx, sessName)
	t.Cleanup(func() {
		_ = server.KillSession(ctx, sessName)
	})

	if err := server.CreateSession(ctx, sessName, t.TempDir(), "sleep 600"); err != nil {
		t.Skip("cannot create tmux session:", err)
	}

	env, err := server.ShowEnvironment(ctx)
	if err != nil {
		t.Fatalf("ShowEnvironment failed: %v", err)
	}

	if _, ok := env["PATH"]; !ok {
		t.Error("expected PATH in tmux environment")
	}
}

func TestSetEnvironment(t *testing.T) {
	server := NewTmuxServer("tmux", "default", nil)
	if err := server.Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	sessName := fmt.Sprintf("schmux-env-set-test-%d", os.Getpid())

	_ = server.KillSession(ctx, sessName)
	t.Cleanup(func() {
		_ = server.KillSession(ctx, sessName)
	})

	if err := server.CreateSession(ctx, sessName, t.TempDir(), "sleep 600"); err != nil {
		t.Skip("cannot create tmux session:", err)
	}

	key := "SCHMUX_TEST_ENV_VAR"
	value := "test_value_123"

	if err := server.SetEnvironment(ctx, key, value); err != nil {
		t.Fatalf("SetEnvironment failed: %v", err)
	}

	env, err := server.ShowEnvironment(ctx)
	if err != nil {
		t.Fatalf("ShowEnvironment failed: %v", err)
	}
	if got := env[key]; got != value {
		t.Errorf("expected %q, got %q", value, got)
	}

	// Clean up the env var from the tmux server
	_ = server.SetEnvironment(ctx, key, "")
}
