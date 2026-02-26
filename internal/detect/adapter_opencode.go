package detect

import (
	"context"
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

func (a *OpencodeAdapter) InteractiveArgs(model *Model) []string {
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		return []string{model.ModelFlag, model.ModelValue}
	}
	return nil
}

func (a *OpencodeAdapter) OneshotArgs(model *Model, jsonSchema string) ([]string, error) {
	args := []string{"run"}
	if model != nil && model.ModelFlag != "" && model.ModelValue != "" {
		args = append(args, model.ModelFlag, model.ModelValue)
	}
	if jsonSchema != "" {
		args = append(args, "--format", "json")
	}
	return args, nil
}

func (a *OpencodeAdapter) StreamingArgs(model *Model, jsonSchema string) ([]string, error) {
	return nil, fmt.Errorf("tool opencode: streaming oneshot mode is not yet supported")
}

func (a *OpencodeAdapter) ResumeArgs() []string {
	return []string{"--continue"}
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
