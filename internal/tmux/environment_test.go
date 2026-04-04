package tmux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
)

func TestShowEnvironment(t *testing.T) {
	if err := NewTmuxServer("tmux", "default", nil).Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	sessName := fmt.Sprintf("schmux-env-test-%d", os.Getpid())

	_ = KillSession(ctx, sessName)
	t.Cleanup(func() {
		_ = KillSession(ctx, sessName)
	})

	if err := CreateSession(ctx, sessName, t.TempDir(), "sleep 600"); err != nil {
		t.Fatal("failed to create session:", err)
	}

	env, err := ShowEnvironment(ctx)
	if err != nil {
		t.Fatalf("ShowEnvironment failed: %v", err)
	}

	if _, ok := env["PATH"]; !ok {
		t.Error("expected PATH in tmux environment")
	}
}

func TestSetEnvironment(t *testing.T) {
	if err := NewTmuxServer("tmux", "default", nil).Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	sessName := fmt.Sprintf("schmux-env-set-test-%d", os.Getpid())

	_ = KillSession(ctx, sessName)
	t.Cleanup(func() {
		_ = KillSession(ctx, sessName)
	})

	if err := CreateSession(ctx, sessName, t.TempDir(), "sleep 600"); err != nil {
		t.Fatal("failed to create session:", err)
	}

	key := "SCHMUX_TEST_ENV_VAR"
	value := "test_value_123"

	if err := SetEnvironment(ctx, key, value); err != nil {
		t.Fatalf("SetEnvironment failed: %v", err)
	}

	env, err := ShowEnvironment(ctx)
	if err != nil {
		t.Fatalf("ShowEnvironment failed: %v", err)
	}
	if got := env[key]; got != value {
		t.Errorf("expected %q, got %q", value, got)
	}

	// Clean up the env var from the tmux server
	args := []string{"set-environment", "-g", "-u", key}
	exec.CommandContext(ctx, binary, args...).Run()
}
