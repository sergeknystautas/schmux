# Workspace Dependencies Spec

**Goal**
Allow workspaces to declare dependencies on other workspaces. This is a bidirectional relationship used for visual indicators in the web dashboard — when workspace A depends on workspace B, and B exists, A is considered "blocked" by that dependency.

**Key Behaviors**

- Dependencies are stored in `state.json` (transient runtime state)
- If workspace A depends on B, and B exists → A is blocked
- If workspace A depends on B, and B is disposed → A is unblocked (link just breaks)
- Transitive: if A depends on B, and B depends on C → A is blocked by both B and C
- Cleanup on dispose: when workspace X is disposed, remove X from all dependency arrays

---

## Data Model

### state.json

```json
{
  "workspaces": {
    "schmux-001": {
      "id": "schmux-001",
      "repo": "git@github.com:user/repo.git",
      "branch": "feature-001",
      "path": "/Users/.../schmux-001",
      "dependencies": ["schmux-002", "schmux-003"]
    },
    "schmux-002": {
      "id": "schmux-002",
      "repo": "git@github.com:user/repo.git",
      "branch": "feature-002",
      "path": "/Users/.../schmux-002",
      "dependencies": ["schmux-003"]
    },
    "schmux-003": {
      "id": "schmux-003",
      "repo": "git@github.com:user/repo.git",
      "branch": "feature-003",
      "path": "/Users/.../schmux-003",
      "dependencies": []
    }
  },
  "sessions": [...]
}
```

### Go Types

```go
// internal/state/state.go
type Workspace struct {
    ID           string   `json:"id"`
    Repo         string   `json:"repo"`
    Branch       string   `json:"branch"`
    Path         string   `json:"path"`
    Dependencies []string `json:"dependencies"` // NEW
    // ... existing fields
}
```

---

## Backend Changes

### State Interface (`internal/state/interfaces.go`)

Add methods:

```go
// SetDependency adds a dependency relationship
SetDependency(workspaceID, dependsOn string) error

// RemoveDependency removes a dependency relationship
RemoveDependency(workspaceID, dependsOn string) error

// GetTransitiveDependencies returns all transitive dependencies for a workspace
// Uses BFS with visited set to handle cycles
GetTransitiveDependencies(workspaceID string) []string
```

### CLI Commands (`internal/cli/workspace.go`)

```bash
# Add a dependency
./schmux workspace depend <workspace> <depends-on>

# Remove a dependency
./schmux workspace undepend <workspace> <no-longer-depends-on>

# Show dependencies
./schmux workspace status --show-deps <workspace>
```

Output format:

```
schmux-001
  Direct dependencies:      schmux-002, schmux-003
  Transitive dependencies:  schmux-002, schmux-003
  Blocked by:               schmux-002, schmux-003
```

### Dashboard API (`internal/dashboard/handlers.go`)

**GET /api/sessions** response - extend `WorkspaceResponse`:

```go
type WorkspaceResponse struct {
    ID                   string   `json:"id"`
    Repo                 string   `json:"repo"`
    Branch               string   `json:"branch"`
    // ... existing fields

    // NEW
    Dependencies           []string `json:"dependencies"`           // direct
    TransitiveDependencies []string `json:"transitiveDependencies"` // computed
    BlockedBy             []string `json:"blockedBy"`               // deps that exist
}
```

**New endpoints**:

```
POST /api/workspaces/{id}/dependencies
  Body: {"dependency": "workspace-id"}

DELETE /api/workspaces/{id}/dependencies/{depId}
```

### Cleanup on Dispose

When `RemoveWorkspace(id)` is called, also remove `id` from all other workspaces' `dependencies` arrays.

---

## Frontend Changes

### TypeScript Types (`contracts.ts` - generated from Go)

```typescript
interface Workspace {
  id: string;
  repo: string;
  branch: string;
  path: string;
  sessionCount: number;
  sessions: Session[];
  gitAhead: number;
  gitBehind: number;
  gitLinesAdded: number;
  gitLinesRemoved: number;
  gitFilesChanged: number;
  dependencies: string[]; // NEW
  transitiveDependencies: string[]; // NEW
  blockedBy: string[]; // NEW
}
```

### Sidebar (`Sidebar.tsx`)

- **Muted visual**: Workspaces with non-empty `blockedBy` appear muted (reduced opacity, desaturated)
- **Ordering**: Best-effort topological sort — blocked workspaces appear below their dependencies
  - Use Kahn's algorithm or similar
  - When cycles exist, break ties and group related workspaces
  - Fall back to workspace numbering for unrelated workspaces

### WorkspaceHeader (`WorkspaceHeader.tsx`)

**Status indicator** (right side, next to VS Code / dispose icons):

- **Green dot**: Unblocked (`blockedBy.length === 0`)
- **Yellow `↓ N` badge**: Blocked by N workspaces
- **Tooltip**: Lists the blocking dependencies by name

**Click handler**: Opens dependencies modal

### Dependencies Modal (`DependenciesModal.tsx`)

- Shows current direct dependencies
- Add: typeahead dropdown of other workspaces (filter out self and existing deps)
- Remove: click × on each dependency chip
- Light validation: warn about circular deps but allow
- Buttons: Cancel, Save

---

## Transitive Dependency Algorithm

BFS with visited set to handle cycles:

```go
func (s *State) GetTransitiveDependencies(workspaceID string) []string {
    result := []string{}
    visited := make(map[string]bool)
    queue := []string{}

    // Start with direct dependencies
    if ws, ok := s.GetWorkspace(workspaceID); ok {
        for _, dep := range ws.Dependencies {
            if !visited[dep] {
                visited[dep] = true
                queue = append(queue, dep)
                result = append(result, dep)
            }
        }
    }

    // BFS for transitive deps
    for i := 0; i < len(queue); i++ {
        current := queue[i]
        if ws, ok := s.GetWorkspace(current); ok {
            for _, dep := range ws.Dependencies {
                if !visited[dep] {
                    visited[dep] = true
                    queue = append(queue, dep)
                    result = append(result, dep)
                }
            }
        }
    }

    return result
}
```

---

## API Contract Changes

### GET /api/sessions Response

```json
{
  "workspaces": [
    {
      "id": "schmux-001",
      "repo": "git@github.com:user/repo.git",
      "branch": "feature-001",
      "path": "/Users/.../schmux-001",
      "branch_url": "https://github.com/user/repo/tree/feature-001",
      "session_count": 2,
      "sessions": [...],
      "git_ahead": 3,
      "git_behind": 0,
      "git_lines_added": 42,
      "git_lines_removed": 15,
      "git_files_changed": 3,
      "dependencies": ["schmux-002", "schmux-003"],
      "transitiveDependencies": ["schmux-002", "schmux-003"],
      "blockedBy": ["schmux-002", "schmux-003"]
    }
  ]
}
```

### POST /api/workspaces/{id}/dependencies

**Request**:

```json
{
  "dependency": "schmux-002"
}
```

**Response**:

```json
{
  "status": "ok"
}
```

### DELETE /api/workspaces/{id}/dependencies/{depId}

**Response**:

```json
{
  "status": "ok"
}
```

---

## Implementation Checklist

- [ ] Add `Dependencies []string` to `state.Workspace` struct
- [ ] Implement state methods: `SetDependency`, `RemoveDependency`, `GetTransitiveDependencies`
- [ ] Update `RemoveWorkspace` to cleanup dependency references
- [ ] Add CLI commands: `depend`, `undepend`, `--show-deps`
- [ ] Extend `GET /api/sessions` response with dependency fields
- [ ] Add `POST /api/workspaces/{id}/dependencies` handler
- [ ] Add `DELETE /api/workspaces/{id}/dependencies/{depId}` handler
- [ ] Run `go run ./cmd/gen-types` to regenerate TypeScript types
- [ ] Sidebar: add muted visual for blocked workspaces
- [ ] Sidebar: implement best-effort topological sort ordering
- [ ] WorkspaceHeader: add green/yellow status indicator with tooltip
- [ ] WorkspaceHeader: click handler to open modal
- [ ] DependenciesModal: add/remove dependencies UI
- [ ] Write tests for state methods
- [ ] Write E2E test for dependency CRUD operations
- [ ] Update `docs/api.md` with new endpoints
