# Lore

## What it does

Lore is the learning system. Agent sessions emit friction signals (failures, reflections, friction events), which are periodically curated by an LLM into discrete rules, reviewed by the user, merged into instruction files, and applied back into the codebase — creating a feedback loop where agents improve their own instructions.

## Key files

| File                                  | Purpose                                                      |
| ------------------------------------- | ------------------------------------------------------------ |
| `internal/lore/scratchpad.go`         | Append-only JSONL storage for raw friction signals           |
| `internal/lore/curator.go`            | LLM prompt for extracting rules from raw signals             |
| `internal/lore/merge_curator.go`      | LLM prompt for merging approved rules into instruction files |
| `internal/lore/proposals.go`          | Proposal data model and JSON-file-backed store               |
| `internal/lore/instructions.go`       | Private instruction file storage (`~/.schmux/instructions/`) |
| `internal/lore/apply.go`              | Apply logic for private layers                               |
| `internal/dashboard/handlers_lore.go` | All HTTP API handlers for the lore pipeline                  |

## Data flow

```
Agent sessions emit events (failure/reflection/friction)
        ↓
  Scratchpad JSONL files (per-session event files in workspace)
        ↓  POST /api/lore/{repo}/curate
  Extraction Curator (LLM) — blind to existing instructions
        ↓
  Proposal with Rules (pending)
        ↓  PATCH rules — user approves/dismisses/edits
  Rules reviewed by user
        ↓  POST /api/lore/{repo}/proposals/{id}/merge
  Merge Curator (LLM) — sees current instruction file + approved rules
        ↓
  MergePreview per layer (user reviews diff)
        ↓  POST /api/lore/{repo}/proposals/{id}/apply-merge
  Private layers → InstructionStore (direct write)
  Public layer  → schmux/lore workspace (unstaged changes)
```

## Three instruction layers

| Layer                | Storage                                                     | Apply method                               |
| -------------------- | ----------------------------------------------------------- | ------------------------------------------ |
| `repo_public`        | Git-tracked file (e.g., `CLAUDE.md`)                        | Unstaged change in `schmux/lore` workspace |
| `repo_private`       | `~/.schmux/instructions/repo_private/{repo}.md`             | Direct file write                          |
| `cross_repo_private` | `~/.schmux/instructions/cross_repo_private/instructions.md` | Direct file write                          |

The final instruction document is assembled by `InstructionStore.Assemble()`: cross_repo_private + repo_private + public content, concatenated with blank-line separators.

## Architecture decisions

- **Two-phase curation (extract then merge)**: Extraction is "blind" — the LLM never sees existing instructions, only raw signals. This prevents it from being biased by what's already written. The merge phase then integrates extracted rules into the existing file, preserving voice and deduplicating.

- **Public layer uses workspace, not auto-push**: The public layer writes merged content as an unstaged change in a `schmux/lore` workspace instead of auto-committing and pushing. This gives the user full control — they review the diff, edit if needed, and commit when satisfied. The previous auto-push flow (`ApplyPublicMerge` → `PushBranch` → `CreatePR`) was removed because it gave users no opportunity to review changes before they left their machine.

- **XML delimiters for merge response**: The merge curator uses `<MERGED>` and `<SUMMARY>` XML tags instead of JSON because the merged content is markdown that may contain backticks, code blocks, and other characters that make JSON escaping fragile.

- **Two-pronged proposal deduplication**: Different raw entries about the same friction pattern can produce duplicate rules across proposals. To prevent this: (1) the extraction prompt includes pending rule texts so the LLM avoids re-extracting them, and (2) a post-extraction `DeduplicateRules` pass removes any rules whose normalized text matches existing pending or dismissed proposals. The LLM hint is best-effort; the text comparison is the safety net. Dismissed rules (individually dismissed within any proposal, or all rules from fully dismissed proposals) are included in both the prompt and the dedup pass, preventing rejected rules from resurfacing.

- **State-change records instead of mutating entries**: Raw friction entries are never modified. Instead, state changes (proposed/applied/dismissed) are written as separate JSONL records. This preserves the original signal and allows the state to be reconstructed.

## Gotchas

- The public layer's `ApplyToLayer()` intentionally returns an error — public applies must go through the workspace-based flow in `handleLoreApplyMerge`, not the instruction store.
- When a `schmux/lore` workspace already exists with uncommitted changes or commits ahead of `origin/main`, the apply endpoint returns 409 Conflict. The user must review or discard existing changes first.
- Raw entries are aggregated from per-session event files across all workspaces AND a central `~/.schmux/lore/{repo}/state.jsonl`. The event files hold the raw signals; the state file holds state-change records.
- After curation, ALL raw entries sent to the LLM are marked as "proposed" via `MarkEntriesDirect` — not just the ones the LLM references in `source_entries`. The LLM's source_entries format is unreliable for text matching; direct timestamp marking ensures entries are never re-curated.
- Curation and merge both run asynchronously in background goroutines. The frontend polls for results via the proposal endpoint. Curation also supports SSE streaming for real-time progress.

## Common modification patterns

- **Adding a new instruction layer**: Add the constant to `Layer` in `proposals.go`, add storage path logic in `instructions.go`, update `Assemble()`, and update the `switch` in `handleLoreApplyMerge`.
- **Changing the extraction prompt**: Edit `BuildExtractionPrompt()` in `curator.go`. The schema is registered in `init()`.
- **Changing the merge prompt**: Edit `BuildMergePrompt()` in `merge_curator.go`. Response parsing is XML-based in `ParseMergeResponse()`.
- **Adding a new lore API endpoint**: Add the handler in `handlers_lore.go`, register the route in the `r.Route("/lore", ...)` block in `server.go`, and update `docs/api.md`.
