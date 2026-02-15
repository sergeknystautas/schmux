package detect

import (
	"fmt"
	"strings"
)

// ToolMode represents how to invoke a detected tool.
type ToolMode string

const (
	ToolModeInteractive ToolMode = "interactive"
	ToolModeOneshot     ToolMode = "oneshot"
	ToolModeResume      ToolMode = "resume"
)

// BuildCommandParts builds command parts for the given detected tool.
// The jsonSchema parameter is required for oneshot mode (structured output);
// it is ignored for interactive and resume modes.
// The model parameter is optional; if provided, it will be used to inject model-specific flags.
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode, jsonSchema string, model *Model) ([]string, error) {
	parts := strings.Fields(detectedCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("tool %s: empty command", toolName)
	}

	if mode == ToolModeInteractive {
		// For interactive mode, inject model flag if specified
		if model != nil && model.ModelFlag != "" {
			parts = append(parts, model.ModelFlag, model.ModelValue)
		}
		return parts, nil
	}

	if mode == ToolModeResume {
		// Return resume command for each tool
		switch toolName {
		case "claude":
			return []string{"claude", "--continue"}, nil
		case "codex":
			return []string{"codex", "resume", "--last"}, nil
		case "gemini":
			return []string{"gemini", "-r", "latest"}, nil
		default:
			return nil, fmt.Errorf("tool %s: resume mode not supported", toolName)
		}
	}

	baseCmd := parts[0]
	existingArgs := parts[1:]

	var newArgs []string
	switch toolName {
	case "claude":
		newArgs = append(existingArgs, "-p", "--dangerously-skip-permissions", "--output-format", "json")
		if jsonSchema != "" {
			newArgs = append(newArgs, "--json-schema", jsonSchema)
		}
	case "codex":
		newArgs = append(existingArgs, "exec", "--json")
		// Inject model flag for Codex if specified
		if model != nil && model.ModelFlag != "" {
			newArgs = append(newArgs, model.ModelFlag, model.ModelValue)
		}
		if jsonSchema != "" {
			newArgs = append(newArgs, "--output-schema", jsonSchema)
		}
	case "gemini":
		// Gemini does not support structured output via JSON schema
		return nil, fmt.Errorf("tool %s: oneshot mode with JSON schema is not supported (supported: claude, codex)", toolName)
	default:
		return nil, fmt.Errorf("unknown tool: %s (supported: claude, codex)", toolName)
	}

	return append([]string{baseCmd}, newArgs...), nil
}
