package detect

import "context"

// SignalingStrategy defines how a tool receives schmux signaling instructions.
type SignalingStrategy int

const (
	// SignalingHooks means the tool uses lifecycle hooks (e.g., Claude's settings.local.json).
	SignalingHooks SignalingStrategy = iota
	// SignalingCLIFlag means signaling is injected via a CLI flag pointing to a file.
	SignalingCLIFlag
	// SignalingInstructionFile means signaling is appended to the tool's instruction file.
	SignalingInstructionFile
)

// ToolAdapter defines how a tool is detected, invoked, and configured.
// Each built-in tool (claude, codex, gemini, opencode) implements this interface.
type ToolAdapter interface {
	// Name returns the canonical tool name (e.g., "claude", "opencode").
	Name() string

	// Detect attempts to find the tool on the system.
	// Returns (tool, true) if found, (Tool{}, false) otherwise.
	Detect(ctx context.Context) (Tool, bool)

	// InteractiveArgs returns extra CLI args for interactive (TUI) mode.
	// The model parameter is optional.
	InteractiveArgs(model *Model) []string

	// OneshotArgs returns extra CLI args for non-interactive oneshot mode.
	// jsonSchema is the inline schema string (may be empty).
	OneshotArgs(model *Model, jsonSchema string) ([]string, error)

	// StreamingArgs returns extra CLI args for streaming oneshot mode.
	// jsonSchema is the inline schema string (may be empty).
	StreamingArgs(model *Model, jsonSchema string) ([]string, error)

	// ResumeArgs returns extra CLI args for resuming the last session.
	ResumeArgs() []string

	// InstructionConfig returns the instruction file location for this tool.
	InstructionConfig() AgentInstructionConfig

	// SignalingStrategy returns how this tool receives schmux signaling.
	SignalingStrategy() SignalingStrategy

	// SignalingArgs returns CLI args for injecting the signaling instructions file.
	// Only meaningful when SignalingStrategy() == SignalingCLIFlag.
	SignalingArgs(filePath string) []string
}

// adapters is the registry of all built-in tool adapters.
var adapters = map[string]ToolAdapter{}

// registerAdapter adds a tool adapter to the registry.
// Called from init() in each adapter_*.go file.
func registerAdapter(a ToolAdapter) {
	adapters[a.Name()] = a
}

// GetAdapter returns the adapter for the named tool, or nil if not found.
func GetAdapter(name string) ToolAdapter {
	return adapters[name]
}

// AllAdapters returns all registered adapters.
func AllAdapters() []ToolAdapter {
	out := make([]ToolAdapter, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, a)
	}
	return out
}
