VERDICT: NEEDS_REVISION

## Summary Assessment

The v2 design adequately addressed the three critical issues from the first review: it acknowledged Codex/Gemini injection as broken, added a concrete remote injection plan, and specified spawn wizard layout changes. However, the remote injection plan is built on a factual mischaracterization of existing code, and a new critical issue was introduced around how the "None" option interacts with the global default fallback.

## Critical Issues (must fix)

### 1. The design mischaracterizes how `appendSignalingFlags` handles remote mode -- undermining the precedent it cites

The design at line 325 says:

> This mirrors how `appendSignalingFlags` already handles remote mode -- it uses inline content when file paths are not available on the remote host (`manager.go:1104-1106`).

This is factually wrong. The actual code at `manager.go:1120-1124` does NOT use inline content for remote mode. It simply **skips signaling injection entirely** for remote CLI-flag tools:

```go
if strategy == detect.SignalingCLIFlag {
    if isRemote {
        // Remote mode: CLI-flag tools reference local file paths that don't exist
        // on the remote host, so skip signaling injection.
        return cmd
    }
```

The function comment at line 1104 is misleading ("In remote mode, uses inline content (--append-system-prompt)") but the code does not do this -- it returns `cmd` unchanged. The design cites this as an established pattern for inline injection, but the pattern does not exist.

This matters because the design's proposed approach (using `--append-system-prompt` inline for remote Claude sessions) is **valid in principle** -- I confirmed that `--append-system-prompt <prompt>` is a real Claude Code CLI flag that accepts inline content. But the design should not cite `appendSignalingFlags` as precedent when that function does the opposite of what the design claims.

**Fix:** Remove the claim that `appendSignalingFlags` uses inline content. Acknowledge that this would be a new pattern (inline CLI content for remote sessions). The approach itself is sound -- the precedent citation is not.

### 2. "None" option in the style dropdown has no wire representation and cannot override the global default

The design at line 407 says:

> Selecting "None" explicitly disables style for that session even if a global default is set

But the design does not specify how "None" is represented on the `SpawnRequest`. The `style_id` field is `string` with `omitempty`. If the dropdown sends an empty string, `omitempty` will omit it from the JSON, and the handler will treat it as "no explicit selection" and fall through to the global default. There is no way for the client to distinguish between "user didn't touch the dropdown" (use global default) and "user explicitly chose None" (suppress global default).

This is the same problem personas avoid because personas have no global default fallback -- an empty `persona_id` always means "no persona." But styles have a three-level resolution (explicit > global default > none), so the absence of a value is ambiguous.

**Fix:** Define a sentinel value for "None" (e.g., `style_id: "none"` or `style_id: "__none__"`) that the handler recognizes as "explicitly no style, skip global default lookup." Alternatively, add a separate boolean field `style_none: true` to the spawn request. The design must specify the wire protocol for this important case.

## Suggestions (nice to have)

### 1. The per-agent-type default lookup has a subtle bug when spawning multiple targets

The design at line 236-243 shows the per-agent-type default lookup happening inside the spawn loop, but the `styleObj` variable is declared before the loop and reused. If a multi-target spawn request has targets mapping to different tools (e.g., `claude` and `codex`), the `styleObj` resolved for the first target's tool would leak into subsequent iterations. The design should clarify that `styleObj` must be re-evaluated per iteration if no explicit `style_id` was provided:

```go
// Current design pseudocode (problematic):
var styleObj *style.Style  // declared once before loop
// Inside loop: if styleObj == nil { look up default }

// Problem: once styleObj is set for target A's tool,
// target B (different tool) inherits target A's style
```

The fix is straightforward -- reset `styleObj` to `nil` at the start of each iteration when no explicit `style_id` was provided, or use a per-iteration local variable.

### 2. The `SpawnOptions` struct needs `StyleID` for local sessions too

The design specifies `StyleID` on the remote `RemoteSpawnOptions` struct (line 288) and on `state.Session` (line 164), but does not mention adding `StyleID` to the existing local `SpawnOptions` struct (`manager.go:652-666`). Currently `SpawnOptions` has `PersonaID` and `PersonaPrompt`. It would need a corresponding `StyleID` field so the session manager can persist it on the session state. This is likely an oversight since the handler already resolves the style and composes it into the prompt, but the `StyleID` needs to flow through to the session state.

### 3. The existing test asserts persona is inside `agent-repo-row` -- this test will break

The test at `SpawnPage.agent-select.test.tsx:320-324` explicitly asserts:

```typescript
const row = screen.getByTestId('agent-repo-row');
expect(within(row).getByTestId('agent-select')).toBeInTheDocument();
expect(within(row).getByTestId('persona-select')).toBeInTheDocument();
expect(within(row).getByTestId('spawn-repo-select')).toBeInTheDocument();
```

The design's layout change moves persona out of `agent-repo-row` into a new `persona-style-row`. This test will break and needs to be updated. The design should note this as a required test change.

### 4. Consider whether the composed prompt file should be renamed

The design says the composed prompt is written to `.schmux/persona-{sessionID}.md`. When the file contains only a style and no persona, this naming is misleading. Consider renaming to `.schmux/system-prompt-{sessionID}.md` or similar, since the file now serves a broader purpose.

### 5. The design does not specify how `resolvedStyleID` is derived in the handler code

The handler pseudocode at line 359 passes `resolvedStyleID` to the remote spawn options, but this variable is never defined in the design's code snippets. It should be the ID of whichever style was ultimately resolved (explicit or global default), stored for the session state. The design should show this resolution explicitly.

## Verified Claims (things I confirmed are correct)

1. **`--append-system-prompt` (inline) is a real Claude Code CLI flag.** Confirmed via `claude --help`: `--append-system-prompt <prompt>` "Append a system prompt to the default system prompt." This is distinct from `--append-system-prompt-file <file>`. The design's remote injection approach of using this flag for inline content is valid.

2. **`PersonaArgs()` for Claude returns `--append-system-prompt-file`.** Confirmed at `adapter_claude.go:81`: `return []string{"--append-system-prompt-file", filePath}`. The local injection path is correctly documented.

3. **`SpawnRemote()` genuinely lacks persona support.** Still confirmed at `manager.go:390` -- positional string args, no persona parameters. The handler at `handlers_spawn.go:353` still does not pass `personaPrompt` or `PersonaID` to it.

4. **`ResolveTargetToTool()` exists and works as described.** Confirmed at `models/manager.go:384-397`. It resolves a target name to its base tool name (e.g., `claude-opus-4-6` resolves to `claude`). Tests confirm this at `manager_test.go:277-298`.

5. **The `## Persona:` heading in `formatPersonaPrompt()` matches the design.** Confirmed at `handlers_spawn.go:821`: `fmt.Fprintf(&b, "## Persona: %s\n\n", p.Name)`. The v2 design correctly uses this heading.

6. **Codex/Gemini persona injection is genuinely broken.** `appendPersonaFlags()` at `manager.go:1147-1158` returns `cmd` unchanged for `PersonaInstructionFile` tools. The ensure package has no persona-related code. The v2 design correctly identifies this as a pre-existing gap.

7. **The spawn wizard has persona dropdowns in three locations.** Confirmed at `SpawnPage.tsx:1129` (fresh mode), `SpawnPage.tsx:1230` (workspace mode), and `SpawnPage.tsx:1447` (multiple/advanced mode). All three would need style dropdowns added alongside.

8. **`state.Session` has `PersonaID` at line 326.** Confirmed. Adding `StyleID` alongside it is straightforward.

9. **The `SpawnContext` struct uses `PersonaPath` (not a generic name).** Confirmed at `adapter.go:38-42`. The composed prompt path can use the same field since the file path doesn't change -- only the content broadens.
