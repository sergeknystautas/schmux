# Lore Review Flow Redesign

## Problem

The current lore page has several UX problems that make reviewing agent learnings unnecessarily painful:

1. **Repo tab misdirection.** The sidebar badge draws attention, but clicking through often lands on a repo tab with nothing to review. The user has to scan tabs to find pending items.

2. **Too much hierarchy.** The page nests repo tabs > sub-tabs (Instructions/Actions) > proposals > rules. Three levels of navigation to reach the thing the user actually acts on: individual rules.

3. **Public rule persistence is too much work.** Private rules save instantly. Public rules create a workspace, requiring the user to find it, commit, push to main, then pull from main in every other workspace. This asymmetry creates a strong incentive to never save rules publicly, breaking the flywheel of shared learning.

4. **Invisible affordances.** Rule text is click-to-edit but has no visual cue. No batch operations for approving multiple rules.

5. **Vague naming.** "Instructions" vs "Actions" sub-tabs are abstract. "Public / Private / Cross-Repo Private" layer names describe implementation, not intent.

## Design

### Mental Model

Lore is about **emergent learning**. Agents discover things during work sessions and the system surfaces those learnings for human review. The user's job is triage: approve, edit, or dismiss each learning. Where the learning gets persisted is a separate concern handled by the system.

### The Card Wall

The `/lore` page becomes a single flat view. No repo tabs, no sub-tabs, no proposal grouping. Just a wall of learning cards, aggregated across all repos, sorted newest first.

There are two types of cards:

**Instruction cards** — rules that persist to instruction files:

```
+------------------------------------------------------+
|  instruction . testing                      schmux    |
|                                                       |
|  "Always run tests from the repo root, not from       |
|   subdirectories."                                    |
|                                                       |
|  - - - - - - - - - - - - - - - - - - - - - - - - -   |
|  x `cd assets/dashboard && go test` -> module error   |
|  * "tests must run from root, go.mod is there"        |
|                                                       |
|  [ ] Commit to repo (visible to collaborators)        |
|                                                       |
|                         [Dismiss]  [Edit]  [Approve]  |
+------------------------------------------------------+
```

**Action cards** — prompts/commands that persist to the spawn system:

```
+------------------------------------------------------+
|  action . build                             schmux    |
|                                                       |
|  "Fix lint errors"                                    |
|  $ ./format.sh && ./test.sh --quick                   |
|                                                       |
|  - - - - - - - - - - - - - - - - - - - - - - - - -   |
|  * "ran format + quick tests 4 times this session"    |
|                                                       |
|                         [Dismiss]  [Edit]  [Approve]  |
+------------------------------------------------------+
```

Card anatomy:

- **Top-left:** Type label + category tag (e.g., "instruction . testing")
- **Top-right:** Repo name (subtle, since cards are cross-repo)
- **Body:** The learning itself. For instructions: rule text. For actions: name + prompt/command.
- **Source signals:** Inline below the body, showing the failures/reflections that triggered the learning. Failures styled with danger-colored left border, reflections with accent-colored left border.
- **Privacy controls (instructions only):** Unchecked checkbox "Commit to repo (visible to collaborators)". When checked, a nested checkbox appears: "Apply to all my repos". Default is private, this repo only. Actions don't have this control since they're always local.
- **Action buttons:** Dismiss (left), Edit (middle), Approve (right).

### Card State Transitions

**Dismiss** — card animates out (fade + slide, ~200ms). Gone from view.

**Edit** — rule text (or action prompt) becomes an inline textarea. Save/Cancel buttons appear below. Save returns to the card view with updated text. Cancel discards changes.

**Approve** — card collapses to a compact single-line summary showing the truncated text and destination:

```
check  "Always run tests from the repo root..."    [Private . this repo]
```

The collapsed card stays in the wall but takes minimal vertical space.

**Batch shortcut:** An "Approve All" button appears at the top of the wall when 2+ cards are pending. Approves every remaining pending card with their current settings (default private). Intended for when most rules look good — dismiss the bad ones first, then approve the rest in one click.

### Persistence Phase

When the last pending card is approved or dismissed, the card wall transitions to a persistence summary:

```
+------------------------------------------------------+
|  7 learnings approved                                 |
|                                                       |
|  Instructions:                                        |
|    3 private (schmux) -- saved immediately             |
|    1 private (all repos) -- saved immediately          |
|    1 public (schmux) -- committed to CLAUDE.md         |
|                                                       |
|  Actions:                                             |
|    2 pinned to spawn                                  |
|                                                       |
|                                             [Apply]   |
+------------------------------------------------------+
```

On clicking Apply:

1. **Private instructions** -- written to `~/.schmux/instructions/` immediately.
2. **Actions** -- pinned to spawn system immediately.
3. **Public instructions** -- LLM merge runs in the background. A spinner replaces the summary line ("Merging public rules into CLAUDE.md..."). When the merge completes, a diff viewer appears inline showing the current vs. merged CLAUDE.md content. The user reviews and optionally edits the merged content, then clicks "Commit & Push" (or "Create PR" depending on config).

After everything completes, a confirmation replaces the summary:

> "Done. 6 learnings saved. 1 commit pushed to schmux/main."

The page then returns to the empty state.

### Privacy Controls

The three current layer names are replaced with intent-based controls:

| Old name             | New UX                                                    | Storage                              |
| -------------------- | --------------------------------------------------------- | ------------------------------------ |
| `repo_public`        | Checkbox: "Commit to repo (visible to collaborators)"     | Git-tracked CLAUDE.md                |
| `repo_private`       | Default (no checkbox checked)                             | `~/.schmux/instructions/` per-repo   |
| `cross_repo_private` | Checkbox: "Apply to all my repos" (under commit checkbox) | `~/.schmux/instructions/` cross-repo |

Default is always private, this repo only — the safest option. Making a rule public requires deliberate opt-in via the "Commit to repo" checkbox. This addresses the concern about accidentally leaking personal or proprietary information to a public CLAUDE.md.

### Public Rule Persistence Modes

Configurable in `/config` (Advanced tab):

- **Direct push to main** (default) — "Commit & Push" button on the lore page commits and pushes directly to main. Lowest friction.
- **Create pull request** — "Create PR" button on the lore page creates a branch, commits, pushes, and opens a PR. For teams with branch protection.

In both modes, the diff is reviewed inline on the lore page before any git action.

### Empty State

When there are no pending cards:

> "Nothing to review. Rules will appear here automatically as agents encounter friction."

The sidebar badge only lights up when there are actually pending cards across any repo. No more clicking through to find nothing.

### Dev Mode

In dev mode, a collapsible "Debug" section appears below the card wall containing:

- Raw signal entries with type-colored left borders
- Type and agent filter dropdowns
- "Trigger Curation" button (with repo selector, since the page is now cross-repo)
- "Delete Signals" button
- Curation progress (CuratorTerminal events)

This section is hidden entirely outside of dev mode.

### Configuration

In the Advanced tab at `/config`:

- **Lore enabled/disabled** — unchanged
- **LLM target** — unchanged
- **Public rule mode** — "Direct push to main" / "Create pull request" (new)
- **Curate on dispose** — unchanged

### Visual Design Notes

- Cards should have a subtle `transition` on `border-color` and `opacity` (~150ms) so state changes feel smooth rather than binary.
- Dismissed cards animate out rather than disappearing instantly.
- Approved cards collapse with a brief height transition.
- Source signals use the existing type-based color scheme: danger (red) for failures, accent (blue) for reflections, warning (yellow) for friction.
- The diff viewer for public rules uses the existing `react-diff-viewer-continued` component in unified mode with dark theme support.

## Implementation Notes

### Source Signal Data Path

The current `Rule.SourceEntries` field is `[]string` storing opaque timestamps — not displayable text. To render inline source signals on cards, the extraction curator must be updated to embed full entry data in each rule:

```go
type RuleSourceEntry struct {
    Type         string `json:"type"`          // "failure", "reflection", "friction"
    Text         string `json:"text"`          // reflection/friction text
    InputSummary string `json:"input_summary"` // for failures: what was attempted
    ErrorSummary string `json:"error_summary"` // for failures: what went wrong
    Tool         string `json:"tool,omitempty"`
}
```

The extraction prompt (`BuildExtractionPrompt`) changes from requesting timestamps to requesting structured entry data. Since all raw entries are available at curation time, this is a prompt + schema change with no new data fetching needed.

### Flat Cards Over Proposal-Scoped API

The card wall hides proposal grouping from the user, but the backend API remains proposal-scoped. Each card carries an internal `proposalId` (not displayed). When the user clicks Apply:

1. The frontend groups approved rules by `(repoName, proposalId)`.
2. For each group, it calls the existing merge and apply-merge endpoints sequentially.
3. If one proposal's merge fails, the error is shown and the user can retry or skip that group. Already-persisted private rules and actions are not rolled back (they are idempotent writes).

This preserves backend semantics without exposing proposal structure to the user.

### Git Operations Use Workspace Infrastructure

The "Commit & Push" / "Create PR" flow reuses existing workspace infrastructure behind the scenes rather than implementing new git operations in the daemon:

1. On Apply (public rules), the daemon creates or reuses a `schmux/lore` workspace (same as today).
2. The merged CLAUDE.md content is written as an unstaged change (same as today).
3. **New:** Instead of leaving the workspace for manual commit, the daemon auto-commits with a generated message (e.g., "lore: add 3 rules from agent learnings") and pushes to the configured target (main or a new branch).
4. **Direct push mode:** Commits to main, pushes, cleans up the workspace.
5. **PR mode:** Creates a branch (e.g., `lore/rules-<date>`), commits, pushes, opens a PR via the GitHub API, returns the PR URL.
6. Auth uses the user's existing git credential configuration (SSH agent or credential helper), which is available to the daemon process since it runs under the user's session.

If the push fails (auth, network, branch protection), the error is shown inline on the lore page with a Retry button. The workspace is preserved so the commit is not lost.

### Lifecycle of Applied Cards

Applied and dismissed proposals are not shown on the card wall. The wall only shows rules with status `pending` from proposals with status `pending` or `merging`. Once a proposal is fully resolved (all rules approved/dismissed and merge applied), it disappears from the wall permanently.

For long-term history, the existing curation run logs at `~/.schmux/lore-curator-runs/` provide an audit trail. A dedicated history view is a future consideration but out of scope for this design.

### Scaling

The frontend fans out `N` requests (one per configured repo) to populate the card wall. For the expected scale (1-5 repos, 1-10 proposals, 1-50 rules total), this is fine. If scaling becomes a concern, a cross-repo proposals endpoint could be added later.

## What This Removes

- Repo tabs on the lore page
- Instructions / Actions sub-tabs
- Proposal grouping (the "proposal" becomes an invisible implementation detail)
- The manual `schmux/lore` workspace commit flow (workspace is now automated)
- The "Merge N Rules" intermediate step (merge still happens, but inline after Apply)
- Layer picker radio buttons (replaced by checkboxes with clear intent)
- Legacy v1 proposal card support

## Future Considerations

- **Actions as slash commands.** Action cards currently pin to the spawn system. In the future, approved actions could become project-level slash commands, which would reintroduce the public/private question for actions too.
- **Quick actions / pastebin / emerged prompts unification.** Three overlapping systems that could be rationalized. Out of scope for this design.
- **Applied rules history.** A "History" section showing what rules have been applied over time would close the feedback loop and build confidence in the system. Out of scope for this design.
