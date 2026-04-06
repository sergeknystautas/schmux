VERDICT: NEEDS_REVISION

## Summary Assessment

The design correctly identifies the orthogonal persona/style axis and proposes a reasonable "compose into one file" strategy, but it contains one critical architectural misunderstanding about how persona injection works for non-Claude agents, underestimates the scope of the remote spawn gap, and has a significant UI usability problem with the spawn wizard layout.

## Critical Issues (must fix)

### 1. The design misunderstands `PersonaInstructionFile` injection -- composing into a single file is not enough for Codex/Gemini

The design says:

> Since we compose into a single file before injection, `appendPersonaFlags()` and all agent adapters (Claude, Codex, Gemini, OpenCode) remain untouched.

This is wrong. The three persona injection strategies work differently:

- **`PersonaCLIFlag` (Claude):** `appendPersonaFlags()` adds `--append-system-prompt-file <path>` to the command. The persona file is written to `.schmux/persona-{sessionID}.md` in the workspace, and the CLI flag points at it. Composing persona+style into that same file works perfectly here.

- **`PersonaInstructionFile` (Codex/Gemini):** Despite the comment in `appendPersonaFlags()` saying "handled by ensure package (instruction file append)," the ensure package (`internal/workspace/ensure/manager.go`) does NOT contain any persona-related code. It only manages signaling instructions within the `<!-- SCHMUX:BEGIN/END -->` markers. For Codex/Gemini, `appendPersonaFlags()` returns the command unchanged, and the persona file is written to disk at `.schmux/persona-{sessionID}.md` but never referenced by any CLI flag or instruction file append. **Persona injection for Codex/Gemini appears to be a no-op today.** The code writes the file, the ensure package ignores it, and no CLI flag references it.

- **`PersonaConfigOverlay` (OpenCode):** Uses `SpawnEnv()` to set `OPENCODE_CONFIG_CONTENT` with a JSON config pointing at the persona file path. This works via the `SpawnContext.PersonaPath` field.

The design's claim of "no adapter changes" is only valid for Claude. For OpenCode, the `SpawnContext` struct would need to be extended (it currently only has `PersonaPath`, not a generic "system prompt path"). For Codex/Gemini, the injection mechanism simply doesn't exist. The design needs to explicitly address what happens to the composed persona+style prompt for each injection strategy, not hand-wave it as "no changes."

**Fix:** Acknowledge that `PersonaInstructionFile` injection for Codex/Gemini is broken (or at least undocumented) today. Decide whether to fix it as part of this work or explicitly scope it out. If scoping it out, note that styles will only work for Claude and OpenCode initially. If fixing it, the ensure package needs to read persona/style content from a well-known path and inject it into the instruction file.

### 2. `SpawnRemote()` lacks persona support and the design hand-waves adding it

The design says:

> `SpawnRemote()` gets the same style resolution logic. The composed prompt is written to the workspace and injected using the same adapter mechanism as local sessions.

But `SpawnRemote()` (line 390 of `internal/session/manager.go`) does not accept `PersonaID`, `PersonaPrompt`, or any style parameter. Its signature is:

```go
func (m *Manager) SpawnRemote(ctx context.Context, profileID, flavorStr, hostID, targetName, prompt, nickname string) (*state.Session, error)
```

The handler at line 353 of `handlers_spawn.go` calls `SpawnRemote` without passing `personaPrompt`:

```go
sess, err = s.session.SpawnRemote(ctx, req.RemoteProfileID, req.RemoteFlavor, remoteHostID, targetName, req.Prompt, nickname)
```

So even today, personas are silently dropped for remote sessions. The design treats remote support as a single sentence ("gets the same style resolution logic") but the actual gap is substantial:

1. `SpawnRemote()` signature needs new parameters (or an options struct like local `SpawnOptions`)
2. The persona file is written locally at `.schmux/persona-{sessionID}.md` -- but remote workspaces don't have local filesystem access. The file would need to be either (a) transmitted to the remote host, or (b) inlined into the command using `--append-system-prompt` instead of `--append-system-prompt-file`.
3. Remote Claude uses `WrapRemoteCommand()` for hooks provisioning, but there's no equivalent for persona/style prompt injection on remote hosts.

**Fix:** Either (a) explicitly scope remote style/persona support out of v1 and note it as follow-up, or (b) add a concrete plan for how the composed prompt reaches the remote agent. Option (b) likely requires adding persona/style content to the remote command inline (similar to how `appendSignalingFlags` skips file-based injection for remote CLI-flag tools) or writing the file to the remote host via the SSH connection before spawning.

### 3. Spawn wizard layout -- adding a third dropdown creates UI density problems

The spawn page already has a tight layout with agent selector, persona dropdown, and repo dropdown in a single flex row (see `data-testid="agent-repo-row"` at line 1089). The persona dropdown only renders when `personas.length > 0`. Adding a style dropdown means the row would have 4 selects: agent, persona, style, repo.

On narrow viewports this will be cramped. The design says "Optional style dropdown added to the spawn form" but doesn't specify placement. The persona dropdown is already conditional and tucked in as `flex-1` next to the agent and repo dropdowns. Adding another `flex-1` dropdown will make all four dropdowns too narrow to read their labels.

**Fix:** Specify concrete placement. Options: (a) combine persona and style into a single row below the agent/repo row, (b) put the style dropdown in a collapsible "Advanced" section, (c) use a persona+style combined display that shows both in the same dropdown slot. The design should address the three spawn modes (fresh + single agent, workspace mode, multiple/advanced mode) since each has its own layout.

## Suggestions (nice to have)

### 1. Consider reusing the persona manager rather than creating a parallel `style.Manager`

The `persona.Manager` and the proposed `style.Manager` are structurally identical: YAML files in a directory, frontmatter + body parsing, CRUD operations, `EnsureBuiltins()`, `ResetBuiltIn()`, embedded `builtins/` via `embed.FS`. The only difference is field names (`Expectations`/`Color` vs. `Tagline`). Consider factoring out a generic `YAMLDocManager` that both persona and style managers use, reducing code duplication and maintenance burden for ~160 lines of near-identical code.

### 2. Style resolution requires reading global config at spawn time

The resolution order is: `style_id` on spawn request > `comm_styles[agentType]` from global config > none. But the handler at `handleSpawnPost()` currently reads `req.PersonaID` directly from the spawn request -- there's no "fallback to global config" step. The design should specify exactly where in the handler the config lookup happens and how `agentType` is determined (the spawn request has `targets` which is a map of target names, and a target name needs to be resolved to its base tool name to look up the default style).

### 3. The "agent type" key in `comm_styles` config is ambiguous

The config example shows `"claude": "pirate"` but users configure targets, not tools. A target named `claude-opus-4-6` runs on the `claude` tool. The design should clarify whether the config key is the target name (e.g., `claude-opus-4-6`), the base tool name (e.g., `claude`), or the agent type string. The base tool name is most natural but the design should be explicit.

### 4. The design references `## Role:` in the composed prompt but the actual code uses `## Persona:`

The design's "Composed prompt structure" example shows `## Role: Security Auditor` but `formatPersonaPrompt()` in `handlers_spawn.go:821` outputs `## Persona: %s`. The design should use the actual heading or explicitly note that the heading is being renamed.

### 5. The `StyleUpdateRequest` uses pointer fields but no ID mutation protection

The `StyleUpdateRequest` allows partial updates via optional pointer fields, which is good. But unlike `PersonaUpdateRequest`, it includes no validation that the style exists before applying updates. The persona handler fetches the existing record first (`s.personaManager.Get(id)`) and applies non-nil fields. Make sure the design calls this out since the persona handler code is the template.

### 6. 25 built-in styles is a lot -- consider starting with 10-15

11 built-in personas exist today. 25 built-in styles means the styles page will be 2.5x denser. More importantly, writing 25 high-quality prompt files that actually produce distinct and enjoyable communication styles is a significant content authoring effort. Several of the proposed styles (particularly celebrity impersonations) may produce inconsistent results across different LLMs. Consider launching with a curated set of ~12 and expanding based on user feedback.

### 7. No mention of validation for the `style_id` on `SpawnRequest`

If the user sends `style_id: "nonexistent"`, the design should specify behavior. For `persona_id`, the handler returns a 400 error. The style_id should get the same treatment.

### 8. The session state should store `style_id` (like it stores `persona_id`)

The design says "No style indicator on session cards" but doesn't specify whether `style_id` is persisted on the session state. `PersonaID` is stored on `state.Session` (line 881 of `manager.go`) and used for badges. Even if no badge is shown, storing the style_id enables future introspection and debugging (e.g., "why is this session talking like a pirate?").

## Verified Claims (things I confirmed are correct)

1. **`formatPersonaPrompt()` exists and works as described.** Located at `handlers_spawn.go:818-827`. It formats persona content with `## Persona:` header, optional `### Behavioral Expectations`, and `### Instructions` section. The design's plan to rename it to `formatAgentSystemPrompt()` and extend it is sound for the Claude path.

2. **`appendPersonaFlags()` exists and dispatches by `PersonaInjection()`.** Located at `manager.go:1141-1158`. It correctly handles `PersonaCLIFlag` by appending CLI args, and returns the command unchanged for other injection methods. The design's claim that this function doesn't need changes is correct for the Claude path (since the composed file is just written to the same path).

3. **`SpawnRemote()` genuinely lacks persona support.** Confirmed at `manager.go:390` and `handlers_spawn.go:353`. The function signature has no persona parameters, and the handler doesn't pass `personaPrompt` or `PersonaID` to it.

4. **The config struct (`internal/config/config.go:66`) can accommodate a new `comm_styles` field.** The struct uses flat fields with `json` tags and the file is already large. Adding `CommStyles map[string]string \`json:"comm_styles,omitempty"\``is straightforward. The`ConfigUpdateRequest` contract would also need a corresponding update field.

5. **Claude's `--append-system-prompt-file` does not conflict with signaling flags.** Claude uses `SignalingHooks` strategy (not `SignalingCLIFlag`), so `appendSignalingFlags()` returns the command unchanged for Claude. Only `appendPersonaFlags()` adds `--append-system-prompt-file`. Multiple `--append-system-prompt-file` flags are supported by Claude Code (confirmed by the existence of both `SignalingArgs` and `PersonaArgs` returning this flag -- they just happen to never both be active for Claude).

6. **The persona CRUD pattern in handlers/contracts is well-established.** `handlers_personas.go` and `contracts/persona.go` provide a clean template. The style CRUD endpoints would follow the same structure with minimal adaptation.

7. **The spawn wizard already has per-mode persona dropdowns.** The `SpawnPage.tsx` renders persona selects in three places: single agent + fresh mode (line 1129), single agent + workspace mode (line 1230), and multiple/advanced mode (line 1447). Each would need a corresponding style dropdown.
