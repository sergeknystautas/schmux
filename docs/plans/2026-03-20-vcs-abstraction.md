# VCS Backend Abstraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Abstract VCS operations behind a `VCSBackend` interface so Schmux can manage both git worktree and sapling workspaces from the same Manager.

**Architecture:** Strategy pattern — Manager gets a `backends map[string]VCSBackend` and delegates VCS operations per-repo based on `config.Repo.VCS`. GitBackend is extracted from existing code; SaplingBackend uses `sl` for observability and configurable command templates for lifecycle operations.

**Tech Stack:** Go, `text/template` for command templates, `sl` CLI (open source sapling), React/TypeScript for config editor.

**Design doc:** `docs/specs/vcs-abstraction-design-2026-03-20.md`

---

## Task Dependencies

Tasks in the same parallel group can be worked on concurrently.
Tasks with dependencies must wait for their prerequisites.

| Task                                          | Parallel Group | Depends On       | Files Touched                                                                                      |
| --------------------------------------------- | -------------- | ---------------- | -------------------------------------------------------------------------------------------------- |
| 1: VCSBackend interface + types               | A              | —                | `internal/workspace/vcs.go`                                                                        |
| 2: Generalize runGit → runCmd                 | A              | —                | `internal/workspace/run_git.go` → `run_cmd.go`                                                     |
| 3: State model: WorktreeBase → RepoBase       | A              | —                | `internal/state/state.go`, `internal/state/interfaces.go`, `internal/state/state_test.go`          |
| 4: Extract GitBackend (lifecycle)             | B              | Tasks 1, 2, 3    | `internal/workspace/vcs_git.go`, `internal/workspace/worktree.go`, `internal/workspace/manager.go` |
| 5: Extract GitBackend (observability)         | C              | Task 4           | `internal/workspace/vcs_git.go`, `internal/workspace/git.go`, `internal/workspace/manager.go`      |
| 6: Extract GitBackend (query repos)           | C              | Task 4           | `internal/workspace/vcs_git.go`, `internal/workspace/origin_queries.go`                            |
| 7: Manager backend wiring                     | D              | Tasks 4, 5, 6    | `internal/workspace/manager.go`, `internal/workspace/interfaces.go`                                |
| 8: Config: add VCS to Repo + SaplingCommands  | D              | Task 3           | `internal/config/config.go`, `internal/config/config_test.go`                                      |
| 9: State: add VCS to Workspace                | D              | Task 3           | `internal/state/state.go`                                                                          |
| 10: Rename poll round + watcher files         | D              | Tasks 5, 7       | `internal/workspace/git_poll_round.go` → `vcs_poll_round.go`                                       |
| 11: Rename WorkspaceManager interface methods | E              | Task 7           | `internal/workspace/interfaces.go`, `internal/dashboard/handlers.go`, all callers                  |
| 12: SaplingBackend: command template engine   | F              | Task 1           | `internal/workspace/vcs_sapling.go`                                                                |
| 13: SaplingBackend: lifecycle methods         | G              | Tasks 12, 8      | `internal/workspace/vcs_sapling.go`                                                                |
| 14: SaplingBackend: observability methods     | G              | Task 12          | `internal/workspace/vcs_sapling.go`                                                                |
| 15: SaplingBackend: query repo methods        | G              | Task 12          | `internal/workspace/vcs_sapling.go`                                                                |
| 16: SaplingBackend tests                      | H              | Tasks 13, 14, 15 | `internal/workspace/vcs_sapling_test.go`                                                           |
| 17: Web config: VCS dropdown on repo form     | H              | Task 8           | `assets/dashboard/src/` (config components)                                                        |
| 18: Web config: SaplingCommands editor        | H              | Task 8           | `assets/dashboard/src/` (config components)                                                        |
| 19: Integration test                          | I              | All above        | Manual testing on sapling machine                                                                  |

**Parallel execution:** Tasks 1-3 (Group A) run first in parallel. Task 4 (Group B) runs next. Tasks 5-6 (Group C) run in parallel. Tasks 7-10 (Group D) run in parallel. Tasks 12-15 and 16-18 form later waves.

---

### Task 1: VCSBackend interface and types

**Parallel group:** A

**Files:**

- Create: `internal/workspace/vcs.go`

**Step 1: Write the interface file**

```go
package workspace

import "context"

type VCSBackend interface {
	EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error)
	CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error
	RemoveWorkspace(ctx context.Context, workspacePath string) error
	PruneStale(ctx context.Context, repoBasePath string) error
	Fetch(ctx context.Context, path string) error
	IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error)
	GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error)
	GetChangedFiles(ctx context.Context, workspacePath string) ([]VCSChangedFile, error)
	GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error)
	GetCurrentBranch(ctx context.Context, workspacePath string) (string, error)
	EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error
	FetchQueryRepo(ctx context.Context, path string) error
	ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error)
	GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error)
}

type VCSStatus struct {
	Dirty            bool
	CurrentBranch    string
	AheadOfDefault   int
	BehindDefault    int
	LinesAdded       int
	LinesRemoved     int
	FilesChanged     int
	SyncedWithRemote bool
	RemoteBranchExists bool
	LocalUniqueCommits  int
	RemoteUniqueCommits int
	DefaultBranchOrphaned bool
}

type VCSChangedFile struct {
	Path         string
	Status       string
	LinesAdded   int
	LinesRemoved int
}
```

Note: `VCSChangedFile` is separate from the existing `GitChangedFile` in `git.go:30`. Once GitBackend is extracted, `GitChangedFile` can be removed and callers switched to `VCSChangedFile`. The fields are identical.

**Step 2: Verify it compiles**

```bash
go build ./internal/workspace/
```

Expected: PASS (no references to the new types yet)

**Step 3: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): define VCSBackend interface and status types

Introduces the abstraction boundary for multi-VCS support.
No behavioral changes — types are not yet referenced.
EOF
)"
```

---

### Task 2: Generalize runGit → runCmd

**Parallel group:** A

**Files:**

- Modify: `internal/workspace/run_git.go` (rename to `run_cmd.go`)

**Step 1: Read the existing file**

Read `internal/workspace/run_git.go` — it's 72 lines. The function `runGit` hardcodes `"git"` as the binary name at line 20.

**Step 2: Add a general-purpose `runCmd` alongside `runGit`**

Add a new method `runCmd` that accepts a `binary` parameter. Keep `runGit` as a thin wrapper calling `runCmd("git", ...)` so nothing breaks.

```go
func (m *Manager) runCmd(ctx context.Context, binary string, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir

	releaseWatcherSuppression := func() {}
	if m != nil && m.gitWatcher != nil {
		releaseWatcherSuppression = m.gitWatcher.BeginInternalGitSuppressionForDir(dir)
	}
	defer releaseWatcherSuppression()

	start := time.Now()
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	duration := time.Since(start)

	exitCode := 0
	stderrBytes := int64(0)
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			exitErr.Stderr = append([]byte(nil), stderrBuf.Bytes()...)
			stderrBytes = int64(len(exitErr.Stderr))
		}
	}

	stdout := stdoutBuf.Bytes()
	stdoutBytes := int64(len(stdout))

	if m.ioTelemetry != nil {
		m.ioTelemetry.RecordCommand(binary, args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
	} else if m.config != nil && m.config.GetIOWorkspaceTelemetryEnabled() {
		ioTelemetryMu.Lock()
		if m.ioTelemetry == nil {
			m.ioTelemetry = NewIOWorkspaceTelemetry()
		}
		ioTelemetryMu.Unlock()
		if m.ioTelemetry != nil {
			m.ioTelemetry.RecordCommand(binary, args, workspaceID, dir, trigger, duration, exitCode, stdoutBytes, stderrBytes)
		}
	}

	return stdout, err
}

func (m *Manager) runGit(ctx context.Context, workspaceID string, trigger RefreshTrigger, dir string, args ...string) ([]byte, error) {
	return m.runCmd(ctx, "git", workspaceID, trigger, dir, args...)
}
```

**Step 3: Rename the file**

```bash
git mv internal/workspace/run_git.go internal/workspace/run_cmd.go
git mv internal/workspace/run_git_test.go internal/workspace/run_cmd_test.go
```

**Step 4: Run tests**

```bash
./test.sh --quick
```

Expected: All tests pass — `runGit` still exists as a wrapper, nothing changes.

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): generalize runGit to runCmd with binary parameter

runGit is now a thin wrapper around runCmd("git", ...).
Enables backends to use runCmd("sl", ...) for sapling.
EOF
)"
```

---

### Task 3: State model — WorktreeBase → RepoBase

**Parallel group:** A

**Files:**

- Modify: `internal/state/state.go:109-113` (rename struct)
- Modify: `internal/state/state.go:22` (rename field on State)
- Modify: `internal/state/state.go:650-690` (rename methods)
- Modify: `internal/state/interfaces.go:45-47` (rename interface methods)
- Modify: `internal/state/state_test.go:1337-1409` (rename test references)
- Modify: all callers in `internal/workspace/` that reference `WorktreeBase`

**Step 1: Rename the struct and add VCS field**

In `internal/state/state.go`, rename `WorktreeBase` → `RepoBase`:

```go
type RepoBase struct {
	RepoURL string `json:"repo_url"`
	Path    string `json:"path"`
	VCS     string `json:"vcs,omitempty"`
}
```

**Step 2: Rename the State field**

At line 22, change `WorktreeBases []WorktreeBase` to `RepoBases []RepoBase`. Keep the JSON tag as `"base_repos"` (it's already `"base_repos"`, not `"worktree_bases"`, so no migration needed for the JSON key).

**Step 3: Rename methods**

Rename all methods: `GetWorktreeBases` → `GetRepoBases`, `AddWorktreeBase` → `AddRepoBase`, `GetWorktreeBaseByURL` → `GetRepoBaseByURL`. Update `internal/state/interfaces.go:45-47` to match.

**Step 4: Update callers**

Search for all references to `WorktreeBase`, `GetWorktreeBase`, `AddWorktreeBase` in `internal/workspace/` and update them. Key files:

- `internal/workspace/worktree.go:86` (`state.WorktreeBase` → `state.RepoBase`, `AddWorktreeBase` → `AddRepoBase`)
- `internal/workspace/worktree.go:49` (`GetWorktreeBaseByURL` → `GetRepoBaseByURL`)
- `internal/workspace/worktree.go:371,381` (same)
- `internal/workspace/manager.go` (search for `WorktreeBase` references)

**Step 5: Update tests**

In `internal/state/state_test.go:1337-1409`, rename all `WorktreeBase` references to `RepoBase` and method names accordingly.

**Step 6: Run tests**

```bash
./test.sh --quick
```

Expected: All tests pass — purely mechanical rename.

**Step 7: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(state): rename WorktreeBase to RepoBase

VCS-neutral naming. Adds VCS field for future backend selection.
JSON key was already "base_repos" so no state file migration needed.
EOF
)"
```

---

### Task 4: Extract GitBackend — lifecycle methods

**Parallel group:** B

**Files:**

- Create: `internal/workspace/vcs_git.go`
- Modify: `internal/workspace/worktree.go` (move methods out)
- Modify: `internal/workspace/manager.go:435-549` (delegate to backend)

**Step 1: Create GitBackend struct**

Create `internal/workspace/vcs_git.go` with:

```go
package workspace

import (
	"context"
	"log/slog"
)

type GitBackend struct {
	manager *Manager
	logger  *slog.Logger
}

func NewGitBackend(m *Manager) *GitBackend {
	return &GitBackend{manager: m, logger: m.logger}
}
```

Note: The GitBackend holds a reference to Manager so it can call `runGit`. This is a pragmatic choice — extracting `runCmd` into a standalone function injectable into the backend would require a larger refactor. The circular reference is acceptable because GitBackend is always owned by Manager.

**Step 2: Move lifecycle methods from worktree.go into GitBackend**

Move these methods from `Manager` receiver to `GitBackend` receiver:

- `cloneBareRepo` (worktree.go:17) → `GitBackend.cloneBareRepo`
- `ensureWorktreeBase` (worktree.go:47) → `GitBackend.EnsureRepoBase` (satisfies interface)
- `addWorktree` (worktree.go:97) → `GitBackend.CreateWorkspace` (satisfies interface)
- `removeWorktree` (worktree.go:260) → `GitBackend.RemoveWorkspace`
- `pruneWorktrees` (worktree.go:273) → `GitBackend.PruneStale`
- `ensureUniqueBranch` (worktree.go:151) → `GitBackend.ensureUniqueBranch`
- `isBranchInWorktree` (worktree.go:223) → `GitBackend.IsBranchInUse`

Each moved method calls `g.manager.runGit(...)` instead of `m.runGit(...)`.

**Step 3: Update manager.go `create()` to call GitBackend**

In `manager.go:435`, change direct calls to use the backend. For now, hardcode `gitBackend`:

```go
// In create():
backend := g.gitBackend  // temporary — Task 7 adds backendFor()
basePath, err := backend.EnsureRepoBase(ctx, repoURL, "")
// ...
err = backend.CreateWorkspace(ctx, basePath, branch, workspacePath)
```

**Step 4: Update manager.go `dispose()` similarly**

In `manager.go:936`, replace `m.removeWorktree` / `m.pruneWorktrees` calls with backend calls.

**Step 5: Delete moved methods from worktree.go**

Remove the methods that were moved. Keep `initLocalRepo`, `cloneRepo`, `cleanupLocalBranch`, and `findWorktreeBaseForWorkspace` on Manager (they're git-specific helpers still needed for legacy paths and are not part of VCSBackend).

**Step 6: Run tests**

```bash
./test.sh --quick
```

Expected: All tests pass — same behavior, different method receivers.

**Step 7: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): extract GitBackend lifecycle methods

Moves worktree create/remove/prune/ensure operations from Manager
to GitBackend struct, satisfying VCSBackend interface for lifecycle tier.
EOF
)"
```

---

### Task 5: Extract GitBackend — observability methods

**Parallel group:** C

**Files:**

- Modify: `internal/workspace/vcs_git.go` (add methods)
- Modify: `internal/workspace/git.go:376-450` (extract status logic)
- Modify: `internal/workspace/manager.go:770-855` (delegate)

**Step 1: Implement `GitBackend.GetStatus`**

Extract the core logic from `Manager.gitStatusWithRound` (git.go:380) into `GitBackend.GetStatus`. This method runs `git status --porcelain`, `git rev-list --left-right --count`, `git diff --stat`, and composes a `VCSStatus`. The Manager's `updateGitStatusWithTriggerAndRound` becomes a thin wrapper that calls `backend.GetStatus()` and maps the result onto `state.Workspace` fields.

Key: the fetch-before-status and default-branch-update logic stays in Manager (or the poll round). `GetStatus` only reads status, it doesn't fetch.

**Step 2: Implement `GitBackend.GetChangedFiles`**

Extract from the existing `GetWorkspaceGitFiles` method. Returns `[]VCSChangedFile`.

**Step 3: Implement `GitBackend.GetDefaultBranch`**

Extract from existing `Manager.GetDefaultBranch`. For git, this runs `git symbolic-ref refs/remotes/origin/HEAD`.

**Step 4: Implement `GitBackend.GetCurrentBranch`**

Extract from existing `gitCurrentBranch`. Runs `git rev-parse --abbrev-ref HEAD`.

**Step 5: Implement `GitBackend.Fetch`**

Extract from `gitFetchInstrumentedWithRound`. For git worktrees, resolves to the bare clone and fetches there. The poll-round deduplication stays in Manager.

**Step 6: Update Manager to delegate**

`updateGitStatusWithTriggerAndRound` calls `backend.GetStatus()` and maps `VCSStatus` → workspace fields. `GetWorkspaceGitFiles` calls `backend.GetChangedFiles()`.

**Step 7: Run tests**

```bash
./test.sh --quick
```

Expected: All tests pass.

**Step 8: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): extract GitBackend observability methods

GetStatus, GetChangedFiles, GetDefaultBranch, GetCurrentBranch,
and Fetch now live on GitBackend. Manager delegates to them.
EOF
)"
```

---

### Task 6: Extract GitBackend — query repo methods

**Parallel group:** C

**Files:**

- Modify: `internal/workspace/vcs_git.go` (add methods)
- Modify: `internal/workspace/origin_queries.go` (extract logic)

**Step 1: Move query repo methods to GitBackend**

Move these from Manager to GitBackend:

- `ensureOriginQueryRepo` → `GitBackend.EnsureQueryRepo`
- `fetchOriginQueryRepo` → `GitBackend.FetchQueryRepo`
- `getRecentBranches` → `GitBackend.ListRecentBranches`
- `getBranchCommitLog` → `GitBackend.GetBranchLog`

Manager's `EnsureOriginQueries`, `FetchOriginQueries`, `GetRecentBranches`, `GetBranchCommitLog` become wrappers that iterate repos and call the backend.

**Step 2: Run tests**

```bash
./test.sh --quick
```

**Step 3: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): extract GitBackend query repo methods

EnsureQueryRepo, FetchQueryRepo, ListRecentBranches, GetBranchLog
moved to GitBackend. Manager iterates repos and delegates.
EOF
)"
```

---

### Task 7: Manager backend wiring

**Parallel group:** D

**Files:**

- Modify: `internal/workspace/manager.go` (add backends map, backendFor, backendForWorkspace)
- Modify: `internal/workspace/interfaces.go` (compile check)

**Step 1: Add backends map to Manager**

```go
type Manager struct {
	// ... existing fields ...
	backends map[string]VCSBackend
}
```

**Step 2: Initialize in constructor**

In the Manager constructor (find `NewManager` or equivalent), add:

```go
gitBackend := NewGitBackend(m)
m.backends = map[string]VCSBackend{
	"git": gitBackend,
	"":    gitBackend, // default
}
```

**Step 3: Add backend resolution methods**

```go
func (m *Manager) backendFor(repoURL string) VCSBackend {
	repo, found := m.findRepoByURL(repoURL)
	if !found || repo.VCS == "" {
		return m.backends["git"]
	}
	if b, ok := m.backends[repo.VCS]; ok {
		return b
	}
	return m.backends["git"]
}

func (m *Manager) backendForWorkspace(workspaceID string) VCSBackend {
	w, found := m.state.GetWorkspace(workspaceID)
	if !found || w.VCS == "" {
		return m.backends["git"]
	}
	if b, ok := m.backends[w.VCS]; ok {
		return b
	}
	return m.backends["git"]
}
```

**Step 4: Replace hardcoded gitBackend references from Task 4**

Change all the temporary `g.gitBackend` references added in Task 4 to use `m.backendFor(repoURL)` or `m.backendForWorkspace(workspaceID)`.

**Step 5: Verify compile-time interface satisfaction**

```go
var _ VCSBackend = (*GitBackend)(nil)
```

**Step 6: Run tests**

```bash
./test.sh --quick
```

**Step 7: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): wire VCSBackend selection into Manager

Manager resolves backend per-repo via config.Repo.VCS field.
Defaults to git when VCS is empty or unrecognized.
EOF
)"
```

---

### Task 8: Config — add VCS to Repo + SaplingCommands

**Parallel group:** D

**Files:**

- Modify: `internal/config/config.go:425-431` (add VCS field to Repo)
- Modify: `internal/config/config.go` (add SaplingCommands struct + field on Config)
- Modify: `internal/config/config_test.go` (add tests)

**Step 1: Write tests for the new config fields**

```go
func TestRepoVCSField(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"default empty", `{"name":"r","url":"u"}`, ""},
		{"explicit git", `{"name":"r","url":"u","vcs":"git"}`, "git"},
		{"sapling", `{"name":"r","url":"u","vcs":"sapling"}`, "sapling"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var repo Repo
			json.Unmarshal([]byte(tt.json), &repo)
			if repo.VCS != tt.want {
				t.Errorf("VCS = %q, want %q", repo.VCS, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/config/ -run TestRepoVCSField
```

Expected: FAIL (VCS field doesn't exist yet)

**Step 3: Add VCS field to Repo struct**

At `internal/config/config.go:425`:

```go
type Repo struct {
	Name                  string   `json:"name"`
	URL                   string   `json:"url"`
	BarePath              string   `json:"bare_path,omitempty"`
	VCS                   string   `json:"vcs,omitempty"`
	OverlayPaths          []string `json:"overlay_paths,omitempty"`
	OverlayNudgeDismissed bool     `json:"overlay_nudge_dismissed,omitempty"`
}
```

**Step 4: Add SaplingCommands struct**

```go
type SaplingCommands struct {
	CreateWorkspace string `json:"create_workspace,omitempty"`
	RemoveWorkspace string `json:"remove_workspace,omitempty"`
	CheckRepoBase   string `json:"check_repo_base,omitempty"`
	CreateRepoBase  string `json:"create_repo_base,omitempty"`
	ListWorkspaces  string `json:"list_workspaces,omitempty"`
}
```

Add to Config struct:

```go
SaplingCommands SaplingCommands `json:"sapling_commands,omitempty"`
```

**Step 5: Add validation for VCS values**

In the existing `validateRepo` or equivalent, add:

```go
if repo.VCS != "" && repo.VCS != "git" && repo.VCS != "sapling" {
	return fmt.Errorf("invalid vcs value: %q (must be 'git' or 'sapling')", repo.VCS)
}
```

**Step 6: Add SaplingCommands defaults getter**

```go
func (sc SaplingCommands) GetCreateWorkspace() string {
	if sc.CreateWorkspace != "" {
		return sc.CreateWorkspace
	}
	return "sl clone {{.RepoIdentifier}} {{.DestPath}}"
}

func (sc SaplingCommands) GetRemoveWorkspace() string {
	if sc.RemoveWorkspace != "" {
		return sc.RemoveWorkspace
	}
	return "rm -rf {{.WorkspacePath}}"
}

func (sc SaplingCommands) GetCreateRepoBase() string {
	if sc.CreateRepoBase != "" {
		return sc.CreateRepoBase
	}
	return "sl clone {{.RepoIdentifier}} {{.BasePath}}"
}
```

**Step 7: Run tests**

```bash
go test ./internal/config/ -run TestRepoVCSField
./test.sh --quick
```

Expected: PASS

**Step 8: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(config): add VCS field to Repo and SaplingCommands config

Repo.VCS selects 'git' (default) or 'sapling' per repo.
SaplingCommands provides configurable lifecycle command templates
with sensible sl-based defaults.
EOF
)"
```

---

### Task 9: State — add VCS to Workspace

**Parallel group:** D

**Files:**

- Modify: `internal/state/state.go:71-92` (add VCS field)

**Step 1: Add VCS field to Workspace struct**

At `internal/state/state.go:71`:

```go
type Workspace struct {
	ID                       string            `json:"id"`
	Repo                     string            `json:"repo"`
	Branch                   string            `json:"branch"`
	Path                     string            `json:"path"`
	VCS                      string            `json:"vcs,omitempty"`
	// ... rest unchanged ...
}
```

**Step 2: Set VCS in Manager.create()**

In `internal/workspace/manager.go`, in the `create()` function where the workspace state is built (around line 518):

```go
w := state.Workspace{
	ID:     workspaceID,
	Repo:   repoURL,
	Branch: branch,
	Path:   workspacePath,
	VCS:    repoConfig.VCS,  // NEW: persist VCS from config
}
```

If `repoConfig.VCS` is empty, leave it empty — the backend resolution already defaults to git.

**Step 3: Run tests**

```bash
./test.sh --quick
```

Expected: PASS

**Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(state): add VCS field to Workspace

Persisted at creation time so Manager can resolve the backend
from state alone, even if config changes later.
EOF
)"
```

---

### Task 10: Rename poll round + watcher files

**Parallel group:** D

**Files:**

- Rename: `internal/workspace/git_poll_round.go` → `internal/workspace/vcs_poll_round.go`
- Rename: `internal/workspace/git_poll_round_test.go` → `internal/workspace/vcs_poll_round_test.go`

**Step 1: Rename files**

```bash
git mv internal/workspace/git_poll_round.go internal/workspace/vcs_poll_round.go
git mv internal/workspace/git_poll_round_test.go internal/workspace/vcs_poll_round_test.go
```

The internal type names (`pollRound`, `gitFetchPollRound`) can stay for now — they're unexported and renaming them is cosmetic. The important thing is the file name reflects VCS-neutrality.

**Step 2: Run tests**

```bash
./test.sh --quick
```

**Step 3: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): rename git_poll_round → vcs_poll_round

File rename only. Internal type names unchanged.
EOF
)"
```

---

### Task 11: Rename WorkspaceManager interface methods

**Parallel group:** E

**Files:**

- Modify: `internal/workspace/interfaces.go:103-111`
- Modify: all callers (dashboard handlers, daemon, etc.)

**Step 1: Find all callers**

```bash
grep -rn "UpdateGitStatus\|UpdateAllGitStatus\|GetWorkspaceGitFiles\|GitSafetyStatus\|GitChangedFile" --include="*.go" .
```

**Step 2: Rename on the interface**

In `internal/workspace/interfaces.go`:

- `UpdateGitStatus` → `UpdateVCSStatus`
- `UpdateAllGitStatus` → `UpdateAllVCSStatus`
- `GetWorkspaceGitFiles` → `GetWorkspaceChangedFiles`

**Step 3: Rename the implementing methods on Manager**

Match the new interface names.

**Step 4: Update all callers**

Dashboard handlers, daemon background goroutine, etc. This is a mechanical find-and-replace.

**Step 5: Rename types**

- `GitSafetyStatus` → `VCSSafetyStatus` (interfaces.go:83)
- `GitChangedFile` → keep as-is if it becomes an alias for `VCSChangedFile`, or remove it

**Step 6: Run tests**

```bash
./test.sh --quick
```

**Step 7: Commit**

```bash
git commit -m "$(cat <<'EOF'
refactor(workspace): rename Git-prefixed interface methods to VCS

UpdateGitStatus → UpdateVCSStatus, UpdateAllGitStatus → UpdateAllVCSStatus,
GetWorkspaceGitFiles → GetWorkspaceChangedFiles.
EOF
)"
```

---

### Task 12: SaplingBackend — command template engine

**Parallel group:** F

**Files:**

- Create: `internal/workspace/vcs_sapling.go`

**Step 1: Write the template engine test**

Create `internal/workspace/vcs_sapling_test.go`:

```go
func TestSaplingCommandTemplate(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		want     string
	}{
		{
			"simple substitution",
			"sl clone {{.RepoIdentifier}} {{.DestPath}}",
			map[string]string{"RepoIdentifier": "myrepo", "DestPath": "/tmp/ws1"},
			"sl clone myrepo /tmp/ws1",
		},
		{
			"no variables",
			"echo hello",
			map[string]string{},
			"echo hello",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderCommandTemplate(tt.template, tt.vars)
			if err != nil {
				t.Fatalf("renderCommandTemplate() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/workspace/ -run TestSaplingCommandTemplate
```

Expected: FAIL

**Step 3: Implement SaplingBackend struct and template renderer**

```go
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/sergeknystautas/schmux/internal/config"
)

type SaplingBackend struct {
	manager  *Manager
	commands config.SaplingCommands
}

func NewSaplingBackend(m *Manager, cmds config.SaplingCommands) *SaplingBackend {
	return &SaplingBackend{manager: m, commands: cmds}
}

func renderCommandTemplate(tmpl string, vars map[string]string) (string, error) {
	t, err := template.New("cmd").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("invalid command template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template execution failed: %w", err)
	}
	return buf.String(), nil
}

func (s *SaplingBackend) runTemplateCommand(ctx context.Context, tmpl string, vars map[string]string) ([]byte, error) {
	rendered, err := renderCommandTemplate(tmpl, vars)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", rendered)
	return cmd.CombinedOutput()
}
```

**Step 4: Run test**

```bash
go test ./internal/workspace/ -run TestSaplingCommandTemplate
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): add SaplingBackend struct with command template engine

Renders Go text/template strings with workspace variables.
Used for configurable lifecycle commands.
EOF
)"
```

---

### Task 13: SaplingBackend — lifecycle methods

**Parallel group:** G

**Files:**

- Modify: `internal/workspace/vcs_sapling.go`

**Step 1: Implement EnsureRepoBase**

```go
func (s *SaplingBackend) EnsureRepoBase(ctx context.Context, repoIdentifier, basePath string) (string, error) {
	vars := map[string]string{
		"RepoIdentifier": repoIdentifier,
	}

	// Check if repo base already exists
	checkCmd := s.commands.CheckRepoBase
	if checkCmd != "" {
		output, err := s.runTemplateCommand(ctx, checkCmd, vars)
		if err == nil {
			path := strings.TrimSpace(string(output))
			if path != "" {
				return path, nil
			}
		}
	}

	// Check state
	if rb, found := s.manager.state.GetRepoBaseByURL(repoIdentifier); found {
		if _, err := os.Stat(rb.Path); err == nil {
			return rb.Path, nil
		}
	}

	// Create repo base
	vars["BasePath"] = basePath
	createCmd := s.commands.GetCreateRepoBase()
	if _, err := s.runTemplateCommand(ctx, createCmd, vars); err != nil {
		return "", fmt.Errorf("create repo base failed: %w", err)
	}

	// Track in state
	s.manager.state.AddRepoBase(state.RepoBase{
		RepoURL: repoIdentifier,
		Path:    basePath,
		VCS:     "sapling",
	})
	s.manager.state.Save()

	return basePath, nil
}
```

**Step 2: Implement CreateWorkspace**

```go
func (s *SaplingBackend) CreateWorkspace(ctx context.Context, repoBasePath, branch, destPath string) error {
	vars := map[string]string{
		"RepoIdentifier": repoBasePath, // for sapling, this is the repo name/path
		"Branch":         branch,
		"DestPath":       destPath,
	}
	cmd := s.commands.GetCreateWorkspace()
	if _, err := s.runTemplateCommand(ctx, cmd, vars); err != nil {
		return fmt.Errorf("create workspace failed: %w", err)
	}
	return nil
}
```

**Step 3: Implement RemoveWorkspace**

```go
func (s *SaplingBackend) RemoveWorkspace(ctx context.Context, workspacePath string) error {
	vars := map[string]string{
		"WorkspacePath": workspacePath,
	}
	cmd := s.commands.GetRemoveWorkspace()
	if _, err := s.runTemplateCommand(ctx, cmd, vars); err != nil {
		return fmt.Errorf("remove workspace failed: %w", err)
	}
	return nil
}
```

**Step 4: Implement PruneStale and IsBranchInUse**

```go
func (s *SaplingBackend) PruneStale(ctx context.Context, repoBasePath string) error {
	return nil // no-op for sapling
}

func (s *SaplingBackend) IsBranchInUse(ctx context.Context, repoBasePath, branch string) (bool, error) {
	return false, nil // sapling workspaces are independent
}
```

**Step 5: Run tests**

```bash
./test.sh --quick
```

**Step 6: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): implement SaplingBackend lifecycle methods

EnsureRepoBase, CreateWorkspace, RemoveWorkspace use configurable
command templates. PruneStale and IsBranchInUse are no-ops.
EOF
)"
```

---

### Task 14: SaplingBackend — observability methods

**Parallel group:** G

**Files:**

- Modify: `internal/workspace/vcs_sapling.go`

**Step 1: Implement GetStatus**

Uses `sl` directly (open source):

```go
func (s *SaplingBackend) GetStatus(ctx context.Context, workspacePath string) (VCSStatus, error) {
	var status VCSStatus

	// sl status (dirty check)
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath, "status")
	if err == nil {
		trimmed := strings.TrimSpace(string(output))
		status.Dirty = len(trimmed) > 0
		if trimmed != "" {
			status.FilesChanged = len(strings.Split(trimmed, "\n"))
		}
	}

	// Current branch/bookmark
	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", ".", "-T", "{activebookmark}")
	if err == nil {
		status.CurrentBranch = strings.TrimSpace(string(output))
	}

	// Ahead/behind vs main
	// sl log -r 'draft()' counts draft (unpushed) commits
	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", "draft()", "-T", "x")
	if err == nil {
		status.AheadOfDefault = len(strings.TrimSpace(string(output)))
	}

	// Line counts via sl diff --stat
	output, err = s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"diff", "--stat")
	if err == nil {
		status.LinesAdded, status.LinesRemoved = parseDiffStat(string(output))
	}

	return status, nil
}
```

**Step 2: Implement parseDiffStat helper**

Parse `sl diff --stat` output (same format as git: `N insertions(+), M deletions(-)`).

**Step 3: Implement GetChangedFiles**

```go
func (s *SaplingBackend) GetChangedFiles(ctx context.Context, workspacePath string) ([]VCSChangedFile, error) {
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath, "status")
	if err != nil {
		return nil, err
	}
	return parseSaplingStatus(string(output)), nil
}
```

**Step 4: Implement GetDefaultBranch, GetCurrentBranch, Fetch**

```go
func (s *SaplingBackend) GetDefaultBranch(ctx context.Context, repoBasePath string) (string, error) {
	return "main", nil // configurable in future if needed
}

func (s *SaplingBackend) GetCurrentBranch(ctx context.Context, workspacePath string) (string, error) {
	output, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, workspacePath,
		"log", "-r", ".", "-T", "{activebookmark}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *SaplingBackend) Fetch(ctx context.Context, path string) error {
	_, err := s.manager.runCmd(ctx, "sl", "", RefreshTriggerExplicit, path, "pull")
	return err
}
```

**Step 5: Run tests**

```bash
./test.sh --quick
```

**Step 6: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): implement SaplingBackend observability methods

Uses sl directly for status, diff, log, and pull operations.
EOF
)"
```

---

### Task 15: SaplingBackend — query repo methods

**Parallel group:** G

**Files:**

- Modify: `internal/workspace/vcs_sapling.go`

**Step 1: Implement stub methods**

For v1, sapling query repos can be simplified. Sapling workspaces are independent mounts, so there's no separate "query repo" concept needed. Return reasonable defaults:

```go
func (s *SaplingBackend) EnsureQueryRepo(ctx context.Context, repoIdentifier, path string) error {
	return nil // sapling workspaces are self-contained
}

func (s *SaplingBackend) FetchQueryRepo(ctx context.Context, path string) error {
	return nil
}

func (s *SaplingBackend) ListRecentBranches(ctx context.Context, path string, limit int) ([]RecentBranch, error) {
	return nil, nil // TODO: implement with sl log for bookmarks
}

func (s *SaplingBackend) GetBranchLog(ctx context.Context, path, branch string, limit int) ([]string, error) {
	return nil, nil // TODO: implement with sl log
}
```

**Step 2: Verify compile-time interface satisfaction**

```go
var _ VCSBackend = (*SaplingBackend)(nil)
```

**Step 3: Run tests**

```bash
./test.sh --quick
```

**Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(workspace): implement SaplingBackend query repo stubs

Returns empty results for v1. Sapling workspaces don't need
separate query repos. Can be enhanced later.
EOF
)"
```

---

### Task 16: SaplingBackend tests

**Parallel group:** H

**Files:**

- Modify: `internal/workspace/vcs_sapling_test.go`

**Step 1: Write unit tests for template rendering edge cases**

Test empty templates, missing variables, special characters in paths.

**Step 2: Write unit tests for parseSaplingStatus**

Test parsing of `sl status` output format:

```
M path/to/modified.go
A path/to/added.go
R path/to/removed.go
? path/to/untracked.go
```

**Step 3: Write unit tests for parseDiffStat**

Test parsing of `sl diff --stat` summary line.

**Step 4: Write integration-style tests with mocked commands**

Test `EnsureRepoBase`, `CreateWorkspace`, `RemoveWorkspace` with command templates that echo/exit rather than calling real tools.

**Step 5: Run tests**

```bash
go test ./internal/workspace/ -run TestSapling -v
./test.sh --quick
```

**Step 6: Commit**

```bash
git commit -m "$(cat <<'EOF'
test(workspace): add SaplingBackend unit tests

Covers template rendering, status parsing, and lifecycle
methods with mocked command execution.
EOF
)"
```

---

### Task 17: Web config — VCS dropdown on repo form

**Parallel group:** H

**Files:**

- Modify: config-related React components in `assets/dashboard/src/`

**Step 1: Find the repo config form component**

```bash
grep -rn "BarePath\|bare_path\|repo.*form\|repo.*config" assets/dashboard/src/ --include="*.tsx" --include="*.ts"
```

**Step 2: Add VCS dropdown**

Add a `<select>` element with options `git` (default) and `sapling`. When `sapling` is selected, change the URL field label to "Repo Identifier".

**Step 3: Update the TypeScript types**

Run `go run ./cmd/gen-types` after the config struct changes are in place, or add `vcs?: string` to the manual types.

**Step 4: Run tests**

```bash
./test.sh --quick
```

**Step 5: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(dashboard): add VCS dropdown to repo config form

Shows git/sapling selector. Changes URL label to 'Repo Identifier'
when sapling is selected.
EOF
)"
```

---

### Task 18: Web config — SaplingCommands editor

**Parallel group:** H

**Files:**

- Modify: config-related React components in `assets/dashboard/src/`

**Step 1: Add SaplingCommands section to settings**

In the settings/config page, add a collapsible section "Sapling Commands" with text inputs for each command template. Show placeholder text with the default values.

**Step 2: Wire to config API**

The existing config save endpoint already persists the full config. Just ensure the new `sapling_commands` field round-trips correctly.

**Step 3: Run tests**

```bash
./test.sh --quick
```

**Step 4: Commit**

```bash
git commit -m "$(cat <<'EOF'
feat(dashboard): add SaplingCommands editor in settings

Allows editing lifecycle command templates for sapling workspaces.
Shows defaults as placeholder text.
EOF
)"
```

---

### Task 19: Integration validation

**Parallel group:** I

**Files:**

- No code changes — manual testing

**Step 1: Validate on a machine with sapling**

1. Configure a repo with `"vcs": "sapling"` in `~/.schmux/config.json`
2. Set appropriate `sapling_commands` for the environment
3. Start the daemon: `./schmux start`
4. Create a workspace via the dashboard or API
5. Verify: workspace directory exists, `sl status` works from inside it
6. Spawn a tmux session into the workspace
7. Verify: agent can read/write files and run `sl` commands
8. Check dashboard: workspace card shows dirty status, branch info
9. Dispose the workspace via dashboard
10. Verify: workspace directory is removed

**Step 2: Test with pre-existing repo base**

If the machine already has a checkout, verify `CheckRepoBase` discovers it and `CreateWorkspace` reuses the backing store.

**Step 3: Test mount namespace**

Verify tmux sessions spawned by the daemon can access workspaces created by environment-specific tools (e.g., FUSE mounts).

**Step 4: Test git repos still work**

Run the full test suite to ensure git workspace functionality is unchanged:

```bash
./test.sh --all
```
