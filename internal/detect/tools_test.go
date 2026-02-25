package detect

import (
	"testing"
)

func TestGetBuiltinToolNames(t *testing.T) {
	t.Parallel()
	names := GetBuiltinToolNames()
	if len(names) == 0 {
		t.Fatal("expected at least one builtin tool name")
	}

	// Verify it returns a copy, not the original slice
	names[0] = "modified"
	original := GetBuiltinToolNames()
	if original[0] == "modified" {
		t.Error("GetBuiltinToolNames should return a copy, not the original slice")
	}
}

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

func TestGetInstructionPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		toolName string
		want     string
	}{
		{"claude", ".claude/CLAUDE.md"},
		{"codex", ".codex/AGENTS.md"},
		{"gemini", ".gemini/GEMINI.md"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			got := GetInstructionPath(tt.toolName)
			if got != tt.want {
				t.Errorf("GetInstructionPath(%q) = %q, want %q", tt.toolName, got, tt.want)
			}
		})
	}
}

func TestGetBaseToolName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want string
	}{
		{"claude", "claude"},
		{"codex", "codex"},
		{"gemini", "gemini"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetBaseToolName(tt.name)
			if got != tt.want {
				t.Errorf("GetBaseToolName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}

	// Test with a known model name - this depends on the models registered in models.go
	// We can verify that model names resolve to their base tool
	t.Run("model name resolves to base tool", func(t *testing.T) {
		models := GetBuiltinModels()
		if len(models) > 0 {
			model := models[0]
			got := GetBaseToolName(model.ID)
			if got != model.BaseTool {
				t.Errorf("GetBaseToolName(%q) = %q, want %q (model's BaseTool)", model.ID, got, model.BaseTool)
			}
		}
	})
}

func TestGetInstructionPathForTarget(t *testing.T) {
	t.Parallel()
	// Direct tool names
	got := GetInstructionPathForTarget("claude")
	if got != ".claude/CLAUDE.md" {
		t.Errorf("GetInstructionPathForTarget('claude') = %q, want '.claude/CLAUDE.md'", got)
	}

	// Unknown target
	got = GetInstructionPathForTarget("totally-unknown")
	if got != "" {
		t.Errorf("GetInstructionPathForTarget('totally-unknown') = %q, want empty", got)
	}
}

func TestGetAgentInstructionConfigForTarget(t *testing.T) {
	t.Parallel()
	// Direct tool name
	cfg, ok := GetAgentInstructionConfigForTarget("claude")
	if !ok {
		t.Fatal("expected ok=true for 'claude'")
	}
	if cfg.InstructionDir != ".claude" {
		t.Errorf("InstructionDir = %q, want '.claude'", cfg.InstructionDir)
	}

	// Unknown target
	_, ok = GetAgentInstructionConfigForTarget("unknown-target")
	if ok {
		t.Error("expected ok=false for unknown target")
	}
}

func TestFindToolInList(t *testing.T) {
	t.Parallel()
	tools := []Tool{
		{Name: "claude", Command: "claude", Agentic: true},
		{Name: "codex", Command: "codex", Agentic: true},
		{Name: "gemini", Command: "gemini", Agentic: true},
	}

	tests := []struct {
		name      string
		searchFor string
		wantFound bool
		wantName  string
	}{
		{"finds first tool", "claude", true, "claude"},
		{"finds middle tool", "codex", true, "codex"},
		{"finds last tool", "gemini", true, "gemini"},
		{"returns false for unknown", "unknown", false, ""},
		{"empty name not found", "", false, ""},
		{"case-sensitive match", "Claude", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, found := FindToolInList(tools, tt.searchFor)
			if found != tt.wantFound {
				t.Errorf("FindToolInList(%q) found = %v, want %v", tt.searchFor, found, tt.wantFound)
			}
			if found && tool.Name != tt.wantName {
				t.Errorf("FindToolInList(%q).Name = %q, want %q", tt.searchFor, tool.Name, tt.wantName)
			}
		})
	}

	t.Run("empty list returns false", func(t *testing.T) {
		_, found := FindToolInList(nil, "claude")
		if found {
			t.Error("expected false for nil tool list")
		}
	})
}
