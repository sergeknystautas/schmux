package detect

import (
	"context"
	"fmt"
)

// GeminiAdapter implements ToolAdapter for Google Gemini CLI.
type GeminiAdapter struct{}

func init() { registerAdapter(&GeminiAdapter{}) }

func (a *GeminiAdapter) Name() string { return "gemini" }

func (a *GeminiAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&geminiDetector{}).Detect(ctx)
}

func (a *GeminiAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *GeminiAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: oneshot mode with JSON schema is not supported")
}

func (a *GeminiAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: streaming oneshot mode is not supported")
}

func (a *GeminiAdapter) ResumeArgs() []string {
	return []string{"-r", "latest"}
}

func (a *GeminiAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".gemini", InstructionFile: "GEMINI.md"}
}

func (a *GeminiAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingInstructionFile
}

func (a *GeminiAdapter) SignalingArgs(filePath string) []string {
	return nil
}
