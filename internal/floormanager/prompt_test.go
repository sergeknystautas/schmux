package floormanager

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGenerateInstructions(t *testing.T) {
	instructions := GenerateInstructions()

	if !strings.Contains(instructions, "floor manager") {
		t.Error("expected role definition in instructions")
	}
	if !strings.Contains(instructions, "schmux status") {
		t.Error("expected CLI documentation in instructions")
	}
	if !strings.Contains(instructions, "[SIGNAL]") {
		t.Error("expected signal handling instructions")
	}
	if !strings.Contains(instructions, "memory.md") {
		t.Error("expected memory file maintenance instructions")
	}
	if !strings.Contains(instructions, "rotation") {
		t.Error("expected rotation instructions")
	}
}

func TestGenerateInstructionsStartup(t *testing.T) {
	instructions := GenerateInstructions()

	// Agent should be told to read memory.md on startup (not have it embedded)
	if !strings.Contains(instructions, "Read `memory.md`") {
		t.Error("expected instruction to read memory.md on startup")
	}
	// Agent should be told to run schmux status on startup (not have it embedded)
	if !strings.Contains(instructions, "Run `schmux status`") {
		t.Error("expected instruction to run schmux status on startup")
	}
}

func TestGenerateInstructionsNoEmbeddedState(t *testing.T) {
	instructions := GenerateInstructions()

	// Should NOT contain embedded state sections (the old approach)
	if strings.Contains(instructions, "## Current System State") {
		t.Error("instructions should not contain embedded system state")
	}
	if strings.Contains(instructions, "## Previous Memory") {
		t.Error("instructions should not contain embedded previous memory")
	}
}

func TestGenerateInstructionsShiftRotation(t *testing.T) {
	instructions := GenerateInstructions()

	if !strings.Contains(instructions, "[SHIFT]") {
		t.Error("expected [SHIFT] in shift rotation instructions")
	}
	if !strings.Contains(instructions, "Shift Rotation") {
		t.Error("expected 'Shift Rotation' section heading in instructions")
	}
}

func TestGenerateInstructionsRotateUsesEventsFile(t *testing.T) {
	instructions := GenerateInstructions()

	// Rotation instructions should reference SCHMUX_EVENTS_FILE, not SCHMUX_STATUS_FILE
	if strings.Contains(instructions, "SCHMUX_STATUS_FILE") {
		t.Error("rotation instructions should use SCHMUX_EVENTS_FILE, not SCHMUX_STATUS_FILE")
	}
	if !strings.Contains(instructions, "SCHMUX_EVENTS_FILE") {
		t.Error("expected SCHMUX_EVENTS_FILE in rotation instructions")
	}
	if !strings.Contains(instructions, `"state":"rotate"`) {
		t.Error("expected rotate state in event format")
	}
}

func TestGenerateSettings(t *testing.T) {
	settings := GenerateSettings()

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(settings), &parsed); err != nil {
		t.Fatalf("GenerateSettings() produced invalid JSON: %v", err)
	}

	// Should have permissions.allow
	perms, ok := parsed["permissions"].(map[string]interface{})
	if !ok {
		t.Fatal("expected permissions object in settings")
	}
	allow, ok := perms["allow"].([]interface{})
	if !ok {
		t.Fatal("expected allow array in permissions")
	}

	// Should include specific schmux subcommands (not wildcard)
	allowStrs := make([]string, len(allow))
	for i, v := range allow {
		allowStrs[i], _ = v.(string)
	}
	for _, expected := range []string{
		"Bash(schmux status)",
		"Bash(schmux spawn *)",
		"Bash(schmux dispose *)",
		"Bash(schmux escalate *)",
	} {
		found := false
		for _, s := range allowStrs {
			if s == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in permissions.allow, got %v", expected, allowStrs)
		}
	}

	// Should NOT include schmux stop
	for _, s := range allowStrs {
		if s == "Bash(schmux stop)" || s == "Bash(schmux *)" {
			t.Errorf("should not include %q in permissions.allow", s)
		}
	}
}
