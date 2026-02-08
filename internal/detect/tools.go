package detect

// Built-in detected tool names.
var builtinToolNames = []string{"claude", "codex", "gemini"}

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
	"claude": {InstructionDir: ".claude", InstructionFile: "CLAUDE.md"},
	"codex":  {InstructionDir: ".codex", InstructionFile: "AGENTS.md"},
	"gemini": {InstructionDir: ".gemini", InstructionFile: "GEMINI.md"},
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

// GetBaseToolName returns the base tool name for a given target.
// If targetName is a model, returns the model's BaseTool.
// If targetName is a tool, returns it directly.
// Returns empty string if not recognized.
func GetBaseToolName(targetName string) string {
	// Check if it's a direct tool name
	if IsBuiltinToolName(targetName) {
		return targetName
	}
	// Check if it's a model and get its base tool
	if model, ok := FindModel(targetName); ok {
		return model.BaseTool
	}
	return ""
}

// GetInstructionPathForTarget returns the instruction file path for a target.
// Works with both tool names (claude) and model names (claude-opus).
// Returns empty string if not recognized.
func GetInstructionPathForTarget(targetName string) string {
	baseTool := GetBaseToolName(targetName)
	if baseTool == "" {
		return ""
	}
	return GetInstructionPath(baseTool)
}

// GetAgentInstructionConfigForTarget returns the instruction config for a target.
// Works with both tool names (claude) and model names (claude-opus).
func GetAgentInstructionConfigForTarget(targetName string) (AgentInstructionConfig, bool) {
	baseTool := GetBaseToolName(targetName)
	if baseTool == "" {
		return AgentInstructionConfig{}, false
	}
	return GetAgentInstructionConfig(baseTool)
}
