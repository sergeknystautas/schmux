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
		{"claude-opus-4-6", "claude-opus-4-6", true},
		{"claude-sonnet-4-6", "claude-sonnet-4-6", true},
		{"claude-haiku-4-5", "claude-haiku-4-5", true},

		// Older Anthropic models
		{"claude-opus-4", "claude-opus-4", true},
		{"claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"claude-sonnet-4", "claude-sonnet-4", true},
		{"claude-sonnet-3-5", "claude-sonnet-3-5", true},
		{"claude-haiku-3-5", "claude-haiku-3-5", true},

		// Third-party models
		{"kimi-thinking", "kimi-thinking", true},
		{"kimi-k2.5", "kimi-k2.5", true},
		{"glm-4.7", "glm-4.7", true},
		{"glm-4.5-air", "glm-4.5-air", true},
		{"glm-5", "glm-5", true},
		{"minimax-m2.1", "minimax-m2.1", true},
		{"minimax-2.5", "minimax-2.5", true},
		{"qwen3-coder-plus", "qwen3-coder-plus", true},

		// Codex models
		{"gpt-5.4", "gpt-5.4", true},
		{"gpt-5.3-codex", "gpt-5.3-codex", true},
		{"gpt-5.2", "gpt-5.2", true},
		{"gpt-5.2-codex", "gpt-5.2-codex", true},
		{"gpt-5.1-codex-max", "gpt-5.1-codex-max", true},
		{"gpt-5.1-codex-mini", "gpt-5.1-codex-mini", true},

		// OpenCode models
		{"opencode-zen", "opencode-zen", true},

		// Google/Gemini models
		{"gemini-2.5-pro", "gemini-2.5-pro", true},
		{"gemini-2.5-flash", "gemini-2.5-flash", true},
		{"gemini-2.0-flash", "gemini-2.0-flash", true},

		// Legacy IDs (resolved via MigrateModelID)
		{"claude-opus", "claude-opus-4-6", true},
		{"claude-sonnet", "claude-sonnet-4-6", true},
		{"claude-haiku", "claude-haiku-4-5", true},
		{"opus", "claude-opus-4-6", true},
		{"sonnet", "claude-sonnet-4-6", true},
		{"haiku", "claude-haiku-4-5", true},
		{"minimax", "minimax-m2.1", true},

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
		{"claude-opus-4-6", true},
		{"claude-sonnet-4-6", true},
		{"claude-haiku-4-5", true},
		{"claude-opus-4", true},
		{"claude-sonnet-4-5", true},
		{"claude-sonnet-4", true},
		{"claude-sonnet-3-5", true},
		{"claude-haiku-3-5", true},
		{"kimi-thinking", true},
		{"kimi-k2.5", true},
		{"glm-4.7", true},
		{"glm-4.5-air", true},
		{"glm-5", true},
		{"minimax-m2.1", true},
		{"minimax-2.5", true},
		{"qwen3-coder-plus", true},
		{"gpt-5.4", true},
		{"gpt-5.3-codex", true},
		{"gpt-5.2", true},
		{"gpt-5.2-codex", true},
		{"gpt-5.1-codex-max", true},
		{"gpt-5.1-codex-mini", true},
		{"opencode-zen", true},
		{"gemini-2.5-pro", true},
		{"gemini-2.5-flash", true},
		{"gemini-2.0-flash", true},

		// Legacy IDs (resolved via MigrateModelID)
		{"claude-opus", true},
		{"claude-sonnet", true},
		{"claude-haiku", true},
		{"opus", true},
		{"sonnet", true},
		{"haiku", true},
		{"minimax", true},

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

func TestGetAvailableModels(t *testing.T) {
	tests := []struct {
		name             string
		detected         []Tool
		shouldContain    []string
		shouldNotContain []string
	}{
		{
			name:          "no tools detected",
			detected:      []Tool{},
			shouldContain: []string{},
		},
		{
			name:     "only claude detected",
			detected: []Tool{{Name: "claude", Command: "/usr/bin/claude", Source: "config", Agentic: true}},
			shouldContain: []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
				"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
				"claude-sonnet-4-5", "claude-sonnet-4", "claude-sonnet-3-5", "claude-haiku-3-5",
				"kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "glm-5",
				"minimax-m2.1", "minimax-2.5", "qwen3-coder-plus"},
			shouldNotContain: []string{"gpt-5.2-codex", "gpt-5.1-codex",
				"gemini-2.5-pro", "gemini-2.5-flash"},
		},
		{
			name: "claude and codex detected",
			detected: []Tool{
				{Name: "claude", Command: "/usr/bin/claude", Source: "config", Agentic: true},
				{Name: "codex", Command: "/usr/bin/codex", Source: "config", Agentic: true},
			},
			shouldContain: []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
				"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
				"claude-sonnet-4-5", "claude-sonnet-4", "claude-sonnet-3-5", "claude-haiku-3-5",
				"kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "glm-5",
				"minimax-m2.1", "minimax-2.5", "qwen3-coder-plus",
				"gpt-5.3-codex", "gpt-5.2-codex", "gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5-codex"},
			shouldNotContain: []string{"gemini-2.5-pro", "gemini-2.5-flash"},
		},
		{
			name:     "only opencode detected - shows models with opencode runner",
			detected: []Tool{{Name: "opencode", Command: "opencode", Source: "PATH", Agentic: true}},
			shouldContain: []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
				"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
				"claude-sonnet-4-5", "claude-sonnet-4", "claude-sonnet-3-5", "claude-haiku-3-5",
				"opencode-zen",
				"kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "glm-5",
				"minimax-m2.1", "minimax-2.5", "qwen3-coder-plus",
				"gpt-5.4", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-codex", "gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5-codex",
				"gemini-3.1-pro-preview", "gemini-3-flash-preview",
				"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash"},
		},
		{
			name:             "only codex detected",
			detected:         []Tool{{Name: "codex", Command: "codex", Source: "PATH", Agentic: true}},
			shouldContain:    []string{"gpt-5.4", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-codex", "gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5-codex"},
			shouldNotContain: []string{"claude-opus-4-6", "claude-sonnet-4-6", "opencode-zen", "gemini-2.5-pro"},
		},
		{
			name:     "only gemini detected",
			detected: []Tool{{Name: "gemini", Command: "gemini", Source: "PATH", Agentic: true}},
			shouldContain: []string{"gemini-3.1-pro-preview", "gemini-3-flash-preview",
				"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash"},
			shouldNotContain: []string{"claude-opus-4-6", "gpt-5.2-codex", "opencode-zen"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			available := GetAvailableModels(tt.detected)

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

func TestOpencodeModelExists(t *testing.T) {
	t.Parallel()
	model, ok := FindModel("opencode-zen")
	if !ok {
		t.Fatal("expected opencode-zen model to exist")
	}
	if _, hasRunner := model.RunnerFor("opencode"); !hasRunner {
		t.Error("expected opencode-zen to have an opencode runner")
	}
	if model.Category != "native" {
		t.Errorf("Category = %q, want 'native'", model.Category)
	}
}

func TestGetBuiltinModels(t *testing.T) {
	models := GetBuiltinModels()

	// Should have 33 models total (10 Anthropic + 8 third-party + 8 Codex + 1 OpenCode + 6 Google)
	if len(models) != 33 {
		t.Errorf("GetBuiltinModels() returned %d models, want 33", len(models))
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
		"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
		"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
		"claude-sonnet-4-5", "claude-sonnet-4", "claude-sonnet-3-5", "claude-haiku-3-5",
		"kimi-thinking", "kimi-k2.5", "glm-4.7", "glm-4.5-air", "glm-5", "minimax-m2.1", "minimax-2.5", "qwen3-coder-plus",
		"gpt-5.4", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-codex", "gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5-codex",
		"opencode-zen",
		"gemini-3.1-pro-preview", "gemini-3-flash-preview",
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash",
	}
	for _, id := range expectedModels {
		if !modelIDs[id] {
			t.Errorf("GetBuiltinModels() missing expected model %q", id)
		}
	}
}

func TestRunnerSpec(t *testing.T) {
	model := Model{
		ID: "test-model",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "test-model"},
			"opencode": {ModelValue: "anthropic/test-model"},
		},
	}
	spec, ok := model.RunnerFor("claude")
	if !ok {
		t.Fatal("expected runner for claude")
	}
	if spec.ModelValue != "test-model" {
		t.Errorf("ModelValue = %q, want %q", spec.ModelValue, "test-model")
	}
	_, ok = model.RunnerFor("nonexistent")
	if ok {
		t.Error("expected no runner for nonexistent tool")
	}
}

func TestBuildRunnerEnv(t *testing.T) {
	adapter := GetAdapter("claude")
	spec := RunnerSpec{
		ModelValue: "kimi-thinking",
		Endpoint:   "https://api.moonshot.ai/anthropic",
	}
	env := adapter.BuildRunnerEnv(spec)
	if env["ANTHROPIC_BASE_URL"] != "https://api.moonshot.ai/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_MODEL"] != "kimi-thinking" {
		t.Errorf("ANTHROPIC_MODEL = %q", env["ANTHROPIC_MODEL"])
	}

	// Native model (no endpoint) should return empty env
	nativeSpec := RunnerSpec{ModelValue: "claude-opus-4-6"}
	nativeEnv := adapter.BuildRunnerEnv(nativeSpec)
	if len(nativeEnv) != 0 {
		t.Errorf("expected empty env for native model, got %v", nativeEnv)
	}

	// Non-claude adapters return empty env
	codexAdapter := GetAdapter("codex")
	codexEnv := codexAdapter.BuildRunnerEnv(RunnerSpec{ModelValue: "gpt-5.2-codex"})
	if len(codexEnv) != 0 {
		t.Errorf("expected empty env for codex adapter, got %v", codexEnv)
	}
}

func TestAllModelsHaveRunners(t *testing.T) {
	models := GetBuiltinModels()
	for _, m := range models {
		if len(m.Runners) == 0 {
			t.Errorf("model %q has no runners", m.ID)
		}
	}
}

func TestSortedRunnerKeys(t *testing.T) {
	runners := map[string]RunnerSpec{
		"opencode": {},
		"claude":   {},
		"codex":    {},
	}
	keys := SortedRunnerKeys(runners)
	if len(keys) != 3 || keys[0] != "claude" || keys[1] != "codex" || keys[2] != "opencode" {
		t.Errorf("SortedRunnerKeys = %v, want [claude codex opencode]", keys)
	}
}

// TestGetAvailableModelsMultiRunner was merged into TestGetAvailableModels
// since GetAvailableModels now uses multi-runner logic.

func TestExpandedCatalogModels(t *testing.T) {
	t.Parallel()
	newModelIDs := []string{
		// Older Anthropic models
		"claude-opus-4",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-sonnet-3-5",
		"claude-haiku-3-5",
		// Google/Gemini models
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
	}
	for _, id := range newModelIDs {
		t.Run(id, func(t *testing.T) {
			model, ok := FindModel(id)
			if !ok {
				t.Fatalf("expected model %q to exist in catalog", id)
			}
			if model.ID != id {
				t.Errorf("FindModel(%q).ID = %q, want %q", id, model.ID, id)
			}
		})
	}
}

func TestGeminiModelsHaveGeminiRunner(t *testing.T) {
	t.Parallel()
	geminiIDs := []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash"}
	for _, id := range geminiIDs {
		t.Run(id, func(t *testing.T) {
			model, ok := FindModel(id)
			if !ok {
				t.Fatalf("model %q not found", id)
			}
			spec, hasGemini := model.RunnerFor("gemini")
			if !hasGemini {
				t.Fatalf("model %q missing gemini runner", id)
			}
			if spec.ModelValue == "" {
				t.Errorf("model %q gemini runner has empty ModelValue", id)
			}
			// Should also have opencode runner
			spec, hasOpencode := model.RunnerFor("opencode")
			if !hasOpencode {
				t.Fatalf("model %q missing opencode runner", id)
			}
			if spec.ModelValue == "" {
				t.Errorf("model %q opencode runner has empty ModelValue", id)
			}
		})
	}
}

func TestOlderClaudeModelsHaveBothRunners(t *testing.T) {
	t.Parallel()
	olderClaudeIDs := []string{
		"claude-opus-4",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-sonnet-3-5",
		"claude-haiku-3-5",
	}
	for _, id := range olderClaudeIDs {
		t.Run(id, func(t *testing.T) {
			model, ok := FindModel(id)
			if !ok {
				t.Fatalf("model %q not found", id)
			}
			spec, hasClaude := model.RunnerFor("claude")
			if !hasClaude {
				t.Fatalf("model %q missing claude runner", id)
			}
			if spec.ModelValue == "" {
				t.Errorf("model %q claude runner has empty ModelValue", id)
			}
			spec, hasOpencode := model.RunnerFor("opencode")
			if !hasOpencode {
				t.Fatalf("model %q missing opencode runner", id)
			}
			if spec.ModelValue == "" {
				t.Errorf("model %q opencode runner has empty ModelValue", id)
			}
		})
	}
}
