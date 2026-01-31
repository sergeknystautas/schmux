# Git History DAG Spec

## Context

schmux workspaces show git status (branch, ahead/behind, dirty state) but have no visual representation of commit history. When agents work on branches that diverge from main, users need to understand the commit topology — which commits are theirs, where the branch diverged, how far ahead/behind they are relative to origin/main — without switching to a terminal and running `git log`.

Sapling ISL (Interactive Smartlog) demonstrates a useful pattern: a vertically-rendered DAG showing commits as nodes on a graph with lane-based layout for parallel branches. We adapt this for schmux, where the key question is "what did my workspace produce relative to main?"

UI entry point: the "git" tab on the session/workspace tab bar, alongside terminal and diff tabs.

## Goals

- Render a vertical commit DAG for a workspace showing the local branch vs origin/main.
- Show the fork point where the branch diverged, commits ahead (local), and commits behind (on origin/main).
- Show commit hash (short), message (first line), author, and relative timestamp.
- Visually distinguish: branch commits, main commits, fork points, merge commits, and HEAD positions.
- Render merge commits with multiple parent edges.
- Serve the commit graph from a workspace-scoped API endpoint on the daemon.
- Query the workspace's git directory (works with both regular clones and worktrees).
- Update when git status changes (piggyback on existing git watcher / poll cycle).

## Non-goals

- Interactive rebase, commit editing, or any write operations on git history.
- Rendering the entire repository history (scope to the divergence region).
- Supporting non-git VCS.
- Showing all repo branches in one view (scope is per-workspace).

## Design

### Data Model

The API returns a graph structure: a list of nodes (commits) and the edges between them (parent relationships). The frontend is responsible for layout (lane assignment, vertical ordering).

Each node carries metadata about which branches reference it and whether it's a HEAD. Edges encode parent-child relationships, which is sufficient for the frontend to compute lanes.

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
    "explore/sapling-isl-integration": {
      "head": "f4e5d6c7890abcdef1234567890abcdef1234567",
      "is_main": false,
      "workspace_ids": ["ws-abc123"]
    }
  }
}
```

**`nodes`**: Commit objects in topological order (newest first), preserving `git log --topo-order` output exactly (do not re-sort by timestamp). Topo order guarantees parents appear after children, which the lane assignment algorithm depends on. Each node lists its parent hashes, which of the included branches contain it, whether it's the HEAD of any branch, and which schmux workspaces are at that commit.

**`branches`**: Map of branch name to metadata. Always contains exactly two entries: the workspace's local branch and the default branch (typically main). `workspace_ids` links branches back to schmux workspaces.

**`parents`**: Array of parent hashes. Length 1 for normal commits, 2+ for merges, 0 for root commits. This is the edge list — the frontend draws lines from each node to its parents.

**`workspace_ids`** on nodes: Only populated for HEAD commits (where `is_head` is non-empty). Interior commits don't carry workspace IDs — the `branches` field plus the top-level `branches` map provides that mapping.

**`branches`** on nodes: Only reflects branches explicitly included (the local branch and main). Derived by walking the graph from each branch HEAD in-process, not by running `git branch --contains` per node.

### Error Handling

- Unknown `workspaceId` (not found in state) → 404.
- Git command failure (corrupted repo, timeout) → 500 with `{"error": "..."}`.
- Empty graph (e.g., branch is main with no divergence) → return valid response with just the HEAD commit(s).

### Backend Implementation

In `internal/workspace/`:

1. Add a `GetGitGraph` function that:
   - Looks up the workspace by ID from state to get `workspace.Path` and `workspace.Branch`.
   - Detects the default branch name (`main` or `master`) via `git symbolic-ref refs/remotes/origin/HEAD`.
   - Runs git commands against the **workspace's git directory** (`workspace.Path`), which has both the local branch and origin refs.
   - Resolves `HEAD` (local branch tip) and `origin/{defaultBranch}` (remote main tip).
   - Finds the fork point via `git merge-base HEAD origin/{defaultBranch}`.
   - Runs `git log --format=%H|%h|%s|%an|%aI|%P --topo-order HEAD origin/{defaultBranch} --ancestry-path` scoped to the divergence region.
   - Trims the output (see Graph Trimming below).
   - Parses output into `GitGraphNode` structs.
   - Derives branch membership by walking the parsed graph from each branch HEAD.

In `internal/dashboard/handlers.go`:

2. Register `GET /api/workspaces/{workspaceId}/git-graph` handler.

### Graph Trimming

The graph should be tightly scoped to the divergence region:

1. Find the fork point: `git merge-base HEAD origin/{defaultBranch}`.
2. Include all commits from the local branch HEAD down to the fork point (these are the "ahead" commits).
3. Include all commits from `origin/{defaultBranch}` HEAD down to the fork point (these are the "behind" commits the local branch is missing).
4. Include the fork point itself.
5. Include up to N additional shared ancestor commits below the fork point for context (default: 5).
6. Apply `max_commits` as a hard cap.

For a typical workspace that is 1 ahead and 1 behind, this produces ~3-8 nodes total: the fork point, the local commit, the remote commit, and a few context commits. This is exactly the right level of detail.

When `HEAD` and `origin/{defaultBranch}` point to the same commit (no divergence), show just the last N commits from HEAD.

### Frontend Component

A `GitHistoryDAG` React component rendered in the "git" tab of the session/workspace view.

**Lane assignment algorithm**:
1. Process nodes in topological order.
2. Main (origin/{defaultBranch}) occupies lane 0 (leftmost).
3. The workspace's local branch occupies lane 1.
4. For merge commits with 2+ parents, the first parent is the "continuation" and others are "incoming" — this matches git's parent ordering convention.
5. Draw vertical lines for each active lane, with curved connector lines for forks and merges.

**Visual encoding**:
```
  origin/main       explore/feature-x
  (lane 0)          (lane 1)

                    ● f4e5d6c HEAD  "Add validation..."
  ○ b2c3d4e HEAD    │               "Clean up tests"
  │                 │
  ◆ a1b2c3d ────────┘               (fork point)
  │
  ○ x1y2z3a                         (context)
  ○ ...
```

- `●` filled circle: local branch commit (colored)
- `○` open circle: main commit
- `◆` diamond: fork point (where the branch diverges)
- Horizontal connector: fork/merge edge
- Main lane uses muted color (`--color-text-muted`)
- Branch lane uses `--color-graph-lane-1`

**Commit row layout**:
```
[graph column ~60px] [hash] [message] [author] [time]
```

The graph column contains the SVG lanes and nodes. The rest is a standard table row.

**Interactivity**:
- Hover on a commit row highlights it and shows full hash in a tooltip.
- Click a commit hash copies it to clipboard.
- Branch labels rendered at HEAD positions, color-coded.
- Workspace ID shown as subtle annotation next to branch label.

**Scrolling**: No virtualization needed in v1 — the tight trimming keeps the graph small (typically <50 nodes).

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

1. User clicks "git" tab → navigates to `/git/{workspaceId}`.
2. Component mounts, fetches `GET /api/workspaces/{workspaceId}/git-graph`.
3. Frontend computes lane assignment from the node/parent data.
4. Renders SVG graph + commit table.
5. On WebSocket session update events (which fire on git status change), refetch if visible.
6. No additional polling beyond the existing mechanism.

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

### Manual Tests

- Start daemon, spawn a session, make commits, verify the branch commit appears in the DAG.
- Advance origin/main (via another workspace or external push), verify "behind" commits appear.
- Test with a branch that has merge commits from main (via linear sync).
