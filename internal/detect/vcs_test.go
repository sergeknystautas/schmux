package detect

import (
	"testing"
)

func TestDetectVCS_GitAvailable(t *testing.T) {
	tools := DetectVCS()
	var found bool
	for _, tool := range tools {
		if tool.Name == "git" {
			found = true
			if tool.Path == "" {
				t.Error("git detected but path is empty")
			}
		}
	}
	if !found {
		t.Skip("git not available in test environment")
	}
}

func TestDetectVCS_UnknownBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	tools := DetectVCS()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools with empty PATH, got %d", len(tools))
	}
}

func TestDetectTmux_Available(t *testing.T) {
	result := DetectTmux()
	if result.Available && result.Path == "" {
		t.Error("tmux detected but path is empty")
	}
}

func TestDetectTmux_NotAvailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	result := DetectTmux()
	if result.Available {
		t.Error("expected tmux unavailable with empty PATH")
	}
}
