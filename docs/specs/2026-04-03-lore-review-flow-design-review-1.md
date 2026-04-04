VERDICT: NEEDS_REVISION

## Summary Assessment

The design dramatically simplifies the lore review experience and correctly identifies real UX pain points (repo tabs, multi-step public persistence, invisible affordances). However, it introduces a new "direct push to main" git flow that has no backend precedent in the codebase, understates the complexity of flattening proposals into a card wall, and leaves the inline source-signal display underspecified given what the data model actually supports.

## Critical Issues (must fix)

### 1. Source signals cannot be displayed inline without a new data-fetching mechanism

The design shows source signals displayed inline on each card (failures and reflections that triggered the rule). But the current `Rule.SourceEntries` field is a `[]string` of timestamps or entry keys (see `internal/lore/curator.go:28` and the extraction prompt at line 66). These are opaque references, not displayable text. The architecture doc itself warns: "the LLM's source_entries format is unreliable for text matching" (`docs/lore.md:67`).

To render the inline signals shown in the mockup (e.g., `x cd assets/dashboard && go test -> module error`), the frontend would need to either: (a) resolve SourceEntries timestamps against the raw entries API to fetch full entry objects, requiring a new join query or endpoint, or (b) change the extraction prompt and Rule struct to embed full entry data (text, type, tool, error) at extraction time. Neither option is trivial, and the design does not specify which approach to take.

**Impact:** The source signal display is a core differentiator of the new design ("cards should show source signals inline so the user can judge why the rule was proposed"). Without a concrete plan for how to populate this data, the feature cannot be implemented.

### 2. "Direct push to main" has no backend implementation and significant safety concerns

The design proposes two public rule persistence modes: "Direct push to main" (default) and "Create pull request." The codebase has no existing handler that commits and pushes directly to main from the dashboard server. The closest pattern is `linear_sync.go:PushToMain` which operates on an existing workspace with a full commit history, not a one-off file write.

Pushing directly to main from the daemon process raises several concerns:

- **No workspace:** The design says "No workspace is created." But git operations require a working tree. The daemon would need to either use the bare repo (which complicates committing) or create a temporary checkout. Neither is addressed.
- **Race conditions with active workspaces:** If an agent is actively working on a workspace checked out from the same repo, pushing to main from the daemon creates a divergence that the workspace does not expect. The existing `linear_sync` flow handles this carefully; a new push path would need the same care.
- **Branch protection:** Making "direct push to main" the default is risky. Many repos have branch protection rules. The design acknowledges this ("For teams with branch protection") but makes the unprotected path the default.
- **Auth:** Git push requires credentials. The daemon currently delegates push to tmux sessions where the user's SSH agent or credential helper is available. A daemon-initiated push would need to handle authentication differently.

**Impact:** This is the highest-friction change in the design and the one most likely to cause data loss or broken repos if implemented incorrectly. It needs a concrete backend implementation sketch covering workspace lifecycle, auth, and race conditions.

### 3. Flattening proposals into a card wall loses important batching semantics

The design says "the proposal becomes an invisible implementation detail" and cards are sorted "newest first" across all repos. But the current backend is organized around proposals: the merge endpoint operates on a proposal, the apply-merge endpoint operates on a proposal, and the dismiss endpoint operates on a proposal. The API at `POST /api/lore/{repo}/proposals/{proposalID}/merge` requires a proposal ID.

The card wall presents individual rules without proposal grouping, but the persistence phase (the summary screen after all cards are resolved) implicitly re-groups by proposal to trigger the merge. The design does not explain:

- How does "Apply" on the persistence summary know which proposal(s) to merge? If cards span multiple proposals (from multiple curation runs), does it merge all of them? The current merge endpoint only handles one proposal at a time.
- If a user approves 3 rules from proposal A and 2 from proposal B, does the persistence phase run two separate merges? What if one fails?
- What happens if new proposals arrive (from background curation) while the user is reviewing the card wall?

**Impact:** Without specifying how the flat card model maps back to the proposal-scoped API, implementers will have to invent the mapping, likely introducing bugs or requiring API changes that the design does not account for.

### 4. No specification for how "Commit & Push" / "Create PR" git operations work without a workspace

The current `handleLoreApplyMerge` for the public layer creates a `schmux/lore` workspace, writes the file there, and lets the user commit manually. The design removes the workspace flow and replaces it with "Commit & Push" or "Create PR" buttons on the lore page itself. But it does not specify:

- Where does the commit happen? In a temporary worktree? In the bare repo?
- What commit message is used?
- For "Create PR": what branch name? How is the branch cleaned up after merge?
- What if the push fails (network error, auth failure, branch protection)? The design only shows a success confirmation ("Done. 6 learnings saved. 1 commit pushed to schmux/main.").

These are not UI questions -- they are backend architecture questions that determine feasibility. The existing workspace-based flow handles all of these (the user commits in a shell session where git is fully available). The proposed inline flow pushes this complexity into the daemon.

## Suggestions (nice to have)

### 1. Consider keeping a minimal workspace flow as the "Create PR" backend

Rather than implementing git operations directly in the daemon, the "Create PR" mode could still create a `schmux/lore` workspace behind the scenes but auto-commit and auto-push, then navigate to the PR URL. This reuses the tested workspace/git infrastructure while removing the manual steps the user dislikes. The "Direct push to main" mode could do the same but target main instead of a branch.

### 2. The "Approve All" batch action needs undo or confirmation

"Approve All" applies default-private to every remaining card. If the user accidentally clicks it before reviewing all cards, there is no undo. Unlike "Dismiss All" (which is recoverable since the signals still exist), approved rules get persisted and may be hard to find and remove. Consider adding a brief undo window (like Gmail's "undo send") or a confirmation dialog.

### 3. The persistence summary phase creates an awkward intermediate state

The design introduces a two-phase UX: first review all cards, then see a summary and click "Apply." This means the user must resolve every card before anything happens. With many cards, this could feel like busywork. Consider allowing incremental persistence: when a card is approved, persist private rules immediately (they are instant writes) and only batch public rules for the merge step.

### 4. Cross-repo card aggregation needs pagination or limits

The card wall aggregates across all repos sorted newest first. A user with 5 repos and active curation could easily have 50+ cards. The design shows no pagination, filtering, or lazy loading. The current API fetches proposals per-repo (`/api/lore/{repo}/proposals`), so the frontend would need to fan out N requests (one per repo) to populate the wall. For many repos, this could be slow and produce a very long page.

### 5. The design should specify what happens to already-approved/applied proposals

The wall shows pending cards. But the current `ProposalStore.List()` returns all proposals including applied and dismissed ones. Are collapsed approved cards shown permanently? If so, the wall will grow indefinitely. If not, when are they removed? The design only covers the immediate approve/dismiss interaction but not the long-term lifecycle.

### 6. The "Trigger Curation" button in dev mode should clarify scope

The design puts "Trigger Curation" in the dev-mode debug section but does not specify whether it curates for a specific repo or all repos. The current handler `handleLoreCurate` is per-repo (`/api/lore/{repo}/curate`). In a flat card wall with no repo tabs, it is unclear which repo's signals get curated. Either show one button per repo, or add a repo selector to the debug section.

### 7. Action cards need more detail on the Edit flow

The design says Edit makes "rule text (or action prompt)" editable. But action cards have multiple fields (name, prompt/command, description, skill_ref, triggers). The current `ProposedActionCard` component is significantly more complex than a rule text edit. The design should specify which action fields are editable and how editing maps to the `SpawnEntry` API (`PUT /api/emergence/{repo}/entries/{id}`), which is a different endpoint from the lore rule update endpoint.

## Verified Claims (things I confirmed are correct)

1. **Repo tabs exist and cause misdirection** -- Confirmed. `LorePage.tsx:818-835` renders repo tabs using `session-tabs` classes, and badge counts are fetched for all repos at lines 632-656.

2. **Three levels of navigation (repo tabs, sub-tabs, proposals)** -- Confirmed. Repo tabs at line 818, Instructions/Actions sub-tabs at line 839-854, then proposal cards with per-rule rows inside.

3. **Public rules create a workspace** -- Confirmed. `handleLoreApplyMerge` at `handlers_lore.go:483` calls `s.workspace.GetOrCreate(r.Context(), repoURL, "schmux/lore")` and writes the file as an unstaged change.

4. **Rule text is click-to-edit with no visual cue** -- Confirmed. `LorePage.tsx:129` has `onClick={() => rule.status === 'pending' && setEditing(true)}` on a plain div with class `ruleText`, no cursor or hover hint.

5. **`react-diff-viewer-continued` is already a dependency** -- Confirmed. Used in `LorePage.tsx`, `GitCommitPage.tsx`, and `DiffPage.tsx`. Listed in `package.json:24`.

6. **The `auto_pr` config field already exists** -- Confirmed at `internal/config/config.go:390`. The design's "Public rule mode" config maps naturally to this existing field, though it would need to be extended from a boolean to an enum (direct-push vs create-PR).

7. **Dev mode detection pattern exists** -- Confirmed. `useDevStatus.ts:12` provides `isDevMode` from `versionInfo?.dev_mode`. The AppShell already conditionally renders dev-mode components at lines 1033-1036. The design's dev-mode-only debug section follows this pattern.

8. **Source entries on rules are timestamps/keys, not displayable text** -- Confirmed. The extraction prompt at `curator.go:66` asks for `"<timestamp or entry key that led to this rule>"`. The `Rule.SourceEntries` field at `proposals.go:42` is `[]string` with no structured entry data.

9. **The current LegacyProposalCard is still rendered** -- Confirmed. `LorePage.tsx:504` defines `isV2Proposal()` and the page renders `LegacyProposalCard` for v1 proposals at line 867. The design's removal of legacy support is a valid cleanup.
