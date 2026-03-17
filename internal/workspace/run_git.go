package workspace

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// ioTelemetryOnce ensures the telemetry collector is created only once per Manager
var ioTelemetryMu sync.Mutex

// runGit is the instrumented replacement for raw exec.CommandContext(ctx, "git", args...).
// It executes a git command, records telemetry (if enabled in config), and returns
// stdout and any error. On non-zero exit, stderr is copied onto exec.ExitError.Stderr
// so callers can inspect it via errors.As.
func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir

	releaseWatcherSuppression := func() {}
	if m != nil && m.gitWatcher != nil {
		releaseWatcherSuppression = m.gitWatcher.BeginInternalGitSuppressionForDir(dir)
	}
	defer releaseWatcherSuppression()

	start := time.Now()
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	duration := time.Since(start)

	// Extract exit code and stderr bytes from the error (if any).
	exitCode := 0
	stderrBytes := int64(0)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			exitErr.Stderr = append([]byte(nil), stderrBuf.Bytes()...)
			stderrBytes = int64(len(exitErr.Stderr))
		}
	}

	stdout := stdoutBuf.Bytes()
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
