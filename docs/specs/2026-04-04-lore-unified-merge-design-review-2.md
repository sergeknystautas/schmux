VERDICT: NEEDS_REVISION

## Summary Assessment

The revised design successfully addressed all five critical issues from Review 1 (mutex on PendingMergeStore, explicit re-merge instead of transparent, DetectDefaultBranch usage, HEAD vs origin/main clarification, server-side rule validation on push). The core architecture is solid. This review focuses on UX gaps in the merge review phase, edge cases around the Edit tab, and interaction states the user can reach that the design does not address.

## Critical Issues (must fix)

### 1. Edit tab content is lost on page navigation or refresh

The design specifies the Edit tab holds user modifications in "component state." But the entire point of the server-driven phase model is that users can "close the tab, walk away, come back -- the page reconstructs exactly where they left off." This contract is broken for edits: the user spends time polishing the merged content in the Edit tab, navigates to a session to check something, comes back, and their edits are gone -- the textarea resets to the original LLM output from `PendingMerge.MergedContent`.

This is a particularly bad UX failure because the Edit tab is designed for exactly the case where the LLM output needs manual fixing. Users who care enough to edit will care a lot about losing those edits.

The fix: persist edits to the server. Either add an `EditedContent` field to `PendingMerge` and save on blur/debounce via a `PATCH /api/lore/{repo}/pending-merge` endpoint, or at minimum persist edits to `localStorage` keyed by `repo + base_sha`. The server-side approach is better because it also survives multiple-tab scenarios and browser restarts.

### 2. "Commit & Push" button is enabled while edit content differs from the displayed diff

When the user is on the Edit tab and modifies content, the design says switching back to the Diff tab "shows the diff against the user's edited version, not the original LLM output." But there is a timing problem: the user is on the Edit tab, makes changes, and clicks "Commit & Push" without switching back to the Diff tab. They are pushing content they can see (the textarea) but have not seen as a diff against the current file. The diff view is the safety net that lets the user verify "this is what will change" -- skipping it defeats the purpose of the review phase.

The design should clarify one of two behaviors: (a) "Commit & Push" from the Edit tab shows a confirmation dialog with the diff first ("You edited the merged content. Review the final diff before pushing."), or (b) "Commit & Push" is only available from the Diff tab (when the Edit tab is active, the button changes to "Review Diff" which switches to the Diff tab).

### 3. Re-merge flow has no loading feedback and an ambiguous intermediate state

The design describes the re-merge scenario: user clicks "Commit & Push," the server detects CLAUDE.md has changed, and returns an instruction to show "CLAUDE.md has changed since this merge was computed" with a Re-merge button. But the design does not specify:

- What happens to the "Commit & Push" button during the staleness check. The user clicked it expecting a push. If the push endpoint returns a non-error response saying "stale, re-merge needed," what HTTP status is this? A 409? A 200 with a status field? The frontend needs to distinguish "push failed (error)" from "push detected staleness (re-merge needed)" to show the right UI.
- What happens to user edits during re-merge. If the user edited the merged content in the Edit tab, then re-merge runs, the LLM produces new content against the new base. The user's edits are lost because the new merge has no knowledge of them. The design should state this explicitly ("Re-merge discards previous edits; the user reviews the new LLM output from scratch") so the user is not surprised.
- What the page looks like between clicking Re-merge and the new merge completing. Presumably it goes back to the `status: "merging"` spinner state. But the user was just in the middle of a review. The transition from "diff review with edit content" to "spinner" back to "new diff review" should be called out as a deliberate UX choice, not left implicit.

### 4. Multi-repo merge review creates confusing "Commit & Push" sequencing

The design says when the user approves public rules from two repos, the merge review shows "one diff card per repo, each with its own Commit & Push button." But it does not address what happens when:

- The user clicks "Commit & Push" on repo A, it fails (network error), and repo B's button is still available. Now the user has a partially-applied state: repo B succeeded but repo A is in error. The page shows one error card and one success confirmation. Is there a consolidated status? Can the user retry repo A without re-triggering repo B?
- The user clicks "Back" to change approvals. Does Back clear all repos' pending merges, or just the one whose Back button was clicked? The design says "Back returns to summary, allows changing approvals" but does not say whether this is per-repo or global. If per-repo, the user sees a mix of merge review cards and summary cards, which is confusing. If global, clicking Back on one repo discards the merge for a different repo the user was happy with.

The design should specify whether the multi-repo scenario uses independent per-repo flows (each card is self-contained with its own Back/Commit/Dismiss) or a unified flow (all repos merge together, all push together, Back applies to all).

## Suggestions (nice to have)

### 1. The Edit tab textarea should have basic safeguards for large files

CLAUDE.md files can be large (the project's own CLAUDE.md is noted as exceeding 25,000 tokens). A plain textarea for a file that large creates a poor editing experience -- no syntax highlighting, no line numbers, no search. The design should acknowledge this limitation and either scope the Edit tab to "minor wording fixes" (not structural reorganization), or specify a code editor component (e.g., CodeMirror or Monaco) instead of a textarea. At minimum, note the known limitation so implementers can make an informed choice.

### 2. The 24-hour TTL on stale merges should show the age

The design says a PendingMerge older than 24 hours shows "Merge is stale, re-merge needed." But it does not show _when_ the merge was created. Adding a relative timestamp ("Merge created 27 hours ago") helps the user understand why the merge is stale and builds trust that the system is not incorrectly marking a recent merge as expired.

### 3. The "Done" confirmation has no forward action

After a successful push, the design says: "Delete PendingMerge, mark all included rules as applied, transition to Done confirmation." But what does Done look like? The card wall review flow design (2026-04-03) specifies a Done message: "Done. 6 learnings saved. 1 commit pushed to schmux/main." But the unified merge design does not specify what happens next. Does the page auto-return to the card wall? Does the user click a button? If there are still pending cards from other proposals that were not part of this merge, the transition should go back to the card wall showing those remaining cards, not to an empty state.

### 4. Error from the merge LLM call has no actionable guidance

When the background merge job fails (LLM timeout, bad response, parse error), the design shows `status: "error"` with a Retry button. But the error message comes from Go `fmt.Errorf` wrapping (`"merge failed for layer %s: %v"`), which will produce messages like "merge failed for layer repo_public: context deadline exceeded." This is not actionable for most users. Consider wrapping known error classes with user-friendly messages: "The merge took too long. This can happen with very large instruction files. Try again, or reduce the number of rules being merged."

### 5. The design should specify what "Back" does to the PendingMerge on the server

The design says "Back returns to summary, allows changing approvals" and separately says PendingMerge is invalidated when "the user explicitly dismisses it (Back + change approvals, or a Dismiss button)." This is ambiguous -- does clicking "Back" alone delete the PendingMerge, or only Back + actually changing an approval? If Back alone deletes it, that is expensive (it triggers a new LLM call when the user returns). If Back preserves the PendingMerge and only invalidates it when approvals change, the user can go Back, look at the summary, decide they are happy, and return to the diff review without re-merging. The latter is better UX and should be explicitly stated.

## Verified Claims (things you confirmed are correct)

1. **Server-driven phase model works as described.** The `GET /api/lore/{repo}/pending-merge` returning 404/merging/ready/error maps cleanly to page states. The codebase's existing `BroadcastCuratorEvent` infrastructure at `server.go:1507` can support the new `lore_merge_complete` event type without architectural changes.

2. **Per-repo PendingMerge file at `~/.schmux/lore/{repo}/pending-merge.json` is consistent with existing patterns.** The lore system already stores per-repo data in `~/.schmux/lore/{repo}/` (state.jsonl, proposals). Adding `pending-merge.json` alongside these is natural.

3. **Rule validation on push endpoint is correctly specified.** The design now explicitly states the push endpoint verifies every rule ID is still approved server-side before committing, with the frontend check as a supplementary UX optimization. This matches the established pattern where the frontend provides optimistic checks but the server is authoritative.

4. **Temporary worktree cleanup on startup is specified.** The design addresses the Review 1 suggestion about orphan cleanup: "On daemon startup, sweep and remove any orphaned temporary worktrees matching the naming convention (e.g., `lore-push-*`)." This is sufficient.

5. **The existing `react-diff-viewer-continued` component is suitable for single unified diffs.** It is already used in the current `LorePage.tsx` at line 512-521 and handles dark theme via `useDarkTheme` prop and configurable context lines via `extraLinesSurroundingDiff`. Using it for the new unified diff is a straightforward reuse.

6. **The PendingMergeStore with per-repo mutex is correctly scoped.** The design specifies it as a separate type from ProposalStore with independent lifecycle, which is correct -- the two stores have different read/write patterns and locking one should not block the other.

7. **The `lore_merge_complete` WebSocket event is correctly described as using `BroadcastCuratorEvent` with a new EventType value.** The existing `CuratorEvent` struct at `curation_state.go:25` already has a string `EventType` field that accepts arbitrary values, so adding `"lore_merge_complete"` requires no schema changes.
