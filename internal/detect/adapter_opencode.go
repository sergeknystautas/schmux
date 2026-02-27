package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// OpencodeAdapter implements ToolAdapter for OpenCode.
type OpencodeAdapter struct{}

func init() { registerAdapter(&OpencodeAdapter{}) }

func (a *OpencodeAdapter) Name() string { return "opencode" }

func (a *OpencodeAdapter) Detect(ctx context.Context) (Tool, bool) {
	// Method 1: PATH lookup
	if commandExists("opencode") {
		if tryCommand(ctx, "opencode", "--version") {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via PATH", "command", "opencode")
			}
			return Tool{Name: "opencode", Command: "opencode", Source: "PATH", Agentic: true}, true
		}
	}

	// Method 2: Native install location (~/.local/bin/opencode)
	if fileExists("~/.local/bin/opencode") {
		cmd := filepath.Join(homeDirOrTilde(), ".local", "bin", "opencode")
		if tryCommand(ctx, cmd, "--version") {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via native install", "command", cmd)
			}
			return Tool{Name: "opencode", Command: cmd, Source: "native install (~/.local/bin/opencode)", Agentic: true}, true
		}
	}

	// Method 3: Homebrew formula
	if homebrewFormulaInstalled(ctx, "opencode") {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via Homebrew formula", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "Homebrew formula opencode", Agentic: true}, true
	}

	// Method 4: npm global
	if npmGlobalInstalled(ctx, "opencode-ai") {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via npm global", "package", "opencode-ai", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "npm global package opencode-ai", Agentic: true}, true
	}

	return Tool{}, false
}

func (a *OpencodeAdapter) InteractiveArgs(model *Model, resume bool) []string {
	if resume {
		return []string{"--continue"}
	}
	if model != nil {
		if spec, ok := model.RunnerFor("opencode"); ok && spec.ModelValue != "" {
			return []string{"--model", spec.ModelValue}
		}
	}
	return nil
}

func (a *OpencodeAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"run"}
	if model != nil {
		if spec, ok := model.RunnerFor("opencode"); ok && spec.ModelValue != "" {
			args = append(args, "--model", spec.ModelValue)
		}
	}
	if jsonSchema != "" {
		args = append(args, "--format", "json")
	}
	return args, nil
}

func (a *OpencodeAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool opencode: streaming oneshot mode is not yet supported")
}

func (a *OpencodeAdapter) InstructionConfig() AgentInstructionConfig {
	return AgentInstructionConfig{InstructionDir: ".opencode", InstructionFile: "AGENTS.md"}
}

func (a *OpencodeAdapter) SignalingStrategy() SignalingStrategy {
	return SignalingInstructionFile
}

func (a *OpencodeAdapter) SignalingArgs(filePath string) []string {
	return nil
}

func (a *OpencodeAdapter) SupportsHooks() bool { return true }

func (a *OpencodeAdapter) SetupHooks(ctx HookContext) error {
	return opencodeSetupHooks(ctx.WorkspacePath)
}

func (a *OpencodeAdapter) CleanupHooks(workspacePath string) error {
	return opencodeCleanupHooks(workspacePath)
}

func (a *OpencodeAdapter) WrapRemoteCommand(command string) (string, error) {
	return opencodeWrapRemoteCommand(command)
}

func (a *OpencodeAdapter) PersonaInjection() PersonaInjection { return PersonaConfigOverlay }

func (a *OpencodeAdapter) PersonaArgs(filePath string) []string { return nil }

func (a *OpencodeAdapter) SpawnEnv(ctx SpawnContext) map[string]string {
	if ctx.PersonaPath == "" {
		return nil
	}
	cfg := map[string]interface{}{
		"instructions": []string{ctx.PersonaPath},
	}
	jsonBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	return map[string]string{
		"OPENCODE_CONFIG_CONTENT": string(jsonBytes),
	}
}

func (a *OpencodeAdapter) ModelFlag() string { return "--model" }

func (a *OpencodeAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	return map[string]string{}
}
