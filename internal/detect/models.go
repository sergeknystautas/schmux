package detect

import (
	"sort"

	"github.com/sergeknystautas/schmux/internal/types"
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

// GetDefaultModels returns synthetic default models for all registered adapters.
// Each model uses the adapter's built-in default (no --model flag).
func GetDefaultModels() []Model {
	var out []Model
	for _, a := range AllAdapters() {
		name := a.Name()
		displayName := name + " (default)"
		if ga, ok := a.(*GenericAdapter); ok && ga.desc.DisplayName != "" {
			displayName = ga.desc.DisplayName + " (default)"
		}
		out = append(out, Model{
			ID:          name,
			DisplayName: displayName,
			Category:    "native",
			Runners:     map[string]RunnerSpec{name: {ModelValue: ""}},
		})
	}
	return out
}

// IsDefaultModel returns true if the model ID matches a registered adapter name.
func IsDefaultModel(id string) bool {
	return GetAdapter(id) != nil
}

// MigrateModelID converts a legacy model ID to the current vendor-defined ID.
// Returns the input unchanged if it's not a legacy ID.
func MigrateModelID(id string) string {
	return types.MigrateModelID(id)
}

// LegacyIDMigrations returns a copy of the legacy ID migration map.
func LegacyIDMigrations() map[string]string {
	return types.LegacyIDMigrations()
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
