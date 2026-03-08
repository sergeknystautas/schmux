package detect

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// OpencodeAdapter implements ToolAdapter for OpenCode.
type OpencodeAdapter struct{}

func init() { registerAdapter(&OpencodeAdapter{}) }

func (a *OpencodeAdapter) Name() string { return "opencode" }

func opencodeVersionCheck(ctx context.Context, command string) (string, error) {
	cmd := exec.CommandContext(ctx, command, "--version")
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func (a *OpencodeAdapter) Detect(ctx context.Context) (Tool, bool) {
	if pkgLogger != nil {
		if deadline, ok := ctx.Deadline(); ok {
			pkgLogger.Info("opencode detection started", "deadline", deadline.Format("2006-01-02T15:04:05.000Z07:00"))
		} else {
			pkgLogger.Info("opencode detection started", "deadline", "none")
		}
	}

	// Method 1: PATH lookup
	if commandExists("opencode") {
		if version, err := opencodeVersionCheck(ctx, "opencode"); err == nil {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via PATH", "command", "opencode", "version", version)
			}
			return Tool{Name: "opencode", Command: "opencode", Source: "PATH", Agentic: true}, true
		} else if pkgLogger != nil {
			pkgLogger.Info("opencode PATH candidate failed version probe", "command", "opencode", "err", err, "output", version, "ctx_err", ctx.Err())
		}
	} else if pkgLogger != nil {
		pkgLogger.Info("opencode not found on PATH")
	}

	// Method 2: Native install location (~/.local/bin/opencode)
	if fileExists("~/.local/bin/opencode") {
		cmd := filepath.Join(homeDirOrTilde(), ".local", "bin", "opencode")
		if version, err := opencodeVersionCheck(ctx, cmd); err == nil {
			if pkgLogger != nil {
				pkgLogger.Info("opencode found via native install", "command", cmd, "version", version)
			}
			return Tool{Name: "opencode", Command: cmd, Source: "native install (~/.local/bin/opencode)", Agentic: true}, true
		} else if pkgLogger != nil {
			pkgLogger.Info("opencode native candidate failed version probe", "command", cmd, "err", err, "output", version, "ctx_err", ctx.Err())
		}
	} else if pkgLogger != nil {
		pkgLogger.Info("opencode native install not found", "path", "~/.local/bin/opencode")
	}

	// Method 3: Homebrew formula
	brewInstalled := homebrewFormulaInstalled(ctx, "opencode")
	if brewInstalled {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via Homebrew formula", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "Homebrew formula opencode", Agentic: true}, true
	} else if pkgLogger != nil {
		pkgLogger.Info("opencode Homebrew formula not detected")
	}

	// Method 4: npm global
	npmInstalled := npmGlobalInstalled(ctx, "opencode-ai")
	if npmInstalled {
		if pkgLogger != nil {
			pkgLogger.Info("opencode found via npm global", "package", "opencode-ai", "command", "opencode")
		}
		return Tool{Name: "opencode", Command: "opencode", Source: "npm global package opencode-ai", Agentic: true}, true
	} else if pkgLogger != nil {
		pkgLogger.Info("opencode npm global package not detected", "package", "opencode-ai")
	}

	if pkgLogger != nil {
		pkgLogger.Info("opencode detection complete", "found", false)
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

func (a *OpencodeAdapter) InjectSkill(workspacePath string, skill SkillModule) error {
	dir := filepath.Join(workspacePath, ".opencode", "commands")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create commands dir: %w", err)
	}
	path := filepath.Join(dir, "schmux-"+skill.Name+".md")
	if err := os.WriteFile(path, []byte(skill.Content), 0644); err != nil {
		return fmt.Errorf("write skill file: %w", err)
	}
	return nil
}

func (a *OpencodeAdapter) RemoveSkill(workspacePath string, skillName string) error {
	path := filepath.Join(workspacePath, ".opencode", "commands", "schmux-"+skillName+".md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove skill file: %w", err)
	}
	return nil
}

func (a *OpencodeAdapter) Capabilities() []string {
	return []string{"interactive", "oneshot"}
}

func (a *OpencodeAdapter) BuildRunnerEnv(spec RunnerSpec) map[string]string {
	return map[string]string{}
}
