# Autolearn

## What it does

Observes agent sessions for two types of signals — friction (failures, errors, reflections) and intent (user prompts, recurring patterns) — curates them into typed learnings (rules or skills) via LLM, and applies approved learnings to the appropriate targets. Rules become instruction file content; skills become agent skill files. The user triages all learnings through a unified card wall in the dashboard.

## Key files

| File                                            | Purpose                                                                |
| ----------------------------------------------- | ---------------------------------------------------------------------- |
| `internal/autolearn/learning.go`                | Core types: `Learning`, `Batch`, `LearningKind`, `Layer`               |
| `internal/autolearn/store.go`                   | `BatchStore`: JSON-file-backed persistence for batches                 |
| `internal/autolearn/signals.go`                 | Unified signal scanner: friction entries + intent signals              |
| `internal/autolearn/curator_friction.go`        | LLM prompt for extracting learnings from friction signals              |
| `internal/autolearn/curator_intent.go`          | LLM prompt for distilling learnings from intent signals                |
| `internal/autolearn/merge_curator.go`           | LLM prompt for merging approved public rules into CLAUDE.md            |
| `internal/autolearn/instructions.go`            | Private instruction file storage (`~/.schmux/autolearn/instructions/`) |
| `internal/autolearn/pending_merge.go`           | `PendingMerge` store for the async merge-then-push flow                |
| `internal/autolearn/apply.go`                   | Apply routing (kind × layer → target), deduplication                   |
| `internal/autolearn/skillfile.go`               | Renders a skill learning into a SKILL.md file                          |
| `internal/autolearn/history.go`                 | Learning history filter/query functions                                |
| `internal/autolearn/disabled.go`                | `noautolearn` build tag stubs                                          |
| `internal/spawn/store.go`                       | Spawn entry CRUD (always compiled, no build tag)                       |
| `internal/spawn/metadata.go`                    | Emergence metadata store for skill confidence/evidence                 |
| `internal/spawn/history.go`                     | Prompt history for spawn dropdown autocomplete                         |
| `internal/dashboard/handlers_autolearn.go`      | All HTTP API handlers for the autolearn pipeline                       |
| `internal/dashboard/handlers_spawn_entries.go`  | Spawn entry CRUD handlers (always compiled)                            |
| `internal/dashboard/curation_helpers.go`        | Shared curation utilities (logging, debug files)                       |
| `assets/dashboard/src/routes/AutolearnPage.tsx` | Card wall UI for triaging learnings                                    |
| `assets/dashboard/src/components/LoreCard.tsx`  | Individual learning card component                                     |

## Data flow

```
Agent sessions emit events (failure/reflection/friction/intent)
        ↓
  Signal files: per-session JSONL in workspace .schmux/events/
        ↓  POST /api/autolearn/{repo}/curate
  Two LLM calls (can run in parallel):
    Friction curator → []Learning (mostly rules, can produce skills)
    Intent curator   → []Learning (mostly skills, can produce rules)
        ↓
  Merge + dedup → single Batch (status: pending)
        ↓
  User triages via card wall (approve / dismiss / edit per learning)
        ↓
  Apply routes by kind × layer:
    rule  + private  → direct file write to instruction store
    rule  + public   → LLM merge into CLAUDE.md → user reviews diff → push
    skill + private  → inject into workspaces via adapters (.git/info/exclude)
    skill + public   → write skill file to worktree → push
```

## Three instruction layers

| Layer                | Storage                                                         | Visibility |
| -------------------- | --------------------------------------------------------------- | ---------- |
| `repo_public`        | Git-tracked file (e.g., CLAUDE.md) or `.claude/skills/`         | Shared     |
| `repo_private`       | `~/.schmux/autolearn/instructions/repo_private/{repo}.md`       | Local only |
| `cross_repo_private` | `~/.schmux/autolearn/instructions/cross_repo_private/global.md` | Local only |

Both rules and skills support all three layers. Public-layer items always require explicit push — the user sees the exact diff before anything leaves the machine.

## Architecture decisions

- **Two LLM calls, not one.** Each curator focuses on its signal type (friction or intent) but can produce either kind of learning. A combined prompt degrades both tasks. The prompts evolve independently.

- **Cross-kind learnings allowed.** The friction curator can propose skills (recurring patterns worth automating). The intent curator can propose rules (users working around footguns). Both signal types feed into both kinds.

- **Two-phase curation for rules (extract then merge).** Extraction is "blind" — the LLM never sees existing instructions, only raw signals. This prevents bias from existing content. The merge phase integrates extracted rules into the instruction file, preserving voice and deduplicating.

- **Skills bypass the merge curator.** Each skill is a standalone SKILL.md file — there is no existing document to merge into. The push handler writes skill files directly to the worktree.

- **Dismissals are durable and reversible.** All dismissed learnings are fed back into both curator prompts to prevent re-extraction. Users can un-dismiss from the history view, making the learning eligible again.

- **Spawn entries are a separate concern.** The spawn dropdown registry (`internal/spawn/`) is always compiled (no build tag). Skill application creates spawn entries as a side effect. The autolearn pipeline and spawn entries share no data model — they interact only at the apply step.

- **Config key alias.** The config loader accepts both `lore` and `autolearn` as the JSON key. Saves always write `autolearn`. This provides backward compatibility with existing config files.

- **PendingMerge handles mixed-content pushes.** The `SkillFiles` field on `PendingMerge` carries public skill file content alongside the merged instruction file, enabling a single atomic commit for all public-layer learnings.

- **XML delimiters for merge response.** The merge curator uses `<MERGED>` and `<SUMMARY>` XML tags instead of JSON because the merged content is markdown that may contain backticks and code blocks.

## Gotchas

- **Forget endpoint is stubbed.** `POST /api/autolearn/{repo}/forget/{lid}` returns 501. Implementing "forget" for public rules requires a new commit removing the text — it can't just delete a file.
- **The `schmux-` prefix on skill directories.** Managed by adapter `InjectSkill`/`RemoveSkill`. Public skills committed to the repo keep the prefix to prevent collisions with user-created skills.
- **24-hour expiry on PendingMerge.** A pending merge older than 24 hours shows "stale" — prevents pushing outdated LLM-generated content.
- **PendingMerge staleness is checked at push time, not on page load.** If the instruction file changes on the default branch between merge and push, the push returns 409 Conflict requiring re-merge.
- **`Category` feeds the merge prompt.** The merge curator formats rules as `[Category] Title`. Both curators must populate this field.
- **Curation runs both curators sequentially.** The friction curator runs first; if intent signals are also present, the intent curator runs second. Both results merge into one batch. Currently only friction curation is fully wired — intent curation has a TODO.
- **State-change records instead of mutating entries.** Raw friction entries are never modified. State changes (proposed/applied/dismissed) are written as separate JSONL records in a central state file.
- **After curation, ALL raw entries are marked as "proposed"** via `MarkEntriesDirect` using direct timestamp marking, not by matching LLM `source_entries` text (which is unreliable).

## Common modification patterns

- **Adding a new learning kind:** Add the constant to `LearningKind` in `learning.go`, update `SkillDetails`/`RuleDetails` if needed, update the apply routing in `apply.go`, and update the card rendering in `LoreCard.tsx`.
- **Changing the friction extraction prompt:** Edit `BuildFrictionPrompt()` in `curator_friction.go`. The output schema is registered in `init()` via `schema.Register`.
- **Changing the intent distillation prompt:** Edit `BuildIntentPrompt()` in `curator_intent.go`.
- **Changing the merge prompt:** Edit `BuildMergePrompt()` in `merge_curator.go`. Response parsing uses XML tags in `ParseMergeResponse()`.
- **Adding a new autolearn API endpoint:** Add the handler in `handlers_autolearn.go`, register the route in the `r.Route("/autolearn/{repo}", ...)` block in `server.go`, add the stub in `handlers_autolearn_disabled.go`, and update `docs/api.md`.
- **Adding a new spawn entry endpoint:** Add the handler in `handlers_spawn_entries.go` (no build tag needed), register in the `r.Route("/spawn/{repo}", ...)` block in `server.go`.
