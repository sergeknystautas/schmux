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
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode) ([]string, error) {
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
		newArgs = append(existingArgs, "-p")
	case "codex":
		newArgs = append(existingArgs, "exec", "--json")
	case "gemini":
		var filtered []string
		for _, arg := range existingArgs {
			if arg != "-i" {
				filtered = append(filtered, arg)
			}
		}
		newArgs = filtered
	default:
		return nil, fmt.Errorf("unknown tool: %s (supported: claude, codex, gemini)", toolName)
	}

	return append([]string{baseCmd}, newArgs...), nil
}
