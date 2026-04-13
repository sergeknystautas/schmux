# Adapter Descriptors

Replace hardcoded Go adapter implementations with declarative YAML descriptors.
Every agent — builtin (Claude, Codex, Gemini, OpenCode) and external (internal
tools, future agents) — is defined by a descriptor file parsed by a single
`GenericAdapter` implementation.

## Problem

schmux has four hardcoded `ToolAdapter` implementations, each registered via
`init()`. Adding a new agent requires writing Go code, recompiling, and
committing the adapter to the open-source repo. This is a problem for
organizations that use internal agent harnesses they cannot reveal publicly.

## Design

### Descriptor Schema

Every field beyond `name` and `detect` is optional, with sensible defaults
(no model flag, no signaling, no hooks, capabilities = `[interactive]`).

YAML parsing uses strict mode — unknown fields are rejected, not silently
ignored. A typo like `capabilites` produces a parse error, not a silent
default.

Implementation note: the current Go enum zero-values are `SignalingHooks`
and `PersonaCLIFlag` (iota=0). New `SignalingNone` and `PersonaNone` values
must be added as explicit enum entries. Omitted descriptor fields must
default to `none`, not to the iota=0 value.

```yaml
# ── Identity ──────────────────────────────────────────
name: claude # required, unique key
display_name: Claude Code # optional, defaults to name

# ── Detection (ordered, first match wins) ─────────────
detect:
  - type: path_lookup # exec.LookPath
    command: claude
  - type: file_exists # stat + optional verify
    path: '~/.local/bin/claude'
    verify: '-v' # run binary with this flag to confirm
  - type: file_exists
    path: '~/.claude/local/claude'
    verify: '-v'
  - type: homebrew_cask
    name: claude-code
  - type: npm_global
    package: '@anthropic-ai/claude-code'

# ── Capabilities ──────────────────────────────────────
capabilities: [interactive, oneshot, streaming]
model_flag: '--model' # omit if agent doesn't accept one
command_args: [] # args appended to binary before mode args
  # e.g., gemini uses ["-i"] here

# ── Instruction file ─────────────────────────────────
instruction:
  dir: '.claude'
  file: 'CLAUDE.md'

# ── Spawn args per mode ──────────────────────────────
# Args support {model} placeholder for tools that need model injected
# at a specific position (e.g., codex: ["exec", "--json", "-m", "{model}"]).
# If no {model} placeholder is present, model_flag + value are appended
# at the end when a model is selected.
#
# resume_args replaces all mode args (not appends). This supports tools
# where resume is a subcommand (e.g., codex: ["resume", "--last"]).
interactive:
  base_args: []
  resume_args: ['--continue'] # replaces base_args, not appends

oneshot:
  base_args: ['-p', '--dangerously-skip-permissions', '--output-format', 'json']
  schema_flag: '--json-schema'

streaming:
  base_args: ['-p', '--dangerously-skip-permissions', '--output-format', 'stream-json', '--verbose']
  schema_flag: '--json-schema'

# ── Signaling (how schmux communicates state) ─────────
# strategy: hooks    — delivered via hook injection (no CLI flags)
# strategy: cli_flag — flag/value_template inject a file path as CLI arg
#   e.g., codex: flag: "-c", value_template: "model_instructions_file={path}"
# strategy: instruction_file — appended to instruction file
# strategy: none     — no signaling
signaling:
  strategy: hooks # hooks | cli_flag | instruction_file | none
  # flag and value_template are only used with strategy: cli_flag

# ── Persona injection ────────────────────────────────
# strategy: cli_flag       — pass persona file as CLI flag
# strategy: instruction_file — append persona to instruction file
# strategy: config_overlay — set env var with JSON containing persona path
#   requires env_var and value_template, e.g.:
#   env_var: "OPENCODE_CONFIG_CONTENT"
#   value_template: '{"instructions":["{path}"]}'
# strategy: none           — no persona injection
persona:
  strategy: cli_flag
  flag: '--append-system-prompt-file'

# ── Hook injection ───────────────────────────────────
hooks:
  strategy: json-settings-merge # json-settings-merge | plugin-file | none
  settings_file: '.claude/settings.local.json'
  ownership_prefix: 'schmux:'

# ── Skill injection ──────────────────────────────────
# Two patterns supported:
#   dir + file: .claude/skills/schmux-{name}/SKILL.md (one dir per skill)
#   file_pattern: .opencode/commands/schmux-{name}.md  (flat file per skill)
skills:
  dir_pattern: '.claude/skills/schmux-{name}'
  file_name: 'SKILL.md'

# ── Setup commands ───────────────────────────────────
# Files written to workspace before first spawn. Content is provided by
# schmux (embedded templates), not by the descriptor. The descriptor
# declares WHERE to write, schmux decides WHAT.
setup_files:
  - target: '.opencode/commands/commit.md'
    source: commit-command # key into schmux's embedded templates

# ── Environment variables ────────────────────────────
spawn_env: # static env vars, always set at spawn
  CLAUDE_CODE_EFFORT_LEVEL: 'max'
  MAX_THINKING_TOKENS: '4096'
```

A minimal descriptor for a thin-wrapper agent:

```yaml
name: orc
display_name: Orc

detect:
  - type: path_lookup
    command: orc
  - type: file_exists
    path: /opt/facebook/bin/orc

capabilities: [interactive]

interactive:
  base_args: ['--new', '-y']

spawn_env:
  TMUX: '1'
```

### GenericAdapter

A single struct that implements `ToolAdapter` by reading fields from a parsed
descriptor. Methods like `SignalingStrategy()` return the enum value named in
the YAML. Methods like `InteractiveArgs()` return the string slice from the
YAML. Unset fields return safe defaults (empty args, `SignalingNone`,
`PersonaNone`).

When a descriptor is loaded, the `GenericAdapter` automatically registers the
agent's name into the tool name registry (replacing the hardcoded
`builtinToolNames` list) and its instruction config into the instruction
config map (replacing the hardcoded `agentInstructionConfigs` map).

Hook injection and persona-via-config-overlay are the only behaviors that
require compiled Go. The descriptor names a strategy; `GenericAdapter` looks
it up in a strategy registry and delegates to it. Strategy implementations
are reusable building blocks, not per-adapter code.

### Loading

Descriptors are loaded from two sources at daemon startup, merged into a
single registry:

```
1. Embedded descriptors    (//go:embed descriptors/*.yaml, contrib/*.yaml)
2. Runtime descriptors     (~/.schmux/adapters/*.yaml)
```

`descriptors/` contains the four builtin agents (claude, codex, gemini,
opencode). `contrib/` ships empty in the OSS repo and is populated by
internal CI before `go build` to bake in proprietary adapters.

Name collisions between embedded and runtime descriptors are resolved in
favor of embedded (intentional build-time choice). Collisions within the
same source are errors (refuse to start).

### Hook Strategies

The descriptor schema separates WHAT schmux injects (status events, failure
capture, stop gates — same for all agents) from HOW it injects (varies by
agent config format). Strategies are compiled Go implementations registered
in a strategy map:

| Strategy              | Used by            | Mechanism                                                  |
| --------------------- | ------------------ | ---------------------------------------------------------- |
| `json-settings-merge` | Claude             | Merge hooks into JSON settings file with ownership tagging |
| `plugin-file`         | OpenCode           | Write TypeScript plugin file to agent's plugin directory   |
| `none`                | Codex, Gemini, Orc | No hook integration                                        |

Each strategy implementation handles `SetupHooks`, `CleanupHooks`, and
`WrapRemoteCommand` for its format. The descriptor provides the strategy
name and format-specific parameters (file paths, ownership prefix).

Adding support for a new hook format means writing one Go strategy
implementation — not an entire adapter.

### Build-Time Embedding

For organizations with internal agents:

```
┌───────────────────────────────────────────────────┐
│ OSS repo                                          │
│                                                   │
│  internal/detect/contrib/                         │
│    .gitkeep                                       │
│                                                   │
│  .gitignore:                                      │
│    internal/detect/contrib/*.yaml                 │
│    !internal/detect/contrib/.gitkeep              │
└───────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────┐
│ Internal CI                                       │
│                                                   │
│  cp $INTERNAL_REPO/adapters/*.yaml \              │
│     internal/detect/contrib/                      │
│  go build ./cmd/schmux                            │
└───────────────────────────────────────────────────┘
```

The resulting binary is self-contained. The OSS codebase reveals only "you
can define custom adapters" — a generic extension mechanism, not a hint
about what might be defined.

During development, drop descriptors into `~/.schmux/adapters/` for runtime
loading without rebuilding.

## Migration

### Phase 1 — GenericAdapter for external descriptors

Add `GenericAdapter`, descriptor parsing, and the loader. The four builtin
adapters remain as Go code. External agents (orc, etc.) go through
`GenericAdapter`. Ships the feature without a risky migration.

File additions:

```
internal/detect/adapter_generic.go   GenericAdapter implementation
internal/detect/descriptor.go        YAML parsing + validation (strict mode)
internal/detect/loader.go            embed + runtime loading
internal/detect/strategies/          hook strategy registry
  json_settings.go                   extracted from adapter_claude_hooks.go
  plugin_file.go                     extracted from adapter_opencode_hooks.go
internal/detect/contrib/.gitkeep     empty embed directory
```

### Phase 2 — Builtins become descriptors

Replace each Go adapter with a YAML descriptor, one at a time:

1. Gemini (simplest — no hooks, no skills, interactive only)
2. Codex (no hooks, simple oneshot)
3. OpenCode (hooks via plugin-file strategy)
4. Claude (most complex — hooks via json-settings-merge, all capabilities)

Each conversion is independently testable. The Go adapter file is deleted
when its descriptor equivalent passes all existing tests. Claude goes last
because it is the hardest test of the schema.

End state: `GenericAdapter` is the only `ToolAdapter` implementation. All
agent definitions are data.

## Decisions

| Decision                      | Choice                   | Rationale                                                                 |
| ----------------------------- | ------------------------ | ------------------------------------------------------------------------- |
| Descriptor format             | YAML                     | Human-readable, widely supported, good for config                         |
| All agents as descriptors     | Yes                      | One code path, schema battle-tested against Claude                        |
| Hook injection                | Strategy registry        | Separates WHERE/HOW (per-agent) from WHAT (schmux-universal)              |
| Model routing env vars        | Not in descriptor        | Belongs in the models layer, not the tool layer                           |
| Spawn env vars                | Static only              | Simple; tuning knobs can be added later as a richer concept               |
| Embedded vs. runtime priority | Embedded wins            | Build-time = intentional choice; runtime = dev convenience                |
| Strict YAML parsing           | Yes                      | Typos in field names produce errors, not silent defaults                  |
| `resume_args` semantics       | Replaces all args        | Supports subcommand-style resume (codex: `resume --last`)                 |
| Model position in args        | `{model}` placeholder    | Tools that need model at a specific position use placeholder in base_args |
| Skill injection patterns      | dir+file OR file_pattern | Claude uses subdirectories, OpenCode uses flat files                      |
