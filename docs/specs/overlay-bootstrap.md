# Overlay Bootstrap & Path Registry Design

## Problem

New schmux users have no guided way to configure overlay files. The current process requires:

1. Knowing that `~/.schmux/overlays/<repo-name>/` exists
2. Manually copying files into that directory
3. Ensuring each file is in `.gitignore`

There's no dashboard UI, no CLI wizard, and no auto-detection. Additionally, agent-generated files (e.g., `.claude/settings.local.json`) that don't exist at workspace creation time are never picked up by the compounding loop, even though users want them synced.

## Solution

Three changes:

1. **Path registry** — a declared list of overlay-managed paths (hardcoded defaults + user config), replacing pure filesystem discovery
2. **Dashboard overlay page** — a UI for viewing, scanning, and adding overlay files per repo
3. **First-spawn nudge** — an informational banner telling users what's being auto-managed and linking to the overlay page

## Path Registry

### Declared Paths

The overlay system gains a concept of **declared paths** — file paths that should be overlay-managed. The effective set for a repo is the union of three sources:

```
effective_paths = hardcoded_defaults ∪ global_config_paths ∪ repo_config_paths
```

#### Hardcoded defaults

Baked into the binary. Always active. Cannot be disabled (YAGNI — per-repo opt-out may come later).

```go
var DefaultOverlayPaths = []string{
    ".claude/settings.json",
    ".claude/settings.local.json",
}
```

#### Global config paths

User-configured in `~/.schmux/config.json`. Applies to all repos.

```json
{
  "overlay": {
    "paths": [".tool-versions", ".nvmrc"]
  }
}
```

#### Per-repo paths

Configured on individual repo entries. For repo-specific secrets and config.

```json
{
  "repos": [
    {
      "name": "myapp",
      "url": "git@github.com:org/myapp.git",
      "overlay_paths": [".env", "config/local.yaml"]
    }
  ]
}
```

### Resolution

A new function `GetOverlayPaths(repoName)` returns the deduplicated union of all three sources. This is the single source of truth for what the compounding loop watches and what the dashboard displays.

### Gitignore Constraint

Only gitignored files can be overlayed. Tracked files are already shared via git — overlaying them would cause conflicts. This is enforced:

- During dashboard scan: only gitignored files appear as candidates
- During custom path input: the path is validated against `.gitignore` before accepting
- At copy time: `CopyOverlay` continues to skip non-gitignored files (existing safety check)

No automatic `.gitignore` editing. If a file isn't gitignored, the user must add it themselves through normal git workflow.

## Dashboard UX

### Overlay Page

**Route:** `/overlays/:repoName`

Accessible from workspace header and config page.

#### Empty state (no repo-specific overlays)

```
┌─────────────────────────────────────────────────┐
│ Overlay Files — myapp                           │
│                                                 │
│ Overlay files are shared across all workspaces   │
│ for this repo. Agent configs, secrets, and       │
│ dotfiles are automatically copied to new         │
│ workspaces and kept in sync.                     │
│                                                 │
│ ── Auto-managed ──────────────────────────────  │
│ ✓ .claude/settings.json          Built-in       │
│ ✓ .claude/settings.local.json    Built-in       │
│                                                 │
│ ── Repo-specific ─────────────────────────────  │
│ No repo-specific overlay files configured.       │
│                                                 │
│                    [+ Add files]                 │
└─────────────────────────────────────────────────┘
```

#### Setup flow (user clicks "Add files")

1. **Pick source workspace** — dropdown of workspaces for this repo
2. **Scan results** — schmux scans the workspace and shows gitignored files in two groups:
   - **Detected** — files at well-known paths (`.env`, `.npmrc`, etc.). Pre-checked.
   - **Other gitignored** — any other gitignored files found. Unchecked but selectable.
3. **Custom path input** — text field to type a path for agent-generated files that don't exist yet. Validated against `.gitignore`.
4. **Confirm** — selected files are copied from the source workspace to `~/.schmux/overlays/<repo>/`. Paths are added to the repo's `overlay_paths` config. Custom (non-existent) paths are added to config only (no file to copy yet).

#### Populated state

```
┌─────────────────────────────────────────────────┐
│ Overlay Files — myapp                           │
│                                                 │
│ ── Auto-managed ──────────────────────────────  │
│ ✓ .claude/settings.json          Built-in       │
│ ✓ .claude/settings.local.json    Built-in       │
│                                                 │
│ ── Repo-specific ─────────────────────────────  │
│   .env                    Synced     [Remove]   │
│   config/local.yaml       Synced     [Remove]   │
│   .credentials.json       Pending    [Remove]   │
│                                                 │
│                    [+ Add files]                 │
└─────────────────────────────────────────────────┘
```

Status values:

- **Synced** — file exists in overlay dir and has been copied to workspaces
- **Pending** — path is declared but file doesn't exist in overlay dir yet (waiting for agent to create it)

### First-Spawn Nudge

When a user spawns a session for a repo, a banner appears on the workspace card:

> "Overlay is active for this repo. Agent config files (`.claude/settings.json`, `.claude/settings.local.json`) are automatically synced across workspaces. [Manage overlays →]"

The banner links to `/overlays/:repoName`. It appears once and is dismissible. Dismissal is tracked on the repo config entry:

```json
{
  "name": "myapp",
  "overlay_nudge_dismissed": true
}
```

The nudge does not reappear once dismissed or once the user has visited the overlay page.

## Backend Changes

### Config Schema

```go
// Top-level overlay config (new)
type OverlayConfig struct {
    Paths []string `json:"paths,omitempty"` // additional global paths
}

// On existing Repo struct (new fields)
type Repo struct {
    // ... existing fields ...
    OverlayPaths         []string `json:"overlay_paths,omitempty"`
    OverlayNudgeDismissed bool    `json:"overlay_nudge_dismissed,omitempty"`
}
```

### New API Endpoints

#### `GET /api/overlays/:repoName`

Returns the effective overlay configuration for a repo.

```json
{
  "paths": [
    {
      "path": ".claude/settings.json",
      "source": "builtin",
      "status": "synced"
    },
    {
      "path": ".env",
      "source": "repo",
      "status": "synced"
    },
    {
      "path": ".credentials.json",
      "source": "repo",
      "status": "pending"
    }
  ]
}
```

- `source`: `"builtin"` | `"global"` | `"repo"`
- `status`: `"synced"` (file exists in overlay dir) | `"pending"` (path declared, no file yet)

#### `POST /api/overlays/:repoName/scan`

Scans a source workspace for gitignored files. Returns candidates.

Request:

```json
{
  "workspace_id": "myapp-001"
}
```

Response:

```json
{
  "candidates": [
    { "path": ".env", "size": 256, "detected": true },
    { "path": "config/local.yaml", "size": 1024, "detected": true },
    { "path": ".vscode/settings.json", "size": 512, "detected": false }
  ]
}
```

- `detected`: true if the path matches a well-known pattern

#### `POST /api/overlays/:repoName/add`

Copies selected files from a source workspace to the overlay dir and updates config.

Request:

```json
{
  "workspace_id": "myapp-001",
  "paths": [".env", "config/local.yaml"],
  "custom_paths": [".credentials.json"]
}
```

- `paths`: files to copy from the source workspace to overlay dir
- `custom_paths`: paths to register without copying (agent-generated, don't exist yet)

#### `DELETE /api/overlays/:repoName/paths/:path`

Removes a path from the repo's overlay config. Does not delete the file from the overlay dir.

### Watcher Changes: Monitoring Declared Paths

The current watcher only monitors files present in the overlay manifest (files copied at spawn time). This must change to support agent-generated files.

#### Current behavior

```
AddWorkspace(wsID, path, manifest)
  → for each file in manifest, watch its parent directory
  → on change event, check if file is in manifest
```

#### New behavior

```
AddWorkspace(wsID, path, manifest, declaredPaths)
  → for each declared path, watch its parent directory
  → on change event, check if file matches a declared path
  → if file is new (not in manifest), treat as fast-path merge
```

Key changes:

1. **Watch declared paths, not just manifest entries.** The watcher receives the full set of declared paths (from `GetOverlayPaths`). It watches parent directories for all of them.

2. **Handle missing parent directories.** For paths like `.claude/settings.local.json`, the `.claude/` directory might not exist at workspace creation. The watcher should:
   - Watch the workspace root for directory creation events
   - When a directory matching a declared path prefix is created, add a watch on it
   - This handles the case where an agent runs `mkdir -p .claude && echo '{}' > .claude/settings.local.json`

3. **New files enter the manifest.** When a file appears at a declared path for the first time, the compounder:
   - Hashes it and adds it to the in-memory manifest
   - Persists the hash via `ManifestUpdateFunc`
   - Copies it to the overlay dir (fast path — no overlay version exists)
   - Propagates to sibling workspaces

#### Data flow for agent-generated files

```
Agent creates .claude/settings.local.json in workspace A
    │
    ▼
[fsnotify] Create event on .claude/ directory
    │
    ▼
[Watcher] matches .claude/settings.local.json against declared paths → hit
    │
    ▼
[Compounder] no manifest entry, no overlay file → fast path
    │
    ▼
File copied to ~/.schmux/overlays/<repo>/.claude/settings.local.json
    │
    ▼
[Propagator] copies to workspaces B, C, D
    │
    ▼
Manifest updated for all workspaces
```

## Implementation Steps

1. **Config schema** — add `OverlayConfig`, `OverlayPaths`, `OverlayNudgeDismissed` to config structs. Add `DefaultOverlayPaths` constant. Implement `GetOverlayPaths(repoName)`.

2. **Watcher refactor** — change `AddWorkspace` to accept declared paths in addition to manifest. Watch parent directories for all declared paths. Handle missing parent directories with workspace-root watching.

3. **Compounder integration** — pass declared paths through from daemon to compounder. Handle new file detection (file at declared path with no manifest entry).

4. **API endpoints** — implement `GET /api/overlays/:repoName`, `POST /api/overlays/:repoName/scan`, `POST /api/overlays/:repoName/add`, `DELETE /api/overlays/:repoName/paths/:path`.

5. **Dashboard overlay page** — new route `/overlays/:repoName` with empty state, setup flow (source workspace picker, scan results, custom path input), and populated state.

6. **First-spawn nudge** — banner component on workspace card, dismissal tracking.

7. **Tests** — unit tests for path registry resolution, watcher declared-path monitoring, API endpoints. Integration test for the full flow: declare path → agent creates file → file synced to overlay → propagated to sibling.
