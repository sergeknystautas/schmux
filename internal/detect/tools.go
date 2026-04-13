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
var agentInstructionConfigs = map[string]AgentInstructionConfig{
	"claude":   {InstructionDir: ".claude", InstructionFile: "CLAUDE.md"},
	"codex":    {InstructionDir: ".codex", InstructionFile: "AGENTS.md"},
	"gemini":   {InstructionDir: ".gemini", InstructionFile: "GEMINI.md"},
	"opencode": {InstructionDir: ".opencode", InstructionFile: "AGENTS.md"},
}

// GetAgentInstructionConfig returns the instruction configuration for a tool.
// Returns the config and true if found, empty config and false otherwise.
func GetAgentInstructionConfig(toolName string) (AgentInstructionConfig, bool) {
	config, ok := agentInstructionConfigs[toolName]
	return config, ok
}
