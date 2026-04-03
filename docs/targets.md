# Targets

## What it does

A target is anything schmux can spawn into a tmux session. There are three kinds:

1. **Builtin tools** -- auto-detected CLI tools (claude, codex, gemini, opencode)
2. **Models** -- AI models from the registry, user-defined, or defaults
3. **User-defined commands** -- arbitrary commands configured in `run_targets`

These three kinds are managed by completely separate subsystems. The `run_targets` array in config is only for user-defined commands.

## Builtin tools

schmux auto-detects four CLI tools at startup by checking `$PATH`:

| Tool       | Instruction File      |
| ---------- | --------------------- |
| `claude`   | `.claude/CLAUDE.md`   |
| `codex`    | `.codex/AGENTS.md`    |
| `gemini`   | `.gemini/GEMINI.md`   |
| `opencode` | `.opencode/AGENTS.md` |

No configuration needed. If the binary is on `$PATH`, it is available.

Detection is handled by `internal/detect/tools.go`. `IsBuiltinToolName()` returns true for these four names and nothing else.

## Models

Models are managed entirely by the models subsystem, not by `run_targets`. See [models.md](models.md) for the full reference.

Key runtime function: `models.Manager.IsModel(name)` determines whether a target name is promptable. It checks:

1. Is it a known model ID? If so, can it be resolved to an available tool?
2. Is it a builtin tool name? (always promptable)
3. Is it a user-defined command? (never promptable)

## User-defined commands

The `run_targets` config array holds user-supplied commands. Each entry has exactly two fields:

```json
{
  "run_targets": [
    { "name": "shell", "command": "zsh" },
    { "name": "lint", "command": "./scripts/lint.sh" }
  ]
}
```

There is no `type`, `source`, or `promptable` field. User-defined commands are never promptable -- they cannot receive a prompt argument.

### Rules

- `name` and `command` are both required
- Names must be unique
- Names must not collide with builtin tool names
- These commands appear as slash commands in the spawn prompt textarea (e.g., `/shell`)

### Migration

Old configs with `type`, `source`, or `promptable` fields are cleaned automatically. The `drop_run_target_bridge_fields` migration strips these fields and drops entries injected by the old bridge system (entries with `source: "detected"` or `source: "model"`). Only genuine user-defined commands survive.

## How the spawn page uses targets

- **Models** are fetched from `/api/config` (the `models` array) and shown as agent options. Availability depends on runner detection and secrets.
- **User-defined commands** are fetched from the same endpoint (the `run_targets` array) and shown as slash commands in the prompt textarea.

The two never mix. Models are selected via the agent dropdown. Commands are invoked via `/name` in the prompt.

## Key files

| File                             | Purpose                                                    |
| -------------------------------- | ---------------------------------------------------------- |
| `internal/config/run_targets.go` | `RunTarget` struct (Name + Command), validation            |
| `internal/detect/tools.go`       | `IsBuiltinToolName()`, tool detection, instruction configs |
| `internal/models/manager.go`     | `IsModel()`, `ResolveModel()` -- model-to-tool resolution  |
| `internal/detect/commands.go`    | `BuildCommandParts()` -- assembles CLI args for any target |
