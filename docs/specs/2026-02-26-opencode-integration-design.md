# OpenCode Integration Design

Add [opencode](https://opencode.ai) as the 4th tool in schmux's multi-agent ecosystem, alongside claude, codex, and gemini. Use this as an opportunity to refactor the tool spawning system from switch-statement sprawl into a clean adapter pattern.

## Context

### Current State

schmux detects and manages 3 built-in tools (claude, codex, gemini). Each tool has:

- A `ToolDetector` implementation in `internal/detect/agents.go`
- Instruction file mapping in `internal/detect/tools.go`
- Command building via switch statements in `internal/detect/commands.go`
- Signaling injection via tool-name checks in `internal/session/manager.go`
- Workspace setup in `internal/workspace/ensure/manager.go`

The command building logic (`BuildCommandParts`) is a growing switch statement where each tool has bespoke logic for interactive/oneshot/resume modes. Adding a 4th tool would make this worse.

### Pain Points

- `commands.go` has hardcoded switch/case per tool for every mode (interactive, oneshot, streaming, resume)
- `manager.go` has `appendSignalingFlags` and `appendPersonaFlags` with per-tool switches
- `ensure/manager.go` has `SupportsHooks` and `SupportsSystemPromptFlag` with hardcoded tool names
- Detection runs a hardcoded list of detectors, not a registry

### Claude-Specific Features (Phase 2 Promotion Candidates)

Significant functionality is currently Claude-only:

| Feature              | Claude                        | Codex                         | Gemini           | Notes                                   |
| -------------------- | ----------------------------- | ----------------------------- | ---------------- | --------------------------------------- |
| Lifecycle hooks      | 5 events                      | No                            | No               | `.claude/settings.local.json`           |
| Hook scripts         | 3 scripts                     | No                            | No               | capture-failure, stop-status, stop-lore |
| `/commit` skill      | Yes                           | No                            | No               | Definition-of-done enforcement          |
| CLI prompt injection | `--append-system-prompt-file` | `-c model_instructions_file=` | No               | Varies per tool                         |
| Persona injection    | CLI flag                      | Instruction file              | Instruction file |                                         |

OpenCode likely supports hooks, commands/skills, and prompt injection — making it a candidate for supporting most or all Claude-specific features.

### What is OpenCode?

OpenCode is an open-source AI coding agent (TypeScript/Bun) with a terminal TUI. Key characteristics:

- **Binary**: `opencode`
- **Install**: `curl -fsSL https://opencode.ai/install | bash`, brew, npm (`opencode-ai`)
- **Interactive**: `opencode` (TUI), `opencode --model provider/model`
- **Non-interactive**: `opencode run "prompt"`, `opencode run --model provider/model "prompt"`
- **Resume**: `opencode --continue`, `opencode --session <id>`
- **Model syntax**: `provider/model` (e.g., `anthropic/claude-sonnet-4-5`)
- **Config**: `opencode.json` (project root), `~/.config/opencode/opencode.json` (global)
- **Project dir**: `.opencode/` (commands, skills, tools, agents, themes)
- **Instruction files**: Reads `AGENTS.md` natively, falls back to `CLAUDE.md`
- **75+ providers** including Anthropic, OpenAI, Google, local (Ollama), and OpenCode Zen
- **Zen free tier**: Free models (Grok Code Fast 1, GLM 4.7, MiniMax M2.1, Big Pickle) via opencode.ai login

## Design

### Approach: ToolAdapter Interface Pattern

Replace scattered switch statements with a `ToolAdapter` interface. Each tool implements the interface. Detection, command building, and signaling are colocated per tool rather than scattered across files.

### Phase 1: Detection + Command Building Refactor + Basic Spawn

**Goal**: opencode is detected, spawnable with Zen free-tier model, and the command building mess is cleaned up.

#### ToolAdapter Interface (Phase 1 Scope)

```go
// ToolAdapter defines how a tool is detected, invoked, and configured.
type ToolAdapter interface {
    // Identity
    Name() string

    // Detection (replaces current ToolDetector)
    Detect(ctx context.Context) (Tool, bool)

    // Command modes (replaces BuildCommandParts switch)
    InteractiveArgs(model *Model) []string
    OneshotArgs(model *Model, jsonSchema string) ([]string, error)
    StreamingArgs(model *Model) ([]string, error)
    ResumeArgs() []string

    // Instruction & signaling (enough to spawn)
    InstructionConfig() AgentInstructionConfig
    SignalingStrategy() SignalingStrategy
    SignalingArgs(filePath string) []string
}

type SignalingStrategy int

const (
    SignalingHooks           SignalingStrategy = iota // Claude: settings.local.json hooks
    SignalingCLIFlag                                  // Codex: CLI flag for instruction file
    SignalingInstructionFile                          // Gemini/opencode: append to instruction file
)
```

#### Registry

```go
var registry = map[string]ToolAdapter{
    "claude":   &ClaudeAdapter{},
    "codex":    &CodexAdapter{},
    "gemini":   &GeminiAdapter{},
    "opencode": &OpencodeAdapter{},
}
```

Detection and command building use the registry — no more hardcoded lists or switch statements in callers.

#### OpencodeAdapter

**Detection** (4 methods, consistent with other tools):

1. PATH: `which opencode` + `opencode --version`
2. Native install: `~/.local/bin/opencode`
3. Homebrew formula: `brew list --formula | grep opencode`
4. npm global: `npm list -g opencode-ai`

**Command modes**:

- Interactive: `opencode [--model provider/model]`
- Oneshot: `opencode run [--model provider/model] [--format json] <prompt>`
- Resume: `opencode --continue`
- Streaming: TBD (may not be needed for phase 1)

**Instruction & signaling**:

- Instruction dir: `.opencode`, file: `AGENTS.md`
- Signaling: instruction file append (same as Gemini) — `<!-- SCHMUX:BEGIN -->` block

**Model** (one for phase 1):

```go
{
    ID:          "opencode-zen",
    DisplayName: "opencode zen (free)",
    BaseTool:    "opencode",
    Provider:    "opencode-zen",
    ModelValue:  "",  // uses opencode's default Zen model
    Category:    "native",
}
```

#### Simplified BuildCommandParts

The current 85-line switch-statement function becomes a thin dispatcher:

```go
func BuildCommandParts(adapter ToolAdapter, base string, mode ToolMode, jsonSchema string, model *Model) ([]string, error) {
    parts := strings.Fields(base)
    var modeArgs []string
    var err error
    switch mode {
    case ToolModeInteractive:
        modeArgs = adapter.InteractiveArgs(model)
    case ToolModeOneshot:
        modeArgs, err = adapter.OneshotArgs(model, jsonSchema)
    case ToolModeOneshotStreaming:
        modeArgs, err = adapter.StreamingArgs(model)
    case ToolModeResume:
        modeArgs = adapter.ResumeArgs()
    }
    if err != nil {
        return nil, err
    }
    return append(parts, modeArgs...), nil
}
```

#### File Organization

```
internal/detect/
  adapter.go           # ToolAdapter interface, registry, BuildCommandParts dispatcher
  adapter_claude.go    # ClaudeAdapter (detection + command modes)
  adapter_codex.go     # CodexAdapter
  adapter_gemini.go    # GeminiAdapter
  adapter_opencode.go  # OpencodeAdapter
  models.go            # Model definitions (unchanged for phase 1)
  tools.go             # AgentInstructionConfig type + helpers (kept, used by adapters)
  agents.go            # Shared utilities (tryCommand, fileExists, homebrew*, npm*)
  commands.go          # DELETED — logic moves into adapter files
```

### Phase 2: Capability Discovery + Abstraction Promotion

**Goal**: Map opencode's full capabilities. Promote Claude-only features into the ToolAdapter interface where opencode supports them.

#### Discovery Questions

- Does opencode have lifecycle hooks? (SessionStart, Stop, etc.)
- Does opencode support `--append-system-prompt-file` or similar CLI injection?
- Can `.opencode/commands/` host a `/commit` equivalent?
- How do `.opencode/skills/SKILL.md` files map to Claude's skills?
- What's opencode's hook/event system for signaling?

#### Interface Expansion (Based on Discovery)

```go
type ToolAdapter interface {
    // ... phase 1 methods ...

    // Phase 2: Capability reporting
    SupportsHooks() bool
    SupportsOneshotJSON() bool
    SupportsStreamingOneshot() bool

    // Phase 2: Lifecycle hooks
    BuildHooksConfig(workspacePath, hooksDir string) ([]byte, error)

    // Phase 2: Instruction ecosystem
    NativeInstructionFiles() []string
    GenerateWorkspaceConfig(workspace string) error

    // Phase 2: Persona/prompt injection
    PersonaArgs(filePath string) []string
}
```

#### Key Deliverable

Replace tool-name conditionals (`if baseTool == "claude"`) with capability checks (`if adapter.SupportsHooks()`). This makes the session manager, ensure package, and signaling pipeline tool-agnostic.

### Phase 3: Model Routing + OpenCode as Alternative Runner

**Goal**: opencode can execute models from any provider. Users configure "use opencode to run claude sonnet" instead of requiring native claude CLI.

#### Conceptual Model

Decouple "model" from "runner tool":

- A **model** says what it is (provider, model ID, capabilities)
- A **runner** says how to execute it (which CLI, what flags, what env)
- opencode becomes a universal runner since it speaks 75+ providers

#### Involves

- Rethinking model definitions (curated defaults + user-defined, not a growing hardcoded list)
- Runner preference config (per-model or per-provider routing)
- Auth/login management for opencode's provider connections
- UI changes — model dropdown no longer 1:1 with detected tools

This phase is deliberately underspecified — depends on phases 1-2 learnings.

## Migration Path

Phase 1 refactoring must not break existing tool behavior:

1. Write adapter implementations that produce identical output to current switch statements
2. Test by comparing old `BuildCommandParts` output vs new adapter output for all modes
3. Swap callers to use registry once adapters are verified
4. Delete `commands.go` and hardcoded detector lists

## Risks

- **OpenCode Zen availability**: Free tier models may change or require account setup. Phase 1 should gracefully handle "opencode detected but no model configured."
- **Refactoring scope**: Migrating 3 existing tools to adapters while adding a 4th is significant. Could split into "refactor existing" then "add opencode" if needed.
- **Phase 2 unknowns**: opencode's hook/command capabilities are unverified. Phase 2 starts with discovery, not implementation.
