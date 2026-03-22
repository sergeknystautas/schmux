package detect

import (
	"sort"
)

// RunnerSpec describes how a specific tool executes a model.
type RunnerSpec struct {
	ModelValue      string   // Value passed to the tool (e.g., "claude-opus-4-6", "anthropic/claude-opus-4-6")
	Endpoint        string   // API endpoint override (empty = tool's default)
	RequiredSecrets []string // Secrets needed when using THIS tool for THIS model
}

// Model represents an AI model that can be used for spawning sessions.
type Model struct {
	ID          string                // e.g., "claude-sonnet-4-6", "kimi-thinking"
	DisplayName string                // e.g., "claude sonnet 4.5", "Kimi K2 Thinking"
	Provider    string                // e.g., "anthropic", "moonshot", "zai", "minimax"
	UsageURL    string                // Signup/pricing page
	Category    string                // "native" or "third-party" (for UI grouping)
	Runners     map[string]RunnerSpec // tool name -> how to run this model with that tool
}

// RunnerFor returns the RunnerSpec for the given tool, if this model supports it.
func (m Model) RunnerFor(tool string) (RunnerSpec, bool) {
	if m.Runners == nil {
		return RunnerSpec{}, false
	}
	spec, ok := m.Runners[tool]
	return spec, ok
}

// SortedRunnerKeys returns the tool names from a Runners map in sorted order.
func SortedRunnerKeys(runners map[string]RunnerSpec) []string {
	keys := make([]string, 0, len(runners))
	for k := range runners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// defaultModels are synthetic models that use each runner's built-in default.
// They pass no --model flag, letting the harness use whatever it defaults to.
var defaultModels = []Model{
	{
		ID:          "claude",
		DisplayName: "Claude (default)",
		Provider:    "anthropic",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"claude": {ModelValue: ""}},
	},
	{
		ID:          "codex",
		DisplayName: "Codex (default)",
		Provider:    "openai",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"codex": {ModelValue: ""}},
	},
	{
		ID:          "gemini",
		DisplayName: "Gemini (default)",
		Provider:    "google",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"gemini": {ModelValue: ""}},
	},
	{
		ID:          "opencode",
		DisplayName: "OpenCode (default)",
		Provider:    "opencode",
		Category:    "native",
		Runners:     map[string]RunnerSpec{"opencode": {ModelValue: ""}},
	},
}

// GetDefaultModels returns the synthetic default models.
func GetDefaultModels() []Model {
	out := make([]Model, len(defaultModels))
	copy(out, defaultModels)
	return out
}

// IsDefaultModel returns true if the model ID is a default_* model.
func IsDefaultModel(id string) bool {
	for _, m := range defaultModels {
		if m.ID == id {
			return true
		}
	}
	return false
}

// legacyIDMigrations maps old model IDs to current vendor-defined IDs.
var legacyIDMigrations = map[string]string{
	// Short aliases → current model
	"claude-opus":   "claude-opus-4-6",
	"claude-sonnet": "claude-sonnet-4-6",
	"claude-haiku":  "claude-haiku-4-5-20251001",
	"opus":          "claude-opus-4-6",
	"sonnet":        "claude-sonnet-4-6",
	"haiku":         "claude-haiku-4-5-20251001",
	// Old builtin IDs not in catalog
	"claude-opus-4-5":   "claude-opus-4-5-20251101",
	"claude-opus-4-1":   "claude-opus-4-1-20250805",
	"claude-sonnet-4-5": "claude-sonnet-4-5-20250929",
	"claude-opus-4":     "claude-opus-4-20250514",
	"claude-sonnet-4":   "claude-sonnet-4-20250514",
	"claude-haiku-4-5":  "claude-haiku-4-5-20251001",
	// Old default_* model IDs → bare tool names
	"default_claude":   "claude",
	"default_codex":    "codex",
	"default_gemini":   "gemini",
	"default_opencode": "opencode",
	// models.dev ID normalization
	"kimi-thinking": "kimi-k2-thinking",
	"minimax-m2.1":  "MiniMax-M2.1",
	"minimax-2.5":   "MiniMax-M2.5",
	"minimax-2.7":   "MiniMax-M2.7",
	"minimax":       "MiniMax-M2.1",
}

// MigrateModelID converts a legacy model ID to the current vendor-defined ID.
// Returns the input unchanged if it's not a legacy ID.
func MigrateModelID(id string) string {
	for i := 0; i < 10; i++ { // max depth to prevent infinite loops
		next, ok := legacyIDMigrations[id]
		if !ok {
			return id
		}
		id = next
	}
	return id
}

// LegacyIDMigrations returns a copy of the legacy ID migration map.
func LegacyIDMigrations() map[string]string {
	out := make(map[string]string, len(legacyIDMigrations))
	for k, v := range legacyIDMigrations {
		out[k] = v
	}
	return out
}

// FirstRunnerKey returns the first sorted runner key from the model's Runners map.
// Returns empty string if the model has no runners.
func (m Model) FirstRunnerKey() string {
	keys := SortedRunnerKeys(m.Runners)
	if len(keys) > 0 {
		return keys[0]
	}
	return ""
}

// FirstRunnerRequiredSecrets returns the RequiredSecrets from the first sorted runner.
// Returns nil if no runners exist or the first runner has no required secrets.
func (m Model) FirstRunnerRequiredSecrets() []string {
	keys := SortedRunnerKeys(m.Runners)
	if len(keys) > 0 {
		return m.Runners[keys[0]].RequiredSecrets
	}
	return nil
}

// FirstRunnerModelValue returns the ModelValue from the first sorted runner.
// Returns empty string if no runners exist.
func (m Model) FirstRunnerModelValue() string {
	keys := SortedRunnerKeys(m.Runners)
	if len(keys) > 0 {
		return m.Runners[keys[0]].ModelValue
	}
	return ""
}
