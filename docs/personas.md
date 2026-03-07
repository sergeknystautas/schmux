# Personas

## What it does

A persona is a named, reusable behavioral profile that shapes how an agent operates. It carries a system prompt, output expectations, and a visual identity (icon + color). Personas are global (stored in `~/.schmux/personas/`), optional at spawn time, and orthogonal to model selection.

## Key files

| File                                      | Purpose                                                                          |
| ----------------------------------------- | -------------------------------------------------------------------------------- |
| `internal/persona/manager.go`             | `Manager` struct: CRUD operations, `EnsureBuiltins()`, `ResetBuiltIn()`          |
| `internal/persona/parse.go`               | `Persona` struct, `ParsePersona()` (YAML frontmatter + body), `MarshalPersona()` |
| `internal/persona/builtins.go`            | `embed.FS` directive embedding `builtins/*.yaml`                                 |
| `internal/persona/builtins/*.yaml`        | 11 built-in persona definitions (api-designer, docs-writer, mentor, etc.)        |
| `internal/dashboard/handlers_personas.go` | REST API: `GET/POST /api/personas`, `GET/PUT/DELETE /api/personas/{id}`          |

## Architecture decisions

- **YAML frontmatter + body format.** The frontmatter carries metadata (id, name, icon, color, expectations, built_in). The body after the closing `---` is the system prompt, written as natural prose. This makes personas easy to author in both a text editor and the dashboard UI.
- **Prompt delivered via agent-native mechanisms, separate from the task prompt.** Claude gets `--append-system-prompt-file`, Codex gets `.codex/AGENTS.md`, Gemini gets `.gemini/GEMINI.md`. The persona prompt shapes _how_ the agent works; the user's task prompt says _what_ to do. The two are never concatenated.
- **Built-ins are embedded in the binary.** `EnsureBuiltins()` copies them to `~/.schmux/personas/` on first run only -- it never overwrites existing files, so user edits persist. `ResetBuiltIn()` re-copies the original from the embedded filesystem.
- **Global, not per-project.** Personas are stored in `~/.schmux/personas/` and available across all repositories. Per-project scoping is explicitly out of scope.
- **Delete vs. reset distinction.** Deleting a built-in persona calls `ResetBuiltIn()` (restores the default). Deleting a user-created persona removes the file.

## Gotchas

- The `Prompt` field uses `yaml:"-"` so it is excluded from frontmatter marshaling. It is populated from the body text after the closing `---` delimiter during `ParsePersona()`.
- `ParsePersona()` requires both opening and closing `---\n` delimiters. Files without frontmatter fail to parse.
- `Manager.List()` silently skips files that fail to parse or read. A malformed YAML file does not break the listing.
- `Manager.Create()` checks for existence before writing and fails if the ID already exists. `Manager.Update()` fails if the ID does not exist.
- The personas directory is created with `0700` permissions (user-only), not `0755`.
- Built-in persona files on disk can be freely modified by the user. The `built_in: true` field in the YAML is informational -- the canonical check for "is this a built-in?" is whether the file exists in the embedded filesystem (`builtinsFS.ReadFile()`).

## Built-in personas

| ID                       | Name                   | Icon      | Color                                                    | Focus |
| ------------------------ | ---------------------- | --------- | -------------------------------------------------------- | ----- |
| `api-designer`           | API Designer           | `#1abc9c` | API design, RESTful patterns, error handling             |
| `docs-writer`            | Docs Writer            | `#3498db` | Documentation drift, stale docs, clarity                 |
| `mentor`                 | Mentor                 | `#e67e22` | Teaching, explaining, educational code review            |
| `performance-engineer`   | Performance Engineer   | `#f1c40f` | Bottlenecks, optimization, profiling                     |
| `qa-engineer`            | QA Engineer            | `#2ecc71` | Edge cases, test coverage gaps, boundary conditions      |
| `refactoring-specialist` | Refactoring Specialist | `#95a5a6` | Code structure, design patterns, reducing complexity     |
| `security-auditor`       | Security Auditor       | `#e74c3c` | Vulnerabilities, OWASP top 10, attack surfaces           |
| `software-architect`     | Software Architect     | `#2c3e50` | System design, scalability, separation of concerns       |
| `spec-implementer`       | Spec Implementer       | `#8e44ad` | Implementing from specifications, task-by-task execution |
| `technical-pm`           | Technical PM           | `#f39c12` | Commit analysis, trend identification, progress reports  |
| `ux-designer`            | UX Designer            | `#9b59b6` | Usability, UI consistency, accessibility                 |

## Common modification patterns

- **To add a new built-in persona:** Create a YAML file in `internal/persona/builtins/` with frontmatter (id, name, icon, color, expectations, `built_in: true`) and a body containing the system prompt. The `go:embed` directive picks it up automatically.
- **To change the persona file format:** Edit `ParsePersona()` and `MarshalPersona()` in `internal/persona/parse.go`. The `Persona` struct fields must stay in sync.
- **To change how personas are delivered to agents:** Edit the spawn command construction in `internal/session/manager.go` where persona prompt files are written and agent flags are assembled.
- **To add persona management UI:** The API is already complete (`GET/POST/PUT/DELETE`). The dashboard page at `/personas` consumes `internal/dashboard/handlers_personas.go`.
- **To scope personas per-project:** This is explicitly out of scope in the current design. It would require changing the storage path from `~/.schmux/personas/` to a per-repo location and updating the manager to accept a repo context.
