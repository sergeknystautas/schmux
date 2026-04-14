package detect

import (
	"testing"
)

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
	// TODO: The endpoint-conditional env var logic (setting ANTHROPIC_* vars when
	// endpoint is present) needs to move to internal/models/manager.go. For now,
	// all descriptor-based adapters return nil from BuildRunnerEnv.
	adapter := GetAdapter("claude")
	spec := RunnerSpec{
		ModelValue: "kimi-thinking",
		Endpoint:   "https://api.moonshot.ai/anthropic",
	}
	env := adapter.BuildRunnerEnv(spec)
	if len(env) != 0 {
		t.Errorf("expected empty env from descriptor adapter, got %v", env)
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

func TestDefaultModels(t *testing.T) {
	defaults := GetDefaultModels()
	if len(defaults) != 4 {
		t.Fatalf("expected 4 default models, got %d", len(defaults))
	}

	expectedIDs := map[string]string{
		"claude":   "claude",
		"codex":    "codex",
		"gemini":   "gemini",
		"opencode": "opencode",
	}

	for _, m := range defaults {
		expectedRunner, ok := expectedIDs[m.ID]
		if !ok {
			t.Errorf("unexpected default model: %s", m.ID)
			continue
		}
		if _, hasRunner := m.RunnerFor(expectedRunner); !hasRunner {
			t.Errorf("%s: missing runner %q", m.ID, expectedRunner)
		}
		spec, _ := m.RunnerFor(expectedRunner)
		if spec.ModelValue != "" {
			t.Errorf("%s: expected empty ModelValue, got %q", m.ID, spec.ModelValue)
		}
		// Should only have one runner
		if len(m.Runners) != 1 {
			t.Errorf("%s: expected 1 runner, got %d", m.ID, len(m.Runners))
		}
	}
}

func TestIsDefaultModel(t *testing.T) {
	tests := []struct {
		id     string
		expect bool
	}{
		{"claude", true},
		{"codex", true},
		{"gemini", true},
		{"opencode", true},
		{"claude-opus-4-6", false},
		{"claude-sonnet-4-6", false},
		{"kimi-thinking", false},
	}
	for _, tt := range tests {
		got := IsDefaultModel(tt.id)
		if got != tt.expect {
			t.Errorf("IsDefaultModel(%q) = %v, want %v", tt.id, got, tt.expect)
		}
	}
}

func TestMigrateModelID_NewMigrations(t *testing.T) {
	tests := []struct {
		old, want string
	}{
		// Old default_* IDs
		{"default_claude", "claude"},
		{"default_codex", "codex"},
		{"default_gemini", "gemini"},
		{"default_opencode", "opencode"},
		// Short Claude aliases
		{"opus", "claude-opus-4-6"},
		{"sonnet", "claude-sonnet-4-6"},
		{"haiku", "claude-haiku-4-5-20251001"},
		{"claude-opus", "claude-opus-4-6"},
		{"claude-sonnet", "claude-sonnet-4-6"},
		{"claude-haiku", "claude-haiku-4-5-20251001"},
		// Old builtin IDs not in catalog
		{"claude-opus-4-5", "claude-opus-4-5-20251101"},
		{"claude-opus-4-1", "claude-opus-4-1-20250805"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"},
		{"claude-opus-4", "claude-opus-4-20250514"},
		{"claude-sonnet-4", "claude-sonnet-4-20250514"},
		{"claude-haiku-4-5", "claude-haiku-4-5-20251001"},
		// models.dev ID normalization
		{"kimi-thinking", "kimi-k2-thinking"},
		{"minimax-m2.1", "MiniMax-M2.1"},
		{"minimax-2.5", "MiniMax-M2.5"},
		{"minimax-2.7", "MiniMax-M2.7"},
		{"minimax", "MiniMax-M2.1"},
		// Already current IDs — no migration
		{"kimi-k2-thinking", "kimi-k2-thinking"},
		{"MiniMax-M2.1", "MiniMax-M2.1"},
		{"claude-opus-4-6", "claude-opus-4-6"},
	}
	for _, tt := range tests {
		got := MigrateModelID(tt.old)
		if got != tt.want {
			t.Errorf("MigrateModelID(%q) = %q, want %q", tt.old, got, tt.want)
		}
	}
}

func TestLegacyIDMigrations_AllResolveToRegistryIDs(t *testing.T) {
	// Every old builtin model ID must either:
	//   a) exist in the models.dev registry as-is, OR
	//   b) migrate (via MigrateModelID) to an ID that exists in the registry
	// Exceptions: qwen3-coder-plus and opencode-zen are intentionally dropped.

	// These IDs are known to exist in the catalog after dedup/filtering.
	// Taken from /api/config models[] at the time of the builtinModels deletion.
	catalogIDs := map[string]bool{
		// anthropic
		"claude-opus-4-6": true, "claude-sonnet-4-6": true,
		"claude-opus-4-5-20251101": true, "claude-opus-4-1-20250805": true,
		"claude-opus-4-20250514": true, "claude-sonnet-4-5-20250929": true,
		"claude-sonnet-4-20250514": true, "claude-haiku-4-5-20251001": true,
		// moonshotai
		"kimi-k2-thinking": true, "kimi-k2.5": true,
		// zai
		"glm-4.7": true, "glm-4.5-air": true, "glm-5": true, "glm-5-turbo": true,
		// minimax
		"MiniMax-M2.1": true, "MiniMax-M2.5": true, "MiniMax-M2.7": true,
		// openai
		"gpt-5.4": true, "gpt-5.3-codex": true, "gpt-5.2": true, "gpt-5.2-codex": true,
		"gpt-5.1-codex-max": true, "gpt-5.1-codex": true, "gpt-5.1-codex-mini": true, "gpt-5-codex": true,
		// google
		"gemini-3.1-pro-preview": true, "gemini-3-flash-preview": true,
		"gemini-2.5-flash-lite": true,
	}

	// Legacy IDs with no catalog equivalent — dropped from the system
	dropped := map[string]bool{
		"claude-sonnet-3-5": true,
		"claude-haiku-3-5":  true,
		"qwen3-coder-plus":  true,
		"opencode-zen":      true,
		"gemini-2.5-pro":    true,
		"gemini-2.5-flash":  true,
		"gemini-2.0-flash":  true,
	}

	// Every old builtin model ID that was in the hardcoded list
	oldBuiltinIDs := []string{
		// Anthropic native
		"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
		"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4",
		"claude-sonnet-4-5", "claude-sonnet-4",
		"claude-sonnet-3-5", "claude-haiku-3-5",
		// Third-party
		"kimi-thinking", "kimi-k2.5",
		"glm-4.7", "glm-4.5-air", "glm-5", "glm-5-turbo",
		"minimax-m2.1", "minimax-2.5", "minimax-2.7",
		"qwen3-coder-plus",
		// OpenAI
		"gpt-5.4", "gpt-5.3-codex", "gpt-5.2", "gpt-5.2-codex",
		"gpt-5.1-codex-max", "gpt-5.1-codex", "gpt-5.1-codex-mini", "gpt-5-codex",
		// Google
		"gemini-3.1-pro-preview", "gemini-3-flash-preview",
		"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite", "gemini-2.0-flash",
		// OpenCode
		"opencode-zen",
	}

	// Legacy aliases
	legacyAliases := []string{
		"claude-opus", "claude-sonnet", "claude-haiku",
		"opus", "sonnet", "haiku",
		"minimax",
	}

	for _, id := range oldBuiltinIDs {
		if dropped[id] {
			continue
		}
		resolved := MigrateModelID(id)
		if !catalogIDs[resolved] {
			t.Errorf("old builtin %q resolves to %q which is not in the catalog", id, resolved)
		}
	}

	for _, id := range legacyAliases {
		resolved := MigrateModelID(id)
		if !catalogIDs[resolved] {
			t.Errorf("legacy alias %q resolves to %q which is not in the catalog", id, resolved)
		}
	}
}
