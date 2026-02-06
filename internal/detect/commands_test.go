package detect

import (
	"strings"
	"testing"
)

func TestBuildCommandParts_ResumeMode(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		detectedCmd string
		mode        ToolMode
		jsonSchema  string
		model       *Model
		wantParts   []string
		wantErr     bool
		errContains string
	}{
		{
			name:        "claude resume",
			toolName:    "claude",
			detectedCmd: "claude",
			mode:        ToolModeResume,
			wantParts:   []string{"claude", "--continue"},
		},
		{
			name:        "codex resume",
			toolName:    "codex",
			detectedCmd: "codex",
			mode:        ToolModeResume,
			wantParts:   []string{"codex", "resume", "--last"},
		},
		{
			name:        "gemini resume",
			toolName:    "gemini",
			detectedCmd: "gemini",
			mode:        ToolModeResume,
			wantParts:   []string{"gemini", "-r", "latest"},
		},
		{
			name:        "unknown tool resume",
			toolName:    "unknown",
			detectedCmd: "unknown",
			mode:        ToolModeResume,
			wantErr:     true,
			errContains: "resume mode not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildCommandParts(tt.toolName, tt.detectedCmd, tt.mode, tt.jsonSchema, tt.model)

			if tt.wantErr {
				if err == nil {
					t.Errorf("BuildCommandParts() expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("BuildCommandParts() error=%q, want error containing %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("BuildCommandParts() unexpected error: %v", err)
				return
			}

			if len(got) != len(tt.wantParts) {
				t.Errorf("BuildCommandParts() got %d parts, want %d", len(got), len(tt.wantParts))
				return
			}

			for i, want := range tt.wantParts {
				if got[i] != want {
					t.Errorf("BuildCommandParts() part[%d]=%q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestBuildCommandParts_ResumeWithModel(t *testing.T) {
	// Resume mode should ignore model flags (uses agent's resume command directly)
	model := &Model{
		ID:         "test-model",
		BaseTool:   "claude",
		ModelFlag:  "--model",
		ModelValue: "custom-model",
	}

	got, err := BuildCommandParts("claude", "claude", ToolModeResume, "", model)
	if err != nil {
		t.Fatalf("BuildCommandParts() unexpected error: %v", err)
	}

	want := []string{"claude", "--continue"}
	if len(got) != len(want) {
		t.Fatalf("BuildCommandParts() got %d parts, want %d", len(got), len(want))
	}

	for i, wantPart := range want {
		if got[i] != wantPart {
			t.Errorf("BuildCommandParts() part[%d]=%q, want %q", i, got[i], wantPart)
		}
	}
}
