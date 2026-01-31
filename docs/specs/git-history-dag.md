# Git History DAG Spec

## Context

schmux workspaces show git status (branch, ahead/behind, dirty state) but have no visual representation of commit history. When multiple agents work on branches that diverge from main, users need to understand the commit topology — which commits are theirs, where branches diverged, how branches relate to each other, and how far ahead/behind they are — without switching to a terminal and running `git log`.

Sapling ISL (Interactive Smartlog) demonstrates a useful pattern: a vertically-rendered DAG showing commits as nodes on a graph with lane-based layout for parallel branches. We adapt this for schmux, where the key question is "what did my agents produce across all active branches in this repo?"

UI entry point is TBD. The component and API are repo-scoped and designed to be embedded wherever makes sense (dedicated route, workspace view, etc.).

## Goals

- Render a vertical commit DAG for a repo showing all active workspace branches and their relationship to main.
- Support multiple parallel branches with proper lane assignment (branches rendered side-by-side without overlap).
- Show commit hash (short), message (first line), author, and relative timestamp.
- Visually distinguish: branch commits, main commits, fork points, merge commits, and HEAD positions per branch.
- Render merge commits with multiple parent edges.
- Serve the commit graph from a repo-scoped API endpoint on the daemon.
- Work with both regular clones and worktrees.
- Update when git status changes (piggyback on existing git watcher / poll cycle).

## Non-goals

- Interactive rebase, commit editing, or any write operations on git history.
- Rendering the entire repository history (scope to active workspace branches + relevant main context).
- Supporting non-git VCS.
- Deciding where in the UI this component lives (that's a separate UX decision).

## Design

### Data Model

The API returns a graph structure: a list of nodes (commits) and the edges between them (parent relationships). The frontend is responsible for layout (lane assignment, vertical ordering).

Each node carries metadata about which branches reference it and whether it's a HEAD for any workspace. Edges encode parent-child relationships, which is sufficient for the frontend to compute lanes.

### Definitions

**Active workspace branch**: Any branch belonging to a non-disposed workspace in state (`internal/state/state.go`). Session status (running/stopped) doesn't matter — if the workspace exists, its branch is active.

### API Endpoint

**GET /api/repos/{repoName}/git-graph**

Returns the commit graph for all active workspace branches in a repo. `repoName` is the repo name as configured in `config.json` (the `name` field on a repo entry, matched via `Config.FindRepo()`).

Query parameters:
- `max_commits` (optional): Max total commits to return across all branches (default: 200).
- `branches` (optional): Comma-separated branch names to include. Main is always included automatically. Unknown or nonexistent branch names are silently ignored. If omitted, includes all active workspace branches plus main.

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
      "branches": ["feature-foo"],
      "is_head": ["feature-foo"],
      "workspace_ids": ["ws-abc123"]
    },
    {
      "hash": "a1b2c3d4890abcdef1234567890abcdef1234567",
      "short_hash": "a1b2c3d",
      "message": "Merge PR #42",
      "author": "dev",
      "timestamp": "2026-01-29T10:00:00Z",
      "parents": [
        "x1y2z3a4890abcdef1234567890abcdef1234567",
        "b2c3d4e5890abcdef1234567890abcdef1234567"
      ],
      "branches": ["main"],
      "is_head": [],
      "workspace_ids": []
    }
  ],
  "branches": {
    "main": {
      "head": "b2c3d4e5890abcdef1234567890abcdef1234567",
      "is_main": true,
      "workspace_ids": []
    },
    "feature-foo": {
      "head": "f4e5d6c7890abcdef1234567890abcdef1234567",
      "is_main": false,
      "workspace_ids": ["ws-abc123"]
    },
    "feature-bar": {
      "head": "c8d9e0f1890abcdef1234567890abcdef1234567",
      "is_main": false,
      "workspace_ids": ["ws-def456", "ws-ghi789"]
    }
  }
}
```

**`nodes`**: Commit objects in topological order (newest first), preserving `git log --topo-order` output exactly (do not re-sort by timestamp). Topo order guarantees parents appear after children, which the lane assignment algorithm depends on. Each node lists its parent hashes, which of the included branches contain it, whether it's the HEAD of any branch, and which schmux workspaces are at that commit.

**`branches`**: Map of branch name to metadata. `workspace_ids` links branches back to schmux workspaces so the UI can label and color-code them.

**`parents`**: Array of parent hashes. Length 1 for normal commits, 2+ for merges, 0 for root commits. This is the edge list — the frontend draws lines from each node to its parents.

**`workspace_ids`** on nodes: Only populated for HEAD commits (where `is_head` is non-empty). Interior commits don't carry workspace IDs — the `branches` field plus the top-level `branches` map provides that mapping.

**`branches`** on nodes: Only reflects branches explicitly included in the request (active workspace branches + main). Derived by walking the graph from each branch HEAD in-process, not by running `git branch --contains` per node.

### Error Handling

- Unknown `repoName` (not found via `Config.FindRepo()`) → 404.
- Git command failure (corrupted repo, timeout) → 500 with `{"error": "..."}`.
- Never return an empty graph pretending success.

### Backend Implementation

In `internal/workspace/`:

1. Add a `GitGraph` function that:
   - Identifies all active workspace branches for the repo, plus main.
   - Runs `git log --format=%H|%P|%s|%an|%aI --topo-order --parents <branch1> <branch2> ... main` to get the combined commit history with parent info in a single pass. Uses pipe-delimited format consistent with existing git output parsing in `origin_queries.go`.
   - Trims the output (see Graph Trimming below).
   - Parses output into `GitGraphNode` structs.
   - Derives branch membership by walking the parsed graph from each branch HEAD. Do not shell out to `git branch --contains` per node — that's O(N) git processes. One `git log` call gives us the full graph with parent info, then membership is computed in-process by traversing parent pointers from each HEAD.
2. Cache the result per repo. Invalidate on git watcher events for any workspace in that repo.
3. Handle detached HEAD: schmux always checks out a named branch when creating workspaces, so this is an edge case (manual user action). If encountered, include the workspace using the commit hash as a synthetic branch label, marked as detached.

In `internal/dashboard/handlers.go`:

3. Register `GET /api/git-graph` handler. Cache invalidation hooks into the existing `GitWatcher` (`internal/workspace/git_watcher.go`) — when any workspace's git metadata changes, invalidate the cached graph for that workspace's repo.

### Graph Trimming

The full `git log` across all branches could be huge. Trimming rules:

1. Start from the HEAD of each included branch.
2. Walk parents until reaching a commit that is an ancestor of main's HEAD. Use `git merge-base --all <branch> main` to find fork points. When a branch has merged main multiple times, this returns all merge bases — use the oldest (most ancestral) one as the cutoff for that branch.
3. Include that fork point commit plus up to N commits of main context beyond it (default: 5).
4. For main itself, include commits from HEAD back to the oldest fork point across all branches (plus context).
5. Apply `max_commits` as a hard cap after trimming.

This keeps the graph focused on "what diverged" without showing irrelevant ancient history.

### Frontend Component

A `GitHistoryDAG` React component that takes graph data and renders an SVG-based vertical DAG.

**Lane assignment algorithm**:
1. Process nodes in topological order.
2. Main occupies lane 0 (leftmost).
3. When a branch forks from main (or from another branch), assign it the next available lane.
4. When a branch merges, the second parent's lane (the branch being merged in) is freed. The first parent continues in its lane. For merge commits with 2+ parents, the first parent is the "continuation" and all others are "incoming" — this matches git's parent ordering convention.
5. Draw vertical lines for each active lane, with curved connector lines for forks and merges.

**Visual encoding**:
```
  main          feature-foo     feature-bar
  (lane 0)      (lane 1)        (lane 2)

  ○ b2c3d4e     ● f4e5d6c HEAD
  │              │
  ○ x1y2z3a     ● d3e4f5a       ● c8d9e0f HEAD
  │              │               │
  ○ a1b2c3d ────◆ c2d3e4f       │
  │                              │
  ○ m4n5o6p ─────────────────────◆ q7r8s9t
  │
  ○ ...
```

- `●` filled circle: branch commit (colored per branch)
- `○` open circle: main commit
- `◆` diamond: fork point (where a branch diverges)
- Horizontal connector: fork/merge edge
- Each lane gets a distinct color; main is always muted (`--color-text-muted`)
- Branch colors use a fixed palette of 8 CSS custom properties (`--color-graph-lane-1` through `--color-graph-lane-8`), cycling for repos with more than 8 branches. Colors should be distinguishable in both light and dark themes. Added to `global.css` alongside existing design tokens.

**Commit row layout**:
```
[graph column ~80px] [hash] [message] [author] [time]
```

The graph column contains the SVG lanes and nodes. The rest is a standard table row.

**Interactivity**:
- Hover on a commit row highlights it and shows full hash in a tooltip.
- Click a commit hash copies it to clipboard.
- Branch labels rendered at HEAD positions, color-coded.
- Workspace IDs shown as subtle annotations next to branch labels (linking back to schmux context).

**Scrolling**: The graph can be long. Virtualized rendering (only render visible rows) for performance with large histories.

### TypeScript Types

Generated via `go run ./cmd/gen-types` from Go structs:

```typescript
interface GitGraphResponse {
  repo: string;
  nodes: GitGraphNode[];
  branches: Record<string, GitGraphBranch>;
}

interface GitGraphNode {
  hash: string;
  short_hash: string;
  message: string;
  author: string;
  timestamp: string;
  parents: string[];
  branches: string[];
  is_head: string[];
  workspace_ids: string[];
}

interface GitGraphBranch {
  head: string;
  is_main: boolean;
  workspace_ids: string[];
}
```

### Data Flow

1. Component mounts, fetches `GET /api/repos/{repoName}/git-graph`.
2. Frontend computes lane assignment from the node/parent data.
3. Renders SVG graph + commit table.
4. On WebSocket session update events (which fire on git status change), refetch if visible.
5. No additional polling beyond the existing mechanism.

## Testing

### Backend Unit Tests (`workspace/git_graph_test.go`)

- `TestGitGraph_SingleBranch` — one branch ahead of main, correct nodes and parent edges.
- `TestGitGraph_MultipleBranches` — two branches forking from different points on main.
- `TestGitGraph_MergeCommit` — merge commit has two parents in the output.
- `TestGitGraph_ForkPointDetection` — fork points correctly identified for each branch.
- `TestGitGraph_Trimming` — commits beyond the context window are excluded.
- `TestGitGraph_MaxCommits` — hard cap applied correctly.
- `TestGitGraph_Worktree` — works with worktree-based workspaces.
- `TestGitGraph_BranchFilter` — `branches` query param limits which branches appear.
- `TestGitGraph_WorkspaceAnnotation` — workspace_ids correctly mapped to branches and only on HEAD nodes.
- `TestGitGraph_DetachedHead` — detached HEAD workspace included with commit hash as synthetic branch label.
- `TestGitGraph_UnknownBranchIgnored` — unknown branch name in `branches` param is silently dropped.
- `TestGitGraph_MultipleMergeBases` — branch that merged main multiple times uses oldest merge base for trimming.

### API Handler Tests (`dashboard/handlers_test.go`)

- `TestGitGraphEndpoint_Success` — returns 200 with valid JSON graph structure.
- `TestGitGraphEndpoint_UnknownRepo` — returns 404.
- `TestGitGraphEndpoint_BranchFilter` — query param filters branches correctly.

### Frontend Tests

- Lane assignment produces non-overlapping lanes for parallel branches.
- Merge commits render edges to both parents.
- Fork points render as diamond nodes.
- HEAD commits per branch are visually distinguished.
- Virtualized rendering only renders visible rows.
- Empty state when repo has no workspace branches.

### Manual Tests

- Start daemon, spawn sessions on two different branches of the same repo, verify both branches appear in the DAG.
- Make commits in one workspace, verify the graph updates.
- Test with a branch that has merge commits from main (via linear sync).
- Test with worktree-based workspaces.
