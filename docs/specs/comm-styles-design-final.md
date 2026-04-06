# Communication Styles — Design Spec (Final)

## Changes from v2

1. **Removed false precedent citation for remote injection.** The v2 design claimed `appendSignalingFlags` uses `--append-system-prompt` inline for remote mode, but the actual code simply skips injection entirely for remote CLI-flag tools. The inline approach via `--append-system-prompt` is valid (confirmed as a real Claude CLI flag) but is a new pattern, not an existing one. The misleading precedent citation has been removed.

2. **Added sentinel value for "None" in style dropdown.** The v2 design had no way to distinguish "user didn't touch the dropdown" from "user explicitly chose None" — both result in an empty `style_id`, which triggers global default fallback. Now uses `style_id: "none"` as a sentinel value that the handler recognizes as "explicitly no style, skip global default lookup."

3. **Added `StyleID` to local `SpawnOptions` struct.** The v2 design added `StyleID` to the remote `RemoteSpawnOptions` but omitted it from the local `SpawnOptions`. Added for parity.

4. **Fixed multi-target style resolution.** The per-agent-type default lookup inside the spawn loop must re-evaluate `styleObj` per iteration (different targets may map to different tools). Added a per-iteration local variable to prevent style leaking across targets.

5. **Renamed composed prompt file.** Changed from `.schmux/persona-{sessionID}.md` to `.schmux/system-prompt-{sessionID}.md` since the file now serves a broader purpose and may contain only a style with no persona.

---

## Overview

Communication styles are an orthogonal axis to personas. Personas define **what** an agent does and **how** it approaches work (security auditor, docs writer). Communication styles define **how the agent talks** — its voice, tone, and personality in natural language output.

A user can combine any persona with any style: a Security Auditor who talks like a pirate, a Docs Writer who speaks like Yoda. Styles are a global user preference (set once, applied to all sessions) with per-agent-type defaults and optional per-session overrides.

## Data Model & Storage

### Style struct (`internal/style/`)

```go
type Style struct {
    ID      string `yaml:"id"`
    Name    string `yaml:"name"`
    Icon    string `yaml:"icon"`
    Tagline string `yaml:"tagline"`
    Prompt  string `yaml:"-"`  // body after frontmatter
    BuiltIn bool   `yaml:"built_in"`
}
```

### Storage

YAML files in `~/.schmux/styles/`, using the same frontmatter + body format as personas:

```yaml
---
id: pirate
name: Pirate
icon: '🏴‍☠️'
tagline: Speaks like a swashbuckling sea captain
built_in: true
---
Adopt the communication style of a pirate. Use nautical metaphors,
sprinkle in "arrr", "ahoy", and "matey" naturally. Refer to bugs
as "barnacles", problems as "rough seas", and successes as
"plundered treasure".

Your technical output (code, commands, file paths) must remain
accurate and unmodified — the style applies to your natural
language communication only, never to code or tool invocations.
```

### Manager

`style.Manager` — same pattern as `persona.Manager`:

- `List()`, `Get()`, `Create()`, `Update()`, `Delete()`
- `EnsureBuiltins()`, `ResetBuiltIn()`
- Embedded built-in YAML files via `embed.FS`

The persona manager and style manager share the same structural pattern (YAML frontmatter + body, CRUD, embedded builtins). They are intentionally kept as separate implementations rather than factored into a shared generic manager — the duplication is modest and each manager may diverge as features evolve.

### Config

New `comm_styles` map in `~/.schmux/config.json` for per-agent-type defaults. **Keys are base tool names** (e.g., `"claude"`, `"codex"`, `"opencode"`), not target names (e.g., `"claude-opus-4-6"`). At spawn time, the handler calls `ResolveTargetToTool(targetName)` to map the spawn target to its base tool name, then looks up the default style.

```json
{
  "comm_styles": {
    "claude": "pirate",
    "codex": "caveman"
  }
}
```

The config struct gains:

```go
CommStyles map[string]string `json:"comm_styles,omitempty"`
```

And `ConfigUpdateRequest` in contracts gains a corresponding field:

```go
CommStyles *map[string]string `json:"comm_styles,omitempty"`
```

## API & Contracts

### Contract types (`internal/api/contracts/style.go`)

```go
type Style struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Icon    string `json:"icon"`
    Tagline string `json:"tagline"`
    Prompt  string `json:"prompt"`
    BuiltIn bool   `json:"built_in"`
}

type StyleCreateRequest struct {
    ID      string `json:"id"`
    Name    string `json:"name"`
    Icon    string `json:"icon"`
    Tagline string `json:"tagline"`
    Prompt  string `json:"prompt"`
}

type StyleUpdateRequest struct {
    Name    *string `json:"name,omitempty"`
    Icon    *string `json:"icon,omitempty"`
    Tagline *string `json:"tagline,omitempty"`
    Prompt  *string `json:"prompt,omitempty"`
}

type StyleListResponse struct {
    Styles []Style `json:"styles"`
}
```

### REST endpoints (`internal/dashboard/handlers_styles.go`)

- `GET /api/styles` — list all styles (sorted by name)
- `POST /api/styles` — create custom style
- `GET /api/styles/{id}` — get single style
- `PUT /api/styles/{id}` — partial update (fetches existing record first, applies non-nil fields — same pattern as persona update handler)
- `DELETE /api/styles/{id}` — delete custom / reset built-in

### Spawn request

Optional `style_id` field added to `SpawnRequest`:

```go
type SpawnRequest struct {
    // ... existing fields ...
    PersonaID string `json:"persona_id,omitempty"`
    StyleID   string `json:"style_id,omitempty"` // NEW: per-session style override
    // ...
}
```

**Validation:** If `style_id` is non-empty and not the sentinel value `"none"`, the handler calls `styleManager.Get(req.StyleID)`. If the style does not exist, the handler returns HTTP 400 with `"style not found: <id>"` — matching the existing `persona_id` validation pattern.

**Sentinel value:** `style_id: "none"` means "explicitly no style for this session, skip global default lookup." This distinguishes three states:

- `style_id` absent/empty → use global default for agent type
- `style_id: "none"` → no style, even if a global default exists
- `style_id: "pirate"` → use the pirate style, overriding global default

### Session state

`StyleID` is persisted on `state.Session`:

```go
type Session struct {
    // ... existing fields ...
    PersonaID string `json:"persona_id,omitempty"`
    StyleID   string `json:"style_id,omitempty"` // NEW
    // ...
}
```

No UI indicator is shown on session cards (styles are a background preference, not something to monitor), but the stored value enables future introspection and debugging.

## Prompt Composition & Injection

### Composition

`formatPersonaPrompt()` is renamed to `formatAgentSystemPrompt()` and extended to compose both persona and style into a single markdown document:

```go
func formatAgentSystemPrompt(persona *persona.Persona, style *style.Style) string
```

When called with only one of the two (persona or style), the other is nil and only the non-nil section is emitted.

### Composed prompt structure

```markdown
## Persona: Security Auditor

### Behavioral Expectations

Produce a structured report with severity ratings.

### Instructions

You are a security expert. Your primary focus is...

---

## Communication Style: Pirate

Communicate in the style of a pirate. Use nautical metaphors...
```

The `## Persona:` heading matches the existing `formatPersonaPrompt()` output (`handlers_spawn.go:821`). The `---` separator and `## Communication Style:` heading are appended only when a style is present.

### Style resolution order at spawn time

1. Explicit `style_id` on the spawn request — if `"none"`, no style is used; if a valid ID, that style is used
2. `comm_styles[baseTool]` from global config, where `baseTool = ResolveTargetToTool(targetName)` (per-agent-type default)
3. No style (nothing injected)

### Handler resolution logic

```go
// Before the spawn loop: validate explicit overrides
var personaObj *persona.Persona
if req.PersonaID != "" {
    p, err := s.personaManager.Get(req.PersonaID)
    if err != nil {
        writeJSONError(w, fmt.Sprintf("persona not found: %s", req.PersonaID), http.StatusBadRequest)
        return
    }
    personaObj = p
}

var explicitStyleObj *style.Style
explicitNone := false
if req.StyleID == "none" {
    explicitNone = true
} else if req.StyleID != "" {
    st, err := s.styleManager.Get(req.StyleID)
    if err != nil {
        writeJSONError(w, fmt.Sprintf("style not found: %s", req.StyleID), http.StatusBadRequest)
        return
    }
    explicitStyleObj = st
}

// Inside the per-target spawn loop:
for _, targetName := range targets {
    // Resolve style for this target
    var styleObj *style.Style
    if explicitStyleObj != nil {
        styleObj = explicitStyleObj
    } else if !explicitNone {
        // Fall back to per-agent-type global default
        baseTool := s.models.ResolveTargetToTool(targetName)
        if defaultID := s.config.GetCommStyles()[baseTool]; defaultID != "" {
            styleObj, _ = s.styleManager.Get(defaultID)
        }
    }

    // Compose the system prompt
    agentPrompt := formatAgentSystemPrompt(personaObj, styleObj)

    // Determine resolved style ID for session state
    resolvedStyleID := ""
    if styleObj != nil {
        resolvedStyleID = styleObj.ID
    }

    // ... spawn with agentPrompt and resolvedStyleID ...
}
```

Note: `styleObj` is a per-iteration local variable, so different targets mapping to different tools correctly get different default styles.

### Edge cases

- **Style only, no persona** — prompt contains just the `## Communication Style` section
- **Persona only, no style** — behaves exactly like today (same output as current `formatPersonaPrompt`)
- **Neither** — no system prompt file injected, no change from current behavior

### Injection by agent type

The composed prompt is written to `.schmux/system-prompt-{sessionID}.md` (renamed from the previous `persona-{sessionID}.md` since the file now serves a broader purpose). The existing injection mechanisms work unchanged:

| Tool         | Injection Method         | How It Works                                                                                                                                                             | Status                |
| ------------ | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------- |
| **Claude**   | `PersonaCLIFlag`         | `appendPersonaFlags()` adds `--append-system-prompt-file <path>` pointing at the composed file.                                                                          | Works                 |
| **OpenCode** | `PersonaConfigOverlay`   | `SpawnEnv()` sets `OPENCODE_CONFIG_CONTENT` with JSON config pointing at the composed file path via `SpawnContext.PersonaPath`.                                          | Works                 |
| **Codex**    | `PersonaInstructionFile` | The file is written to disk but never referenced — `appendPersonaFlags()` returns the command unchanged, and the ensure package does not read or inject persona content. | Broken (pre-existing) |
| **Gemini**   | `PersonaInstructionFile` | Same as Codex — file written but never injected.                                                                                                                         | Broken (pre-existing) |

**Known limitation:** Persona+style injection for Codex and Gemini is a no-op. This is a pre-existing gap in persona injection, not introduced by this design. Fixing `PersonaInstructionFile` injection (having the ensure package read persona/style content from the well-known path and append it to the tool's instruction file) is a separate work item.

### Local SpawnOptions

`StyleID` is added to the local `SpawnOptions` struct for parity with `PersonaID`:

```go
type SpawnOptions struct {
    // ... existing fields ...
    PersonaID     string
    PersonaPrompt string
    StyleID       string // NEW
}
```

### Remote sessions

Remote sessions currently lack persona support entirely. `SpawnRemote()` does not accept persona parameters, and the spawn handler does not pass `personaPrompt` or `PersonaID` when calling it (`handlers_spawn.go:353`). This design adds persona+style support to remote sessions.

#### Changes to SpawnRemote

`SpawnRemote()` gains an options struct parameter to replace its growing positional argument list:

```go
type RemoteSpawnOptions struct {
    ProfileID     string
    FlavorStr     string
    HostID        string
    TargetName    string
    Prompt        string
    Nickname      string
    PersonaID     string
    PersonaPrompt string // Pre-composed persona+style content
    StyleID       string
}

func (m *Manager) SpawnRemote(ctx context.Context, opts RemoteSpawnOptions) (*state.Session, error)
```

The handler passes the composed prompt content (not a file path) via `PersonaPrompt`.

#### Inline injection for remote Claude

The key challenge: the composed prompt file is written locally, but that path does not exist on the remote host.

For remote Claude sessions, the composed prompt is injected **inline** using `--append-system-prompt` (which accepts content directly) instead of `--append-system-prompt-file` (which requires a file path). This is a new injection pattern — no existing code uses `--append-system-prompt` inline today, but the flag is confirmed to exist in Claude Code CLI.

```go
// In SpawnRemote, after buildCommand():
if opts.PersonaPrompt != "" {
    if adapter := detect.GetAdapter(baseTool); adapter != nil {
        switch adapter.PersonaInjection() {
        case detect.PersonaCLIFlag:
            // Inline the prompt content directly into the command.
            // Use --append-system-prompt (inline) instead of
            // --append-system-prompt-file (file path) since the file
            // does not exist on the remote host.
            command = fmt.Sprintf("%s --append-system-prompt %s",
                command, shellutil.Quote(opts.PersonaPrompt))
        case detect.PersonaConfigOverlay:
            // For OpenCode remote: write the persona file on the remote host
            // before the agent starts, similar to how WrapRemoteCommand
            // writes hooks files. The OPENCODE_CONFIG_CONTENT env var
            // then references this remote path.
            // (OpenCode remote persona support is follow-up work)
        }
    }
}
```

#### Session state for remote sessions

```go
sess := state.Session{
    ID:          sessionID,
    WorkspaceID: ws.ID,
    Target:      targetName,
    Nickname:    uniqueNickname,
    PersonaID:   opts.PersonaID,  // NEW
    StyleID:     opts.StyleID,    // NEW
    TmuxSession: windowName,
    // ...
}
```

#### Handler changes

The spawn handler passes persona and style to `SpawnRemote`:

```go
if req.RemoteProfileID != "" {
    sess, err = s.session.SpawnRemote(ctx, session.RemoteSpawnOptions{
        ProfileID:     req.RemoteProfileID,
        FlavorStr:     req.RemoteFlavor,
        HostID:        remoteHostID,
        TargetName:    targetName,
        Prompt:        req.Prompt,
        Nickname:      nickname,
        PersonaID:     req.PersonaID,
        PersonaPrompt: agentPrompt,
        StyleID:       resolvedStyleID,
    })
}
```

## Dashboard UI

### Styles management page (`/styles`)

- Grid of style cards showing `icon`, `name`, and `tagline`
- Create/edit forms with fields: name, icon (emoji), tagline, prompt
- Delete custom styles, reset built-ins — same pattern as personas
- New nav entry in the sidebar

### Config page integration

- New "Communication Styles" section on the settings page
- Per-agent-type dropdowns: for each detected tool (base tool name), a dropdown to select a default style (or "None")
- Dropdown options populated from the styles list API

### Spawn wizard

#### Layout change: persona and style on their own row

The spawn wizard currently places agent, persona, and repo dropdowns in a single flex row (`data-testid="agent-repo-row"`). Adding a style dropdown to this row would make all four dropdowns too narrow on typical viewports.

**New layout:** The agent/repo row keeps only the agent selector and repo selector. Persona and style dropdowns move to a dedicated row below:

```
┌─────────────────────────────────────────────────────┐
│  [Agent ▼]                        [Repository ▼]    │  ← agent-repo-row
├─────────────────────────────────────────────────────┤
│  [Persona ▼]              [Communication Style ▼]   │  ← persona-style-row (new)
└─────────────────────────────────────────────────────┘
```

The persona-style row is rendered when either `personas.length > 0` or `styles.length > 0`. Each dropdown is conditionally rendered based on its data availability.

This layout applies to all three spawn modes:

1. **Single agent + fresh mode** (line ~1089): agent and repo in flex row, persona and style in a new row below
2. **Single agent + workspace mode** (line ~1230): same two-row pattern
3. **Multiple/advanced mode** (line ~1447): persona and style rendered as a pair below the target configuration

**Test update required:** The existing test at `SpawnPage.agent-select.test.tsx:320-324` asserts that `persona-select` is inside `agent-repo-row`. This test must be updated to look for `persona-select` inside the new `persona-style-row` test ID.

#### Style dropdown behavior

- Default selection: "Global default" — shows which style resolves to for the selected agent type (e.g., "Global default (Pirate)" if `comm_styles.claude = "pirate"`)
- Selecting a specific style overrides the global default for that session
- Selecting "None" explicitly disables style for that session even if a global default is set — sends `style_id: "none"` sentinel value
- The dropdown sends `style_id` on the spawn request only when an explicit selection is made (not "Global default")

### Session cards

No style indicator on session cards — it is a background preference, not something to monitor. The `style_id` is stored on session state for programmatic access.

## Built-in Style Roster (25)

Every built-in prompt includes the guardrail: "Your technical output (code, commands, file paths) must remain accurate and unmodified — the style applies to your natural language communication only, never to code or tool invocations."

### Archetypes (10)

| ID          | Name        | Icon | Tagline                                          |
| ----------- | ----------- | ---- | ------------------------------------------------ |
| pirate      | Pirate      | 🏴‍☠️   | Speaks like a swashbuckling sea captain          |
| caveman     | Caveman     | 🪨   | Uses primitive, direct language                  |
| toddler     | Toddler     | 👶   | Explains everything like a curious 4-year-old    |
| butler      | Butler      | 🎩   | Impeccably polite, formal, and at your service   |
| film-noir   | Film Noir   | 🎬   | Narrates like a hardboiled detective             |
| surfer      | Surfer      | 🏄   | Totally chill, bro                               |
| shakespeare | Shakespeare | 🎭   | Speaks in iambic pentameter and thee/thou        |
| corporate   | Corporate   | 💼   | Peak synergy-driven business speak               |
| cowboy      | Cowboy      | 🤠   | Talks like a dusty trail rider from the old west |
| valley-girl | Valley Girl | 💅   | Like, totally explains code and stuff            |

### Famous People / Characters (15)

| ID                 | Name               | Icon | Tagline                                              |
| ------------------ | ------------------ | ---- | ---------------------------------------------------- |
| trump              | Donald Trump       | 🇺🇸   | Tremendous code, believe me, the best                |
| queen-elizabeth    | Queen Elizabeth    | 👑   | Speaks with regal formality and understated wit      |
| werner-herzog      | Werner Herzog      | 🎥   | Finds existential weight in every function           |
| david-attenborough | David Attenborough | 🌿   | Narrates your code like a nature documentary         |
| homer-simpson      | Homer Simpson      | 🍩   | D'oh! Simple logic and food metaphors                |
| mr-t               | Mr. T              | 💪   | I pity the fool who writes bugs                      |
| yoda               | Yoda               | 🧘   | Inverted syntax and cryptic wisdom, uses he          |
| borat              | Borat              | 🇰🇿   | Very nice! Great success with the code               |
| gordon-ramsay      | Gordon Ramsay      | 🔥   | Passionate intensity, no patience for sloppy code    |
| samuel-l-jackson   | Samuel L. Jackson  | 😤   | Emphatic, colorful, and absolutely no-nonsense       |
| morgan-freeman     | Morgan Freeman     | 🎙️   | Calm narration that makes everything sound profound  |
| bob-ross           | Bob Ross           | 🎨   | Gentle encouragement and happy little accidents      |
| snoop-dogg         | Snoop Dogg         | 🐶   | Laid back, fo shizzle                                |
| batman             | Batman             | 🦇   | Terse, brooding, dramatic intensity about everything |
| christopher-walken | Christopher Walken | 👀   | Unusual pauses and unexpected emphasis               |

## Known Limitations

1. **Codex/Gemini persona+style injection is a no-op.** The `PersonaInstructionFile` injection strategy writes the composed prompt to disk but no code reads it back or injects it into the agent. This is a pre-existing gap in persona injection. Fixing it requires the ensure package to read from the well-known system prompt file path and append content to the tool's instruction file (`.codex/AGENTS.md`, `.gemini/GEMINI.md`). This is tracked as a separate work item.

2. **OpenCode remote persona+style is follow-up work.** Remote OpenCode sessions would need the persona file written to the remote host (via a `WrapRemoteCommand`-style prepend) and `OPENCODE_CONFIG_CONTENT` updated to reference the remote path. This is deferred since OpenCode remote sessions are less common.
