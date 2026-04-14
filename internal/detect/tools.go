package detect

import "github.com/sergeknystautas/schmux/internal/types"

// IsBuiltinToolName reports whether name matches a built-in detected tool.
func IsBuiltinToolName(name string) bool {
	return types.IsBuiltinToolName(name)
}

// AgentInstructionConfig defines how an agent receives instructions.
type AgentInstructionConfig struct {
	// InstructionDir is the subdirectory in the workspace (e.g., ".claude", ".codex")
	InstructionDir string
	// InstructionFile is the filename (e.g., "CLAUDE.md", "AGENTS.md")
	InstructionFile string
}

// agentInstructionConfigs maps tool names to their instruction file configuration.
var agentInstructionConfigs = map[string]AgentInstructionConfig{}

// GetAgentInstructionConfig returns the instruction configuration for a tool.
// Returns the config and true if found, empty config and false otherwise.
func GetAgentInstructionConfig(toolName string) (AgentInstructionConfig, bool) {
	config, ok := agentInstructionConfigs[toolName]
	return config, ok
}

// descriptorToolNames tracks tool names registered from YAML descriptors.
var descriptorToolNames []string

// IsToolName returns true if the name is any registered tool (builtin or descriptor).
func IsToolName(name string) bool {
	if IsBuiltinToolName(name) {
		return true
	}
	for _, n := range descriptorToolNames {
		if n == name {
			return true
		}
	}
	return false
}

func registerToolName(name string) {
	if !IsToolName(name) {
		descriptorToolNames = append(descriptorToolNames, name)
	}
}

func registerInstructionConfig(name string, cfg AgentInstructionConfig) {
	agentInstructionConfigs[name] = cfg
}
