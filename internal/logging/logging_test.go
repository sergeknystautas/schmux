package logging

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

// In dev mode the daemon's stderr is a pipe to the dev-runner TUI, so Go panic
// tracebacks (which bypass the structured logger) are never persisted. New(true)
// must register daemon-startup.log as a runtime crash sink so a panic lands on
// disk, not just on screen. Verified via a child process that panics for real.
func TestNew_DevModePersistsPanicToDaemonLog(t *testing.T) {
	if os.Getenv("SCHMUX_CRASH_CHILD") == "1" {
		schmuxdir.Set(os.Getenv("SCHMUX_CRASH_DIR"))
		New(true)
		panic("boom-from-test-child")
	}

	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestNew_DevModePersistsPanicToDaemonLog$")
	cmd.Env = append(os.Environ(), "SCHMUX_CRASH_CHILD=1", "SCHMUX_CRASH_DIR="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected child to crash from panic, but it exited cleanly:\n%s", out)
	}

	logPath := filepath.Join(dir, "daemon-startup.log")
	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("reading %s: %v", logPath, readErr)
	}
	if !strings.Contains(string(data), "boom-from-test-child") {
		t.Fatalf("panic traceback not persisted to daemon-startup.log; file contents:\n%s", data)
	}
}

func TestNew_DefaultLevel(t *testing.T) {
	logger := New()
	if logger.GetLevel() != log.InfoLevel {
		t.Errorf("expected InfoLevel, got %v", logger.GetLevel())
	}
}

func TestNew_EnvOverride(t *testing.T) {
	t.Setenv("SCHMUX_LOG_LEVEL", "debug")
	logger := New()
	if logger.GetLevel() != log.DebugLevel {
		t.Errorf("expected DebugLevel, got %v", logger.GetLevel())
	}
}

func TestNew_InvalidEnv(t *testing.T) {
	t.Setenv("SCHMUX_LOG_LEVEL", "bogus")
	logger := New()
	if logger.GetLevel() != log.InfoLevel {
		t.Errorf("expected InfoLevel fallback, got %v", logger.GetLevel())
	}
}

func TestNew_ReturnsNonNil(t *testing.T) {
	logger := New()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSub_HasPrefix(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{})
	sub := Sub(logger, "workspace")
	sub.Info("test")
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("workspace")) {
		t.Errorf("expected prefix 'workspace' in output, got: %s", output)
	}
}
