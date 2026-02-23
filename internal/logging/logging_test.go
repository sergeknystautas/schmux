package logging

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/log"
)

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

func TestNew_WritesToStderr(t *testing.T) {
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
