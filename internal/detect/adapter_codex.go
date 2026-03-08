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

func (a *CodexAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if resume {
		return []string{"resume", "--last"}
	}
	if model != nil {
		if spec, ok := model.RunnerFor("codex"); ok && spec.ModelValue != "" {
			return []string{"-m", spec.ModelValue}
		}
	}
	return nil
}

func (a *CodexAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"exec", "--json"}
	if model != nil {
		if spec, ok := model.RunnerFor("codex"); ok && spec.ModelValue != "" {
			args = append(args, "-m", spec.ModelValue)
		}
	}
	if jsonSchema != "" {
		args = append(args, "--output-schema", jsonSchema)
	}
	return args, nil
}

func (a *CodexAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool codex: oneshot-streaming mode is not supported")
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

func (a *CodexAdapter) SupportsHooks() bool { return false }

func (a *CodexAdapter) SetupHooks(ctx HookContext) error { return nil }

func (a *CodexAdapter) CleanupHooks(workspacePath string) error { return nil }

func (a *CodexAdapter) WrapRemoteCommand(command string) (string, error) { return command, nil }

func (a *CodexAdapter) PersonaInjection() PersonaInjection { return PersonaInstructionFile }

func (a *CodexAdapter) PersonaArgs(filePath string) []string { return nil }

func (a *CodexAdapter) SpawnEnv(ctx SpawnContext) map[string]string { return nil }

func (a *CodexAdapter) SetupCommands(workspacePath string) error { return nil }

func (a *CodexAdapter) InjectSkill(workspacePath string, skill SkillModule) error { return nil }

func (a *CodexAdapter) RemoveSkill(workspacePath string, skillName string) error { return nil }

func (a *CodexAdapter) ModelFlag() string { return "-m" }

func (a *CodexAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot"}
}

func (a *CodexAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	return map[string]string{}
}
