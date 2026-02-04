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
)

// BuildCommandParts builds command parts for the given detected tool.
// The jsonSchema parameter is optional; if provided, it should be a JSON schema
// for structured output. For Claude, this is inline JSON; for Codex, a file path.
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode, jsonSchema string) ([]string, error) {
	parts := strings.Fields(detectedCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("tool %s: empty command", toolName)
	}

	if mode == ToolModeInteractive {
		return parts, nil
	}

	baseCmd := parts[0]
	existingArgs := parts[1:]

	var newArgs []string
	switch toolName {
	case "claude":
		newArgs = append(existingArgs, "-p", "--output-format", "json")
		if jsonSchema != "" {
			newArgs = append(newArgs, "--json-schema", jsonSchema)
		}
	case "codex":
		newArgs = append(existingArgs, "exec", "--json")
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
