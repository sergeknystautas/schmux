package detect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ClaudeAdapter implements ToolAdapter for Claude Code.
type ClaudeAdapter struct{}

func init() { registerAdapter(&ClaudeAdapter{}) }

func (a *ClaudeAdapter) Name() string { return "claude" }

func (a *ClaudeAdapter) Detect(ctx context.Context) (Tool, bool) {
	return (&claudeDetector{}).Detect(ctx)
}

func (a *ClaudeAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if resume {
		return []string{"--continue"}
	}
	if model != nil {
		if spec, ok := model.RunnerFor("claude"); ok && spec.ModelValue != "" {
			return []string{"--model", spec.ModelValue}
		}
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

func (a *ClaudeAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".claude", InstructionFile: "CLAUDE.md"}
}

func (a *ClaudeAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingHooks
}

func (a *ClaudeAdapter) SignalingArgs(filePath string) []string {
	return []string{"--append-system-prompt-file", filePath}
}

func (a *ClaudeAdapter) SupportsHooks() bool { return true }

func (a *ClaudeAdapter) SetupHooks(ctx HookContext) error {
	return claudeSetupHooks(ctx.WorkspacePath, ctx.HooksDir)
}

func (a *ClaudeAdapter) CleanupHooks(workspacePath string) error {
	return claudeCleanupHooks(workspacePath)
}

func (a *ClaudeAdapter) WrapRemoteCommand(command string) (string, error) {
	return claudeWrapRemoteCommand(command)
}

func (a *ClaudeAdapter) PersonaInjection() PersonaInjection { return PersonaCLIFlag }

func (a *ClaudeAdapter) PersonaArgs(filePath string) []string {
	if filePath == "" {
		return nil
	}
	return []string{"--append-system-prompt-file", filePath}
}

func (a *ClaudeAdapter) SpawnEnv(ctx SpawnContext) map[string]string { return nil }

func (a *ClaudeAdapter) SetupCommands(workspacePath string) error { return nil }

func (a *ClaudeAdapter) InjectSkill(workspacePath string, skill SkillModule) error {
	dir := filepath.Join(workspacePath, ".claude", "skills", "schmux-"+skill.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create skill dir: %w", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(skill.Content), 0644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	return nil
}

func (a *ClaudeAdapter) RemoveSkill(workspacePath string, skillName string) error {
	dir := filepath.Join(workspacePath, ".claude", "skills", "schmux-"+skillName)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove skill dir: %w", err)
	}
	return nil
}

func (a *ClaudeAdapter) ModelFlag() string { return "--model" }

func (a *ClaudeAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot", "streaming"}
}

func (a *ClaudeAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	env := map[string]string{}
	if spec.Endpoint != "" {
		env["ANTHROPIC_MODEL"] = spec.ModelValue
		env["ANTHROPIC_BASE_URL"] = spec.Endpoint
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = spec.ModelValue
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = spec.ModelValue
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = spec.ModelValue
		env["CLAUDE_CODE_SUBAGENT_MODEL"] = spec.ModelValue
	}
	return env
}
