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

func (a *GeminiAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if resume {
		return []string{"-r", "latest"}
	}
	if model != nil {
		if spec, ok := model.RunnerFor("gemini"); ok && spec.ModelValue != "" {
			return []string{"--model", spec.ModelValue}
		}
	}
	return nil
}

func (a *GeminiAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: oneshot mode with JSON schema is not supported")
}

func (a *GeminiAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool gemini: streaming oneshot mode is not supported")
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

func (a *GeminiAdapter) SupportsHooks() bool { return false }

func (a *GeminiAdapter) SetupHooks(ctx HookContext) error { return nil }

func (a *GeminiAdapter) CleanupHooks(workspacePath string) error { return nil }

func (a *GeminiAdapter) WrapRemoteCommand(command string) (string, error) { return command, nil }

func (a *GeminiAdapter) PersonaInjection() PersonaInjection { return PersonaInstructionFile }

func (a *GeminiAdapter) PersonaArgs(filePath string) []string { return nil }

func (a *GeminiAdapter) SpawnEnv(ctx SpawnContext) map[string]string { return nil }

func (a *GeminiAdapter) SetupCommands(workspacePath string) error { return nil }

func (a *GeminiAdapter) ModelFlag() string { return "--model" }

func (a *GeminiAdapter) Capabilities() []string {
	return []string{"interactive"}
}

func (a *GeminiAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	return map[string]string{}
}
