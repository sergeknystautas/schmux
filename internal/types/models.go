package types

// LegacyModelIDMigrations maps old model IDs to current vendor-defined IDs.
var LegacyModelIDMigrations = map[string]string{
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
		next, ok := LegacyModelIDMigrations[id]
		if !ok {
			return id
		}
		id = next
	}
	return id
}

// LegacyIDMigrations returns a copy of the legacy ID migration map.
func LegacyIDMigrations() map[string]string {
	out := make(map[string]string, len(LegacyModelIDMigrations))
	for k, v := range LegacyModelIDMigrations {
		out[k] = v
	}
	return out
}
