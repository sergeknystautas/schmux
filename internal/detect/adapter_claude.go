package detect

import "context"

// ClaudeAdapter implements ToolAdapter for Claude Code.
type ClaudeAdapter struct{}

func init() { registerAdapter(&ClaudeAdapter{}) }

func (a *ClaudeAdapter) Name() string { return "claude" }

func (a *ClaudeAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&claudeDetector{}).Detect(ctx)
}

func (a *ClaudeAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *ClaudeAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"-p", "--dangerously-skip-permissions", "--output-format", "json"}
	if jsonSchema != "" {
		args = append(args, "--json-schema", jsonSchema)
	}
	return args, nil
}

func (a *ClaudeAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"-p", "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose"}
	if jsonSchema != "" {
		args = append(args, "--json-schema", jsonSchema)
	}
	return args, nil
}

func (a *ClaudeAdapter) ResumeArgs() []string {
	return []string{"--continue"}
}

func (a *ClaudeAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".claude", InstructionFile: "CLAUDE.md"}
}

func (a *ClaudeAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingHooks
}

func (a *ClaudeAdapter) SignalingArgs(filePath string) []string {
	return []string{"--append-system-prompt-file", filePath}
}
