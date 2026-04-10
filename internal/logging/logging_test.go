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

func TestNew_ReturnsNonNil(t *testing.T) {
	logger := New()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSetLevel_UpdatesAllLoggers(t *testing.T) {
	// Reset registry for test isolation
	registryMu.Lock()
	savedRegistry := registry
	savedLevel := currentLevel
	registry = nil
	currentLevel = log.InfoLevel
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registry = savedRegistry
		currentLevel = savedLevel
		registryMu.Unlock()
	}()

	root := New()
	child := Sub(root, "test")

	if root.GetLevel() != log.InfoLevel {
		t.Fatalf("root: expected InfoLevel, got %v", root.GetLevel())
	}
	if child.GetLevel() != log.InfoLevel {
		t.Fatalf("child: expected InfoLevel, got %v", child.GetLevel())
	}

	SetLevel(log.DebugLevel)

	if root.GetLevel() != log.DebugLevel {
		t.Errorf("root: expected DebugLevel, got %v", root.GetLevel())
	}
	if child.GetLevel() != log.DebugLevel {
		t.Errorf("child: expected DebugLevel, got %v", child.GetLevel())
	}
	if GetLevel() != log.DebugLevel {
		t.Errorf("GetLevel: expected DebugLevel, got %v", GetLevel())
	}

	SetLevel(log.InfoLevel)

	if root.GetLevel() != log.InfoLevel {
		t.Errorf("root: expected InfoLevel after reset, got %v", root.GetLevel())
	}
	if child.GetLevel() != log.InfoLevel {
		t.Errorf("child: expected InfoLevel after reset, got %v", child.GetLevel())
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
