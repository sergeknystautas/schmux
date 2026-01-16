package detect

import "sort"

// Variant represents an LLM provider variant.
type Variant struct {
	Name            string
	DisplayName     string
	BaseTool        string
	Env             map[string]string
	RequiredSecrets []string
	UsageURL        string
}

var builtinVariants = []Variant{
	{
		Name:        "kimi-thinking",
		DisplayName: "Kimi K2 Thinking",
		BaseTool:    "claude",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":             "https://api.moonshot.ai/anthropic",
			"ANTHROPIC_MODEL":                "kimi-thinking",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   "kimi-thinking",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-thinking",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "kimi-thinking",
			"CLAUDE_CODE_SUBAGENT_MODEL":     "kimi-thinking",
		},
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.moonshot.ai/console/account",
	},
	{
		Name:        "glm-4.7",
		DisplayName: "GLM 4.7",
		BaseTool:    "claude",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":             "https://api.z.ai/api/anthropic",
			"ANTHROPIC_MODEL":                "glm-4.7",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-4.7",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-4.7",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-4.7",
			"CLAUDE_CODE_SUBAGENT_MODEL":     "glm-4.7",
		},
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://z.ai/manage-apikey/subscription",
	},
	{
		Name:        "minimax",
		DisplayName: "MiniMax M2.1",
		BaseTool:    "claude",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":             "https://api.minimax.io/anthropic",
			"ANTHROPIC_MODEL":                "minimax",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   "minimax",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "minimax",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "minimax",
			"CLAUDE_CODE_SUBAGENT_MODEL":     "minimax",
		},
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.minimax.io/user-center/payment/coding-plan",
	},
}

// GetBuiltinVariants returns a copy of the built-in variants.
func GetBuiltinVariants() []Variant {
	out := make([]Variant, len(builtinVariants))
	copy(out, builtinVariants)
	return out
}

// FindVariant returns a built-in variant by name.
func FindVariant(name string) (Variant, bool) {
	for _, v := range builtinVariants {
		if v.Name == name {
			return v, true
		}
	}
	return Variant{}, false
}

// IsVariantName reports whether name matches a built-in variant.
func IsVariantName(name string) bool {
	_, ok := FindVariant(name)
	return ok
}

// GetAvailableVariants returns variants whose base tool is detected.
func GetAvailableVariants(detected []Agent) []Variant {
	tools := make(map[string]bool, len(detected))
	for _, tool := range detected {
		tools[tool.Name] = true
	}

	var out []Variant
	for _, v := range builtinVariants {
		if tools[v.BaseTool] {
			out = append(out, v)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
