package floormanager

import (
	"strings"
	"testing"
)

func TestGenerateInstructions(t *testing.T) {
	instructions := GenerateInstructions("/usr/local/bin/schmux")

	checks := []string{
		"floor manager",
		"/usr/local/bin/schmux status",
		"/usr/local/bin/schmux spawn",
		"/usr/local/bin/schmux list",
		"/usr/local/bin/schmux tell",
		"/usr/local/bin/schmux events",
		"/usr/local/bin/schmux capture",
		"/usr/local/bin/schmux inspect",
		"/usr/local/bin/schmux branches",
		"memory.md",
		"[SIGNAL]",
		"[SHIFT]",
		"/usr/local/bin/schmux end-shift",
	}
	for _, check := range checks {
		if !strings.Contains(instructions, check) {
			t.Errorf("GenerateInstructions() missing %q", check)
		}
	}
}

func TestGenerateSettings(t *testing.T) {
	settings := GenerateSettings("/usr/local/bin/schmux")

	// Must pre-approve non-destructive commands
	approvedPatterns := []string{
		"/usr/local/bin/schmux status",
		"/usr/local/bin/schmux list",
		"/usr/local/bin/schmux spawn",
		"/usr/local/bin/schmux end-shift",
		"/usr/local/bin/schmux tell",
		"/usr/local/bin/schmux events",
		"/usr/local/bin/schmux capture",
		"/usr/local/bin/schmux inspect",
		"/usr/local/bin/schmux branches",
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
