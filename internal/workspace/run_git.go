package workspace

import (
	"context"
	"os/exec"
	"time"
)

// runGit is the instrumented replacement for raw exec.CommandContext(ctx, "git", args...).
// It executes a git command, records telemetry (if ioTelemetry is non-nil), and returns
// stdout and any error. Stderr bytes are captured from ExitError.Stderr when the command
// fails with a non-zero exit code.
func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	start := time.Now()
	stdout, err := cmd.Output()
	duration := time.Since(start)

	// Extract exit code and stderr bytes from the error (if any)
	exitCode := 0
	var stderrBytes int64
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			stderrBytes = int64(len(exitErr.Stderr))
		}
	}

	stdoutBytes := int64(len(stdout))

	// Record telemetry if enabled (nil-safe: RecordCommand is a no-op on nil receiver)
	if m.ioTelemetry != nil {
		m.ioTelemetry.RecordCommand("git", args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
	}

	return stdout, err
}
