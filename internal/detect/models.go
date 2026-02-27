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

// builtinModels defines the canonical model IDs and display names exposed to the UI.
var builtinModels = []Model{
	// Native Claude models - use vendor-defined model IDs
	{
		ID:          "claude-opus-4-6",
		DisplayName: "Claude Opus 4.6",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "opus"},
			"opencode": {ModelValue: "anthropic/claude-opus-4-6"},
		},
	},
	{
		ID:          "claude-sonnet-4-6",
		DisplayName: "Claude Sonnet 4.6",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "sonnet"},
			"opencode": {ModelValue: "anthropic/claude-sonnet-4-6"},
		},
	},
	{
		ID:          "claude-haiku-4-5",
		DisplayName: "Claude Haiku 4.5",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "haiku"},
			"opencode": {ModelValue: "anthropic/claude-haiku-4-5"},
		},
	},
	{
		ID:          "claude-opus-4",
		DisplayName: "Claude Opus 4",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "claude-opus-4-20250514"},
			"opencode": {ModelValue: "anthropic/claude-opus-4-20250514"},
		},
	},
	{
		ID:          "claude-sonnet-4-5",
		DisplayName: "Claude Sonnet 4.5",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "claude-sonnet-4-5-20250514"},
			"opencode": {ModelValue: "anthropic/claude-sonnet-4-5-20250514"},
		},
	},
	{
		ID:          "claude-sonnet-4",
		DisplayName: "Claude Sonnet 4",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "claude-sonnet-4-20250514"},
			"opencode": {ModelValue: "anthropic/claude-sonnet-4-20250514"},
		},
	},
	{
		ID:          "claude-sonnet-3-5",
		DisplayName: "Claude Sonnet 3.5",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "claude-3-5-sonnet-20241022"},
			"opencode": {ModelValue: "anthropic/claude-3-5-sonnet-20241022"},
		},
	},
	{
		ID:          "claude-haiku-3-5",
		DisplayName: "Claude Haiku 3.5",
		Provider:    "anthropic",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"claude":   {ModelValue: "claude-3-5-haiku-20241022"},
			"opencode": {ModelValue: "anthropic/claude-3-5-haiku-20241022"},
		},
	},
	// Third-party models
	{
		ID:          "kimi-thinking",
		DisplayName: "kimi k2 thinking",
		Provider:    "moonshot",
		UsageURL:    "https://platform.moonshot.ai/console/account",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "kimi-thinking",
				Endpoint:        "https://api.moonshot.ai/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "moonshot/kimi-thinking"},
		},
	},
	{
		ID:          "kimi-k2.5",
		DisplayName: "kimi k2.5",
		Provider:    "moonshot",
		UsageURL:    "https://platform.moonshot.ai/console/account",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "kimi-k2.5",
				Endpoint:        "https://api.moonshot.ai/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "moonshot/kimi-k2.5"},
		},
	},
	{
		ID:          "glm-4.7",
		DisplayName: "glm 4.7",
		Provider:    "zai",
		UsageURL:    "https://z.ai/manage-apikey/subscription",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "glm-4.7",
				Endpoint:        "https://api.z.ai/api/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "zhipu/glm-4.7"},
		},
	},
	{
		ID:          "glm-4.5-air",
		DisplayName: "glm 4.5 air",
		Provider:    "zai",
		UsageURL:    "https://z.ai/manage-apikey/subscription",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "glm-4.5-air",
				Endpoint:        "https://api.z.ai/api/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "zhipu/glm-4.5-air"},
		},
	},
	{
		ID:          "glm-5",
		DisplayName: "glm 5",
		Provider:    "zai",
		UsageURL:    "https://z.ai/manage-apikey/subscription",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "glm-5",
				Endpoint:        "https://api.z.ai/api/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "zhipu/glm-5"},
		},
	},
	{
		ID:          "minimax-m2.1",
		DisplayName: "minimax m2.1",
		Provider:    "minimax",
		UsageURL:    "https://platform.minimax.io/user-center/payment/coding-plan",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "minimax-m2.1",
				Endpoint:        "https://api.minimax.io/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "minimax/minimax-m2.1"},
		},
	},
	{
		ID:          "minimax-2.5",
		DisplayName: "minimax m2.5",
		Provider:    "minimax",
		UsageURL:    "https://platform.minimax.io/user-center/payment/coding-plan",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "minimax-2.5",
				Endpoint:        "https://api.minimax.io/anthropic",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "minimax/minimax-2.5"},
		},
	},
	{
		ID:          "qwen3-coder-plus",
		DisplayName: "qwen 3 coder plus",
		Provider:    "dashscope",
		UsageURL:    "https://dashscope-intl.aliyuncs.com",
		Category:    "third-party",
		Runners: map[string]RunnerSpec{
			"claude": {
				ModelValue:      "qwen3-coder-plus",
				Endpoint:        "https://dashscope-intl.aliyuncs.com/api/v2/apps/claude-code-proxy",
				RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
			},
			"opencode": {ModelValue: "dashscope/qwen3-coder-plus"},
		},
	},
	// Codex models
	{
		ID:          "gpt-5.2-codex",
		DisplayName: "gpt 5.2 codex",
		Provider:    "openai",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"codex": {ModelValue: "gpt-5.2-codex"},
		},
	},
	{
		ID:          "gpt-5.3-codex",
		DisplayName: "gpt 5.3 codex",
		Provider:    "openai",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"codex": {ModelValue: "gpt-5.3-codex"},
		},
	},
	{
		ID:          "gpt-5.1-codex-max",
		DisplayName: "gpt 5.1 codex max",
		Provider:    "openai",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"codex": {ModelValue: "gpt-5.1-codex-max"},
		},
	},
	{
		ID:          "gpt-5.1-codex-mini",
		DisplayName: "gpt 5.1 codex mini",
		Provider:    "openai",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"codex": {ModelValue: "gpt-5.1-codex-mini"},
		},
	},
	// OpenCode models
	{
		ID:          "opencode-zen",
		DisplayName: "opencode zen (free)",
		Provider:    "opencode-zen",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"opencode": {ModelValue: ""},
		},
	},
	// Google models
	{
		ID:          "gemini-2.5-pro",
		DisplayName: "Gemini 2.5 Pro",
		Provider:    "google",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"gemini":   {ModelValue: "gemini-2.5-pro"},
			"opencode": {ModelValue: "google/gemini-2.5-pro"},
		},
	},
	{
		ID:          "gemini-2.5-flash",
		DisplayName: "Gemini 2.5 Flash",
		Provider:    "google",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"gemini":   {ModelValue: "gemini-2.5-flash"},
			"opencode": {ModelValue: "google/gemini-2.5-flash"},
		},
	},
	{
		ID:          "gemini-2.0-flash",
		DisplayName: "Gemini 2.0 Flash",
		Provider:    "google",
		Category:    "native",
		Runners: map[string]RunnerSpec{
			"gemini":   {ModelValue: "gemini-2.0-flash"},
			"opencode": {ModelValue: "google/gemini-2.0-flash"},
		},
	},
}

// GetBuiltinModels returns a copy of the built-in models.
func GetBuiltinModels() []Model {
	out := make([]Model, len(builtinModels))
	copy(out, builtinModels)
	return out
}

// legacyIDMigrations maps old model IDs to current vendor-defined IDs.
var legacyIDMigrations = map[string]string{
	"claude-opus":   "claude-opus-4-6",
	"claude-sonnet": "claude-sonnet-4-6",
	"claude-haiku":  "claude-haiku-4-5",
	"opus":          "claude-opus-4-6",
	"sonnet":        "claude-sonnet-4-6",
	"haiku":         "claude-haiku-4-5",
	"minimax":       "minimax-m2.1",
}

// MigrateModelID converts a legacy model ID to the current vendor-defined ID.
// Returns the input unchanged if it's not a legacy ID.
func MigrateModelID(id string) string {
	if newID, ok := legacyIDMigrations[id]; ok {
		return newID
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

// FindModel returns a built-in model by ID.
func FindModel(id string) (Model, bool) {
	id = MigrateModelID(id)
	for _, m := range builtinModels {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// IsModelID reports whether id matches a built-in model ID.
func IsModelID(id string) bool {
	_, ok := FindModel(id)
	return ok
}

// GetAvailableModels returns models where at least one runner's tool is detected.
func GetAvailableModels(detected []Tool) []Model {
	tools := make(map[string]bool, len(detected))
	for _, tool := range detected {
		tools[tool.Name] = true
	}

	var out []Model
	for _, m := range builtinModels {
		for toolName := range m.Runners {
			if tools[toolName] {
				out = append(out, m)
				break
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
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
