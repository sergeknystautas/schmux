VERDICT: NEEDS_REVISION

## Summary Assessment

The design correctly identifies real problems (multiple diffs, stale workspace, ghost merge state, dismissed diffs reappearing) and the core idea of a unified per-repo merge with a server-side `PendingMerge` object is sound. However, there are critical gaps in the concurrency model, the transparent re-merge on push introduces a silent behavior change the user cannot control, and the design hardcodes `origin/main` without using the existing default-branch detection infrastructure.

## Critical Issues (must fix)

### 1. No concurrency guard on PendingMerge operations

The design stores `PendingMerge` as a JSON file at `~/.schmux/lore/{repo}/pending-merge.json`. There is no mutex or file-locking scheme described. The existing `ProposalStore` uses a `sync.Mutex` (at `proposals.go:175`) for its read-modify-write operations. The `PendingMerge` needs the same protection because:

- The background merge goroutine writes the file when the LLM finishes.
- The push endpoint reads and potentially re-writes the file (re-merge on stale SHA).
- The user can dismiss the merge while the background goroutine is still writing.
- Two browser tabs could trigger merge and push simultaneously.

Without a mutex, concurrent reads and writes to the same JSON file will corrupt it. The design should specify whether `PendingMerge` lives inside the existing `ProposalStore` (inheriting its mutex) or gets its own dedicated store with its own lock.

### 2. Transparent re-merge on push is a silent behavior change the user never consented to

The design says: when `BaseSHA` differs from current `origin/main` at push time, "re-read CLAUDE.md from new tip, re-run the LLM merge with the same rules, update PendingMerge. This happens transparently behind a spinner."

This means the user reviews Diff A, clicks "Commit & Push", and what actually gets pushed is Diff B (produced by a second LLM call against a different base). The user never sees or approves Diff B. This violates the design's own principle of giving users "full control" over what gets pushed -- the exact principle cited in `docs/lore.md` as the reason the previous auto-push flow was removed.

The safer approach: detect staleness, show the user "CLAUDE.md has changed since this merge was computed", and let them re-merge explicitly. The re-merge button can pre-fill the same rules. This keeps the user in the loop without adding much friction -- they just click one extra button and see the new diff.

### 3. Hardcoded `origin/main` ignores existing default-branch detection

The design says: "Create a temporary git worktree from `origin/main`" and the current `handleLoreApplyMerge` pushes to `HEAD:main`. But the codebase already has default-branch detection infrastructure (`cb.DetectDefaultBranch()` at `handlers_vcs.go:124`, `DefaultBranchRef()` at `vcs.go`). Repos can use `master`, `develop`, or any other default branch.

The `ReadFileFromRepo` function at `curator.go:183` already uses `HEAD:` (not `origin/main:`), which is correct for bare repos where HEAD points to the default branch. But the push target and worktree creation in the design assume `main`. This needs to either use the detected default branch or document why `main` is acceptable.

### 4. `ReadFileFromRepo` uses `HEAD:` but design says `origin/main:` -- mismatch to resolve

The design states step 2 uses `git show origin/main:CLAUDE.md`. The existing `ReadFileFromRepo` function at `curator.go:183` uses `git show HEAD:path` with `cmd.Dir` set to the bare repo directory. In a bare repo, `HEAD` typically points to `refs/heads/main`, and `origin/main` does not exist (bare repos don't have remotes -- they _are_ the local mirror). After `git fetch`, the bare repo's `refs/heads/main` is updated, so `HEAD:` works correctly.

The design needs to clarify which ref it actually intends to use. If the implementation uses `ReadFileFromRepo` (which uses `HEAD:`), then the design should say so. If it needs `origin/main:`, a new function is needed. This matters because a bare repo that hasn't been fetched recently will show stale content under `HEAD:`.

### 5. Design does not address how `PendingMerge.ProposalIDs` interact with proposal lifecycle

The `PendingMerge` stores `ProposalIDs` and `RuleIDs`. But proposals can be independently dismissed (`handleLoreDismiss`), and individual rules within proposals can be dismissed via `handleLoreRuleUpdate`. The design says the pending merge is invalidated when "a rule is unapproved, edited, or dismissed after the merge was computed" -- but this invalidation is described as a frontend check ("the frontend checks `rule_ids` against current approved rules").

This is fragile. If the frontend fails to detect the change (e.g., another browser tab dismisses a rule), the push endpoint could push merged content that includes a rule the user already dismissed. The invalidation check must be server-side, in the push endpoint, before committing. The push endpoint should verify that every rule in `PendingMerge.RuleIDs` is still approved in its respective proposal.

## Suggestions (nice to have)

### 1. Consider an expiration TTL on PendingMerge

The design has no TTL or staleness timeout. A `PendingMerge` created days ago could still be pushed if the user returns to the page. The LLM-generated content may no longer make sense relative to the current state of the codebase. Consider adding a `CreatedAt`-based TTL (the field already exists in the struct) and requiring re-merge if the pending merge is older than some threshold (e.g., 24 hours).

### 2. The API surface is underspecified -- "consider a new POST" is not a decision

The design says: "Consider a new `POST /api/lore/{repo}/merge` that replaces the per-proposal merge endpoint entirely." This should be a firm decision, not a "consider." The existing `POST /proposals/{proposalID}/merge` is fundamentally per-proposal, and the new design is cross-proposal. Using the old endpoint shape for cross-proposal semantics will create confusion. Decide now: either introduce the new repo-level endpoint or specify how the old endpoint's semantics change.

### 3. Cleanup of temporary worktree on crash or kill

The design says the temporary worktree is "deleted immediately after (success or failure)." But if the schmux daemon is killed (SIGKILL, power loss, OOM) during the commit+push, the worktree will be orphaned. The existing `PruneStale` function (`vcs_git.go:137-146`) handles this for registered worktrees, but temporary worktrees that are "never registered in state" will not be cleaned up by it. Consider either: (a) registering them transiently in state so prune catches them, or (b) adding a startup sweep that removes any worktrees matching a naming convention (e.g., `/tmp/schmux-lore-*`).

### 4. Missing `docs/api.md` update requirement

Per CLAUDE.md: "Changes to API-related packages must include a corresponding update to `docs/api.md`." The design adds `GET /api/lore/{repo}/pending-merge` and `POST /api/lore/{repo}/push`, and modifies the existing merge/apply endpoints. The design should explicitly note that `docs/api.md` must be updated as part of implementation.

### 5. The `merge_complete` WebSocket event type is new but not defined

The design mentions a `merge_complete` WebSocket event for live transition, but the codebase currently only has `curator_done` / `curator_error` events for lore (via `BroadcastCuratorEvent`). The new event type needs to be defined in the WebSocket message schema. This is a minor gap but worth noting in the design to avoid it being forgotten during implementation.

### 6. Multi-repo edge case is unaddressed

The LorePage currently handles rules across multiple repos (the `repos.map(async (repo) => ...)` loop at `LorePage.tsx:81`). The design's unified merge is "per repo" but does not describe what happens when the user has approved rules from multiple repos simultaneously. Presumably each repo gets its own `PendingMerge` and its own diff review card. The design should confirm this explicitly, especially since the current frontend shows a single summary phase for all repos combined.

## Verified Claims (things you confirmed are correct)

1. **Multiple diffs for the same repo -- confirmed.** The current `handleLoreMerge` at `handlers_lore.go:294` runs per-proposal, and the frontend loops over proposal groups calling `startLoreMerge` for each (`LorePage.tsx:361`). Multiple proposals with `repo_public` rules produce separate merge previews.

2. **Push fails because main moved -- confirmed.** The current `handleLoreApplyMerge` at `handlers_lore.go:571` does `git push origin HEAD:main` from a workspace that may have been created much earlier. No fetch-before-push occurs.

3. **Dismissed diffs reappear on reload -- confirmed.** Merge previews are stored on the `Proposal` object (`proposals.go:127-128` -- `MergePreviews` and `MergeError` fields). The frontend filters what to show, but reloading the page re-reads proposals from disk, restoring dismissed previews.

4. **Ghost merge state -- confirmed.** If the merge goroutine fails after setting `proposal.Status = ProposalMerging` at `handlers_lore.go:332-333`, `finishMerge` at `handlers_lore.go:411` resets to `ProposalPending`, but only if the goroutine reaches `finishMerge`. A panic or daemon crash leaves the proposal permanently stuck in `merging`.

5. **`schmux/lore` workspace is heavyweight -- confirmed.** The `GetOrCreate` call at `handlers_lore.go:489` creates a full workspace tracked in state, and a shell session is spawned at `handlers_lore.go:589-599`. The workspace appears in the sidebar and persists until manually disposed.

6. **`ReadFileFromRepo` reads from bare repo HEAD -- confirmed.** At `curator.go:183`, the function uses `git show HEAD:path` with `Dir` set to the bare repo, which gives the latest fetched content of the default branch.

7. **Private layer apply is immediate and unaffected -- confirmed.** `ApplyToLayer` at `apply.go:7-10` writes directly via `InstructionStore.Write` for non-public layers. The design correctly leaves this path unchanged.

8. **The merge curator prompt and parsing are XML-based -- confirmed.** `BuildMergePrompt` at `merge_curator.go:21` and `ParseMergeResponse` at `merge_curator.go:56` use `<MERGED>` / `<SUMMARY>` XML tags. The design correctly notes these are preserved unchanged.
