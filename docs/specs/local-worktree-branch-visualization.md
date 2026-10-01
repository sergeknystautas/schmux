# Local Worktree Branch Visualization

**Status:** v1 — deferred design draft; implementation not started.

## Problem

schmux can create a new workspace branch from another local git worktree's
unpushed committed tip, but the commit tab still shows a two-lane relationship:
the current local branch versus the default branch. Once a user branches one
local worktree from another, the existing graph does not answer:

- Which sibling workspace holds the parent branch?
- Which sibling workspace holds the child branch?
- Which commits are shared between those branches?
- Which commits are unique to each workspace branch?
- After a parent force-update, where does the child's pre-rewrite ancestry
  diverge from the rewritten parent?

Git already preserves this information. The feature would present the relevant
subset without requiring terminal commands such as `git log --graph` or
`git merge-base`.

## Current Evidence

### Product behavior already available

- `Manager.CreateFromWorkspace` branches from the source worktree's local
  committed tip.
- Worktrees share repository refs and objects, so the child includes unpushed
  parent commits.
- Uncommitted source changes are not copied.
- A parent force-update does not invalidate an existing child; the branches
  diverge.
- The behavior is documented in `docs/workspaces.md`, `docs/web.md`, and
  `docs/api.md`.

### Commit graph behavior today

`workspace.GetGitGraph` resolves the current workspace's `HEAD` and
`origin/<default>`, walks those refs, and returns topology for the default and
current branches. It already builds a `branchWorkspaces` map from all
workspaces using the same repository, but sibling branch information is not
included in the serialized branch set.

The graph contract already has fields useful for this feature:

- `CommitGraphResponse.Branches`
- `CommitGraphBranch.Head`
- `CommitGraphBranch.WorkspaceIDs`
- `CommitGraphNode.Branches`
- `CommitGraphNode.IsHead`
- `CommitGraphNode.WorkspaceIDs`

### Spike result

A synthetic response containing `main`, `parent`, `child`, and unrelated `peer`
branches produced four columns in the existing `computeLayout` primitive.
Multi-branch rendering therefore does not require a wholesale graph rewrite.

The spike also found a required correction: `computeLayout` currently infers
the local branch by selecting the last non-main key in the branches object.
With multiple non-main branches, response key order can choose the wrong
working-copy lane. An implementation must pass the current workspace branch
explicitly into layout computation.

## Goals

- Show the current workspace branch and relevant sibling worktree branches in
  one commit graph.
- Identify which branch belongs to which workspace without leaving the commit
  tab.
- Preserve current two-lane behavior when no sibling worktree branches exist.
- Respect existing `maxTotal` and `mainContext` graph limits.
- Make shared ancestry and branch-unique commits visually distinguishable.
- Represent force-update divergence without implying that the child is invalid.
- Keep sibling lanes observational; all actions remain scoped to the current
  workspace.

## Non-Goals

- No generic visualization of every local and remote git ref.
- No switching workspaces or applying actions from a sibling lane in v1.
- No mutation of sibling workspaces or their branches.
- No merge, rebase, cherry-pick, or force-update automation.
- No regular full-clone sibling visualization; full-clone local refs and objects
  are not shared with the bare worktree base.
- No Sapling visualization; Sapling uses a distinct VCS model and workspace
  labels.
- No guarantee of complete topology when history exceeds the graph limit.
- No inference of a branch parent that git does not record.

## Proposed Experience

### Default graph

The current workspace remains the graph's primary subject. One virtual
working-copy node remains at the top of the current branch lane, followed by
the existing commit workflow, push controls, and uncommit action.

### Sibling worktree lanes

For a local git worktree repository, sibling workspace branches appear as
separate lanes when their heads or unique commits fit the bounded topology.
Branch-head labels identify branch and workspace ownership:

```text
feature/auth
workspace schmux-003

feature/session-child
workspace schmux-004
```

The current lane is visually highlighted. Shared commits remain represented by
the common/default lane or their actual topology. Sibling branch heads display
branch names and associated workspace IDs.

### Topology, not provenance

Git does not permanently record that schmux created one branch from another.
The graph shows reachability and shared ancestry, not an authoritative creation
parent. A child created from a parent connects through shared commits;
unrelated feature branches may share only an older base commit.

### Force-update case

If a parent is rewritten while a child remains based on old commits, the graph
may show both reachable histories:

```text
new parent history:    A - B' - C'
                                  /
old shared history:  A - B - C
                            \
child history:             C - D - E
```

The exact rendering depends on which refs remain reachable and fit under the
commit limit. The requirement is that the child lane remains valid and its old
ancestry is not hidden merely because the parent ref moved.

## Design

### Backend flow

1. Resolve the current workspace and repository.
2. Confirm the workspace is a local git worktree.
3. Collect non-recyclable sibling workspaces for the same repository.
4. Deduplicate branch names and collect every workspace ID per branch.
5. Resolve each branch head through shared worktree refs.
6. Include the current branch, default branch, and relevant sibling heads in a
   bounded git walk.
7. Annotate membership, head labels, and workspace IDs for every represented
   branch.
8. Return the existing `CommitGraphResponse` contract; no breaking API change
   is expected.

### Bounding strategy

- Preserve the current branch's loaded ancestry first.
- Add sibling branch heads and commits not already represented.
- Stop adding sibling-only history when the total node budget is reached.
- Report truncation clearly; do not imply complete topology.
- Prefer sibling branches with commits shared by the loaded history over
  disconnected or much older branches.

Open choice: include every sibling branch metadata entry but render only those
whose head or unique commits fit the bounded walk, or omit branches entirely
when they cannot fit. The former is more explicit; the latter is simpler.

### Frontend flow

1. Pass the current workspace branch explicitly into layout computation.
2. Assign column 0 to the default branch.
3. Assign the current branch the first working-copy lane.
4. Assign sibling branches deterministic subsequent columns.
5. Render branch and workspace labels at each branch head.
6. Continue deriving push, commit, amend, and uncommit eligibility from the
   current workspace only.

Stable ordering should prioritize current branch, default branch, then sibling
workspace ID or branch name. JSON object order must not determine visual order.

### Implementation seams

| Seam                    | Current role                                | Needed change                                                                 |
| ----------------------- | ------------------------------------------- | ----------------------------------------------------------------------------- |
| `workspace.GetGitGraph` | Walk current and default branches           | Add bounded sibling branch heads and workspace mappings                       |
| `BuildGraphResponse`    | Annotate two branch memberships             | Generalize membership and head annotation across a branch set                 |
| `computeLayout`         | Allocate one column per response branch     | Accept current branch explicitly and stabilize multi-lane ordering            |
| `CommitHistoryDAG`      | Render labels and current workspace actions | Add workspace labels and sibling-lane treatment without widening action scope |

## Cases

| Case                              | Expected result                                                                                                    |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| No siblings                       | Existing two-lane graph and behavior remain unchanged                                                              |
| One child from current parent     | Parent and child lanes share the child's base ancestry; unique commits remain separated                            |
| Current workspace is the child    | Working-copy node stays on the child lane; parent remains observational                                            |
| Multiple workspaces on one branch | One branch lane lists every associated workspace ID                                                                |
| Unrelated sibling branch          | Include only when it fits the bounded topology; otherwise omit or summarize rather than displacing current history |
| Parent force-update               | Existing child remains valid; old shared commits remain reachable through the child and shown where budget allows  |
| Deleted sibling workspace         | Its workspace ID disappears; branch remains only if another workspace or ref makes it relevant                     |
| Regular full clone                | No sibling visualization                                                                                           |
| Sapling                           | No git branch-lane visualization                                                                                   |
| Remote workspace                  | No local sibling worktree visualization                                                                            |

## Acceptance Criteria

### Backend

- A graph for a repository with parent and child workspaces includes both
  branch metadata entries.
- Each branch entry reports its actual head and every associated workspace ID.
- Shared commits are annotated as members of every represented branch that can
  reach them.
- Current-branch membership remains correct with multiple non-main branches.
- The response respects the requested total commit limit.
- Repositories without relevant siblings retain the existing shape and behavior.
- Regular full-clone, Sapling, and remote graphs do not attempt local sibling
  ref expansion.

### Frontend

- The working-copy node is always attached to the current workspace branch,
  independent of response object order.
- Each rendered branch head identifies branch name and workspace IDs.
- The current lane is visually distinguished from sibling lanes.
- Push, commit, amend, and uncommit controls continue to act only on the current
  workspace.
- Sibling lanes do not overlap row content at supported viewport sizes.
- Truncation is visible and does not imply a complete topology.

## Testing

- Go integration test with two local worktree branches sharing an unpushed
  commit.
- Go test for force-update divergence where the child retains old ancestry.
- Layout unit test with current, parent, child, peer, and default branches.
- Component test asserting current-lane selection and workspace labels.
- Existing commit graph regressions remain green.
- Full gates: `./test.sh`, `./format.sh`, and `./badcode.sh`.

## Risks

- **Graph explosion.** Many long-lived workspaces can contribute many branch
  heads and duplicate histories. The backend needs a deliberate budget and a
  stable preference for current-workspace relevance.
- **Ambiguous relevance.** Git does not record schmux's creation relationship.
  Shared ancestry can be old and weak. The UI must not claim unproven
  provenance.
- **Layout density.** Additional lanes consume width and crowd labels. The
  commit tab is operational; readability matters more than showing every branch.
- **Sorting stability.** Map or JSON object order cannot define lane order.
- **Force-update complexity.** A rewritten parent may require both old and new
  object chains, but only refs and commits still reachable within the loaded
  set can be shown.
- **Feature value.** The largest risk is investing before local-first branching
  becomes a settled workflow.

## Open Questions

1. Should v1 show all sibling worktree branches, or only branches whose heads
   or unique commits fit the bounded walk?
2. Should sibling lanes be visible by default or behind a summary toggle?
3. Should labels use full workspace IDs, short IDs, or user-facing labels?
4. Should pre-force-update ancestry be emphasized when a sibling child retains
   it?
5. What is the maximum lane count before the UI collapses sibling branches?

## Decision Gate

Keep this feature deferred until local-capable worktree branching is used often
enough that its absence from the commit tab creates recurring diagnostic work.

Evidence supporting implementation:

- Users repeatedly create child branches before pushing the parent.
- Users need to identify parent and sibling workspaces after leaving spawn.
- Force-updates or rewrites repeatedly make child divergence hard to reason
  about.
- Users manually run `git log --graph`, `git merge-base`, or equivalent.
- The two-lane graph repeatedly misleads rather than merely omitting detail.

Evidence against implementation:

- Local worktree branching remains occasional.
- Workspace labels and existing git commands remain sufficient.
- Most sessions still involve only a feature branch and default branch.
- Users are more often blocked on review or integration than topology discovery.

**Current decision:** do not implement yet. Revisit after real usage answers the
relevance and density questions.
