package detect

import (
	"context"
	"fmt"
	"log"
)

// Variant represents an LLM provider variant.
// Variants redirect existing AI tools to alternative API endpoints via environment variables.
type Variant struct {
	Name            string            // e.g., "kimi-thinking"
	DisplayName     string            // e.g., "Kimi K2 Thinking"
	BaseTool        string            // e.g., "claude" - must be detected
	Env             map[string]string // template environment variables
	RequiredSecrets []string          // keys user must provide (e.g., API keys)
	UsageURL        string            // link to provider's usage dashboard
}

// builtinVariants is the registry of known LLM provider variants.
// All currently supported variants are based on the Claude Code CLI.
var builtinVariants = []Variant{
	{
		Name:        "kimi-thinking",
		DisplayName: "Kimi K2 Thinking",
		BaseTool:    "claude",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":           "https://api.moonshot.ai/anthropic",
			"ANTHROPIC_MODEL":              "kimi-k2-thinking-turbo",
			"ANTHROPIC_DEFAULT_OPUS_MODEL": "kimi-k2-thinking-turbo",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "kimi-k2-thinking-turbo",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "kimi-k2-thinking-turbo",
			"CLAUDE_CODE_SUBAGENT_MODEL":    "kimi-k2-thinking-turbo",
		},
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.moonshot.ai/console/account",
	},
	{
		Name:        "glm-4.7",
		DisplayName: "GLM 4.7",
		BaseTool:    "claude",
		Env: map[string]string{
			"ANTHROPIC_BASE_URL":           "https://api.z.ai/api/anthropic",
			"ANTHROPIC_MODEL":              "GLM-4.7",
			"ANTHROPIC_DEFAULT_OPUS_MODEL": "GLM-4.7",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "GLM-4.7",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "GLM-4.5-Air",
			"CLAUDE_CODE_SUBAGENT_MODEL":    "GLM-4.7",
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
			"ANTHROPIC_MODEL":                "MiniMax-M2.1",
			"ANTHROPIC_DEFAULT_OPUS_MODEL":   "MiniMax-M2.1",
			"ANTHROPIC_DEFAULT_SONNET_MODEL": "MiniMax-M2",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "MiniMax-M2.1-lightning",
			"CLAUDE_CODE_SUBAGENT_MODEL":     "MiniMax-M2.1",
		},
		RequiredSecrets: []string{"ANTHROPIC_AUTH_TOKEN"},
		UsageURL:        "https://platform.minimax.io/user-center/payment/coding-plan",
	},
}

// GetBuiltinVariants returns all built-in variants.
func GetBuiltinVariants() []Variant {
	return builtinVariants
}

// GetVariantByName returns a variant by name, or false if not found.
func GetVariantByName(name string) (Variant, bool) {
	for _, v := range builtinVariants {
		if v.Name == name {
			return v, true
		}
	}
	return Variant{}, false
}

// GetAvailableVariants returns variants whose base tool is available.
// It checks if the base tool exists in the provided list of detected agents.
func GetAvailableVariants(detectedAgents []Agent) []Variant {
	availableTools := make(map[string]bool)
	for _, agent := range detectedAgents {
		availableTools[agent.Name] = true
	}

	var available []Variant
	for _, variant := range builtinVariants {
		if availableTools[variant.BaseTool] {
			log.Printf("[detect] variant %s: available (base tool %s detected)", variant.Name, variant.BaseTool)
			available = append(available, variant)
		} else {
			log.Printf("[detect] variant %s: not available (base tool %s not detected)", variant.Name, variant.BaseTool)
		}
	}

	return available
}

// IsVariantName checks if a given name matches a known variant.
func IsVariantName(name string) bool {
	_, found := GetVariantByName(name)
	return found
}

// VariantResolver provides dependencies for variant resolution.
// This allows the detect package to resolve variants without direct dependencies
// on config or other packages.
type VariantResolver interface {
	// GetBaseToolCommand returns the command for the base tool.
	GetBaseToolCommand(baseTool string) (string, bool)
	// GetVariantSecrets returns user-provided secrets for a variant.
	GetVariantSecrets(variantName string) (map[string]string, error)
}

// ResolvedVariant contains the resolved command and environment variables for a variant.
type ResolvedVariant struct {
	Command string   // command with exports (e.g., "export FOO='bar' && export BAZ='qux' && claude")
	EnvVars []string // environment variables as KEY=value pairs for exec.Cmd
}

// ResolveVariant resolves a variant to its command and environment variables.
// It handles fetching the base tool command, loading secrets, and building the full command.
func ResolveVariant(ctx context.Context, variant Variant, resolver VariantResolver) (ResolvedVariant, error) {
	// Get base tool command
	baseCmd, found := resolver.GetBaseToolCommand(variant.BaseTool)
	if !found {
		return ResolvedVariant{}, fmt.Errorf("variant %s requires base tool %s which is not available", variant.Name, variant.BaseTool)
	}

	// Load user secrets
	secrets, err := resolver.GetVariantSecrets(variant.Name)
	if err != nil {
		return ResolvedVariant{}, fmt.Errorf("failed to load secrets for variant %s: %w", variant.Name, err)
	}

	// Validate required secrets
	for _, key := range variant.RequiredSecrets {
		if _, ok := secrets[key]; !ok {
			return ResolvedVariant{}, fmt.Errorf("missing required secret: %s", key)
		}
	}

	// Build command string with exports
	cmdStr := ""
	for key, value := range variant.Env {
		cmdStr += fmt.Sprintf("export %s='%s' && ", key, value)
	}
	for key, value := range secrets {
		cmdStr += fmt.Sprintf("export %s='%s' && ", key, value)
	}
	cmdStr += baseCmd

	// Build env vars slice for exec.Cmd
	var envVars []string
	for k, v := range variant.Env {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range secrets {
		envVars = append(envVars, fmt.Sprintf("%s=%s", k, v))
	}

	return ResolvedVariant{
		Command: cmdStr,
		EnvVars: envVars,
	}, nil
}

// ResolveVariantCommand returns the full command string for a variant.
// It includes environment variable exports followed by the base tool command.
// The secrets map should contain user-provided values for RequiredSecrets.
//
// Deprecated: Use ResolveVariant instead, which also returns environment variables
// for use with exec.Cmd.
func ResolveVariantCommand(ctx context.Context, variant Variant, baseToolCommand string, secrets map[string]string) (string, error) {
	// Check that all required secrets are provided
	for _, key := range variant.RequiredSecrets {
		if _, ok := secrets[key]; !ok {
			return "", fmt.Errorf("missing required secret: %s", key)
		}
	}

	// Build export string: "export KEY1='value1' && export KEY2='value2' && command"
	// Values are single-quoted to handle spaces and special characters
	cmdStr := ""
	for key, value := range variant.Env {
		cmdStr += fmt.Sprintf("export %s='%s' && ", key, value)
	}

	// Add user-provided secrets
	for key, value := range secrets {
		cmdStr += fmt.Sprintf("export %s='%s' && ", key, value)
	}

	// Add base tool command
	cmdStr += baseToolCommand

	return cmdStr, nil
}
