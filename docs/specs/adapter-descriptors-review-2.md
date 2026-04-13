VERDICT: NEEDS_REVISION

## Summary Assessment

The revision successfully addresses 6 of the 7 round 1 critical issues. The `{model}` placeholder, `setup_files`, `plugin-file` hook strategy, strict YAML parsing, and `resume_args` semantics are all sound. Two issues remain: the Gemini `-i` flag is still not expressible, and the Claude descriptor's `signaling` section shows a flag that is dead configuration under `strategy: hooks`, indicating a conceptual confusion between the signaling and hooks layers.

## Round 1 Issue Resolution Status

### Issue 1: OpenCode hooks misclassified as config-overlay -- FIXED

The hook strategy table now correctly shows OpenCode using `plugin-file`, not `config-overlay`. The spec adds a `plugin-file` strategy that writes a TypeScript plugin to `.opencode/plugins/schmux.ts`. This matches the actual code in `adapter_opencode_hooks.go`.

### Issue 2: BuildRunnerEnv missing -- FIXED (by design decision)

The Decisions table explicitly states "Model routing env vars: Not in descriptor -- Belongs in the models layer, not the tool layer." The `GenericAdapter` can return an empty map, matching what Codex, Gemini, and OpenCode already do. Claude's non-trivial `BuildRunnerEnv` logic would stay as a strategy or be moved to the models layer during migration. Acceptable.

### Issue 3: SetupCommands missing -- FIXED

The spec adds `setup_files` to the schema with a `target` + `source` pattern. The descriptor declares WHERE to write; schmux provides the WHAT from embedded templates. This can express OpenCode's commit command file (`target: ".opencode/commands/commit.md"`, `source: commit-command`).

### Issue 4: SpawnEnv dynamic logic -- FIXED (by design decision)

The Decisions table states "Spawn env vars: Static only." The dynamic persona-via-env-var behavior for OpenCode is handled by the `config_overlay` persona strategy, not by `spawn_env`. The `GenericAdapter` would detect `persona.strategy: config_overlay` and produce the `OPENCODE_CONFIG_CONTENT` JSON dynamically.

### Issue 5: Codex model inside args -- FIXED

The spec adds `{model}` placeholder support: "Args support {model} placeholder for tools that need model injected at a specific position." When no `{model}` placeholder is present, `model_flag` + value are appended at the end. This correctly handles both Claude (model appended) and Codex (model at specific position in oneshot args).

### Issue 6: Gemini `-i` flag in detected command -- NOT FIXED

See Critical Issue 1 below.

### Issue 7: Codex resume args semantics -- FIXED

The spec explicitly states: "resume_args replaces all mode args (not appends)." The Decisions table confirms: "resume_args semantics: Replaces all args -- Supports subcommand-style resume."

### Suggestion C (builtinToolNames/agentInstructionConfigs) -- FIXED

The spec states that `GenericAdapter` automatically registers tool names and instruction configs.

### Suggestion D (OpenCode skill pattern) -- FIXED

The spec adds `file_pattern` as an alternative to `dir_pattern` + `file_name`.

### Suggestion F (Strict YAML parsing) -- FIXED

The spec requires strict mode with unknown field rejection.

## Critical Issues (must fix)

### 1. Gemini's `-i` flag baked into detected command is still not expressible

Round 1 Issue 6 was not addressed. Gemini's `Detect()` returns `Command: "gemini -i"` (verified at `internal/detect/agents.go:384,392,400`). When `BuildCommandParts` runs `strings.Fields(detectedCommand)`, it splits this into `["gemini", "-i"]` and then appends mode args. The `-i` flag is part of the detected command string, not part of `InteractiveArgs`.

The descriptor's `detect` section only specifies how to find the binary -- it produces a binary path, not a command string with embedded flags. There is no field for "after detection, the command string should be `gemini -i`, not just `gemini`."

**Why this matters**: Without this, a Gemini descriptor would produce `["gemini", ...]` instead of `["gemini", "-i", ...]` for all invocations. Since `-i` enables interactive mode for Gemini CLI, this breaks Gemini entirely during Phase 2 migration.

**Fix**: Add a top-level `command_args: ["-i"]` field (or put it in the detect section). These args are appended to the resolved binary path to form the full detected command string, before any mode-specific args. For Gemini, the detected command becomes `"gemini -i"` just as it is today. For all other adapters, `command_args` is omitted and the command is the bare binary name.

### 2. The Claude descriptor's `signaling` section shows a flag that is unused dead configuration

The spec's Claude descriptor declares:

```yaml
signaling:
  strategy: hooks
  flag: '--append-system-prompt-file'
  value_template: '{path}'
```

But in the codebase, `strategy: hooks` (i.e., `SignalingHooks`) means signaling is delivered via lifecycle hooks in `settings.local.json` -- no CLI flags are used. The session manager explicitly skips CLI arg injection for this strategy (`session/manager.go:865-866`):

```go
case detect.SignalingHooks:
    // Handled by ensurer.ForSpawn above
```

Claude's `SignalingArgs()` method (which returns `["--append-system-prompt-file", filePath]`) is dead code -- it is never called because the session manager only calls `SignalingArgs` when `strategy == SignalingCLIFlag`.

The `--append-system-prompt-file` flag IS used, but for persona injection (`PersonaArgs`), not signaling. It appears correctly in the separate `persona` section.

**Why this matters**: An implementer reading the spec would reasonably conclude that `strategy: hooks` tools still get CLI signaling flags injected, which is wrong. The `flag` and `value_template` fields are meaningless when `strategy: hooks`. Showing them in the Claude example actively misleads.

**Fix**: Either (a) remove `flag` and `value_template` from the Claude descriptor example, since they are unused when `strategy: hooks`, or (b) add a spec note that `flag`/`value_template` are only meaningful for `strategy: cli_flag`, and simplify the Claude example to just `signaling: { strategy: hooks }`.

## Suggestions (nice to have)

### A. The Claude detect section is missing one detection method

The actual Claude detection has 5 methods (`internal/detect/agents.go:290-339`):

1. PATH lookup
2. `~/.local/bin/claude` with `-v` verify
3. `~/.claude/local/claude` with `-v` verify
4. Homebrew cask `claude-code`
5. npm global `@anthropic-ai/claude-code`

The spec's detect section only shows 4 entries. The `~/.claude/local/claude` alternative native install location is missing. This would cause a behavioral regression if someone has Claude installed only at that path.

### B. `SignalingNone` and `PersonaNone` must be added as new enum values

The spec says: "Unset fields return safe defaults (empty args, `SignalingNone`, `PersonaNone`)." But the current codebase has no `SignalingNone` or `PersonaNone` enum value. The zero values are `SignalingHooks` (iota=0) and `PersonaCLIFlag` (iota=0). During implementation, new enum values must be added and the zero-value behavior must remain unchanged for existing code. Worth noting to avoid a subtle bug where an omitted signaling strategy defaults to "hooks" instead of "none."

### C. The `config_overlay` persona strategy needs env var and template parameters

OpenCode's `config_overlay` persona constructs `OPENCODE_CONFIG_CONTENT` with a JSON structure `{"instructions":["path/to/persona.md"]}`. For `GenericAdapter` to implement this declaratively, the descriptor needs:

- `persona.env_var: "OPENCODE_CONFIG_CONTENT"`
- `persona.value_template: '{"instructions":["{path}"]}'`

Without these, the strategy implementation would need to hardcode OpenCode-specific knowledge, which defeats the purpose of descriptors.

### D. Document that `WrapRemoteCommand` is derived from the hook strategy

The spec mentions hook strategies handle `SetupHooks`, `CleanupHooks`, and `WrapRemoteCommand`. Since `WrapRemoteCommand` is always a function of the hook strategy (Claude generates JSON inline, OpenCode generates TypeScript inline), it requires no additional descriptor fields. This is correct but worth stating explicitly so implementers know that remote support is automatically provided by the hook strategy, not configured separately.

### E. Include a Codex descriptor snippet to illustrate `value_template`

The spec mentions Codex's compound flag format only in a comment: `flag: "-c", value_template: "model_instructions_file={path}"`. The Claude descriptor example shows `value_template: "{path}"` (simple path). Including a brief Codex descriptor snippet would make the compound `value_template` usage concrete and testable.

### F. OpenCode detection uses `homebrewFormulaInstalled`, not `homebrewCaskInstalled`

Minor accuracy note for implementation: OpenCode uses `homebrewFormulaInstalled(ctx, "opencode")` (formula, not cask), and npm uses `npmGlobalInstalled(ctx, "opencode-ai")` (package name `opencode-ai`, not `opencode`).

## Verified Claims (things confirmed are correct)

1. **OpenCode hooks use `plugin-file` strategy** -- Confirmed. `adapter_opencode_hooks.go` writes a TypeScript plugin to `.opencode/plugins/schmux.ts` via `opencodeSetupHooks`, cleans up via `opencodeCleanupHooks`, and wraps remote commands via `opencodeWrapRemoteCommand`. All three operations match the `plugin-file` strategy pattern.

2. **Codex signaling uses `value_template` format** -- Confirmed. `adapter_codex.go:57` returns `["-c", "model_instructions_file=" + filePath]`. The compound `flag: "-c"` with `value_template: "model_instructions_file={path}"` correctly describes this.

3. **`setup_files` can express OpenCode commands** -- Confirmed. `adapter_opencode_commands.go` writes a 106-line commit workflow to `.opencode/commands/commit.md`. The `setup_files` mechanism with `target` + `source` (keyed into embedded templates) matches this pattern.

4. **`resume_args` replacing all args works for Codex** -- Confirmed. Codex `InteractiveArgs(nil, true)` returns `["resume", "--last"]` -- a complete subcommand replacement.

5. **`{model}` placeholder handles Codex oneshot correctly** -- Confirmed. Codex `OneshotArgs` builds `["exec", "--json", "-m", modelValue, "--output-schema", schema]` with model at a specific position. The `{model}` placeholder in `base_args` expands to the same result.

6. **Skill injection has two patterns** -- Confirmed. Claude: `.claude/skills/schmux-{name}/SKILL.md` (directory per skill). OpenCode: `.opencode/commands/schmux-{name}.md` (flat file).

7. **`json-settings-merge` with ownership prefix** -- Confirmed. `adapter_claude_hooks.go` uses `schmuxStatusMessagePrefix = "schmux:"` to identify managed hooks during merge/cleanup. The spec's `ownership_prefix: "schmux:"` correctly parameterizes this.

8. **Embedded vs runtime priority** -- Embedded wins. Sound design choice matching the stated rationale.

9. **`GenericAdapter` auto-registers tool names and instruction configs** -- The spec explicitly replaces the hardcoded `builtinToolNames` list and `agentInstructionConfigs` map.

10. **Migration order (Gemini -> Codex -> OpenCode -> Claude)** -- Confirmed correct by complexity. Gemini: interactive-only, no hooks, no skills. Claude: all three capabilities, hooks, skills, non-trivial BuildRunnerEnv.
