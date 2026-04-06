package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ioTelemetryOnce ensures the telemetry collector is created only once per Manager
var ioTelemetryMu sync.Mutex

func (m *Manager) runCmd(ctx context.Context, binary string, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir

	// Start the command in its own process group so we can kill the entire
	// tree (including children like git-remote-https) on context timeout.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Kill the entire process group (negative PID).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second

	// Prevent VCS commands from prompting for credentials on a terminal,
	// which would hang indefinitely in a daemon process.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

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
			stderrContent := stderrBuf.Bytes()
			exitErr.Stderr = append([]byte(nil), stderrContent...)
			stderrBytes = int64(len(stderrContent))
			// Include stderr in the error message so callers see the actual
			// git/command error (e.g. "already checked out at ...") instead of
			// just "exit status 128".
			if trimmed := strings.TrimSpace(string(stderrContent)); trimmed != "" {
				err = fmt.Errorf("%w: %s", err, trimmed)
			}
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
