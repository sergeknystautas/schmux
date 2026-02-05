package detect

import (
	"sort"
	"strings"
)

// Provider constants
const (
	ProviderAnthropic = "anthropic"
	ProviderMoonshot  = "moonshot"
	ProviderZai       = "zai"
	ProviderMinimax   = "minimax"
	ProviderDashscope = "dashscope"
	ProviderOllama    = "ollama"
)

// Category constants
const (
	CategoryNative     = "native"
	CategoryThirdParty = "third-party"
	CategoryOllama     = "ollama"
)

// Model represents an AI model that can be used for spawning sessions.
type Model struct {
	ID              string   // e.g., "claude-sonnet", "kimi-thinking"
	DisplayName     string   // e.g., "claude sonnet 4.5", "Kimi K2 Thinking"
	BaseTool        string   // e.g., "claude" (the CLI tool to invoke), "ollama" for Ollama models
	Provider        string   // e.g., "anthropic", "moonshot", "zai", "minimax", "ollama"
	Endpoint        string   // API endpoint (empty = default Anthropic)
	ModelValue      string   // Value for ANTHROPIC_MODEL env var, or Ollama model name
	RequiredSecrets []string // e.g., ["ANTHROPIC_AUTH_TOKEN"] for third-party
	UsageURL        string   // Signup/pricing page
	Category        string   // "native", "third-party", or "ollama" (for UI grouping)
}

// BuildEnv builds the environment variables map for this model.
func (m Model) BuildEnv() map[string]string {
	env := map[string]string{
		"ANTHROPIC_MODEL": m.ModelValue,
	}
	if m.Endpoint != "" {
		env["ANTHROPIC_BASE_URL"] = m.Endpoint
		// Third-party models need all tier overrides
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m.ModelValue
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m.ModelValue
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m.ModelValue
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = m.ModelValue
	}
	return env
}

// builtinModels defines the canonical model IDs and display names exposed to the UI.
var builtinModels = []Model{
	// Native Claude models
	{
		ID:          "claude-opus",
		DisplayName: "claude opus 4.5",
		BaseTool:    "claude",
		Provider:    "anthropic",
		ModelValue:  "claude-opus-4-5-20251101",
		Category:    "native",
	},
	{
		ID:          "claude-sonnet",
		DisplayName: "claude sonnet 4.5",
		BaseTool:    "claude",
		Provider:    "anthropic",
		ModelValue:  "claude-sonnet-4-5-20250929",
		Category:    "native",
	},
	{
		ID:          "claude-haiku",
		DisplayName: "claude haiku 4.5",
		BaseTool:    "claude",
		Provider:    "anthropic",
		ModelValue:  "claude-haiku-4-5-20251001",
		Category:    "native",
	},
	// Third-party models
	{
		ID:              "kimi-thinking",
		DisplayName:     "kimi k2 thinking",
		BaseTool:        "claude",
		Provider:        "moonshot",
		Endpoint:        "https://api.moonshot.ai/anthropic",
		ModelValue:      "kimi-thinking",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.moonshot.ai/console/account",
		Category:        "third-party",
	},
	{
		ID:              "kimi-k2.5",
		DisplayName:     "kimi k2.5",
		BaseTool:        "claude",
		Provider:        "moonshot",
		Endpoint:        "https://api.moonshot.ai/anthropic",
		ModelValue:      "kimi-k2.5",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.moonshot.ai/console/account",
		Category:        "third-party",
	},
	{
		ID:              "glm-4.7",
		DisplayName:     "glm 4.7",
		BaseTool:        "claude",
		Provider:        "zai",
		Endpoint:        "https://api.z.ai/api/anthropic",
		ModelValue:      "glm-4.7",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://z.ai/manage-apikey/subscription",
		Category:        "third-party",
	},
	{
		ID:              "glm-4.5-air",
		DisplayName:     "glm 4.5 air",
		BaseTool:        "claude",
		Provider:        "zai",
		Endpoint:        "https://api.z.ai/api/anthropic",
		ModelValue:      "glm-4.5-air",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://z.ai/manage-apikey/subscription",
		Category:        "third-party",
	},
	{
		ID:              "minimax",
		DisplayName:     "minimax m2.1",
		BaseTool:        "claude",
		Provider:        "minimax",
		Endpoint:        "https://api.minimax.io/anthropic",
		ModelValue:      "minimax-m2.1",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.minimax.io/user-center/payment/coding-plan",
		Category:        "third-party",
	},
	{
		ID:              "qwen3-coder-plus",
		DisplayName:     "qwen 3 coder plus",
		BaseTool:        "claude",
		Provider:        "dashscope",
		Endpoint:        "https://dashscope-intl.aliyuncs.com/api/v2/apps/claude-code-proxy",
		ModelValue:      "qwen3-coder-plus",
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://dashscope-intl.aliyuncs.com",
		Category:        "third-party",
	},
}

// ollamaModels defines the Ollama models available when Ollama is enabled.
// These are not included in builtinModels by default - they are merged in dynamically.
var ollamaModels = []Model{
	{
		ID:          "ollama-falcon-h1",
		DisplayName: "falcon h1 tiny 90m",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "hf.co/tiiuae/Falcon-H1-Tiny-90M-Instruct-GGUF:Q8_0",
		Category:    CategoryOllama,
		UsageURL:    "https://huggingface.co/tiiuae/Falcon-H1-Tiny-90M-Instruct-GGUF",
	},
	{
		ID:          "ollama-llama3.3",
		DisplayName: "llama 3.3 70b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "llama3.3",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/llama3.3",
	},
	{
		ID:          "ollama-llama3.2",
		DisplayName: "llama 3.2 3b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "llama3.2",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/llama3.2",
	},
	{
		ID:          "ollama-qwen2.5-coder",
		DisplayName: "qwen 2.5 coder 7b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "qwen2.5-coder",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/qwen2.5-coder",
	},
	{
		ID:          "ollama-deepseek-coder-v2",
		DisplayName: "deepseek coder v2",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "deepseek-coder-v2",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/deepseek-coder-v2",
	},
	{
		ID:          "ollama-codellama",
		DisplayName: "code llama 7b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "codellama",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/codellama",
	},
	{
		ID:          "ollama-mistral",
		DisplayName: "mistral 7b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "mistral",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/mistral",
	},
	{
		ID:          "ollama-gemma2",
		DisplayName: "gemma 2 9b",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "gemma2",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/gemma2",
	},
	{
		ID:          "ollama-phi3",
		DisplayName: "phi 3 mini",
		BaseTool:    "ollama",
		Provider:    ProviderOllama,
		ModelValue:  "phi3",
		Category:    CategoryOllama,
		UsageURL:    "https://ollama.com/library/phi3",
	},
}

// modelAliases maps short aliases and old version IDs to current model IDs.
var modelAliases = map[string]string{
	"opus":         "claude-opus",
	"sonnet":       "claude-sonnet",
	"haiku":        "claude-haiku",
	"minimax-m2.1": "minimax", // backward compat for old ID
}

// GetBuiltinModels returns a copy of the built-in models (excluding Ollama models).
func GetBuiltinModels() []Model {
	out := make([]Model, len(builtinModels))
	copy(out, builtinModels)
	return out
}

// GetOllamaModels returns the list of available Ollama models.
func GetOllamaModels() []Model {
	out := make([]Model, len(ollamaModels))
	copy(out, ollamaModels)
	return out
}

// GetAllModels returns all models including Ollama models if ollamaEnabled is true.
func GetAllModels(ollamaEnabled bool) []Model {
	out := make([]Model, len(builtinModels))
	copy(out, builtinModels)
	if ollamaEnabled {
		out = append(out, ollamaModels...)
	}
	return out
}

// FindModel returns a built-in model by ID or alias.
// Set includeOllama to true to also search Ollama models.
func FindModel(id string) (Model, bool) {
	return FindModelWithOllama(id, false)
}

// FindModelWithOllama returns a model by ID or alias, optionally including Ollama models.
func FindModelWithOllama(id string, includeOllama bool) (Model, bool) {
	// Check for alias first
	if fullID, ok := modelAliases[id]; ok {
		id = fullID
	}
	for _, m := range builtinModels {
		if m.ID == id {
			return m, true
		}
	}
	if includeOllama {
		for _, m := range ollamaModels {
			if m.ID == id {
				return m, true
			}
		}
	}
	return Model{}, false
}

// IsModelID reports whether id matches a built-in model ID or alias.
func IsModelID(id string) bool {
	return IsModelIDWithOllama(id, false)
}

// IsModelIDWithOllama reports whether id matches a model ID or alias, optionally including Ollama.
func IsModelIDWithOllama(id string, includeOllama bool) bool {
	if _, ok := modelAliases[id]; ok {
		return true
	}
	_, ok := FindModelWithOllama(id, includeOllama)
	return ok
}

// IsOllamaModel returns true if the model ID is an Ollama model.
func IsOllamaModel(id string) bool {
	return strings.HasPrefix(id, "ollama-")
}

// GetAvailableModels returns models whose base tool is detected.
func GetAvailableModels(detected []Tool) []Model {
	tools := make(map[string]bool, len(detected))
	for _, tool := range detected {
		tools[tool.Name] = true
	}

	var out []Model
	for _, m := range builtinModels {
		if tools[m.BaseTool] {
			out = append(out, m)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}
