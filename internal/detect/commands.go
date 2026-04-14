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
// Delegates to the tool's adapter for mode-specific argument construction.
// The jsonSchema parameter is used for oneshot mode (structured output).
// The model parameter is optional; if provided, used for model-specific flags.
func BuildCommandParts(toolName, detectedCommand string, mode ToolMode, jsonSchema string, model *Model) ([]string, error) {
	parts := strings.Fields(detectedCommand)
	if len(parts) == 0 {
		return nil, fmt.Errorf("tool %s: empty command", toolName)
	}

	adapter := GetAdapter(toolName)
	if adapter == nil {
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}

	var modeArgs []string
	var err error

	switch mode {
	case ToolModeInteractive:
		modeArgs = adapter.InteractiveArgs(model, false)
	case ToolModeOneshot:
		modeArgs, err = adapter.OneshotArgs(model, jsonSchema)
	case ToolModeResume:
		modeArgs = adapter.InteractiveArgs(nil, true)
	default:
		return nil, fmt.Errorf("tool %s: unknown mode %q", toolName, mode)
	}

	if err != nil {
		return nil, err
	}

	return append(parts, modeArgs...), nil
}
