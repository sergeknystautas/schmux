# Spawn "Create new branch?" Option

Design notes for adding a "Create new branch?" option when spawning into an existing workspace.

**Status**: Spec

## Current State

When navigating to `/spawn?workspace_id=X` (workspace mode), the spawn form:

- Pre-fills repo and branch from the existing workspace
- Hides the branch input (workspace's branch is used implicitly)
- On spawn, adds sessions to the **existing workspace** (`workspace_id` is passed through)

Branch suggestion only works in "fresh" spawn mode — it generates a branch name from the user's prompt, then creates a new workspace with that branch.

**Problem**: No way to create a new workspace with a new branch, branching from an **existing workspace's current branch** (not from default branch).

## Motivating Example

User has workspace `myrepo-001` on branch `feature-a` (which itself is off `main`):

- They want to spawn into `feature-b`, branched from the **tip of `feature-a`**
- Current behavior: Can only spawn into existing workspace (same branch), or create new workspace from default branch

## Proposed Design

Add a checkbox **"Create new branch?"** that only appears in workspace mode.

### Checkbox Behavior

| Checkbox State | Behavior                                                            |
| -------------- | ------------------------------------------------------------------- |
| Unchecked      | Spawn into existing workspace (current behavior)                    |
| Checked        | Create new workspace with new branch from source workspace's branch |

When checked:

1. Reveal branch input (currently hidden in workspace mode)
2. Call branch suggestion API with user's prompt
3. Pass suggested branch name as new field to backend

### Form Changes

**SpawnPage.tsx state:**

```typescript
createBranch: boolean = false; // NEW: stored in draft per-workspace
```

**Spawn draft storage** — add to `SpawnDraft` interface:

```typescript
interface SpawnDraft {
  prompt: string;
  spawnMode: 'promptable' | 'command' | 'resume';
  selectedCommand: string;
  targetCounts: Record<string, number>;
  modelSelectionMode: 'single' | 'multiple' | 'advanced';
  repo?: string;
  newRepoName?: string;
  createBranch?: boolean; // NEW: only relevant in workspace mode
}
```

### Spawn API Changes

**Request** (`POST /api/spawn`):

Add one new field:

| Field          | Type                          | Description                                                                                                                          |
| -------------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `new_branch`   | `string` (optional, nullable) | When set with `workspace_id`, create a new workspace with this branch name, branching from the workspace specified by `workspace_id` |
| `workspace_id` | `string` (optional)           | Source workspace when `new_branch` is set; target workspace when `new_branch` is empty (existing behavior)                           |

**Semantic change**:

- `workspace_id` + **no** `new_branch`: Spawn into existing workspace (current behavior)
- `workspace_id` + `new_branch`: Create new workspace branching from `workspace_id`'s current branch

**Response** — no changes (returns `workspace_id` of newly created workspace)

### Backend Changes

**handlers.go** — `SpawnRequest` struct:

```go
type SpawnRequest struct {
    Repo            string         `json:"repo"`
    Branch          string         `json:"branch"`
    Prompt          string         `json:"prompt"`
    Nickname        string         `json:"nickname,omitempty"`
    Targets         map[string]int `json:"targets"`
    WorkspaceID     string         `json:"workspace_id,omitempty"`     // source when new_branch is set
    Command         string         `json:"command,omitempty"`
    QuickLaunchName string         `json:"quick_launch_name,omitempty"`
    Resume          bool           `json:"resume,omitempty"`
    RemoteFlavorID  string         `json:"remote_flavor_id,omitempty"`
    NewBranch        string         `json:"new_branch,omitempty"`        // NEW
}
```

**session/manager.go** — `Spawn()` function:

```go
func (m *Manager) Spawn(ctx context.Context, repoURL, branch, targetName, prompt, nickname string, workspaceID string, resume bool, newBranch string) (*state.Session, error) {
    // ...
    if workspaceID != "" && newBranch != "" {
        // NEW: Create new workspace from source workspace's branch
        w, err = m.workspace.CreateFromWorkspace(ctx, workspaceID, newBranch)
    } else if workspaceID != "" {
        // Existing: Spawn into specific workspace
        ws, found := m.workspace.GetByID(workspaceID)
        // ...
    } else {
        // Existing: Get or create workspace by repo/branch
        w, err = m.workspace.GetOrCreate(ctx, repoURL, branch)
        // ...
    }
    // ...
}
```

**workspace/manager.go** — New method:

```go
// CreateFromWorkspace creates a new workspace with a new branch,
// branching from the source workspace's branch on origin.
func (m *Manager) CreateFromWorkspace(ctx context.Context, sourceWorkspaceID, newBranch string) (*state.Workspace, error) {
    // 1. Get source workspace
    source, found := m.state.GetWorkspace(sourceWorkspaceID)
    if !found {
        return nil, fmt.Errorf("source workspace not found: %s", sourceWorkspaceID)
    }

    // 2. Get source workspace's current branch
    currentBranch, err := m.gitCurrentBranch(ctx, source.Path)
    if err != nil {
        return nil, fmt.Errorf("failed to get current branch: %w", err)
    }
    if currentBranch == "HEAD" {
        return nil, fmt.Errorf("source workspace is on detached HEAD - please checkout a branch first")
    }

    // 3. Create new branch from origin/<source-branch>
    // (requires source branch to be pushed to origin first)
    sourceRef := "origin/" + currentBranch
    return m.createBranchFromRef(ctx, worktreeBasePath, newBranch, sourceRef)
}
```

### Frontend Flow (Workspace Mode with Checkbox Checked)

1. User navigates to `/spawn?workspace_id=myrepo-001`
2. Form pre-fills repo/branch from workspace
3. User checks "Create new branch?"
4. Branch input is revealed (or auto-triggered)
5. User types prompt, clicks Engage
6. Frontend calls `generateBranchName(prompt)` → gets `feature-b`
7. Frontend calls `spawnSessions({ workspace_id: 'myrepo-001', new_branch: 'feature-b', ... })`
8. Backend creates `myrepo-002` with branch `feature-b`, from tip of `myrepo-001`'s current branch

### State Persistence

The `createBranch` checkbox state should:

- Be stored in the **spawn draft** (session storage, keyed to workspace_id)
- Default to `false` (unchecked)
- Survive page refresh within same tab
- Be cleared on successful spawn (like other draft fields)

## Prerequisite: Source Branch Must Be on Origin

The "Create new branch" checkbox is **disabled** if the source workspace's branch is not synced with origin. This is indicated by `commits_synced_with_remote` being `false`.

**Why this matters**: To create a new branch from the source branch, that branch must exist on origin. The worktree creation commands reference `origin/<branch>`, not a local commit hash.

The `commits_synced_with_remote` field is `true` when:

- `origin/<branch>` exists, AND
- Local HEAD points to the same commit as `origin/<branch>`

This ensures the branch has been pushed before we try to branch from it.

**Frontend behavior**:

- When `commits_synced_with_remote` is `false`: checkbox is disabled with tooltip "Branch must be pushed to origin first"
- When `commits_synced_with_remote` is `true`: checkbox is enabled

## Git Implementation Details

When creating a workspace from a source workspace:

```bash
# Instead of (current behavior - branches from default):
git worktree add /path/to/workspace-002 -b feature-b origin/main

# Do (branch from source workspace's branch):
git worktree add /path/to/workspace-002 -b feature-b origin/feature-a
```

This requires `feature-a` to exist on origin. The new branch `feature-b` will point to the same commit as `origin/feature-a`.

This allows chaining branches: if workspace-001 is on `feature-a` (off `main`), the new workspace's `feature-b` branches from `feature-a`'s tip, not from `main`.

## What This Does NOT Include

- Tracking branch parentage/history (no "branched from" metadata stored)
- Merging source branch into default before branching
- Visual branch graph/diff visualization in the UI
- Changes to "fresh" spawn mode (branch suggestion there is unchanged)

## Open Questions

1. Should `create_branch` checkbox persist per-source-workspace, or globally default to false?
   → **Decision**: Default to `false`, stored in draft per-workspace

2. What if source workspace has uncommitted changes?
   → **Decision**: Workspace prepare step already resets/cleans, so this is handled

3. What if source workspace is on detached HEAD?
   → **Decision**: Return error, ask user to commit or checkout a branch first
