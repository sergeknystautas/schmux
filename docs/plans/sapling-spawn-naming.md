# Plan: Sapling Workspace Naming on Spawn Page

**Goal**: When a sapling repo is selected on the spawn page, hide all branch UI and let the user optionally type a workspace label that defaults to the auto-generated workspace ID.

**Architecture**: New `Label` field on `state.Workspace` and `SpawnRequest.WorkspaceLabel` field (distinct from existing session `Nickname`). Backend leaves `Workspace.Branch == ""` for sapling and substitutes `"main"` only at the sapling backend's template-variable boundary. Frontend gets a `workspaceDisplayLabel(ws, computedBranch?)` helper applied at six render sites; spawn page detects sapling via `repo.vcs === "sapling"` and conditionally hides five branch-related UI elements plus a sixth LLM-call code path.

**Tech Stack**: Go (backend), React/TypeScript (frontend), Vitest (frontend tests), Playwright (scenarios), `./test.sh` orchestrator.

**Source spec**: [`docs/specs/sapling-spawn-naming.md`](../specs/sapling-spawn-naming.md) v3.

## Conventions used in this plan

- All commands run from repo root (`/Users/stefanomaz/code/workspaces/schmux-005/`).
- Frontend builds use `go run ./cmd/build-dashboard`, never `npm` directly.
- Frontend tests run via `./test.sh --quick` (includes Vitest).
- TS type regeneration uses `go run ./cmd/gen-types`.
- Commits use the `/commit` slash command, never `git commit` directly.
- For each step's test, run from repo root.

## Task Dependencies

| Group | Steps       | Can Parallelize                        |
| ----- | ----------- | -------------------------------------- |
| 1     | Steps 1–3   | Yes (independent contract files)       |
| 2     | Step 4      | No (depends on Group 1 — TS regen)     |
| 3     | Steps 5–8   | Sequential (all touch `manager.go`)    |
| 4     | Step 9      | No (depends on Group 3)                |
| 5     | Step 10     | No (depends on Group 1)                |
| 6     | Step 11     | No (depends on Step 10)                |
| 7     | Steps 12–17 | Sequential (all touch `SpawnPage.tsx`) |
| 8     | Step 18     | No (final E2E — depends on everything) |

---

## Step 1: Add `Label` field to `state.Workspace`

**File**: `internal/state/state.go`

### 1a. Write failing test

Add to `internal/state/state_test.go`:

```go
func TestWorkspace_LabelRoundTrip(t *testing.T) {
	ws := Workspace{
		ID:     "myrepo-007",
		Repo:   "git@github.com:foo/myrepo",
		Branch: "",
		Path:   "/tmp/myrepo-007",
		VCS:    "sapling",
		Label:  "Login bug fix",
	}
	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"label":"Login bug fix"`) {
		t.Fatalf("label not serialized: %s", data)
	}
	var got Workspace
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Label != "Login bug fix" {
		t.Fatalf("expected Label=%q, got %q", "Login bug fix", got.Label)
	}

	// Empty Label must omit from JSON
	wsEmpty := Workspace{ID: "x", Repo: "r", Path: "/p"}
	data2, _ := json.Marshal(wsEmpty)
	if strings.Contains(string(data2), `"label"`) {
		t.Fatalf("empty label should be omitted: %s", data2)
	}
}
```

### 1b. Run test to verify it fails

```bash
go test ./internal/state/ -run TestWorkspace_LabelRoundTrip
```

Expected: compile error or test failure (no `Label` field).

### 1c. Write implementation

In `internal/state/state.go` inside `type Workspace struct` (around line 125), add after the `VCS` field:

```go
	Label                   string            `json:"label,omitempty"`              // Optional human-friendly display label (used by sapling workspaces today)
```

### 1d. Run test to verify it passes

```bash
go test ./internal/state/ -run TestWorkspace_LabelRoundTrip
```

### 1e. Commit

Use `/commit` with message body describing the addition of the optional workspace label field.

---

## Step 2: Add `WorkspaceLabel` to `SpawnRequest`

**File**: `internal/api/contracts/spawn_request.go`

### 2a. Inspect existing struct

```bash
# (Use Read tool to view the file — locate the SpawnRequest struct definition)
```

### 2b. Add field

In `internal/api/contracts/spawn_request.go`, add to the `SpawnRequest` struct (anywhere; convention is grouped near other optional string fields):

```go
	WorkspaceLabel string `json:"workspace_label,omitempty"` // Optional workspace display label (sapling-only today; ignored in workspace mode)
```

### 2c. Verify compile

```bash
go build ./...
```

### 2d. Commit

Use `/commit`.

---

## Step 3: Add `Label` to workspace API response

**File**: `internal/api/contracts/sessions.go`

### 3a. Locate the workspace response struct

The struct is `WorkspaceResponseItem` (around line 50–70 — the one that has `VCS string \`json:"vcs,omitempty"\`` at line 65).

### 3b. Add field

In `WorkspaceResponseItem`, add adjacent to `VCS`:

```go
	Label string `json:"label,omitempty"` // Optional workspace display label
```

### 3c. Verify compile

```bash
go build ./...
```

### 3d. Commit

Use `/commit`.

---

## Step 4: Regenerate TypeScript types

**Files**: `assets/dashboard/src/lib/types.generated.ts` (auto-written)

### 4a. Run generator

```bash
go run ./cmd/gen-types
```

### 4b. Verify output

```bash
# Confirm the new fields appear
grep -n 'workspace_label\|label?:' assets/dashboard/src/lib/types.generated.ts
```

Expected: at least three matches — `WorkspaceResponseItem.label`, `SpawnRequest.workspace_label`, and any other generated fields named `label`.

### 4c. Commit

Use `/commit`. (`types.generated.ts` is auto-generated but checked in.)

---

## Step 5: Skip `ValidateBranchName` for sapling in `GetOrCreate`

**File**: `internal/workspace/manager.go`

### 5a. Write failing test

Add to `internal/workspace/manager_test.go` (or whichever existing file holds `GetOrCreate` tests; create a focused new file `manager_sapling_branch_test.go` if cleaner):

```go
func TestGetOrCreate_SaplingAcceptsEmptyBranch(t *testing.T) {
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreate(context.Background(), "sl:saplingrepo", "")
	if err != nil {
		t.Fatalf("expected empty-branch sapling spawn to succeed, got: %v", err)
	}
	if w.Branch != "" {
		t.Fatalf("expected Workspace.Branch to remain empty for sapling, got %q", w.Branch)
	}
}
```

(Use the existing test-helper pattern in `vcs_sapling_test.go` for `newTestManagerWithSaplingRepo`. If a helper does not yet exist, factor one out from `vcs_sapling_test.go:328` first as part of this step.)

### 5b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run TestGetOrCreate_SaplingAcceptsEmptyBranch
```

Expected: fails because `ValidateBranchName("")` returns `ErrInvalidBranchName`.

### 5c. Write implementation

In `GetOrCreate` at `internal/workspace/manager.go:416`, before the `ValidateBranchName(branch)` call, add:

```go
	// Sapling workspaces have no branch concept — accept empty branch.
	if branch == "" {
		if repo, found := m.findRepoByURL(repoURL); found && repo.VCS == "sapling" {
			// Skip ValidateBranchName; the empty value is intentional and
			// will be substituted only at the sapling backend boundary.
		} else {
			if err := ValidateBranchName(branch); err != nil {
				return nil, fmt.Errorf("failed to get workspace: %w", err)
			}
		}
	} else {
		if err := ValidateBranchName(branch); err != nil {
			return nil, fmt.Errorf("failed to get workspace: %w", err)
		}
	}
```

(Replace the original `ValidateBranchName` call accordingly. Remove the original `ValidateBranchName` call — do not double-validate.)

### 5d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run TestGetOrCreate_SaplingAcceptsEmptyBranch
```

### 5e. Commit

Use `/commit`.

---

## Step 6: Substitute `"main"` only at sapling backend call in `create()`

**File**: `internal/workspace/manager.go`

### 6a. Write failing test

Add to the same test file as Step 5:

```go
func TestCreate_SaplingPersistsEmptyBranch(t *testing.T) {
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreate(context.Background(), "sl:saplingrepo", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The persisted Branch must be empty — substitution happens only at
	// the sapling backend boundary.
	if w.Branch != "" {
		t.Fatalf("expected persisted Branch=\"\", got %q", w.Branch)
	}
}
```

### 6b. Run test to verify it fails or passes accidentally

```bash
go test ./internal/workspace/ -run TestCreate_SaplingPersistsEmptyBranch
```

If Step 5 passed empty through, this may already pass — but the goal of this step is to ensure `create()` **does not overwrite** `branch` with `"main"`. Inspect the current `create()` body and confirm.

### 6c. Write implementation

In `internal/workspace/manager.go::create()` (around line 624), find the call to `backend.CreateWorkspace`. The current call is:

```go
		if err := backend.CreateWorkspace(ctx, worktreeBasePath, branch, workspacePath); err != nil {
```

Replace with a local-variable substitution that affects only the backend call:

```go
		backendBranch := branch
		if backendBranch == "" && repoConfig.VCS == "sapling" {
			backendBranch = "main"
		}
		if err := backend.CreateWorkspace(ctx, worktreeBasePath, backendBranch, workspacePath); err != nil {
```

Apply the same substitution at the second `backend.CreateWorkspace` call site within `create()` (the non-worktree branch).

The `state.Workspace` struct constructed at line 716–723 must keep `Branch: branch` (the original empty string), not `backendBranch`.

### 6d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run TestCreate_SaplingPersistsEmptyBranch
```

Also re-run Step 5's test to confirm no regression:

```bash
go test ./internal/workspace/ -run 'TestGetOrCreate_SaplingAcceptsEmptyBranch|TestCreate_SaplingPersistsEmptyBranch'
```

### 6e. Commit

Use `/commit`.

---

## Step 7: Persist `Label` in workspace creation

**File**: `internal/workspace/manager.go`

### 7a. Write failing test

```go
func TestCreate_SaplingPersistsLabel(t *testing.T) {
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreateWithLabel(context.Background(), "sl:saplingrepo", "", "Login bug fix")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Label != "Login bug fix" {
		t.Fatalf("expected Label=%q, got %q", "Login bug fix", w.Label)
	}
}
```

### 7b. Run test to verify it fails

```bash
go test ./internal/workspace/ -run TestCreate_SaplingPersistsLabel
```

Expected: compile error (no `GetOrCreateWithLabel` method).

### 7c. Write implementation

Two API choices for the manager:

**Option A — new method**: Add `GetOrCreateWithLabel(ctx, repoURL, branch, label string)` that mirrors `GetOrCreate` and threads `label` through to `create()` and into `state.Workspace.Label`.

**Option B — extend existing**: Change `GetOrCreate` signature to take an optional struct (breaking — touches all call sites).

Choose **A** (lower blast radius). Implementation:

1. Add `GetOrCreateWithLabel(ctx, repoURL, branch, label string)` adjacent to `GetOrCreate`. Body is `GetOrCreate`'s body, but constructs `state.Workspace` with `Label: label`.
2. Refactor `GetOrCreate` to delegate: `return m.GetOrCreateWithLabel(ctx, repoURL, branch, "")`.
3. In `create()`, accept an extra `label string` argument. Set `Label: label` on the `state.Workspace` literal at line 716–723.
4. Update existing internal callers of `create()` to pass `""`.

### 7d. Run test to verify it passes

```bash
go test ./internal/workspace/ -run TestCreate_SaplingPersistsLabel
```

### 7e. Commit

Use `/commit`.

---

## Step 8: Relax handler branch check + thread `workspace_label`

**File**: `internal/dashboard/handlers_spawn.go`

### 8a. Write failing test

Add to `internal/dashboard/handlers_spawn_test.go`:

```go
func TestSpawn_SaplingAcceptsEmptyBranch(t *testing.T) {
	srv, cleanup := newTestServerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	body := `{"repo":"sl:saplingrepo","branch":"","workspace_label":"Login bug fix","targets":{"claude":1}}`
	resp := srv.postSpawn(t, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	// Confirm the created workspace has the label.
	ws := srv.lastCreatedWorkspace(t)
	if ws.Label != "Login bug fix" {
		t.Fatalf("expected Label=%q, got %q", "Login bug fix", ws.Label)
	}
}

func TestSpawn_GitStillRejectsEmptyBranch(t *testing.T) {
	srv, cleanup := newTestServerWithGitRepo(t, "gitrepo")
	defer cleanup()

	body := `{"repo":"git@github.com:foo/gitrepo","branch":"","targets":{"claude":1}}`
	resp := srv.postSpawn(t, body)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for git+empty branch, got %d", resp.Code)
	}
}

func TestSpawn_WorkspaceModeIgnoresWorkspaceLabel(t *testing.T) {
	srv, cleanup := newTestServerWithExistingSaplingWorkspace(t, "myrepo-001", "Original")
	defer cleanup()

	body := `{"workspace_id":"myrepo-001","workspace_label":"Renamed","targets":{"claude":1}}`
	resp := srv.postSpawn(t, body)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}
	ws := srv.workspaceByID(t, "myrepo-001")
	if ws.Label != "Original" {
		t.Fatalf("expected Label unchanged (%q), got %q", "Original", ws.Label)
	}
}
```

(The `newTestServerWith…` helpers may not exist — extend existing test scaffolding accordingly. Reuse fixtures from `handlers_spawn_test.go`.)

### 8b. Run tests to verify they fail

```bash
go test ./internal/dashboard/ -run 'TestSpawn_SaplingAcceptsEmptyBranch|TestSpawn_GitStillRejectsEmptyBranch|TestSpawn_WorkspaceModeIgnoresWorkspaceLabel'
```

### 8c. Write implementation

In `internal/dashboard/handlers_spawn.go`:

1. **Branch check at line 134-137**: change

   ```go
   if req.Branch == "" {
       writeJSONError(w, "branch is required ...", http.StatusBadRequest)
       return
   }
   ```

   to also check VCS — only fail if the resolved repo is **not** sapling:

   ```go
   if req.Branch == "" {
       repoCfg, _ := h.config.FindRepoByURL(req.Repo)
       if repoCfg == nil || repoCfg.VCS != "sapling" {
           writeJSONError(w, "branch is required ...", http.StatusBadRequest)
           return
       }
   }
   ```

   (Use the actual config helper that the handler already has access to — likely via `h.config` or `h.cfg`. Read the file to confirm the lookup pattern in use elsewhere.)

2. **Wire `WorkspaceLabel` into manager call**: locate the spawn site that calls into the workspace manager (around line 240–250 where `req.Nickname` is currently used). When `req.WorkspaceID == ""`, call `m.GetOrCreateWithLabel(ctx, req.Repo, req.Branch, req.WorkspaceLabel)`. When `req.WorkspaceID != ""`, leave existing workspace-mode flow untouched (it does not reach `create()`, so `WorkspaceLabel` is naturally ignored).

### 8d. Run tests to verify they pass

```bash
go test ./internal/dashboard/ -run 'TestSpawn_SaplingAcceptsEmptyBranch|TestSpawn_GitStillRejectsEmptyBranch|TestSpawn_WorkspaceModeIgnoresWorkspaceLabel'
```

### 8e. Commit

Use `/commit`.

---

## Step 9: Verify backend slice end-to-end with full backend test run

```bash
go test ./internal/state/ ./internal/workspace/ ./internal/dashboard/
```

Expected: all pass. No frontend changes yet — the schema and backend are now sapling-empty-branch-aware and label-aware.

No commit needed — verification only.

---

## Step 10: Add `workspaceDisplayLabel` helper + tests

**Files**:

- `assets/dashboard/src/lib/workspace-display.ts` (new)
- `assets/dashboard/src/lib/workspace-display.test.ts` (new)

### 10a. Write failing test

Create `assets/dashboard/src/lib/workspace-display.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { workspaceDisplayLabel } from './workspace-display';
import type { WorkspaceResponseItem } from './types.generated';

const baseWs: WorkspaceResponseItem = {
  id: 'myrepo-007',
  repo: 'r',
  branch: '',
  path: '/p',
  status: 'running',
  commits_synced_with_remote: false,
  default_branch_orphaned: false,
} as WorkspaceResponseItem;

describe('workspaceDisplayLabel', () => {
  it('returns label when set', () => {
    expect(workspaceDisplayLabel({ ...baseWs, label: 'My label', branch: 'main' })).toBe(
      'My label'
    );
  });

  it('returns computedBranch when label is empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: 'main' }, 'feature/x')).toBe('feature/x');
  });

  it('returns branch when label and computedBranch are empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: 'main' })).toBe('main');
  });

  it('returns id when label, computedBranch, and branch are all empty (sapling fallback)', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: '' })).toBe('myrepo-007');
  });

  it('treats whitespace-only label as empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, label: '   ', branch: 'main' })).toBe('main');
  });
});
```

### 10b. Run test to verify it fails

```bash
./test.sh --quick 2>&1 | grep -E 'workspace-display|FAIL'
```

Expected: import failure (file doesn't exist).

### 10c. Write implementation

Create `assets/dashboard/src/lib/workspace-display.ts`:

```ts
import type { WorkspaceResponseItem } from './types.generated';

/**
 * Resolve the display string for a workspace.
 *
 * Fallback chain:
 *   1. `ws.label` (if non-empty after trim)
 *   2. `computedBranch` (caller-supplied; lets remote-aware logic compose)
 *   3. `ws.branch` (raw)
 *   4. `ws.id` (the on-disk workspace ID — final fallback for sapling)
 */
export function workspaceDisplayLabel(ws: WorkspaceResponseItem, computedBranch?: string): string {
  const label = ws.label?.trim();
  if (label) return label;
  if (computedBranch) return computedBranch;
  if (ws.branch) return ws.branch;
  return ws.id;
}
```

### 10d. Run test to verify it passes

```bash
./test.sh --quick
```

### 10e. Commit

Use `/commit`.

---

## Step 11: Apply `workspaceDisplayLabel` to all 6 render sites

**Files**:

- `assets/dashboard/src/components/WorkspaceHeader.tsx` (lines 177–188, 210–214)
- `assets/dashboard/src/components/AppShell.tsx` (lines 736–784)
- `assets/dashboard/src/routes/HomePage.tsx` (lines 656, 1205)
- `assets/dashboard/src/routes/RepofeedPage.tsx` (line 90)
- `assets/dashboard/src/routes/OverlayPage.tsx` (line 586)

For each site:

1. Import the helper at the top of the file:

   ```ts
   import { workspaceDisplayLabel } from '../lib/workspace-display';
   // (adjust relative path per file)
   ```

2. Replace the existing `ws.branch` / `workspace.branch` / `displayBranch` use with a helper call:
   - **WorkspaceHeader / AppShell**: these compute a remote-aware `displayBranch`. Pass it as the second argument: `workspaceDisplayLabel(workspace, displayBranch)`.
   - **HomePage / RepofeedPage / OverlayPage**: pass only the workspace: `workspaceDisplayLabel(ws)`.

### 11a. Write failing tests (where component tests exist)

For at least `WorkspaceHeader` and `AppShell`, find any existing snapshot/component tests and add a sapling case:

```ts
it('renders workspace ID when sapling workspace has no label and empty branch', () => {
  const ws = makeMockWorkspace({ id: 'sapling-007', branch: '', vcs: 'sapling' });
  render(<WorkspaceHeader workspace={ws} />);
  expect(screen.getByText('sapling-007')).toBeInTheDocument();
});
```

(Adapt to whatever fixture/render helpers each test file uses.)

### 11b. Run tests

```bash
./test.sh --quick
```

Expected: new tests fail (sites still use `branch` or `displayBranch` directly and render empty).

### 11c. Apply the helper at each of the 6 sites

Edit each file as described above.

### 11d. Re-run tests

```bash
./test.sh --quick
```

Expected: pass at all 6 sites.

### 11e. Commit

Use `/commit`. Single commit for all 6 sites is acceptable — they are mechanical edits.

---

## Step 12: Add `isSapling` / `isSaplingWorkspace` memos to `SpawnPage`

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

### 12a. Add the memos near the top of the component body, after `currentWorkspace` is computed (around line 237):

```tsx
const isSapling = useMemo(
  () => repos.find((r) => r.url === repo)?.vcs === 'sapling',
  [repos, repo]
);

const isSaplingWorkspace = currentWorkspace?.vcs === 'sapling';
```

### 12b. Compile-check

```bash
./test.sh --quick
```

Expected: no failures (memo is unused yet).

### 12c. Commit

Use `/commit`. (Pure addition; no behavior change.)

---

## Step 13: Hide branch-related UI elements 1–5 for sapling

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

### 13a. Write failing test

Add to `assets/dashboard/src/routes/SpawnPage.test.tsx` (or create a new `SpawnPage.sapling.test.tsx` if cleaner):

```tsx
it('hides the branch input when a sapling repo is selected', async () => {
  // Arrange: getConfig mock returns a sapling repo.
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: '' }, // forces branch input to show for git
  });
  render(<SpawnPage />);
  // Select the sapling repo
  const repoSelect = await screen.findByTestId('spawn-repo-select');
  fireEvent.change(repoSelect, { target: { value: 'sl:saplingrepo' } });

  expect(screen.queryByPlaceholderText(/feature\/my-branch/i)).not.toBeInTheDocument();
});

it('does not error on submit when sapling repo is selected and branch is empty', async () => {
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: '' },
  });
  const spawnSpy = mockSpawnSessions();
  render(<SpawnPage />);
  fireEvent.change(await screen.findByTestId('spawn-repo-select'), {
    target: { value: 'sl:saplingrepo' },
  });
  // Select an agent so totalPromptableCount > 0
  fireEvent.change(await screen.findByTestId('agent-select'), {
    target: { value: 'claude' },
  });
  fireEvent.click(screen.getByTestId('spawn-submit'));
  await waitFor(() => expect(spawnSpy).toHaveBeenCalled());
  expect(spawnSpy.mock.calls[0][0].branch).toBe('');
});
```

### 13b. Run tests to verify they fail

```bash
./test.sh --quick
```

### 13c. Implementation

In `SpawnPage.tsx`, apply these gates (line numbers from spec; verify with grep before editing):

1. **Element 1 — single-agent branch input (line 1226)**: change `{showBranchInput && (` to `{showBranchInput && !isSapling && (`.

2. **Element 2 — multi/advanced branch input (line 1584)**: change the existing condition to also include `&& !isSapling`.

3. **Element 3 — `showBranchInput` auto-set effect (line 256)**:

   ```tsx
   useEffect(() => {
     if (mode === 'fresh' && !branchSuggestTarget && config && !isSapling) {
       setShowBranchInput(true);
     }
   }, [mode, branchSuggestTarget, config, isSapling]);
   ```

4. **Element 4 — `Create new branch from here` checkbox (line 1632)**: wrap in `{!isSaplingWorkspace && (...)}`.

5. **Element 5 — `validateForm` branch check (line 584)**: change
   ```tsx
   if (mode === 'fresh' && !branchSuggestTarget && !branch.trim()) {
   ```
   to
   ```tsx
   if (mode === 'fresh' && !isSapling && !branchSuggestTarget && !branch.trim()) {
   ```

### 13d. Run tests to verify they pass

```bash
./test.sh --quick
```

### 13e. Commit

Use `/commit`.

---

## Step 14: Render the optional label input for sapling

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

### 14a. Add new state

Near the existing `const [branch, setBranch] = useState('');` (line 160):

```tsx
const [workspaceLabel, setWorkspaceLabel] = useState('');
```

### 14b. Compute the prospective workspace ID for the placeholder

Add a memo near the existing memos:

```tsx
const prospectiveWorkspaceId = useMemo(() => {
  if (!isSapling) return '';
  const repoName = repos.find((r) => r.url === repo)?.name || '';
  if (!repoName) return '';
  // Best-effort: count existing workspaces for this repo and add 1.
  // Daemon's findNextWorkspaceNumber actually fills gaps — we accept this
  // hint may be off by 1–2 (per spec).
  const count = (workspaces || []).filter((w) => {
    const r = repos.find((rr) => rr.url === w.repo);
    return r?.name === repoName;
  }).length;
  return `${repoName}-${String(count + 1).padStart(3, '0')}`;
}, [isSapling, repos, repo, workspaces]);
```

### 14c. Render the input in the same slot as the branch input

In the single-agent fresh-mode block (around line 1226), after the gated branch input, add:

```tsx
{
  isSapling && (
    <div className="spawn-selector">
      <span className="spawn-selector__label">Label</span>
      <input
        type="text"
        className="input"
        value={workspaceLabel}
        onChange={(e) => setWorkspaceLabel(e.target.value)}
        placeholder={prospectiveWorkspaceId}
        data-testid="workspace-label-input"
      />
    </div>
  );
}
```

Mirror the same render in the multi/advanced branch input block (around line 1584), placed adjacently in the layout.

### 14d. Wire `workspace_label` into the spawn request payload

In `handleEngage` (around line 879 — the `spawnSessions({...})` call), add:

```tsx
workspace_label: isSapling ? workspaceLabel.trim() : undefined,
```

### 14e. Run tests

```bash
./test.sh --quick
```

The Step 13 tests should still pass plus a new assertion for the label input. Add:

```tsx
it('shows the label input with the prospective workspace ID as placeholder', async () => {
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: '' },
  });
  render(<SpawnPage />);
  fireEvent.change(await screen.findByTestId('spawn-repo-select'), {
    target: { value: 'sl:saplingrepo' },
  });
  const input = await screen.findByTestId('workspace-label-input');
  expect(input).toHaveAttribute('placeholder', expect.stringMatching(/^saplingrepo-\d{3}$/));
});

it('passes workspace_label to spawnSessions', async () => {
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: '' },
  });
  const spawnSpy = mockSpawnSessions();
  render(<SpawnPage />);
  fireEvent.change(await screen.findByTestId('spawn-repo-select'), {
    target: { value: 'sl:saplingrepo' },
  });
  fireEvent.change(await screen.findByTestId('agent-select'), {
    target: { value: 'claude' },
  });
  fireEvent.change(await screen.findByTestId('workspace-label-input'), {
    target: { value: 'Login bug fix' },
  });
  fireEvent.click(screen.getByTestId('spawn-submit'));
  await waitFor(() => expect(spawnSpy).toHaveBeenCalled());
  expect(spawnSpy.mock.calls[0][0].workspace_label).toBe('Login bug fix');
});
```

### 14f. Commit

Use `/commit`.

---

## Step 15: Short-circuit LLM branch suggester for sapling

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

### 15a. Write failing test

```tsx
it('does not call the LLM branch suggester for sapling repos', async () => {
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: 'opus' }, // non-empty triggers suggester for git
  });
  const suggestSpy = mockSuggestBranch();
  const spawnSpy = mockSpawnSessions();
  render(<SpawnPage />);
  fireEvent.change(await screen.findByTestId('spawn-repo-select'), {
    target: { value: 'sl:saplingrepo' },
  });
  fireEvent.change(await screen.findByTestId('agent-select'), {
    target: { value: 'claude' },
  });
  fireEvent.change(screen.getByTestId('spawn-prompt'), {
    target: { value: 'fix the login bug' },
  });
  fireEvent.click(screen.getByTestId('spawn-submit'));
  await waitFor(() => expect(spawnSpy).toHaveBeenCalled());
  expect(suggestSpy).not.toHaveBeenCalled();
});
```

### 15b. Run test to verify it fails

```bash
./test.sh --quick
```

### 15c. Implementation

In `handleEngage` at the fresh-mode branch-determination block (around line 830–857), wrap the conditional that calls `generateBranchName`:

```tsx
} else if (prompt.trim() && branchSuggestTarget && !isSapling) {
  // existing LLM suggestion path
  ...
}
```

Also at the workspace-mode LLM block (around line 861) — defensive guard:

```tsx
if (mode === 'workspace' && createBranch && prompt.trim() && branchSuggestTarget && !isSaplingWorkspace) {
  ...
}
```

### 15d. Run tests

```bash
./test.sh --quick
```

### 15e. Commit

Use `/commit`.

---

## Step 16: Fix `/resume` and command-target paths to send empty branch for sapling

**File**: `assets/dashboard/src/routes/SpawnPage.tsx`

### 16a. Write failing test

```tsx
it('/resume sends empty branch for sapling repos', async () => {
  mockGetConfig({
    repos: [{ name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' }],
    branch_suggest: { target: '' },
  });
  const spawnSpy = mockSpawnSessions();
  render(<SpawnPage />);
  fireEvent.change(await screen.findByTestId('spawn-repo-select'), {
    target: { value: 'sl:saplingrepo' },
  });
  fireEvent.change(await screen.findByTestId('agent-select'), {
    target: { value: 'claude' },
  });
  // Trigger /resume via slash command
  fireEvent.change(screen.getByTestId('spawn-prompt'), { target: { value: '/resume' } });
  // (Adapt to whatever event drives onSelectCommand in the test fixture)
  await waitFor(() => expect(spawnSpy).toHaveBeenCalled());
  expect(spawnSpy.mock.calls[0][0].branch).toBe('');
});
```

### 16b. Run test to verify it fails

```bash
./test.sh --quick
```

### 16c. Implementation

In `/resume` handler (around line 697):

```tsx
const actualBranch =
  mode === 'fresh' ? (isSapling ? '' : branch.trim() || getDefaultBranch(actualRepo)) : '';
```

In command-target handler (around line 766):

```tsx
const actualBranch =
  mode === 'fresh' ? (isSapling ? '' : branch.trim() || getDefaultBranch(actualRepo)) : branch;
```

### 16d. Run tests

```bash
./test.sh --quick
```

### 16e. Commit

Use `/commit`.

---

## Step 17: Add Playwright scenario

**File**: `test/scenarios/sapling-spawn-label.txt` (new)

### 17a. Write the scenario

Use the existing scenario format (look at any existing `test/scenarios/*.txt` for the convention). The scenario should cover:

1. Configure schmux with a sapling repo (use a test fixture).
2. Open the spawn page.
3. Select the sapling repo.
4. Verify the branch input is not visible.
5. Verify a label input is visible with the prospective workspace ID as placeholder.
6. **Without typing a label**, select an agent and click Engage.
7. Verify the new workspace appears in the sidebar with its workspace ID as the displayed label (not "main").
8. Open the spawn page again.
9. Select the sapling repo, type "Login bug fix" in the label field, select an agent, click Engage.
10. Verify the new workspace appears in the sidebar with "Login bug fix" as the displayed label.

### 17b. Generate the Playwright file

Use the `/scenario` slash command if available, or invoke the scenario generator manually. Refer to existing scenarios for the exact tooling.

### 17c. Run the scenario

```bash
./test.sh --scenarios
```

### 17d. Commit

Use `/commit`.

---

## Step 18: End-to-end verification

### 18a. Build the dashboard

```bash
go run ./cmd/build-dashboard
```

### 18b. Run the full test suite

```bash
./test.sh
```

Expected: all unit, frontend, e2e, and scenario tests pass.

### 18c. Manual smoke test via dev mode

```bash
./dev.sh
```

In the browser at http://localhost:7337:

1. Add a sapling repo via Config (or use an existing one).
2. Navigate to `/spawn`.
3. Select the sapling repo. Verify:
   - No branch input is visible.
   - A label input is visible with placeholder like `saplingrepo-007`.
4. Spawn without a label → confirm sidebar shows the workspace ID.
5. Spawn with a label → confirm sidebar shows the label.
6. Switch to a git repo on the same page. Verify the branch input reappears, the LLM suggester still works, and `/resume` still sends a non-empty branch.
7. Open an existing sapling workspace; navigate to its `/spawn?workspace_id=…`. Verify "Create new branch from here" is hidden.

### 18d. Final cleanup

If any docs reference the new field (e.g., `docs/api.md`), update them to mention `workspace_label` on `SpawnRequest` and `label` on the workspace response. The `/commit` hook enforces `docs/api.md` updates when API packages change — address it then.

### 18e. Commit any docs updates and merge

Use `/commit`.

---

## Notes for the executing agent

- **CLAUDE.md hard rules**: never run `npm`/`vite` directly; never edit `types.generated.ts` by hand; never run `npx vitest` from a subdirectory; never use `--quick` to satisfy the definition of done — Step 18 must use `./test.sh` (no flag).
- **Memory hint**: there is a memory rule against using internal-tool names in user-facing placeholder text. The label input's placeholder is the workspace ID (e.g. `myrepo-007`), never the VCS name itself — verify before merging.
- **Don't touch other worktrees**: this plan operates entirely within `/Users/stefanomaz/code/workspaces/schmux-005/`.
- If a test command refers to a helper that doesn't exist, factor it out from the closest existing test of the same shape rather than reinventing.
