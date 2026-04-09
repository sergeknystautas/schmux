# Communication Styles

## What it does

Communication styles define **how an agent talks** — its voice, tone, and personality in natural language output. They are orthogonal to personas: a persona defines **what** an agent does and **how** it approaches work (security auditor, docs writer), while a style defines the agent's voice. Any persona can be combined with any style: a Security Auditor who talks like a pirate, a Docs Writer who speaks like Yoda.

Styles are a global user preference (set once per agent type) with optional per-session overrides via the spawn wizard.

## Key files

| File                                    | Purpose                                                                    |
| --------------------------------------- | -------------------------------------------------------------------------- |
| `internal/style/manager.go`             | `Manager` struct: CRUD operations, `EnsureBuiltins()`, `ResetBuiltIn()`    |
| `internal/style/parse.go`               | `Style` struct, `ParseStyle()` (YAML frontmatter + body), `MarshalStyle()` |
| `internal/style/builtins.go`            | `embed.FS` directive embedding `builtins/*.yaml`                           |
| `internal/style/builtins/*.yaml`        | 25 built-in style definitions                                              |
| `internal/dashboard/handlers_styles.go` | REST API: `GET/POST /api/styles`, `GET/PUT/DELETE /api/styles/{id}`        |
| `internal/api/contracts/style.go`       | Contract types for style API                                               |

## Architecture decisions

- **YAML frontmatter + body format.** Same format as personas. The frontmatter carries metadata (id, name, icon, tagline, built_in). The body after the closing `---` is the style prompt. Easy to author in both a text editor and the dashboard UI.
- **Shared structural pattern with personas, separate implementations.** The style manager and persona manager have the same CRUD/embedded-builtins shape. They are intentionally kept as separate implementations rather than factored into a shared generic manager — the duplication is modest and each may diverge independently.
- **Per-agent-type defaults via `comm_styles` config.** A `comm_styles` map in `~/.schmux/config.json` maps base tool names (e.g., `"claude"`, `"codex"`) to default style IDs. At spawn time, the handler resolves the target to its base tool name via `ResolveTargetToTool()`, then looks up the default. Command targets use their target name directly as the key.
- **Three-state resolution at spawn.** (1) Explicit `style_id` on spawn request — overrides everything. (2) `style_id: "none"` sentinel — explicitly disables style for this session, even if a global default exists. (3) Empty/absent `style_id` — falls back to per-agent-type default from config.
- **Style applies to natural language only.** Every built-in prompt includes: "Your technical output (code, commands, file paths) must remain accurate and unmodified — the style applies to your natural language communication only."
- **Built-ins embedded in binary.** `EnsureBuiltins()` copies them to `~/.schmux/styles/` on first run only (never overwrites). `ResetBuiltIn()` restores the original from embedded FS.
- **No session card indicator.** Styles are a background preference, not something to monitor. The `style_id` is stored on session state for programmatic access.

## Prompt composition

`formatAgentSystemPrompt()` (renamed from `formatPersonaPrompt()`) composes both persona and style into a single markdown document:

```markdown
## Persona: Security Auditor

### Behavioral Expectations

Produce a structured report with severity ratings.

### Instructions

You are a security expert...

---

## Communication Style: Pirate

Communicate in the style of a pirate...
```

When only one is present, the other section is omitted. When neither is present, no system prompt file is created.

The composed prompt is written to `.schmux/system-prompt-{sessionID}.md` and injected via agent-native mechanisms:

| Tool         | Injection Method                                                          | Status                |
| ------------ | ------------------------------------------------------------------------- | --------------------- |
| **Claude**   | `--append-system-prompt-file` (local) / `--append-system-prompt` (remote) | Works                 |
| **OpenCode** | `OPENCODE_CONFIG_CONTENT` env var                                         | Works                 |
| **Codex**    | File written but not injected                                             | Broken (pre-existing) |
| **Gemini**   | File written but not injected                                             | Broken (pre-existing) |

## Config

```json
{
  "comm_styles": {
    "claude": "pirate",
    "codex": "caveman"
  }
}
```

Keys are base tool names, not target names. The `CommStyles` field on the config struct is `map[string]string` with `json:"comm_styles,omitempty"`.

## Gotchas

- The `Prompt` field uses `yaml:"-"` so it is excluded from frontmatter marshaling. It is populated from the body text after the closing `---` delimiter during `ParseStyle()`.
- `ParseStyle()` requires both opening and closing `---\n` delimiters. Files without frontmatter fail to parse.
- `Manager.List()` silently skips files that fail to parse or read. A malformed YAML file does not break the listing.
- `Manager.Create()` checks for existence before writing and fails if the ID already exists.
- The styles directory is created with `0700` permissions (user-only).
- The style resolution in the spawn loop uses a per-iteration local variable (`styleObj`), so different targets mapping to different tools correctly get different default styles.
- `style_id: "none"` is a sentinel value, not an actual style ID. The handler checks for it before attempting `styleManager.Get()`.

## Built-in styles (25)

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

## Common modification patterns

- **To add a new built-in style:** Create a YAML file in `internal/style/builtins/` with frontmatter (id, name, icon, tagline, `built_in: true`) and a body containing the style prompt. The `go:embed` directive picks it up automatically.
- **To change the style file format:** Edit `ParseStyle()` and `MarshalStyle()` in `internal/style/parse.go`. The `Style` struct fields must stay in sync.
- **To change how styles are composed with personas:** Edit `formatAgentSystemPrompt()` in `internal/dashboard/handlers_spawn.go`.
- **To add per-agent-type default styles:** Set keys in `comm_styles` config. Keys are base tool names (e.g., `"claude"`) resolved via `ResolveTargetToTool()`, or command target names used directly.
- **To add style management UI:** The API is already complete (`GET/POST/PUT/DELETE`). The dashboard page at `/styles` consumes `internal/dashboard/handlers_styles.go`.

## See Also

- [Personas](personas.md) — Behavioral profiles (orthogonal to styles)
- [Sessions](sessions.md) — Spawn lifecycle where styles are resolved
- [API Reference](api.md) — Style REST endpoints and spawn request schema
