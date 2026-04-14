# Adapter Descriptors

## What it does

Defines how schmux detects, spawns, and integrates with AI coding agents
(Claude, Codex, Gemini, OpenCode, and any custom agents) using declarative
YAML descriptors instead of hardcoded Go code. A single `GenericAdapter`
implementation handles all agents.

## Key files

| File                                        | Purpose                                                                           |
| ------------------------------------------- | --------------------------------------------------------------------------------- |
| `internal/detect/descriptor.go`             | YAML schema structs + `ParseDescriptor` with strict validation                    |
| `internal/detect/adapter_generic.go`        | `GenericAdapter` — the only `ToolAdapter` implementation                          |
| `internal/detect/loader.go`                 | Loads descriptors from embedded dirs and `~/.schmux/adapters/`, registers at init |
| `internal/detect/hook_strategy.go`          | `HookStrategy` interface + strategy registry + `none` strategy                    |
| `internal/detect/hooks_json_settings.go`    | `json-settings-merge` strategy (Claude)                                           |
| `internal/detect/hooks_plugin_file.go`      | `plugin-file` strategy (OpenCode)                                                 |
| `internal/detect/adapter_claude_hooks.go`   | Hook merge logic + `EnsureGlobalHookScripts` (shared)                             |
| `internal/detect/adapter_opencode_hooks.go` | TypeScript plugin template (shared)                                               |
| `internal/detect/setup_templates.go`        | Embedded file templates for `setup_files`                                         |
| `internal/detect/descriptors/*.yaml`        | Builtin agent descriptors (embedded into binary)                                  |
| `internal/detect/contrib/`                  | External descriptors (empty in OSS, populated by CI)                              |

## Architecture decisions

- **YAML over Go code** — adding a new agent is a file drop, not a
  recompilation. The descriptor format is strict (unknown fields rejected)
  to catch typos early.

- **Single `GenericAdapter`** — one code path for all agents. The schema
  was battle-tested against Claude (the most complex adapter) before
  adopting. If it handles Claude, it handles anything.

- **Hook strategies separate WHAT from HOW** — schmux always injects the
  same lifecycle hooks (status events, failure capture, stop gates). The
  descriptor says WHERE to inject (file path, format), the strategy says
  HOW (JSON merge, TypeScript plugin, etc.). New hook formats require one
  Go strategy implementation, not an entire adapter.

- **Per-mode model flag** — `ModeDesc.ModelFlag` can override the
  top-level `model_flag`. Set to `"-"` to disable model injection for a
  mode (Claude's oneshot ignores model). Empty inherits top-level.

- **`{model}` placeholder** — tools that need model injected at a specific
  position in args (not appended at the end) use `{model}` in `base_args`.
  When model is nil/empty, the placeholder AND its preceding flag are
  stripped.

- **`schema_args` vs `schema_flag`** — `schema_flag` passes the schema
  string as a value (e.g., `--json-schema <schema>`). `schema_args` adds
  fixed args when schema is present without passing the schema string
  (OpenCode only adds `--format json`).

- **Model routing env vars NOT in descriptors** — `BuildRunnerEnv` (which
  sets `ANTHROPIC_*` vars for custom endpoints) belongs in the models
  layer, not the tool layer. `GenericAdapter.BuildRunnerEnv` returns nil.

- **Embedded wins over runtime** — descriptors in `descriptors/` and
  `contrib/` (baked into the binary) take priority over
  `~/.schmux/adapters/` (runtime). Build-time is an intentional choice;
  runtime is for development convenience.

- **Auto-synthesized default models** — `GetDefaultModels()` iterates
  all registered adapters and creates a synthetic default model for each.
  No hardcoded list. A new descriptor automatically gets a default model.

- **Auto-enabled for spawning** — detected tools are implicitly enabled
  in `GetEnabledModels()`. Dropping a YAML descriptor and restarting
  the daemon is sufficient — no manual enablement in the Agents config
  tab required.

- **No streaming mode** — the `ToolAdapter` interface supports
  `interactive` and `oneshot` only. Streaming was removed as unnecessary
  complexity (autolearn uses the standard oneshot executor).

## Adding a new agent

1. Create `~/.schmux/adapters/<name>.yaml` (or `internal/detect/descriptors/<name>.yaml` for builtins):

```yaml
name: myagent
detect:
  - type: path_lookup
    command: myagent
capabilities: [interactive]
interactive:
  base_args: ['--start']
```

2. Restart the daemon. The agent is automatically detected, a default
   model is synthesized, and it's enabled for spawning. No config changes
   needed — it appears in the spawn UI immediately.

Only `name` and `detect` are required. Everything else has safe defaults
(no hooks, no signaling, no model flag, capabilities = `[interactive]`).
The E2E test `TestE2EDescriptorAdapterSpawn` validates this full
zero-config pipeline.

## Adding a new hook strategy

1. Create `internal/detect/hooks_<name>.go`
2. Implement the `HookStrategy` interface (4 methods)
3. Register via `init()`: `RegisterHookStrategy("<name>", &myStrategy{})`
4. Reference in descriptors: `hooks: { strategy: "<name>" }`

## Build-time embedding for proprietary agents

Organizations with internal agents that cannot appear in the OSS repo:

```bash
cp $INTERNAL_REPO/adapters/*.yaml internal/detect/contrib/
go build ./cmd/schmux
```

The resulting binary is self-contained. `contrib/*.yaml` is `.gitignored`.
The OSS codebase reveals only "you can define custom adapters."

## Gotchas

- **Strict YAML parsing** — unknown fields cause parse errors, not silent
  defaults. A typo like `capabilites` (missing 'i') is a hard error.

- **`resume_args` replaces, doesn't append** — for tools where resume is
  a subcommand (codex: `["resume", "--last"]`), `resume_args` replaces
  all interactive args, not appends to `base_args`.

- **`command_args` goes into `Tool.Command`** — `BuildCommandParts` calls
  `strings.Fields(detectedCommand)` to split the command. Gemini's
  `command_args: ["-i"]` produces `Command: "gemini -i"` which splits
  correctly.

- **SpawnEnv returns nil, not empty map** — when there are no env vars to
  set, `SpawnEnv` returns nil to match existing caller expectations.

- **Hook logic lives in separate files** — `adapter_claude_hooks.go` and
  `adapter_opencode_hooks.go` contain the actual hook implementations.
  The strategy files (`hooks_json_settings.go`, `hooks_plugin_file.go`)
  are thin wrappers that delegate to these functions.

- **`EnsureGlobalHookScripts` is NOT a strategy** — it writes shared
  shell scripts to `~/.schmux/hooks/` at daemon startup. It's called
  from `daemon.go`, not from any adapter or strategy.

- **Descriptors load at `init()` time** — the `loader.go` `init()`
  function loads and registers all embedded descriptors. Tests that
  register adapters must use `saveAndRestoreRegistries(t)` for cleanup.

- **`RegisterDescriptorAdapters` skips existing names** — silently skips
  any name already registered (e.g., by another descriptor or by tests).
  This enables incremental migration and avoids init-order crashes.
