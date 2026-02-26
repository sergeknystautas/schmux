# OpenCode Phase 2: Capability Abstraction & Hook Parity

## Context

Phase 1 (complete) introduced the `ToolAdapter` interface pattern with 9 methods covering detection, command building, and signaling. OpenCode is detected and spawnable with the Zen free-tier model. The command building switch-statement sprawl in `commands.go` was eliminated.

Phase 2 promotes Claude-only features into tool-agnostic abstractions and implements full hook parity for OpenCode.

### What's Still Claude-Only

| Feature           | How It Works Today                                                                    | Why It's a Problem                                                                         |
| ----------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| Lifecycle hooks   | `.claude/settings.local.json` with 6 events + 3 shell scripts                         | `ClaudeHooks()`, `buildClaudeHooksMap()`, `WrapCommandWithHooks()` all hardcoded to Claude |
| Persona injection | `appendPersonaFlags()` has `case "claude"` switch using `--append-system-prompt-file` | Other tools use instruction file append; OpenCode needs env var overlay                    |
| `/commit` command | `.claude/commands/commit.md` checked into repo                                        | OpenCode supports `.opencode/commands/` but has no commit command                          |
| Resume mode       | `ResumeArgs()` is a separate method                                                   | Resume is a flavor of interactive; should be merged                                        |

### OpenCode's Capabilities (Discovery Results)

| Capability              | OpenCode Support                                                                      |
| ----------------------- | ------------------------------------------------------------------------------------- |
| Lifecycle hooks         | Rich plugin system: `.opencode/plugins/*.ts` with 25+ events                          |
| Stop gating             | `stop` hook: can block termination, inject continuation via `client.session.prompt()` |
| System prompt transform | `experimental.chat.system.transform` plugin hook                                      |
| Commands                | `.opencode/commands/*.md` with description/agent/model frontmatter                    |
| Skills                  | `.opencode/skills/NAME/SKILL.md` (also reads `.claude/skills/`)                       |
| Config                  | `opencode.json` with `instructions` field for additional prompt files                 |
| Config overlay          | `OPENCODE_CONFIG_CONTENT` env var merges with project config                          |
| Session management      | `--session ID`, `--continue`, `--fork`                                                |
| Environment             | `OPENCODE=1`, `AGENT=1` injected into subprocesses                                    |

## Design

### ToolAdapter Interface Expansion

Phase 1 has 9 methods. Phase 2 adds 8, removes 1 (ResumeArgs merged into InteractiveArgs):

```go
type ToolAdapter interface {
    // --- Identity & Detection (Phase 1) ---
    Name() string
    Detect(ctx context.Context) (Tool, bool)

    // --- Command Building (Phase 1, resume merged in Phase 2) ---
    InteractiveArgs(model *Model, resume bool) []string
    OneshotArgs(model *Model, jsonSchema string) ([]string, error)
    StreamingArgs(model *Model, jsonSchema string) ([]string, error)
    // ResumeArgs() REMOVED — merged into InteractiveArgs(_, true)

    // --- Instruction & Signaling (Phase 1) ---
    InstructionConfig() AgentInstructionConfig
    SignalingStrategy() SignalingStrategy
    SignalingArgs(filePath string) []string

    // --- Hooks (Phase 2) ---
    SupportsHooks() bool
    SetupHooks(ctx HookContext) error
    CleanupHooks(workspacePath string) error
    WrapRemoteCommand(command string) (string, error)

    // --- Persona (Phase 2) ---
    PersonaInjection() PersonaInjection
    PersonaArgs(filePath string) []string
    SpawnEnv(ctx SpawnContext) map[string]string

    // --- Commands (Phase 2) ---
    SetupCommands(workspacePath string) error
}
```

**New types:**

```go
type PersonaInjection int

const (
    PersonaCLIFlag         PersonaInjection = iota // Claude: --append-system-prompt-file
    PersonaInstructionFile                          // Codex/Gemini: append to instruction file
    PersonaConfigOverlay                            // OpenCode: OPENCODE_CONFIG_CONTENT env var
)

type HookContext struct {
    WorkspacePath string
    HooksDir      string // ~/.schmux/hooks/
    SessionID     string
    WorkspaceID   string
}

type SpawnContext struct {
    WorkspacePath string
    SessionID     string
    PersonaPath   string // .schmux/persona-<sessionID>.md
}
```

### Resume Merged into InteractiveArgs

`ResumeArgs()` is removed. `InteractiveArgs(model, resume)` handles both:

| Tool     | `InteractiveArgs(model, false)`   | `InteractiveArgs(nil, true)` |
| -------- | --------------------------------- | ---------------------------- |
| Claude   | `["--model", "sonnet"]`           | `["--continue"]`             |
| Codex    | `["--model", "o3"]`               | `["resume", "--last"]`       |
| Gemini   | `["--model", "2.5-pro"]`          | `["-r", "latest"]`           |
| OpenCode | `["--model", "anthropic/sonnet"]` | `["--continue"]`             |

`BuildCommandParts` update: `ToolModeResume` calls `adapter.InteractiveArgs(nil, true)`.

### Per-Adapter Support Matrix

| Method            | Claude                                       | Codex           | Gemini          | OpenCode                              |
| ----------------- | -------------------------------------------- | --------------- | --------------- | ------------------------------------- |
| SupportsHooks     | true                                         | false           | false           | true                                  |
| SetupHooks        | .claude/settings.local.json + shell scripts  | no-op           | no-op           | .opencode/plugins/schmux.ts           |
| CleanupHooks      | remove schmux hooks from settings.local.json | no-op           | no-op           | remove .opencode/plugins/schmux.ts    |
| WrapRemoteCommand | inline JSON creation                         | pass-through    | pass-through    | inline plugin file creation           |
| PersonaInjection  | CLIFlag                                      | InstructionFile | InstructionFile | ConfigOverlay                         |
| PersonaArgs       | --append-system-prompt-file PATH             | nil             | nil             | nil                                   |
| SpawnEnv          | nil                                          | nil             | nil             | OPENCODE_CONFIG_CONTENT               |
| SetupCommands     | no-op (checked in)                           | no-op           | no-op           | generate .opencode/commands/commit.md |

### OpenCode Hook Plugin

Schmux generates `.opencode/plugins/schmux.ts` — a single TypeScript file that replicates all 3 Claude hook scripts.

**Event mapping:**

| Claude Hook Event         | Script                                    | OpenCode Plugin Hook               | Behavior                                                                  |
| ------------------------- | ----------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------- |
| SessionStart              | inline                                    | `event` → `session.created`        | Emit `{type:"status", state:"working"}`                                   |
| SessionEnd                | inline                                    | `event` → `session.idle`           | Emit `{type:"status", state:"completed"}`                                 |
| UserPromptSubmit          | inline                                    | `event` → `message.updated` (user) | Emit `{type:"status", state:"working", intent:"..."}`                     |
| Stop                      | stop-status-check.sh + stop-lore-check.sh | `stop` hook                        | Check events file for status + reflection, inject continuation if missing |
| Notification (permission) | inline                                    | `event` → `permission.asked`       | Emit `{type:"status", state:"needs_input"}`                               |
| PostToolUseFailure        | capture-failure.sh                        | `event` → `tool.execute.after`     | Classify error, emit `{type:"failure", tool, error, category}`            |

**Plugin structure:**

```typescript
import type { Plugin } from '@opencode-ai/core';

export const SchmuxPlugin: Plugin = async ({ $ }) => {
  const eventsFile = process.env.SCHMUX_EVENTS_FILE;
  if (!eventsFile) return {};

  const appendEvent = (event: Record<string, unknown>) => {
    const line = JSON.stringify({ ts: new Date().toISOString(), ...event });
    Bun.write(Bun.file(eventsFile), line + '\n', { append: true });
  };

  return {
    event: async ({ event }) => {
      // Map OpenCode events to schmux JSONL events
      // session.created → status:working
      // session.idle → status:completed
      // permission.asked → status:needs_input
      // tool.execute.after (with error) → failure event with classification
      // message.updated (user role) → status:working with intent
    },
    stop: async ({ input }) => {
      // Read events file, check for status + reflection
      // If missing, return { result: "prevent", message: "..." }
      // Uses client.session.prompt() to inject continuation
    },
  };
};
```

**Environment variables injected at spawn:**

- `SCHMUX_ENABLED=1`
- `SCHMUX_SESSION_ID=<sessionID>`
- `SCHMUX_WORKSPACE_ID=<workspaceID>`
- `SCHMUX_EVENTS_FILE=<workspacePath>/.schmux/events/<sessionID>.jsonl`

These are the same env vars Claude hooks receive, ensuring the JSONL event format is identical.

### Persona Injection Refactoring

**Current:** `appendPersonaFlags()` has hardcoded `case "claude"` switch.

**New flow:**

1. Session manager calls `adapter.PersonaInjection()` to get strategy
2. Dispatch by strategy:
   - `PersonaCLIFlag` (Claude): `adapter.PersonaArgs(path)` → append to command
   - `PersonaInstructionFile` (Codex/Gemini): no-op at spawn — ensure package already appends persona to instruction file
   - `PersonaConfigOverlay` (OpenCode): `adapter.SpawnEnv(ctx)` → merge into process environment

**OpenCode SpawnEnv:**

```go
func (a *OpencodeAdapter) SpawnEnv(ctx SpawnContext) map[string]string {
    if ctx.PersonaPath == "" {
        return nil
    }
    cfg := map[string]interface{}{
        "instructions": []string{ctx.PersonaPath},
    }
    jsonBytes, _ := json.Marshal(cfg)
    return map[string]string{
        "OPENCODE_CONFIG_CONTENT": string(jsonBytes),
    }
}
```

This injects `OPENCODE_CONFIG_CONTENT={"instructions":[".schmux/persona-abc123.md"]}` into the spawned process without touching the user's `opencode.json`.

### /commit Command for OpenCode

The ensure package generates `.opencode/commands/commit.md` during workspace setup. Content matches Claude's definition-of-done logic with OpenCode-compatible frontmatter:

```markdown
---
description: Create a git commit with definition-of-done enforcement
---

[same commit workflow: test → vet → api docs check → self-assessment → commit]
```

Generation happens in `SetupCommands(workspacePath)` — Claude returns no-op (command is checked into repo), OpenCode generates from embedded template.

### Files Changed

| File                                                | Change                                                                                 |
| --------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `internal/detect/adapter.go`                        | Add Phase 2 methods, PersonaInjection type, HookContext/SpawnContext types             |
| `internal/detect/adapter_claude.go`                 | Merge resume into InteractiveArgs, implement hook/persona methods (delegate to ensure) |
| `internal/detect/adapter_codex.go`                  | Merge resume, stub Phase 2 methods                                                     |
| `internal/detect/adapter_gemini.go`                 | Merge resume, stub Phase 2 methods                                                     |
| `internal/detect/adapter_opencode.go`               | Merge resume, implement hooks/persona/commands                                         |
| `internal/detect/adapter_test.go`                   | Update tests for merged resume, add Phase 2 tests                                      |
| `internal/detect/commands.go`                       | Update BuildCommandParts for merged resume                                             |
| `internal/detect/commands_test.go`                  | Update resume test cases                                                               |
| `internal/workspace/ensure/manager.go`              | Refactor to use adapter.SetupHooks/CleanupHooks, remove SupportsHooks function         |
| `internal/workspace/ensure/opencode_plugin.go`      | NEW — OpenCode plugin template and generation                                          |
| `internal/workspace/ensure/opencode_plugin_test.go` | NEW — tests for plugin generation                                                      |
| `internal/session/manager.go`                       | Replace appendPersonaFlags switch with adapter dispatch, wire SpawnEnv                 |
| `internal/session/manager_test.go`                  | Update persona tests for adapter dispatch                                              |

### What Gets Deleted

- `ResumeArgs()` from all 4 adapters
- `ToolModeResume` case in `BuildCommandParts` (replaced by `ToolModeInteractive` + resume flag)
- `case "claude"` switch in `appendPersonaFlags`
- `SupportsHooks(baseTool)` standalone function (replaced by `adapter.SupportsHooks()`)
- `SupportsSystemPromptFlag(baseTool)` standalone function (replaced by `adapter.PersonaInjection()`)

### Migration Safety

1. Merge resume into InteractiveArgs — verify all callers pass `resume=false` for interactive, `resume=true` for resume
2. Hook refactoring must produce identical `.claude/settings.local.json` output — compare byte-for-byte
3. OpenCode plugin generates valid TypeScript — test with `bun check` or syntax validation
4. SpawnEnv merges into existing environment — verify no clobbering of user-set vars
