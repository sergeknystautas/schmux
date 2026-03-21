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

func (m *Manager) runCmd(ctx context.Context, binary string, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
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

	if m.ioTelemetry != nil {
		m.ioTelemetry.RecordCommand(binary, args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
	} else if m.config != nil && m.config.GetIOWorkspaceTelemetryEnabled() {
		ioTelemetryMu.Lock()
		if m.ioTelemetry == nil {
			m.ioTelemetry = NewIOWorkspaceTelemetry()
		}
		ioTelemetryMu.Unlock()
		if m.ioTelemetry != nil {
			m.ioTelemetry.RecordCommand(binary, args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
		}
	}

	return stdout, err
}

func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	return m.runCmd(ctx, "git", workspaceID, trigger, dir, args...)
}
