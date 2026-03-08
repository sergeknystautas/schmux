# Emergence System

## What it does

Observes user prompts across sessions, distills recurring patterns into reusable skills, and injects them into agents' native skill systems (`.claude/skills/`, `.opencode/commands/`). Skills emerge from real usage rather than manual configuration -- schmux is the emergence engine, agents are the runtime.

## Key files

| File                                                     | Purpose                                                                                    |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `internal/emergence/store.go`                            | Per-repo spawn entry store (`spawn-entries.json`): CRUD, Pin/Dismiss, RecordUse, Import    |
| `internal/emergence/metadata.go`                         | Per-repo skill metadata store (`metadata.json`): confidence, evidence, skill content       |
| `internal/emergence/builtins.go`                         | Embeds `builtins/*.md` via `//go:embed`, exposes `ListBuiltins()`                          |
| `internal/emergence/builtins/commit.md`                  | Built-in `/commit` skill (the only shipped built-in)                                       |
| `internal/emergence/curator.go`                          | LLM prompt building and response parsing for skill distillation                            |
| `internal/emergence/skillfile.go`                        | Renders a `SkillProposal` into a markdown file with YAML frontmatter                       |
| `internal/emergence/history.go`                          | `CollectPromptHistory()`: scans workspace event JSONL for prompt autocomplete data         |
| `internal/emergence/migrate.go`                          | One-time migration from old `~/.schmux/actions/` registry                                  |
| `internal/api/contracts/emergence.go`                    | Contract types: `SpawnEntry`, `EmergenceMetadata`, `SkillProposal`, request/response types |
| `internal/dashboard/handlers_emergence.go`               | HTTP API handlers for spawn entries, prompt history, and curation trigger                  |
| `internal/detect/adapter.go`                             | `InjectSkill`/`RemoveSkill` on the `ToolAdapter` interface                                 |
| `internal/detect/adapter_claude.go`                      | Claude Code skill injection: `.claude/skills/schmux-<name>/SKILL.md`                       |
| `internal/detect/adapter_opencode.go`                    | OpenCode skill injection: `.opencode/commands/<name>.md`                                   |
| `internal/workspace/ensure/manager.go`                   | Injects built-in and pinned skills at workspace setup                                      |
| `assets/dashboard/src/lib/emergence-api.ts`              | Frontend API client for all emergence endpoints                                            |
| `assets/dashboard/src/components/ActionDropdown.tsx`     | Spawn dropdown reading from emergence entries                                              |
| `assets/dashboard/src/components/CreateActionForm.tsx`   | Manual spawn entry creation form                                                           |
| `assets/dashboard/src/components/ProposedActionCard.tsx` | Skill proposal review card (pin/dismiss)                                                   |

## Architecture decisions

- **Schmux proposes, agents execute.** Emerged skills are written into agents' native skill paths (`.claude/skills/schmux-<name>/SKILL.md`, `.opencode/commands/<name>.md`). The agent discovers and invokes them natively -- schmux has no invocation mechanism.
- **Three skill sources coexist.** Built-in (shipped with binary), emerged (distilled from prompts), and manual (user-created). All surface in the spawn dropdown with no functional hierarchy.
- **Invisible to git.** Injected skill files are hidden via `.git/info/exclude` entries managed by the ensure package. The `schmux-` prefix on directory names prevents collisions with user-created skills.
- **Two injection paths.** (1) At workspace setup via `ensure/manager.go` -- all pinned skills are written when a workspace is first prepared. (2) On pin via `handlers_emergence.go` -- when a user pins a proposal, the skill is immediately injected into all existing workspaces for that repo.
- **No standalone curation timer.** Emergence curation piggybacks on lore curation -- it fires as a side-effect at the end of every lore run (session dispose). Manual trigger is available via `POST /api/emergence/{repo}/curate`.
- **Spawn entries are the user-facing registry.** `spawn-entries.json` tracks what appears in the spawn dropdown. Emergence metadata (evidence, confidence) is internal tracking in a separate `metadata.json`.
- **Migration is one-time and automatic.** On first daemon startup, old `~/.schmux/actions/<repo>/registry.json` entries are migrated to spawn entries. The old directory is renamed to `actions.migrated`.
- **`BuiltInSkills` config exists but is not yet wired.** The config struct has a `built_in_skills` map for per-skill enable/disable, but `ListBuiltins()` currently always returns all embedded skills.

## Gotchas

- **`CollectIntentSignals` is not yet implemented.** The curation handler calls it, but the function does not exist in `internal/emergence/`. The manual curation endpoint (`POST /curate`) will fail at runtime until this is added. `CollectPromptHistory` in `history.go` is similar but returns a different type.
- **`FindRepoByURL` vs `FindRepo`.** The spawn handler and ensure package resolve repo URLs to names via `config.FindRepoByURL()`. The emergence API handlers receive repo names directly from the URL path. Don't mix these up.
- **The `schmux-` prefix is critical.** Adapter `InjectSkill` prepends `schmux-` to skill names when creating directories/files. `RemoveSkill` does the same. If you bypass the adapter, the prefix won't be applied and `.git/info/exclude` patterns won't match.
- **Pin side-effects iterate all workspaces.** When a user pins a proposal, the handler loops through all workspaces for that repo and calls `InjectSkill` on every adapter. This can be slow if many workspaces exist.
- **Spawn entry IDs are UUIDs, not sequential.** The store generates IDs via `generateID()` using `crypto/rand`. Don't assume ordering.
- **The store uses file-level locking.** All mutations in `store.go` acquire a mutex, read the full file, modify in memory, and write atomically via temp file + rename. There is no concurrent-write support beyond this mutex.

## Common modification patterns

- **To add a new built-in skill:** Create a `.md` file in `internal/emergence/builtins/`. The `//go:embed` directive picks it up automatically. The filename (minus `.md`) becomes the skill name.
- **To add a new spawn entry type:** Add the constant to `SpawnEntryType` in `contracts/emergence.go`. Update `CreateSpawnEntryRequest` validation in `handlers_emergence.go`. Update the frontend `CreateActionForm.tsx` to show the new type option.
- **To change how skills are injected for a specific agent:** Edit the agent's `InjectSkill`/`RemoveSkill` methods in `adapter_<tool>.go`. The interface contract is in `adapter.go`.
- **To wire `BuiltInSkills` config:** Read `config.BuiltInSkills` in `ensure/manager.go` before calling `ListBuiltins()`, and filter out disabled skills.
- **To implement `CollectIntentSignals`:** Model it after `CollectPromptHistory` in `history.go` but return `[]IntentSignal` (defined in `curator.go`). It should read from the same `.schmux/events/*.jsonl` files.
- **To change curation trigger conditions:** The trigger is in `daemon.go` inside the lore callback closure. The spec calls for minimum thresholds (3 similar signals, 2 sessions, 2 days) but these are not yet enforced.
