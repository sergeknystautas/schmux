# Lore

## What it does

Lore is the learning system. Agent sessions emit friction signals (failures, reflections, friction events), which are periodically curated by an LLM into discrete rules, reviewed by the user, merged into instruction files, and applied back into the codebase — creating a feedback loop where agents improve their own instructions.

## Key files

| File                                           | Purpose                                                      |
| ---------------------------------------------- | ------------------------------------------------------------ |
| `internal/lore/scratchpad.go`                  | Append-only JSONL storage for raw friction signals           |
| `internal/lore/curator.go`                     | LLM prompt for extracting rules from raw signals             |
| `internal/lore/merge_curator.go`               | LLM prompt for merging approved rules into instruction files |
| `internal/lore/proposals.go`                   | Proposal data model and JSON-file-backed store               |
| `internal/lore/instructions.go`                | Private instruction file storage (`~/.schmux/instructions/`) |
| `internal/lore/apply.go`                       | Apply logic for private layers                               |
| `internal/dashboard/handlers_lore.go`          | All HTTP API handlers for the lore pipeline                  |
| `internal/lore/pending_merge.go`               | PendingMergeStore: per-repo mutex, JSON persistence          |
| `assets/dashboard/src/components/LoreCard.tsx` | Individual rule card (card wall UI)                          |

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
  Merge Curator (LLM) — unified per-repo, sees current instruction file + all approved rules
        ↓
  PendingMerge (server-persisted, repo-level)
        ↓  User reviews diff, optionally edits
  Private layers → InstructionStore (direct write)
  Public layer  → temporary git worktree → commit & push (or create PR)
```

## Three instruction layers

| Layer                | Storage                                                     | Apply method                               |
| -------------------- | ----------------------------------------------------------- | ------------------------------------------ |
| `repo_public`        | Git-tracked file (e.g., `CLAUDE.md`)                        | Unstaged change in `schmux/lore` workspace |
| `repo_private`       | `~/.schmux/instructions/repo_private/{repo}.md`             | Direct file write                          |
| `cross_repo_private` | `~/.schmux/instructions/cross_repo_private/instructions.md` | Direct file write                          |

The final instruction document is assembled by `InstructionStore.Assemble()`: cross_repo_private + repo_private + public content, concatenated with blank-line separators.

## Architecture decisions

- **Card wall hides proposals**: The frontend presents a flat list of rules as cards, but internally groups by `(repoName, proposalId)` when calling merge/apply endpoints. Proposals are an invisible implementation detail to the user.

- **Unified merge (one LLM call per repo, not per proposal)**: All approved `repo_public` rules across all proposals are batched into a single merge call. This eliminates duplicate phrasings and multiple diffs for the same file.

- **PendingMerge is repo-level, not per-proposal**: Stored at `~/.schmux/lore/{repo}/pending-merge.json`, with its own `PendingMergeStore` and per-repo mutex (independent lifecycle from `ProposalStore`).

- **Temporary worktree replaces persistent lore workspace**: Public rule commits use a transient git worktree from the default branch HEAD, never registered in state or shown in the sidebar. Eliminates stale workspace state, 409 conflicts, and checkout conflicts.

- **Server-driven page phase**: The lore page phase (triage / merging / ready / error) is derived from `GET /api/lore/{repo}/pending-merge` server state, not frontend state. Survives tab close, browser refresh, and device switching.

- **Three-tier staleness at push time**: (1) same SHA = proceed, (2) different SHA but instruction file unchanged = proceed with updated BaseSHA, (3) instruction file changed = 409 Conflict requiring re-merge.

- **Re-merge discards user edits**: When CLAUDE.md has changed on main and re-merge is needed, previous `EditedContent` is discarded because old edits may not apply cleanly against the new base.

- **Server-side rule validation before push**: The push endpoint independently verifies every rule in `PendingMerge.RuleIDs` is still `approved`, never trusting frontend state alone.

- **Two-phase curation (extract then merge)**: Extraction is "blind" — the LLM never sees existing instructions, only raw signals. This prevents it from being biased by what's already written. The merge phase then integrates extracted rules into the existing file, preserving voice and deduplicating.

- **Public layer uses workspace, not auto-push**: The public layer writes merged content as an unstaged change in a `schmux/lore` workspace instead of auto-committing and pushing. This gives the user full control — they review the diff, edit if needed, and commit when satisfied. The previous auto-push flow (`ApplyPublicMerge` → `PushBranch` → `CreatePR`) was removed because it gave users no opportunity to review changes before they left their machine.

- **XML delimiters for merge response**: The merge curator uses `<MERGED>` and `<SUMMARY>` XML tags instead of JSON because the merged content is markdown that may contain backticks, code blocks, and other characters that make JSON escaping fragile.

- **Two-pronged proposal deduplication**: Different raw entries about the same friction pattern can produce duplicate rules across proposals. To prevent this: (1) the extraction prompt includes pending rule texts so the LLM avoids re-extracting them, and (2) a post-extraction `DeduplicateRules` pass removes any rules whose normalized text matches existing pending or dismissed proposals. The LLM hint is best-effort; the text comparison is the safety net. Dismissed rules (individually dismissed within any proposal, or all rules from fully dismissed proposals) are included in both the prompt and the dedup pass, preventing rejected rules from resurfacing.

- **State-change records instead of mutating entries**: Raw friction entries are never modified. Instead, state changes (proposed/applied/dismissed) are written as separate JSONL records. This preserves the original signal and allows the state to be reconstructed.

## Gotchas

- **24-hour expiry on PendingMerge**: A pending merge older than 24 hours shows "Merge is stale, re-merge needed." Prevents pushing LLM-generated content that no longer makes sense.
- **PendingMerge is NOT eagerly invalidated when default branch moves**: Staleness is only checked at push time, not on every page load.
- **PendingMerge IS invalidated when included rules change**: If a rule is unapproved, edited, or dismissed after the merge was computed, the PendingMerge becomes invalid. Both frontend and push endpoint verify this.
- **Push fails with 409 if instruction file diverged**: Requires explicit re-merge; there is no auto-rebase.
- **Private layer applies are not rolled back if a public merge fails**: Already-persisted private rules are kept (they are idempotent writes).
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
