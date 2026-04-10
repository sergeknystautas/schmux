package detect

import (
	"testing"
)

func TestIsBuiltinToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want bool
	}{
		{"claude", true},
		{"codex", true},
		{"gemini", true},
		{"unknown-tool", false},
		{"", false},
		{"Claude", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBuiltinToolName(tt.name)
			if got != tt.want {
				t.Errorf("IsBuiltinToolName(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestGetAgentInstructionConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		toolName string
		wantDir  string
		wantFile string
		wantOK   bool
	}{
		{"claude", ".claude", "CLAUDE.md", true},
		{"codex", ".codex", "AGENTS.md", true},
		{"gemini", ".gemini", "GEMINI.md", true},
		{"unknown", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			cfg, ok := GetAgentInstructionConfig(tt.toolName)
			if ok != tt.wantOK {
				t.Fatalf("GetAgentInstructionConfig(%q) ok = %v, want %v", tt.toolName, ok, tt.wantOK)
			}
			if ok {
				if cfg.InstructionDir != tt.wantDir {
					t.Errorf("InstructionDir = %q, want %q", cfg.InstructionDir, tt.wantDir)
				}
				if cfg.InstructionFile != tt.wantFile {
					t.Errorf("InstructionFile = %q, want %q", cfg.InstructionFile, tt.wantFile)
				}
			}
		})
	}
}

func TestOpencodeInBuiltinTools(t *testing.T) {
	t.Parallel()
	if !IsBuiltinToolName("opencode") {
		t.Error("opencode should be a builtin tool name")
	}
}

func TestOpencodeInstructionConfig(t *testing.T) {
	t.Parallel()
	cfg, ok := GetAgentInstructionConfig("opencode")
	if !ok {
		t.Fatal("expected opencode instruction config")
	}
	if cfg.InstructionDir != ".opencode" {
		t.Errorf("InstructionDir = %q, want '.opencode'", cfg.InstructionDir)
	}
	if cfg.InstructionFile != "AGENTS.md" {
		t.Errorf("InstructionFile = %q, want 'AGENTS.md'", cfg.InstructionFile)
	}
}
