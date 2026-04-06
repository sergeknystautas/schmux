# Communication Styles — Design Spec

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

### Config

New `comm_styles` map in `~/.schmux/config.json` for per-agent-type defaults:

```json
{
  "comm_styles": {
    "claude": "pirate",
    "codex": "caveman"
  }
}
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
- `PUT /api/styles/{id}` — partial update
- `DELETE /api/styles/{id}` — delete custom / reset built-in

### Spawn request

Optional `style_id` field added to `SpawnRequest`. When set, overrides the global per-agent default for that session.

## Prompt Composition & Injection

### Composition

`formatPersonaPrompt()` becomes `formatAgentSystemPrompt()` which composes both persona and style into a single markdown document:

```go
func formatAgentSystemPrompt(persona *persona.Persona, style *style.Style) string
```

### Composed prompt structure

```markdown
## Role: Security Auditor

### Expectations

Produce a structured report with severity ratings.

### Instructions

You are a security expert. Your primary focus is...

---

## Communication Style: Pirate

Communicate in the style of a pirate. Use nautical metaphors...
```

### Style resolution order at spawn time

1. Explicit `style_id` on the spawn request (per-session override)
2. `comm_styles[agentType]` from global config (per-agent default)
3. No style (nothing injected)

### Edge cases

- **Style only, no persona** — prompt contains just the `## Communication Style` section
- **Persona only, no style** — behaves exactly like today
- **Neither** — no system prompt file injected, no change from current behavior

### No adapter changes

Since we compose into a single file before injection, `appendPersonaFlags()` and all agent adapters (Claude, Codex, Gemini, OpenCode) remain untouched.

### Remote sessions

`SpawnRemote()` gets the same style resolution logic. The composed prompt is written to the workspace and injected using the same adapter mechanism as local sessions.

## Dashboard UI

### Styles management page (`/styles`)

- Grid of style cards showing `icon`, `name`, and `tagline`
- Create/edit forms with fields: name, icon (emoji), tagline, prompt
- Delete custom styles, reset built-ins — same pattern as personas
- New nav entry in the sidebar

### Config page integration

- New "Communication Style" section on the settings page
- Per-agent-type dropdowns: for each configured agent type, a dropdown to select a default style (or "None")
- Dropdown options populated from the styles list API

### Spawn wizard

- Optional style dropdown added to the spawn form
- Defaults to "Global default" (shows which style that resolves to for the selected agent type)
- Selecting a specific style overrides the global default for that session
- Selecting "None" explicitly disables style for that session even if a global default is set

### Session cards

No style indicator on session cards — it's a background preference, not something to monitor.

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
