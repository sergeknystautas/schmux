# Git History DAG Spec

## Context

schmux workspaces show git status (branch, ahead/behind, dirty state) but have no visual representation of commit history. When agents work on branches that diverge from main, users need to understand the commit topology — which commits are theirs, where the branch diverged, how far ahead/behind they are relative to origin/main — without switching to a terminal and running `git log`.

**Reference implementation: Sapling ISL (Interactive Smartlog).** ALL design decisions for the git graph visualization follow ISL's approach. No exceptions. No alternatives. When in doubt, do what ISL does.

UI entry point: the "git" tab on the session/workspace tab bar, alongside terminal and diff tabs.

## Goals

- Render a vertical commit DAG for a workspace showing the local branch vs origin/main.
- Show the fork point where the branch diverged, commits ahead (local), and commits behind (on origin/main).
- Show commit hash (short), message (first line), author, and relative timestamp.
- Show working copy state: "You are here" marker and "View changes" row with dirty file/line counts.
- Follow ISL patterns for layout, sorting, rendering, and interaction.
- Serve the commit graph from a workspace-scoped API endpoint on the daemon.
- Query the workspace's git directory (works with both regular clones and worktrees).

## Non-goals

- Interactive rebase, commit editing, or any write operations on git history.
- Rendering the entire repository history (scope to the divergence region).
- Supporting non-git VCS.
- Showing all repo branches in one view (scope is per-workspace).

## ISL Patterns (Mandatory)

The following ISL patterns MUST be followed. These are non-negotiable:

### 1. Node sorting: draft before public (ISL's `sortAscCompare`)

The backend sorts nodes so that **draft commits** (on the local branch only, not on main) appear **before** public commits (on main or shared). Within each group, topological order from `git log --topo-order` is preserved.

This means the visual order from top to bottom is:
1. Virtual nodes (you-are-here, view-changes) — at the top
2. Draft commits (local branch only) — next
3. Public commits (main or shared) — below draft

### 2. Dynamic N-column layout

Column assignment is **data-driven from branch info**, not hardcoded to 2 lanes:
- Column 0: main/default branch
- Column 1+: each additional branch gets the next available column
- Nodes exclusively on a non-main branch go to that branch's column
- Shared nodes (fork points, on both branches) stay in column 0

### 3. Column-reservation pattern (main extends to top)

Column 0 (main) always has its lane line extend to the **top of the graph**, even where no main commit exists at those rows. This provides visual continuity — the main line runs alongside branch commits, showing the parallel nature of the branches. ISL reserves column slots even when they have no node at a given row.

### 4. Single foreground color

All graph lines and node strokes use a single muted foreground color (`--color-text-muted`). No per-lane coloring. The **working-copy column** (the column containing "you-are-here") uses a highlight color (`--color-graph-lane-1`) for its lane line and node strokes.

### 5. Circles only for node glyphs

All nodes are rendered as circles. No diamonds for fork points or merge commits. ISL uses uniform circle glyphs.
- Virtual nodes (you-are-here, view-changes): filled circle in highlight color
- Regular commits: open (unfilled) circle, stroke in graph color (or highlight color if on the working-copy column)

### 6. Line semantics

- **Solid lines**: direct parent-child edges between commits
- **Dashed lines**: persistent column/lane lines showing column reservation (background, low opacity)

### 7. Branch labels as badges on commit rows

Branch names appear as inline badges on the commit row where `is_head` is non-empty. No separate legend, no synthetic label rows. The `is_head` field on each node already carries this data.

### 8. Virtual working-copy node

One virtual "You are here" node represents the working directory position, inserted above the local HEAD commit. If there are dirty changes, a "View changes" row appears between "You are here" and the HEAD commit. The "View changes" row is clickable and navigates to `/diff/:workspaceId`.

Edge chain: `you-are-here` → [`view-changes` →] HEAD commit.

### 9. S-curve edges for cross-column connections

When an edge connects nodes in different columns, use a cubic bezier S-curve (not a straight diagonal line). Straight lines are used for same-column edges.

## Design

### Data Model

The API returns a graph structure: a list of nodes (commits) sorted ISL-style (draft before public), branch metadata, and optional dirty state. Edges are derived from parent hashes. The frontend computes column layout from branch membership data.

### API Endpoint

**GET /api/workspaces/{workspaceId}/git-graph**

Returns the commit graph for a single workspace, showing the workspace's local branch vs `origin/{defaultBranch}`. The graph is scoped to the divergence region: commits ahead on the local branch, commits ahead on origin/main since the fork point, and the fork point itself with a small amount of shared context.

Query parameters:
- `max_commits` (optional): Max total commits to return (default: 200).
- `context` (optional): Number of shared-ancestor commits to include beyond the fork point (default: 5).

Response:
```json
{
  "repo": "github.com/user/project",
  "nodes": [
    {
      "hash": "f4e5d6c7890abcdef1234567890abcdef1234567",
      "short_hash": "f4e5d6c",
      "message": "Add validation for user input",
      "author": "Claude",
      "timestamp": "2026-01-30T14:22:00Z",
      "parents": ["d3e4f5a6890abcdef1234567890abcdef1234567"],
      "branches": ["explore/sapling-isl-integration"],
      "is_head": ["explore/sapling-isl-integration"],
      "workspace_ids": ["ws-abc123"]
    }
  ],
  "branches": {
    "main": {
      "head": "b2c3d4e5890abcdef1234567890abcdef1234567",
      "is_main": true,
      "workspace_ids": []
    },
    "explore/sapling-isl-integration": {
      "head": "f4e5d6c7890abcdef1234567890abcdef1234567",
      "is_main": false,
      "workspace_ids": ["ws-abc123"]
    }
  },
  "dirty_state": {
    "files_changed": 4,
    "lines_added": 207,
    "lines_removed": 331
  }
}
```

**Node ordering**: Nodes are pre-sorted by the backend following ISL's `sortAscCompare` pattern:
1. Draft nodes (on local branch only, not on main) come first, in topo order
2. Public nodes (on main or shared between branches) come second, in topo order

The frontend MUST NOT re-sort nodes. It processes them in the order received.

**`dirty_state`** (optional): Present when the workspace has uncommitted changes. Contains file count and line add/remove counts. The frontend uses this to render the "View changes" row.

**`nodes`**: Commit objects. Each node lists its parent hashes, which of the included branches contain it, whether it's the HEAD of any branch, and which schmux workspaces are at that commit.

**`branches`**: Map of branch name to metadata. Always contains exactly two entries: the workspace's local branch and the default branch (typically main). `workspace_ids` links branches back to schmux workspaces.

**`parents`**: Array of parent hashes. Length 1 for normal commits, 2+ for merges, 0 for root commits. This is the edge list — the frontend draws lines from each node to its parents.

**`workspace_ids`** on nodes: Only populated for HEAD commits (where `is_head` is non-empty).

**`branches`** on nodes: Only reflects branches explicitly included (the local branch and main). Derived by walking the graph from each branch HEAD in-process, not by running `git branch --contains` per node.

### Error Handling

- Unknown `workspaceId` (not found in state) → 404.
- Git command failure (corrupted repo, timeout) → 500 with `{"error": "..."}`.
- Empty graph (e.g., branch is main with no divergence) → return valid response with just the HEAD commit(s).

### Backend Implementation

**Files**: `internal/workspace/git_graph.go`, `internal/api/contracts/git_graph.go`, `internal/dashboard/handlers.go`

`GetGitGraph` function:
1. Looks up the workspace by ID from state to get `workspace.Path` and `workspace.Branch`.
2. Detects the default branch name (`main` or `master`) via `git symbolic-ref refs/remotes/origin/HEAD`.
3. Resolves `HEAD` (local branch tip) and `origin/{defaultBranch}` (remote main tip).
4. Finds the fork point via `git merge-base HEAD origin/{defaultBranch}`.
5. Runs `git log --format=%H|%h|%s|%an|%aI|%P --topo-order` scoped to the divergence region.
6. Derives branch membership by walking the parsed graph from each branch HEAD.
7. **Sorts nodes ISL-style**: draft (local-only) before public (main/shared), preserving topo order within each group.
8. Populates `dirty_state` from workspace state if `ws.GitFilesChanged > 0`.

Handler registers `GET /api/workspaces/{workspaceId}/git-graph`.

### Graph Trimming

The graph is tightly scoped to the divergence region:

1. Find the fork point: `git merge-base HEAD origin/{defaultBranch}`.
2. Include all commits from the local branch HEAD down to the fork point (the "ahead" commits).
3. Include all commits from `origin/{defaultBranch}` HEAD down to the fork point (the "behind" commits).
4. Include the fork point itself.
5. Include up to N additional shared ancestor commits below the fork point for context (default: 5).
6. Apply `max_commits` as a hard cap.

### Frontend Layout (`gitGraphLayout.ts`)

**`computeLayout(response: GitGraphResponse): GitGraphLayout`**

1. Identify branches from `response.branches` — find main (is_main: true) and local branch.
2. Build column map: main→0, each additional branch→next column.
3. Column assignment per node: if a node is on a non-main branch exclusively (not on main), assign it to that branch's column. Otherwise column 0.
4. Insert virtual nodes before the local HEAD commit:
   - `__you-are-here__` node (nodeType: 'you-are-here')
   - `__view-changes__` node if `dirty_state` has files changed (nodeType: 'view-changes')
5. Build edges: virtual node chain + commit→parent edges.
6. Compute lane lines: each column's line spans from its topmost to bottommost node. **Column 0 (main) is forced to extend to the top of the graph** (ISL column-reservation pattern).
7. Track `youAreHereColumn` for highlight coloring.

**Key types**:
```typescript
interface LayoutNode {
  hash: string;
  column: number;
  y: number;
  node: GitGraphNode;
  nodeType: 'commit' | 'you-are-here' | 'view-changes';
  dirtyState?: { filesChanged: number; linesAdded: number; linesRemoved: number };
}

interface LayoutEdge {
  fromHash: string; toHash: string;
  fromColumn: number; toColumn: number;
  fromY: number; toY: number;
}

interface LaneLine {
  column: number;
  fromY: number; toY: number;
}

interface GitGraphLayout {
  nodes: LayoutNode[];
  edges: LayoutEdge[];
  columnCount: number;
  rowHeight: number;
  laneLines: LaneLine[];
  localBranch: string | null;
  youAreHereColumn: number | null;
}
```

### Frontend Rendering (`GitHistoryDAG.tsx`)

SVG layers (back to front):
1. **Column lines** (dashed, low opacity) — ISL column-reservation lines
2. **Edge paths** (solid) — direct parent-child connections
3. **Node circles** — commit glyphs

Row content (right of SVG):
- **you-are-here row**: "You are here" text
- **view-changes row**: clickable button showing "{N} file(s), +{added} −{removed}", navigates to `/diff/:workspaceId`
- **commit row**: short hash (clickable, copies full hash) | branch badges (from is_head) | message | author | relative time

**Colors**:
- `GRAPH_COLOR = var(--color-text-muted)` — all lines, all non-highlighted node strokes
- `HIGHLIGHT_COLOR = var(--color-graph-lane-1)` — working-copy column lane line, node strokes on that column, filled virtual nodes

**No legend.** Branch identity is conveyed by badges on HEAD commits and column position.

### Visual Example (ISL-style)

```
  main            explore/feature-x
  (col 0)         (col 1)

  ┊               ● You are here
  ┊               ● 3 files, +42 −7
  ┊               ○ f4e5d6c  [explore/feature-x]  "Add validation..."
  ┊               ○ a1b2c3d  "Fix edge case..."
  ○ fff01e5  [main]  "Reduce font sizes..."
  ○ b81131e  "Improve multi-line..."
  ○ 36ea336  "Add multi-line selection..."
  ○ 85ae863  "Detect default branch..."
  ○ b2f2d94  "Clean up Docker..."  (fork point, shared)
  ○ 47b7fd1  "Update Go to 1.24..."
```

Note: main's column line (┊) extends from the top to its bottommost node, running alongside the branch commits even though main has no nodes at those rows. This is ISL's column-reservation pattern.

### Route (`GitGraphPage.tsx`)

Route: `/git/:workspaceId`

- Loads workspace from context, renders `WorkspaceHeader` + `SessionTabs` (with `activeGitTab`) + `GitHistoryDAG`.
- Guards against reload: only redirects to `/` if `!loading && !workspace` (prevents redirect during initial data fetch).

### TypeScript Types

Generated via `go run ./cmd/gen-types` from Go structs. Includes `GitGraphDirtyState` and `dirty_state` field on `GitGraphResponse`. See `assets/dashboard/src/lib/types.generated.ts`.

### Data Flow

1. User clicks "git" tab → navigates to `/git/{workspaceId}`.
2. Component mounts, fetches `GET /api/workspaces/{workspaceId}/git-graph`.
3. Frontend calls `computeLayout(response)` to get column assignments, edges, lane lines.
4. Renders SVG graph + commit rows.
5. On WebSocket session update events (which fire on git status change), refetch if visible.

## Current Implementation State

### What's done
- Backend: `GetGitGraph` with fork point detection, divergence region scoping, branch membership walking, ISL-style draft-before-public sort, dirty state population.
- API contract: `GitGraphResponse` with `GitGraphDirtyState`.
- Frontend layout: `computeLayout` with dynamic column assignment, virtual node insertion, lane line computation with column 0 extension.
- Frontend rendering: SVG with dashed column lines, solid edges, circle-only glyphs, single color + highlight.
- Route: `/git/:workspaceId` with reload guard.
- Generated TypeScript types.

### Known issues being debugged
- **Single-column rendering**: Despite correct backend data (draft nodes first with correct branch membership) and correct frontend logic (nodeColumn should return column 1 for branch-only nodes), the graph was rendering all nodes in column 0. This needs investigation — likely the dashboard assets need to be rebuilt (`go run ./cmd/build-dashboard`) and the daemon restarted to pick up the latest frontend code.

## Testing

### Backend Unit Tests (`workspace/git_graph_test.go`)

- `TestGitGraph_SingleBranch` — one branch ahead of main, correct nodes and parent edges.
- `TestGitGraph_BranchBehind` — branch is behind origin/main, shows the "behind" commits.
- `TestGitGraph_AheadAndBehind` — branch is both ahead and behind, shows divergence clearly.
- `TestGitGraph_MergeCommit` — merge commit has two parents in the output.
- `TestGitGraph_ForkPointDetection` — fork point correctly identified.
- `TestGitGraph_Trimming` — commits beyond the context window are excluded.
- `TestGitGraph_MaxCommits` — hard cap applied correctly.
- `TestGitGraph_NoDivergence` — branch is at same commit as main, shows recent history.
- `TestGitGraph_WorkspaceAnnotation` — workspace_ids correctly mapped to branch HEAD.
- `TestGitGraph_UnknownWorkspace` — unknown workspace ID returns error.
- `TestGitGraph_MultipleMergeBases` — branch that merged main multiple times uses correct fork point.

### API Handler Tests (`dashboard/api_contract_test.go`)

- `TestGitGraphEndpoint_UnknownWorkspace` — returns 404.
- `TestGitGraphEndpoint_MethodNotAllowed` — POST returns 405.

### Build Verification

After any code changes:
1. `go run ./cmd/gen-types` — regenerate TypeScript types if Go contracts changed
2. `go run ./cmd/build-dashboard` — rebuild frontend assets
3. `go test ./...` — run all backend tests
4. Restart daemon to pick up new embedded assets
5. Visual verification in browser

### Manual Tests

- Start daemon, spawn a session, make commits, verify the branch commit appears in the DAG.
- Advance origin/main (via another workspace or external push), verify "behind" commits appear.
- Test with a branch that has merge commits from main.
- Verify "You are here" and "View changes" rows appear when workspace has dirty files.
- Verify clicking "View changes" navigates to `/diff/:workspaceId`.
- Verify clicking a commit hash copies the full hash to clipboard.
- Verify branch labels appear as badges on HEAD commits.
- Verify main column line extends to top of graph alongside branch commits.
- Verify reloading `/git/:workspaceId` stays on the page (doesn't redirect to home).
