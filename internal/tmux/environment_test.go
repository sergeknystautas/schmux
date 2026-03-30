package tmux

import (
	"context"
	"os/exec"
	"testing"
)

func TestShowEnvironment(t *testing.T) {
	if err := TmuxChecker.Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
	env, err := ShowEnvironment(ctx)
	if err != nil {
		t.Fatalf("ShowEnvironment failed: %v", err)
	}

	if _, ok := env["PATH"]; !ok {
		t.Error("expected PATH in tmux environment")
	}
}

func TestSetEnvironment(t *testing.T) {
	if err := TmuxChecker.Check(); err != nil {
		t.Skip("tmux not available")
	}

	ctx := context.Background()
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

	// Clean up
	args := []string{"set-environment", "-g", "-u", key}
	exec.CommandContext(ctx, binary, args...).Run()
}
