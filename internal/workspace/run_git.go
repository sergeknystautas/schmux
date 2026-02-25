package workspace

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// ioTelemetryOnce ensures the telemetry collector is created only once per Manager
var ioTelemetryMu sync.Mutex

// runGit is the instrumented replacement for raw exec.CommandContext(ctx, "git", args...).
// It executes a git command, records telemetry (if enabled in config), and returns
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

	// Record telemetry if:
	// 1. Telemetry collector is already set (via SetIOWorkspaceTelemetry), OR
	// 2. Config has telemetry enabled (hot-reloadable, lazily creates collector)
	if m.ioTelemetry != nil {
		// Already has telemetry set directly
		m.ioTelemetry.RecordCommand("git", args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
	} else if m.config != nil && m.config.GetIOWorkspaceTelemetryEnabled() {
		// Lazily initialize telemetry collector on first enabled command
		ioTelemetryMu.Lock()
		if m.ioTelemetry == nil {
			m.ioTelemetry = NewIOWorkspaceTelemetry()
		}
		ioTelemetryMu.Unlock()
		if m.ioTelemetry != nil {
			m.ioTelemetry.RecordCommand("git", args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
		}
	}

	return stdout, err
}
