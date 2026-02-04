package detect

import (
	"testing"
)

func TestFindModel(t *testing.T) {
	tests := []struct {
		name      string
		wantName  string
		wantFound bool
	}{
		// By exact ID
		{"claude-opus", "claude-opus", true},
		{"claude-sonnet", "claude-sonnet", true},
		{"claude-haiku", "claude-haiku", true},

		// By alias
		{"opus", "claude-opus", true},
		{"sonnet", "claude-sonnet", true},
		{"haiku", "claude-haiku", true},

		// Third-party models
		{"kimi-thinking", "kimi-thinking", true},
		{"kimi-k2.5", "kimi-k2.5", true},
		{"glm-4.7", "glm-4.7", true},
		{"glm-4.5-air", "glm-4.5-air", true},
		{"minimax", "minimax", true},
		{"qwen3-coder-plus", "qwen3-coder-plus", true},

		// Backward compat alias
		{"minimax-m2.1", "minimax", true},

		// Not found
		{"nonexistent", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, found := FindModel(tt.name)
			if found != tt.wantFound {
				t.Errorf("FindModel(%q) found=%v, want %v", tt.name, found, tt.wantFound)
				return
			}
			if found && model.ID != tt.wantName {
				t.Errorf("FindModel(%q) model.ID=%q, want %q", tt.name, model.ID, tt.wantName)
			}
		})
	}
}

func TestIsModelID(t *testing.T) {
	tests := []struct {
		name     string
		wantBool bool
	}{
		// Exact IDs
		{"claude-opus", true},
		{"claude-sonnet", true},
		{"claude-haiku", true},
		{"kimi-thinking", true},
		{"kimi-k2.5", true},
		{"glm-4.7", true},
		{"glm-4.5-air", true},
		{"minimax", true},
		{"qwen3-coder-plus", true},
		{"qwen3-coder-plus", true},

		// Aliases
		{"opus", true},
		{"sonnet", true},
		{"haiku", true},
		{"minimax-m2.1", true},

		// Not models
		{"", false},
		{"nonexistent", false},
		{"claude", false}, // base tool, not a model
		{"codex", false},  // base tool, not a model
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsModelID(tt.name)
			if got != tt.wantBool {
				t.Errorf("IsModelID(%q)=%v, want %v", tt.name, got, tt.wantBool)
			}
		})
	}
}

func TestBuildEnv(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		modelValue  string
		expectedEnv map[string]string
	}{
		{
			name:       "native model - no endpoint",
			endpoint:   "",
			modelValue: "claude-sonnet-4-5-20250929",
			expectedEnv: map[string]string{
				"ANTHROPIC_MODEL": "claude-sonnet-4-5-20250929",
			},
		},
		{
			name:       "third-party model with endpoint",
			endpoint:   "https://api.example.com",
			modelValue: "kimi-thinking",
			expectedEnv: map[string]string{
				"ANTHROPIC_MODEL":                "kimi-thinking",
				"ANTHROPIC_BASE_URL":             "https://api.example.com",
				"ANTHROPIC_DEFAULT_OPUS_MODEL":   "kimi-thinking",
				"ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-thinking",
				"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "kimi-thinking",
				"CLAUDE_CODE_SUBAGENT_MODEL":     "kimi-thinking",
			},
		},
		{
			name:       "third-party model minimax",
			endpoint:   "https://api.minimax.io/anthropic",
			modelValue: "minimax-m2.1",
			expectedEnv: map[string]string{
				"ANTHROPIC_MODEL":                "minimax-m2.1",
				"ANTHROPIC_BASE_URL":             "https://api.minimax.io/anthropic",
				"ANTHROPIC_DEFAULT_OPUS_MODEL":   "minimax-m2.1",
				"ANTHROPIC_DEFAULT_SONNET_MODEL": "minimax-m2.1",
				"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "minimax-m2.1",
				"CLAUDE_CODE_SUBAGENT_MODEL":     "minimax-m2.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := Model{
				ID:          "test-model",
				DisplayName: "Test Model",
				BaseTool:    "claude",
				Endpoint:    tt.endpoint,
				ModelValue:  tt.modelValue,
			}
			env := model.BuildEnv()

			if len(env) != len(tt.expectedEnv) {
				t.Errorf("BuildEnv() returned %d env vars, want %d", len(env), len(tt.expectedEnv))
				return
			}

			for key, wantVal := range tt.expectedEnv {
				gotVal, ok := env[key]
				if !ok {
					t.Errorf("BuildEnv() missing key %q", key)
					return
				}
				if gotVal != wantVal {
					t.Errorf("BuildEnv()[%q]=%q, want %q", key, gotVal, wantVal)
				}
			}
		})
	}
}

func TestGetAvailableModels(t *testing.T) {
	tests := []struct {
		name             string
		detected         []Tool
		expectedCount    int
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:          "no tools detected",
			detected:      []Tool{},
			expectedCount: 0,
		},
		{
			name:          "only claude detected",
			detected:      []Tool{{Name: "claude", Command: "/usr/bin/claude", Source: "config", Agentic: true}},
			expectedCount: 9,
			shouldContain: []string{"claude-opus", "claude-sonnet", "claude-haiku", "kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "minimax", "qwen3-coder-plus"},
		},
		{
			name: "claude and codex detected",
			detected: []Tool{
				{Name: "claude", Command: "/usr/bin/claude", Source: "config", Agentic: true},
				{Name: "codex", Command: "/usr/bin/codex", Source: "config", Agentic: true},
			},
			expectedCount: 9,
			shouldContain: []string{"claude-opus", "claude-sonnet", "claude-haiku", "kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "minimax", "qwen3-coder-plus"},
		},
		{
			name: "all detected tools",
			detected: []Tool{
				{Name: "claude", Command: "/usr/bin/claude", Source: "config", Agentic: true},
				{Name: "codex", Command: "/usr/bin/codex", Source: "config", Agentic: true},
			},
			expectedCount: 9,
			shouldContain: []string{"claude-opus", "claude-sonnet", "claude-haiku", "kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "minimax", "qwen3-coder-plus"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := GetAvailableModels(tt.detected)

			if len(available) != tt.expectedCount {
				t.Errorf("GetAvailableModels() returned %d models, want %d", len(available), tt.expectedCount)
				return
			}

			// Check shouldContain
			for _, id := range tt.shouldContain {
				found := false
				for _, m := range available {
					if m.ID == id {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("GetAvailableModels() missing expected model %q", id)
				}
			}

			// Check shouldNotContain
			for _, id := range tt.shouldNotContain {
				found := false
				for _, m := range available {
					if m.ID == id {
						found = true
						break
					}
				}
				if found {
					t.Errorf("GetAvailableModels() unexpectedly returned model %q", id)
				}
			}
		})
	}
}

func TestGetBuiltinModels(t *testing.T) {
	models := GetBuiltinModels()

	// Should have 9 models total
	if len(models) != 9 {
		t.Errorf("GetBuiltinModels() returned %d models, want 9", len(models))
	}

	// Check that models are copies (not pointers)
	if &models[0] == &GetBuiltinModels()[0] {
		t.Error("GetBuiltinModels() returned pointers, not copies")
	}

	// Verify expected models exist
	modelIDs := make(map[string]bool)
	for _, m := range models {
		modelIDs[m.ID] = true
	}

	expectedModels := []string{
		"claude-opus", "claude-sonnet", "claude-haiku",
		"kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "minimax", "qwen3-coder-plus",
	}
	for _, id := range expectedModels {
		if !modelIDs[id] {
			t.Errorf("GetBuiltinModels() missing expected model %q", id)
		}
	}
}
