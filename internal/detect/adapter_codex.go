package detect

import (
	"context"
	"fmt"
)

// CodexAdapter implements ToolAdapter for OpenAI Codex.
type CodexAdapter struct{}

func init() { registerAdapter(&CodexAdapter{}) }

func (a *CodexAdapter) Name() string { return "codex" }

func (a *CodexAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&codexDetector{}).Detect(ctx)
}

func (a *CodexAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *CodexAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"exec", "--json"}
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		args = append(args, model.ModelFlag, model.ModelValue)
	}
	if jsonSchema != "" {
		args = append(args, "--output-schema", jsonSchema)
	}
	return args, nil
}

func (a *CodexAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool codex: oneshot-streaming mode is not supported")
}

func (a *CodexAdapter) ResumeArgs() []string {
	return []string{"resume", "--last"}
}

func (a *CodexAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".codex", InstructionFile: "AGENTS.md"}
}

func (a *CodexAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingCLIFlag
}

func (a *CodexAdapter) SignalingArgs(filePath string) []string {
	return []string{"-c", "model_instructions_file=" + filePath}
}
