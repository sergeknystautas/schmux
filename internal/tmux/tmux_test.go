package tmux

import (
	"testing"
)

func TestGetAttachCommand(t *testing.T) {
	cmd := GetAttachCommand("test-session")
	expected := `tmux attach -t "test-session"`
	if cmd != expected {
		t.Errorf("expected %s, got %s", expected, cmd)
	}
}

func TestListSessions(t *testing.T) {
	// This test requires tmux to be installed
	// Skip if not available
	t.Skip("requires tmux to be installed")
}

func TestSessionExists(t *testing.T) {
	// This test requires tmux to be installed
	t.Skip("requires tmux to be installed")
}

func TestCaptureOutput(t *testing.T) {
	// This test requires tmux to be installed
	t.Skip("requires tmux to be installed")
}

func TestCreateSession(t *testing.T) {
	// This test requires tmux to be installed
	t.Skip("requires tmux to be installed")
}

func TestKillSession(t *testing.T) {
	// This test requires tmux to be installed
	t.Skip("requires tmux to be installed")
}

func TestSendKeys(t *testing.T) {
	// This test requires tmux to be installed
	t.Skip("requires tmux to be installed")
}
