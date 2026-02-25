package floormanager

import (
	"strings"
	"testing"
)

func TestGenerateInstructions(t *testing.T) {
	instructions := GenerateInstructions()

	checks := []string{
		"floor manager",
		"schmux status",
		"schmux spawn",
		"schmux list",
		"memory.md",
		"[SIGNAL]",
		"[SHIFT]",
		"schmux end-shift",
	}
	for _, check := range checks {
		if !strings.Contains(instructions, check) {
			t.Errorf("GenerateInstructions() missing %q", check)
		}
	}
}

func TestGenerateSettings(t *testing.T) {
	settings := GenerateSettings()

	// Must pre-approve non-destructive commands
	approvedPatterns := []string{
		"schmux status",
		"schmux list",
		"schmux spawn",
		"schmux end-shift",
		"cat memory.md",
	}
	for _, pattern := range approvedPatterns {
		if !strings.Contains(settings, pattern) {
			t.Errorf("GenerateSettings() missing approved pattern %q", pattern)
		}
	}

	// Must NOT pre-approve destructive commands
	if strings.Contains(settings, "schmux dispose") {
		t.Error("GenerateSettings() must not pre-approve schmux dispose")
	}
	if strings.Contains(settings, "schmux stop") {
		t.Error("GenerateSettings() must not pre-approve schmux stop")
	}
}
