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

// PersonaInjection defines how a tool receives persona/system-prompt overrides.
type PersonaInjection int

const (
	// PersonaCLIFlag means the persona is injected via a CLI flag (e.g., Claude's --append-system-prompt-file).
	PersonaCLIFlag PersonaInjection = iota
	// PersonaInstructionFile means the persona is appended to the tool's instruction file (Codex/Gemini).
	PersonaInstructionFile
	// PersonaConfigOverlay means the persona is injected via a config env var (OpenCode's OPENCODE_CONFIG_CONTENT).
	PersonaConfigOverlay
)

// HookContext provides context for setting up tool-specific lifecycle hooks.
type HookContext struct {
	WorkspacePath string
	HooksDir      string // ~/.schmux/hooks/
	SessionID     string
	WorkspaceID   string
}

// SpawnContext provides context for spawning a session with persona and workspace info.
type SpawnContext struct {
	WorkspacePath string
	SessionID     string
	PersonaPath   string // path to persona markdown file
}

// ToolAdapter defines how a tool is detected, invoked, and configured.
// Each built-in tool (claude, codex, gemini, opencode) implements this interface.
type ToolAdapter interface {
	// Name returns the canonical tool name (e.g., "claude", "opencode").
	Name() string

	// Detect attempts to find the tool on the system.
	// Returns (tool, true) if found, (Tool{}, false) otherwise.
	Detect(ctx context.Context) (Tool, bool)

	// InteractiveArgs returns extra CLI args for interactive (TUI) mode.
	// The model parameter is optional. When resume is true, returns resume
	// flags instead of model flags (resume is a flavor of interactive).
	InteractiveArgs(model *Model, resume bool) []string

	// OneshotArgs returns extra CLI args for non-interactive oneshot mode.
	// jsonSchema is the inline schema string (may be empty).
	OneshotArgs(model *Model, jsonSchema string) ([]string, error)

	// StreamingArgs returns extra CLI args for streaming oneshot mode.
	// jsonSchema is the inline schema string (may be empty).
	StreamingArgs(model *Model, jsonSchema string) ([]string, error)

	// InstructionConfig returns the instruction file location for this tool.
	InstructionConfig() AgentInstructionConfig

	// SignalingStrategy returns how this tool receives schmux signaling.
	SignalingStrategy() SignalingStrategy

	// SignalingArgs returns CLI args for injecting the signaling instructions file.
	// Only meaningful when SignalingStrategy() == SignalingCLIFlag.
	SignalingArgs(filePath string) []string

	// SupportsHooks returns whether this tool supports lifecycle hooks
	// (e.g., pre/post-session scripts via settings or config files).
	SupportsHooks() bool

	// SetupHooks installs tool-specific lifecycle hooks into the workspace.
	SetupHooks(ctx HookContext) error

	// CleanupHooks removes any lifecycle hooks previously installed in the workspace.
	CleanupHooks(workspacePath string) error

	// WrapRemoteCommand wraps a command string for remote execution if needed.
	WrapRemoteCommand(command string) (string, error)

	// PersonaInjection returns how this tool receives persona/system-prompt overrides.
	PersonaInjection() PersonaInjection

	// PersonaArgs returns CLI args for injecting a persona file.
	// Only meaningful when PersonaInjection() == PersonaCLIFlag.
	PersonaArgs(filePath string) []string

	// SpawnEnv returns additional environment variables needed when spawning a session.
	SpawnEnv(ctx SpawnContext) map[string]string

	// SetupCommands runs any tool-specific setup commands in the workspace
	// before the agent session starts (e.g., writing config files).
	SetupCommands(workspacePath string) error

	// BuildRunnerEnv constructs environment variables for running a model with this tool.
	BuildRunnerEnv(spec RunnerSpec) map[string]string

	// ModelFlag returns the CLI flag this tool uses for model selection.
	// Returns empty string if the tool doesn't use a CLI flag.
	ModelFlag() string
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
