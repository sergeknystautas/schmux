# Lore Unified Merge & Push

## Problem

The last step of the lore pipeline — committing approved public rules to CLAUDE.md — is broken in several ways:

1. **Multiple diffs for the same learning.** Each proposal gets its own LLM merge call, producing separate diffs that insert slightly different phrasings of the same rule at the same location. The user sees N "Review Changes" cards with N "Commit & Push" buttons for what is conceptually one change.

2. **Push fails because main moved.** The merge runs against a `schmux/lore` workspace that was created at some earlier point. By the time the user clicks "Commit & Push", `origin/main` has moved forward and the push is rejected.

3. **Dismissed diffs reappear on reload.** Merge previews are stored on proposal objects. Dismissing them in the UI only clears frontend state — reloading the page resurfaces them.

4. **"Merging already in progress" ghost.** If the merge goroutine fails or the user navigates away during merge, the proposal stays in `merging` status permanently, blocking future merges with no indication of what's wrong.

5. **A full workspace for one file.** The system creates a tracked workspace with a sidebar entry and a shell session just to write a single file to `origin/main`. This is heavyweight and creates stale-state problems (uncommitted changes, checkout conflicts, 409 errors).

## Design

### Unified Merge

Instead of merging per-proposal, the system collects all approved `repo_public` rules across all proposals into a single batch and runs **one** LLM merge call.

The flow:

1. User clicks **Apply** on the summary screen.
2. Backend does `git fetch` on the bare repo, then reads the instruction file via `ReadFileFromRepo` (which uses `git show HEAD:<path>` on the bare repo — `HEAD` points to the default branch after fetch).
3. All approved public rules from all proposals are gathered into one list.
4. One call to the merge curator: "Here is the current CLAUDE.md, here are N rules to integrate."
5. Result stored as a **repo-level `PendingMerge`** — a single server-side object, not scattered across proposal objects.
6. Private layers still apply immediately per-proposal (unchanged).

### PendingMerge Data Model

```go
type PendingMerge struct {
    Repo           string    `json:"repo"`
    Status         string    `json:"status"`          // "merging", "ready", "error"
    BaseSHA        string    `json:"base_sha"`        // default branch HEAD SHA the merge was based on
    RuleIDs        []string  `json:"rule_ids"`        // rule IDs included (across all proposals)
    ProposalIDs    []string  `json:"proposal_ids"`    // source proposals
    MergedContent  string    `json:"merged_content"`  // full merged instruction file
    CurrentContent string    `json:"current_content"` // instruction file before merge (for diff)
    Summary        string    `json:"summary"`         // LLM-generated description of changes
    EditedContent  *string   `json:"edited_content,omitempty"` // user edits override MergedContent
    Error          string    `json:"error,omitempty"`
    CreatedAt      time.Time `json:"created_at"`
}
```

Stored as a JSON file at `~/.schmux/lore/{repo}/pending-merge.json`. One file per repo. Overwritten on each new merge, deleted on successful push or explicit dismissal.

### PendingMergeStore

`PendingMerge` operations are protected by a `sync.Mutex`, following the same pattern as `ProposalStore`. The store handles:

- Background merge goroutine writing results
- Push endpoint reading and potentially re-writing (on re-merge)
- Dismiss/invalidation from the frontend
- Concurrent access from multiple browser tabs

The store lives as its own type (`PendingMergeStore`) with a per-repo mutex, not inside `ProposalStore`, since the two have independent lifecycles.

### Invalidation

The `PendingMerge` is invalidated (deleted) when:

- The user successfully commits & pushes it.
- The user explicitly dismisses it (Back + change approvals, or a Dismiss button).
- The included rules change (a rule is unapproved, edited, or dismissed after the merge was computed). Both the frontend and the push endpoint check `rule_ids` against current approved rules — the push endpoint verifies server-side before committing, never trusting frontend state alone.

The `PendingMerge` is **not** invalidated when the default branch moves forward. The stored `base_sha` is used at push time to detect staleness (see Push Mechanics below).

A `PendingMerge` older than 24 hours is treated as expired — the page shows "Merge is stale, re-merge needed" instead of the diff. This prevents pushing LLM-generated content that no longer makes sense relative to the current codebase.

### Push Mechanics

When the user clicks "Commit & Push":

1. **Server-side rule validation**: The push endpoint verifies every rule in `PendingMerge.RuleIDs` is still `approved` in its respective proposal. If any rule has been dismissed, edited, or unapproved since the merge was computed, the push is rejected with a clear error ("Rules changed since merge — re-merge needed"). This is the authoritative check — the frontend also checks, but the server is the source of truth.

2. **Freshness check**: `git fetch` the bare repo, compare `PendingMerge.BaseSHA` against current default branch HEAD.
   - **Same SHA**: Proceed directly.
   - **Different SHA, instruction file unchanged**: The default branch moved but the instruction file content is identical. Proceed — update `BaseSHA` to the new HEAD.
   - **Different SHA, instruction file changed**: Return HTTP **409 Conflict** with `{ "reason": "stale", "message": "CLAUDE.md has changed since this merge was computed" }`. The frontend shows a banner: "CLAUDE.md was updated on main since this merge. Re-merge to incorporate the latest version." with a **Re-merge** button. This keeps the user in control of what gets pushed.

   **Re-merge UX**: Clicking Re-merge transitions the card back to the spinner state ("Re-merging N rules..."). Any previous user edits (`EditedContent`) are **discarded** — the re-merge produces fresh LLM output against the new base, and old edits may not apply cleanly. The banner text makes this explicit: "Re-merging will produce a fresh merge against the latest CLAUDE.md. Previous edits will need to be re-applied." When the re-merge completes, the card transitions back to the Diff tab with the new diff.

3. **Commit**: Create a temporary git worktree from the default branch HEAD (using `DetectDefaultBranch()`), write the merged content, commit with message `lore: add N rules from agent learnings`, push to the default branch (or create a branch for PR mode).

4. **Cleanup**: Remove the temporary worktree immediately. It never appears in the workspace list or sidebar. On daemon startup, sweep and remove any orphaned temporary worktrees matching the naming convention (e.g., `lore-push-*`) to handle crash/kill scenarios.

5. **Error handling**: If the push fails (auth, network, branch protection), show the error inline with a Retry button. The `PendingMerge` is preserved — no work is lost.

6. **After success**: Delete `PendingMerge`, mark all included rules as applied across their respective proposals, transition to "Done" confirmation.

### No More `schmux/lore` Workspace

The `schmux/lore` workspace concept is removed entirely for public rule persistence. The temporary worktree used at push time is:

- Created from the default branch HEAD (detected via `DetectDefaultBranch()`)
- Used only for the commit + push operation
- Deleted immediately after (success or failure)
- Never registered in state, never shown in the sidebar
- Orphans cleaned up on daemon startup

This eliminates: stale workspace state, 409 "pending changes" conflicts, checkout conflicts with other worktrees, orphaned shell sessions.

### Server-Driven Page Phase

The lore page's phase is **derived from server state**, not managed in frontend state. On load, the page calls `GET /api/lore/{repo}/pending-merge`:

| Server returns           | Page shows                                   |
| ------------------------ | -------------------------------------------- |
| `404` (no pending merge) | Triage card wall                             |
| `{ status: "merging" }`  | Spinner: "Merging N rules into CLAUDE.md..." |
| `{ status: "ready" }`    | Diff review with "Commit & Push"             |
| `{ status: "error" }`    | Error message with Retry                     |

The user can close the tab, walk away, come back — the page reconstructs exactly where they left off. The merge is a background job. A WebSocket event (`lore_merge_complete`) enables live transition if the user is watching, but the page doesn't depend on it.

The `lore_merge_complete` event is broadcast via the existing `BroadcastCuratorEvent` infrastructure, using a new `EventType` value. The frontend `CurationContext` (or a new dedicated hook) listens for it and triggers a re-fetch of the pending merge endpoint.

### Frontend Merge Review

The merge review phase renders a single card per repo that has a pending merge:

- **Header**: "Review Changes"
- **Summary**: LLM-generated description (e.g., "Added zsh glob quoting and test root conventions to Code Conventions")
- **View toggle**: "Diff" (default) and "Edit" tabs
  - **Diff tab**: Read-only unified diff (existing `react-diff-viewer-continued` component). Shows the merged content (or user-edited content, if edited) against the current file. "Commit & Push" button is available here.
  - **Edit tab**: Full-file textarea pre-filled with the merged content. The user can modify wording, reorder sections, fix formatting, or remove parts they don't like. Instead of "Commit & Push", this tab shows a "Review Diff" button that switches to the Diff tab — ensuring the user always sees the final diff before pushing.
- **Actions**: "Back" (returns to summary, preserves the PendingMerge — see below) and "Commit & Push" (Diff tab only) or "Review Diff" (Edit tab only)

No per-diff dismiss buttons — there's only one diff per repo. If the user doesn't want it, they go Back and change which rules are approved.

The edit capability is important because the LLM merge sometimes produces awkward phrasing, inserts rules in suboptimal locations, or duplicates existing guidance in different words. Letting the user polish the result before pushing avoids a cycle of "push, notice problem, manually fix CLAUDE.md."

#### Edit Persistence

User edits are saved to the server via `PATCH /api/lore/{repo}/pending-merge` (debounced, ~1 second after last keystroke). The `PendingMerge` gains an `EditedContent *string` field — when non-nil, the diff and push use this content instead of `MergedContent`. This means edits survive page navigation, browser refresh, and even switching devices. The Edit tab loads from `EditedContent` if present, otherwise falls back to `MergedContent`.

#### "Back" Behavior

Clicking "Back" returns to the summary phase but **preserves the PendingMerge** on the server. If the user returns to the merge review without changing any approvals, the existing merge (and any edits) are still there — no re-merge needed. The PendingMerge is only invalidated when the user actually changes an approval, edits a rule, or dismisses a rule — i.e., when the inputs to the merge have changed.

#### Multi-Repo Behavior

When the user has approved public rules from multiple repos, each repo gets its own independent merge review card. Each card has its own Back and Commit & Push. They are fully independent:

- Pushing repo A does not affect repo B's pending merge.
- If repo A's push fails, repo B's card remains unaffected — the user can push B and retry A later.
- "Back" on repo A returns only repo A to the summary phase. Repo B's merge review stays.
- The "Done" transition only fires when all repos with pending merges have been resolved (pushed, dismissed, or returned to triage).

### Multi-Repo Behavior

The card wall aggregates rules across all repos. Each repo gets its own independent `PendingMerge`. If the user approves public rules from two repos (e.g., `schmux` and `other-project`), the merge review phase shows one diff card per repo, each with its own "Commit & Push" button. This is distinct from the old problem (multiple diffs for the same repo from different proposals) — here the diffs are genuinely independent files in independent repos.

### API Changes

**New endpoints:**

- `GET /api/lore/{repo}/pending-merge` — Returns the current `PendingMerge` for a repo, or 404 if none exists.
- `POST /api/lore/{repo}/merge` — Accepts a list of `{ proposal_id, rule_ids }` pairs, creates a `PendingMerge`, starts the background merge job, returns 202. Replaces the per-proposal `POST /api/lore/{repo}/proposals/{id}/merge` for public layer merges.
- `POST /api/lore/{repo}/push` — Takes the current `PendingMerge`, handles freshness check + re-merge + commit + push. Returns the commit SHA on success. Returns 409 with `{ "reason": "stale" }` if the instruction file changed on the default branch since the merge was computed.
- `PATCH /api/lore/{repo}/pending-merge` — Saves user edits. Accepts `{ "edited_content": "..." }`. Debounced by the frontend (~1s after last keystroke). Sets `EditedContent` on the `PendingMerge`.
- `DELETE /api/lore/{repo}/pending-merge` — Dismisses/clears the current pending merge.

**Removed behavior:**

- `schmux/lore` workspace creation/reuse in `handleLoreApplyMerge`
- Shell session spawning for lore workspaces
- Per-proposal `merge_previews` and `merge_error` fields on `Proposal`
- Per-proposal `POST /api/lore/{repo}/proposals/{id}/merge` endpoint (replaced by repo-level merge)

**Documentation:** `docs/api.md` must be updated with the new endpoints as part of implementation.

### What This Removes

- Per-proposal merge previews stored on proposal objects
- The `schmux/lore` workspace concept
- Shell session spawning for manual lore commits
- Multiple diff cards in the merge review phase
- Frontend-managed phase state that doesn't survive navigation
- The "merging already in progress" ghost from stuck proposal status

### What This Preserves

- The triage card wall (unchanged)
- The summary phase showing rule counts by layer (unchanged)
- Private layer persistence (unchanged — still per-proposal, still immediate)
- The merge curator LLM prompt and response format (unchanged)
- The diff viewer component (unchanged, just rendered once instead of N times)
- The `direct_push` / `create_pr` config mode (unchanged)

## Future Considerations

- **Multi-file support**: If lore expands to write to multiple instruction files (not just CLAUDE.md), the `PendingMerge` would need to hold multiple file diffs. Out of scope for now.
