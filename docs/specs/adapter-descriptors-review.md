VERDICT: NEEDS_REVISION

## Summary Assessment

The design correctly identifies the problem (hardcoded adapters block proprietary extensions) and the phased migration is sound. However, the YAML schema has several gaps where it cannot express behaviors that exist in the current adapter implementations, and the hook strategy taxonomy misclassifies OpenCode's actual mechanism.

## Critical Issues (must fix)

### 1. OpenCode hooks are NOT `config-overlay` -- they are a TypeScript plugin written to disk

The spec's hook strategy table says OpenCode uses `config-overlay` via `OPENCODE_CONFIG_CONTENT` env var. This is wrong. Looking at `adapter_opencode_hooks.go`, OpenCode hooks work by generating a 96-line TypeScript file at `.opencode/plugins/schmux.ts`. This is a completely different mechanism from `config-overlay`. The `OPENCODE_CONFIG_CONTENT` env var is used only for _persona injection_ (in `SpawnEnv`), not for hooks.

The spec needs a third hook strategy (e.g., `plugin-file-template`) that writes a file from an embedded template to a specific path. The `json-settings-merge` and `config-overlay` strategies do not cover this pattern.

### 2. `BuildRunnerEnv` is missing from the schema entirely

The `ToolAdapter` interface has a `BuildRunnerEnv(spec RunnerSpec) map[string]string` method. Claude's implementation is non-trivial -- when an endpoint is provided, it sets 6 environment variables (`ANTHROPIC_MODEL`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL`). This is called from `internal/models/manager.go` during model resolution.

The descriptor schema has no way to express this. The `spawn_env` field is static key-value pairs; `BuildRunnerEnv` is dynamic (conditional on `spec.Endpoint != ""`, and uses `spec.ModelValue` as the value for multiple keys). Without this, third-party model routing through Claude would break.

### 3. `SetupCommands` is missing from the schema

The `ToolAdapter` interface has `SetupCommands(workspacePath string) error`. OpenCode uses this to write a 106-line `/commit` command file to `.opencode/commands/commit.md` (see `adapter_opencode_commands.go`). This is called from `ensure/manager.go` during workspace preparation.

The descriptor schema has no field for setup commands. An external agent with a similar need (writing command/workflow files into the workspace before spawn) has no way to express this declaratively.

### 4. `SpawnEnv` has dynamic logic that static YAML cannot represent

The spec's `spawn_env` is a static map. But OpenCode's `SpawnEnv` is conditional on `ctx.PersonaPath` -- when a persona file path is provided, it dynamically constructs a JSON config `{"instructions":["path/to/persona.md"]}` and sets `OPENCODE_CONFIG_CONTENT`. When no persona is provided, it returns nil.

This conditional, context-dependent env var construction cannot be expressed as a flat YAML map. The `GenericAdapter` would need a richer concept (e.g., persona injection strategy mapping to env var templates) or the persona-config-overlay strategy would need to handle this implicitly.

### 5. Codex oneshot args include model inside the args, breaking the `base_args` model

The spec shows oneshot args as a flat `base_args` list with a separate `schema_flag`. But Codex's `OneshotArgs` interleaves the model flag _inside_ the args: `["exec", "--json", "-m", modelValue, "--output-schema", schema]`. The model is not at the end -- it comes between `--json` and `--output-schema`.

The current schema assumes model args are always appended separately (via `model_flag`), but Codex needs the model flag injected into oneshot args specifically, not just interactive mode. The `GenericAdapter` needs to know when to inject model args for each mode, not just interactive.

### 6. Gemini bakes `-i` flag into the detected command string, not into `base_args`

Gemini's `Detect()` returns `Command: "gemini -i"` -- the `-i` flag is part of the detected command, not part of `InteractiveArgs`. This means `BuildCommandParts` splits `"gemini -i"` via `strings.Fields` and gets `["gemini", "-i"]` as the base before appending mode args.

The descriptor schema has no way to express this. The `detect` section only specifies how to find the binary, not what the resulting command string should be. Either the schema needs a `command_suffix` field in the detect section, or the Gemini descriptor's `interactive.base_args` must include `-i`, and the spec must clarify that `base_args` are appended to the bare binary name (not the detected command).

### 7. Resume args for Codex are structurally different from other tools

Codex's resume args are `["resume", "--last"]` -- where `resume` is a _subcommand_, not a flag. The spec shows `resume_args: ["--continue"]` as if resume is always a simple flag. The schema needs to be clear that `resume_args` replaces all mode args (including subcommands), not just appends flags. This might work as-is if `GenericAdapter` treats `resume_args` as the complete arg list (which is what the existing code does), but the spec should explicitly state this.

## Suggestions (nice to have)

### A. Codex signaling args use a non-standard format

Codex's `SignalingArgs` returns `["-c", "model_instructions_file=" + filePath]` -- note the `=` concatenation within a single arg. The spec's `signaling.flag` field would need to handle this format. Consider documenting how flag values are assembled (e.g., `flag: "-c"` with `value_format: "model_instructions_file={path}"`).

### B. The `WrapRemoteCommand` behavior is missing from the schema

Both Claude and OpenCode implement `WrapRemoteCommand` to prepend file-creation shell commands before the agent binary invocation for remote sessions. This is tightly coupled to the hook strategy (Claude generates JSON, OpenCode generates TypeScript). The spec does not address remote session support for YAML-defined adapters. If a hook strategy implementation includes `WrapRemoteCommand` logic, this should be documented.

### C. Consider the `IsBuiltinToolName` and `agentInstructionConfigs` registries

Currently `tools.go` maintains a separate `builtinToolNames` list and `agentInstructionConfigs` map that are hardcoded independently of the adapter registry. The design should specify how these are populated when adapters come from YAML descriptors. The `GenericAdapter` should automatically register names and instruction configs from the parsed descriptor.

### D. Skill injection for OpenCode uses a different path pattern than Claude

Claude: `.claude/skills/schmux-{name}/SKILL.md` (directory per skill, fixed filename).
OpenCode: `.opencode/commands/schmux-{name}.md` (flat file, no subdirectory).

The spec's `skills` section only shows Claude's pattern (`dir_pattern` + `file_name`). OpenCode's flat-file pattern would need a different expression, perhaps `file_pattern: ".opencode/commands/schmux-{name}.md"` as an alternative to the dir+file approach.

### E. The `display_name` field is new -- verify nothing depends on the name for display

The spec introduces `display_name` separate from `name`. Verify that all UI code currently uses the adapter name directly for display, and that the migration plan accounts for updating display references.

### F. Validation should reject unknown fields, not silently ignore them

The spec does not mention YAML validation behavior. If someone typos `capabilities` as `capabilites` or `signaling` as `signalling`, silent acceptance would lead to confusing defaults. The parser should use strict mode and reject unknown fields.

### G. The spec mentions `internal/detect/contrib/` but file additions say `internal/detect/adapter_generic.go`

The build-time embedding diagram references `internal/detect/contrib/`, but the embedded descriptors path says `descriptors/*.yaml, contrib/*.yaml`. Clarify whether the builtin YAML files live at `internal/detect/descriptors/` or alongside the Go files. Also clarify the `//go:embed` directive location.

## Verified Claims (things confirmed are correct)

1. **Four hardcoded adapters exist, registered via `init()`** -- Confirmed. `adapter_claude.go`, `adapter_codex.go`, `adapter_gemini.go`, `adapter_opencode.go` each call `registerAdapter()` in `init()`.

2. **Claude is the most complex adapter** -- Confirmed. It is the only one with all three capabilities (interactive, oneshot, streaming), has non-trivial `BuildRunnerEnv` logic, uses `json-settings-merge` hooks with merge semantics, supports skills via directory-based injection, and has 3 embedded shell scripts.

3. **Hook strategy separation (WHAT vs HOW)** -- The architectural principle is sound. The hook content (status events, failure capture, stop gates) is indeed universal, while the injection mechanism varies per tool.

4. **`SignalingStrategy` and `PersonaInjection` enums match the spec** -- The spec correctly identifies the three signaling strategies and three persona injection strategies that exist in the codebase.

5. **The phased migration order (Gemini -> Codex -> OpenCode -> Claude) is correct** -- Gemini is truly the simplest (interactive-only, no hooks, no skills, no special env). Claude is truly the hardest.

6. **The `adapters` registry map and `GetAdapter`/`AllAdapters` pattern exists** -- Confirmed at `adapter.go` lines 129-149.

7. **Claude's hooks use `schmuxStatusMessagePrefix` for ownership** -- Confirmed. The `"schmux:"` prefix is used in `isSchmuxMatcherGroup` to distinguish schmux hooks from user hooks during merge/cleanup.

8. **Embedded vs runtime priority (embedded wins)** -- This is a reasonable design choice for build-time intentionality.
