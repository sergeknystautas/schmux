package detect

// Built-in detected tool names.
var builtinToolNames = []string{"claude", "codex", "gemini", "opencode"}

// GetBuiltinToolNames returns the built-in tool names.
func GetBuiltinToolNames() []string {
	out := make([]string, len(builtinToolNames))
	copy(out, builtinToolNames)
	return out
}

// IsBuiltinToolName reports whether name matches a built-in detected tool.
func IsBuiltinToolName(name string) bool {
	for _, tool := range builtinToolNames {
		if tool == name {
			return true
		}
	}
	return false
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

// GetInstructionPath returns the relative path to the instruction file for a tool.
// Returns empty string if the tool is not recognized.
func GetInstructionPath(toolName string) string {
	config, ok := agentInstructionConfigs[toolName]
	if !ok {
		return ""
	}
	return config.InstructionDir + "/" + config.InstructionFile
}
