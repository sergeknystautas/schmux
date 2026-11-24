# Remove Recycling Workspaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the recycling-workspaces feature so `Dispose` always deletes, with no migration for leftover `"recyclable"` records.

**Architecture:** Three deletion slices, each leaving the tree compiling and green: (1) the purge API and its dashboard indicator, (2) the `recycle_workspaces` setting and the recycle/reuse code paths it gates, (3) the `"recyclable"` status constant and every filter on it, with tests pinning how leftover records now behave. A final task runs the full gates.

**Tech Stack:** Go (`internal/workspace`, `internal/dashboard`, `internal/config`, `internal/state`, `internal/daemon`), React/TypeScript dashboard (`assets/dashboard/src`), Markdown docs.

**Spec:** `docs/superpowers/specs/2026-11-24-remove-recycling-workspaces-design.md`

## Global Constraints

- No migration, startup sweep, or status rewrite for leftover `"recyclable"` records.
- Keep `DisposeForcePurge`, `disposal.Request.ForcePurge`, Tier 1 and Tier 2 matching rules, and remote persistent-host reuse unchanged.
- Leave `CHANGES.md`, earlier files under `docs/superpowers/`, and the PGID "recycled" comments in `internal/session/reaper.go:229-232` alone.
- Never edit `assets/dashboard/src/lib/types.generated.ts` by hand; run `go run ./cmd/gen-types`.
- Frontend tests run only through `./test.sh --quick`; dashboard builds only through `go run ./cmd/build-dashboard`.
- Edit files with the editor tools, not `sed`/`python`.
- No `git commit`, `stash`, `push`. Commits go through `/commit` when the user asks; `docs/api.md` must land in the same commit as any `internal/dashboard`, `internal/config`, `internal/state`, `internal/workspace` change.
- Every changed test follows `docs/testing.md`; run `test-rules-review` before claiming done.

## Review Focus

1. A leftover record holding branch X: `POST /api/check-branch-conflict` for X reports a conflict naming that workspace — Task 3 test `TestHandleCheckBranchConflict_LeftoverRecyclableWorkspaceConflicts`.
2. A leftover record on the next spawn for its repo is reused and promoted to `running` — Task 3 keeps and renames `TestGetOrCreate_BranchReuse_PromotesLeftoverRecyclableRecord`.
3. A leftover record appears in the `/ws/dashboard` payload — Task 3 test `TestBuildSessionsResponse_IncludesLeftoverRecyclableWorkspace`.
4. A leftover record disposes through plain `Dispose` (directory and state gone) — Task 3 test `TestDispose_LeftoverRecyclableRecordIsDeleted`.
5. A `config.json` still carrying `"recycle_workspaces": true` loads without error — Task 2 test `TestLoad_IgnoresRetiredRecycleWorkspacesKey`.

---

### Task 1: Remove the purge API and the recyclable indicator

**Files:**

- Modify: `internal/workspace/manager.go:1599-1639` (delete `Purge`, `PurgeAll`)
- Modify: `internal/workspace/interfaces.go:100-101`
- Modify: `internal/dashboard/handlers_dispose.go:220-287` (delete `handlePurgeWorkspace`, `handlePurgeAll`, `handleGetRecyclableWorkspaces`)
- Modify: `internal/dashboard/server.go:1041,1042,1171` (delete the three routes)
- Delete: `assets/dashboard/src/components/RecyclableIndicator.tsx`
- Modify: `assets/dashboard/src/routes/HomePage.tsx:44,724,1302`
- Modify: `assets/dashboard/src/lib/api.ts:1788-1814` (delete `RecyclableWorkspacesResponse`, `PurgeWorkspacesResponse`, `getRecyclableWorkspaces`, `purgeWorkspaces`)
- Modify: `docs/api.md:1166-1189,1232-1241` (delete the three endpoint sections; keep `backburner` at `:1190` and `share-intent` at `:1211`)
- Modify: `docs/workspaces.md` — delete the `### Purge API` (`:145`) and `### Dashboard behavior` (`:157`) subsections
- Test: `internal/workspace/manager_test.go` (delete `TestPurge_DeletesRecyclableWorkspace`, `TestPurge_RejectsNonRecyclableWorkspace`, `TestPurgeAll_DeletesAllRecyclable` at `:1737-1831`)
- Test: `internal/dashboard/handlers_dispose_test.go` (delete `TestHandlePurgeWorkspace`, `TestHandlePurgeAll` at `:397-449`)

**Interfaces:**

- Consumes: nothing.
- Produces: `workspace.WorkspaceManager` interface without `Purge`/`PurgeAll`. `dispose(ctx, id, force, skipRecycling, keepLocalBranch)` is untouched here (Task 2 changes it).

- [x] **Step 1: Delete the backend code and its tests listed above.**

- [x] **Step 2: Build and vet**

Run: `go build ./... && go vet ./internal/workspace/ ./internal/dashboard/`
Expected: no output, exit 0.

- [x] **Step 3: Run the affected Go packages**

Run: `go test ./internal/workspace/ ./internal/dashboard/`
Expected: `ok` for both.

- [x] **Step 4: Delete the frontend indicator, its two `HomePage` usages and import, and the `api.ts` block.**

- [x] **Step 5: Run quick tests**

Run: `./test.sh --quick`
Expected: all backend and frontend suites pass.

- [x] **Step 6: Edit `docs/api.md` and `docs/workspaces.md` as listed.**

- [x] **Step 7: Check nothing references the removed symbols**

Run: `grep -rnE "handlePurge|getRecyclableWorkspaces|purgeWorkspaces|RecyclableIndicator|PurgeAll|\bPurge\(|workspaces/recyclable|workspaces/purge|/purge\b" internal cmd assets/dashboard/src docs --include='*.go' --include='*.ts' --include='*.tsx' --include='*.md' | grep -v "docs/superpowers/"`
Expected: no output.

---

### Task 2: Remove the `recycle_workspaces` setting and the recycle paths it gates

**Files:**

- Modify: `internal/config/config.go:127` (delete `RecycleWorkspaces`)
- Modify: `internal/api/contracts/config.go:217,439`; regenerate `assets/dashboard/src/lib/types.generated.ts`
- Modify: `internal/dashboard/handlers_config.go:290,1045-1046`
- Modify: `internal/workspace/manager.go`:
  - `:465-466` reword the per-repo lock comment (it says the lock protects Tier 0; it protects reuse from concurrent callers claiming the same workspace)
  - `:471-531` delete Tier 0 (keep the per-repo lock above it); rewrite the `:533` comment so it no longer mentions Tier 0
  - `:597-606` delete step 2 and the `purgeRecyclableWithBranch` call; keep the stale-worktree prune and renumber the comment
  - `:645-652` delete the purge-and-retry after `create()`
  - `:1562-1577` delete `purgeRecyclableWithBranch`
  - `:1641-1645` change `dispose` to `dispose(ctx context.Context, workspaceID string, force bool, keepLocalBranch bool) error`; update `Dispose` → `dispose(ctx, id, false, false)`, `DisposeForce` → `dispose(ctx, id, true, false)`, `DisposeForcePurge` → `dispose(ctx, id, true, true)`, and drop "without recycling" from their doc comments
  - `:1688` drop "and recycling" from the zombie comment; `:1720-1746` delete the recycle branch
- Modify: `internal/daemon/daemon.go:1238-1242` comment: stuck `disposing` records are retried with `DisposeForce`, which deletes them
- Modify: `internal/disposal/service.go:37` comment: drop "without recycling"
- Modify: `internal/workspace/overlay.go:253` comment: drop the recycling clause
- Modify: `assets/dashboard/src/routes/config/WorkspacesTab.tsx:10,24,77-~90` (prop and the checkbox `form-group` with its hint)
- Modify: `assets/dashboard/src/routes/config/useConfigForm.ts:119,335`, `buildConfigUpdate.ts:14`, `ConfigPage.tsx:146,1224`
- Modify: `docs/web.md:289`, `docs/api.md:1291,1572` (config examples), `docs/architecture.md:266`, `docs/workspaces.md` (Tier 0 row `:94`; subsections "Why they exist" `:105`, "How it works" `:109`, "Dispose path" `:119`; gotchas `:740-742`; reword `:334`)
- Test: `internal/config/config_test.go:2895-2918`, `internal/config/disable_auth_raw_test.go`, `internal/dashboard/handlers_config_authenable_test.go:111-114`, `internal/dashboard/api_contract_test.go:568-…`, `internal/workspace/manager_test.go:1410-1536`, `internal/workspace/manager_integration_test.go`, `internal/workspace/delete_remote_branch_test.go:207-240`, `internal/workspace/force_purge_test.go:18-…`, `assets/dashboard/src/routes/config/WorkspacesTab.test.tsx:12`

**Interfaces:**

- Consumes: Task 1's interface (no `Purge`).
- Produces: `func (m *Manager) dispose(ctx context.Context, workspaceID string, force bool, keepLocalBranch bool) error`. `config.Config` without `RecycleWorkspaces`. `state.WorkspaceStatusRecyclable` still exists (Task 3 removes it).

- [x] **Step 1: Replace `TestRecycleWorkspaces_ParsesFromJSON` with the retired-key test**

In `internal/config/config_test.go`, delete `TestRecycleWorkspaces_DefaultFalse` and replace `TestRecycleWorkspaces_ParsesFromJSON` with:

```go
func TestLoad_IgnoresRetiredRecycleWorkspacesKey(t *testing.T) {
	t.Parallel()
	jsonData := `{"workspace_path": "/tmp/test", "recycle_workspaces": true, "repos": [], "run_targets": []}`
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte(jsonData), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a config carrying retired recycle_workspaces", err)
	}
	if cfg.WorkspacePath != "/tmp/test" {
		t.Errorf("WorkspacePath = %q, want %q", cfg.WorkspacePath, "/tmp/test")
	}
}
```

- [x] **Step 2: Swap the incidental field in two tests**

`disable_auth_raw_test.go`: replace `"recycle_workspaces": true` with `"local_echo_remote": true`, the raw struct field with `LocalEchoRemote bool \`json:"local_echo_remote"\``, and the assertion message with `"expected unrelated field local_echo_remote preserved"`.
`handlers_config_authenable_test.go:111-114`: replace `recycle := true`/`RecycleWorkspaces: &recycle`with`echo := true`/`LocalEchoRemote: &echo`.

- [x] **Step 3: Delete the setting-only tests**

- `api_contract_test.go`: `TestAPIContract_ConfigUpdatePersistsRecycleWorkspaces`
- `manager_test.go`: `TestDispose_RecycleWorkspaces_KeepsDirectory`, `TestDispose_RecycleWorkspaces_ForceStillRecycles`, `TestDispose_RecycleDisabled_StillDeletesDirectory`
- `manager_integration_test.go`: `TestGetOrCreate_RecyclableWorkspace_ReusedBeforeCreate`, `TestGetOrCreate_RecyclableLocalRepo_Reused`, `TestGetOrCreate_RecyclableBranchCollision_PurgesAndRetries`, `TestGetOrCreate_BranchReuse_PurgesConflictingRecyclable`, `TestGetOrCreate_RecycleSameDivergedBranch`, `TestGetOrCreate_RecycleAutoSyncsFromDefault`. Keep `TestGetOrCreate_BranchReuse_PromotesRecyclableStatus` (Task 3 renames it).
- `delete_remote_branch_test.go`: `TestDeleteRemoteBranch_RecyclingKeepsWorkspace`; in `TestDeleteRemoteBranch_ThenDisposeReapsLocalBranch` delete `m.config.RecycleWorkspaces = false` and change the comment to "disposal now reaps the local branch, because cleanupLocalBranch no longer sees a remote branch to preserve it for."
- `force_purge_test.go`: drop the `recycle` column and the `{"recycling on is bypassed", …}` row, the `m.config.RecycleWorkspaces = tt.recycle` line, and change the `:48` message to `"workspace still in state after DisposeForcePurge"`.

- [x] **Step 4: Remove the config field from `config.go`, both contracts, and `handlers_config.go`; regenerate types**

Run: `go run ./cmd/gen-types`
Expected: `types.generated.ts` loses both `recycle_workspaces?: boolean;` lines.

- [x] **Step 5: Make the `manager.go`, `daemon.go`, `service.go`, `overlay.go` edits listed in Files.**

- [x] **Step 6: Build, vet, run Go tests**

Run: `go build ./... && go vet ./... && go test ./internal/config/ ./internal/workspace/ ./internal/dashboard/ ./internal/disposal/ ./internal/flights/ ./internal/daemon/`
Expected: `ok` for each package, including `TestLoad_IgnoresRetiredRecycleWorkspacesKey`, `TestDisposeForcePurge_RemovesWorkspaceAndKeepsEveryBranch`, `TestDispose`.

- [x] **Step 7: Remove the frontend field and checkbox; update `WorkspacesTab.test.tsx:12` to drop `recycleWorkspaces`.**

- [x] **Step 8: Run quick tests**

Run: `./test.sh --quick`
Expected: all suites pass.

- [x] **Step 9: Edit the docs listed in Files.**

- [x] **Step 10: Check nothing references the setting or the recycle path**

Run: `grep -rnE "RecycleWorkspaces|recycle_workspaces|recycleWorkspaces|skipRecycling|purgeRecyclableWithBranch|Tier 0" internal cmd assets/dashboard/src docs --include='*.go' --include='*.ts' --include='*.tsx' --include='*.md' | grep -v "docs/superpowers/" | grep -v "config_test.go"`
Expected: no output. (`config_test.go` keeps the literal key inside `TestLoad_IgnoresRetiredRecycleWorkspacesKey`.)

---

### Task 3: Remove the `"recyclable"` status and pin leftover-record behavior

**Files:**

- Modify: `internal/state/state.go:93` (delete `WorkspaceStatusRecyclable`)
- Modify: `internal/dashboard/handlers_sessions.go:88-91` (delete the hide filter)
- Modify: `internal/dashboard/handlers_spawn.go:929-932` (delete the exclusion and its comment line)
- Modify: `internal/dashboard/handlers_buildmonitor.go:454,460`
- Modify: `internal/workspace/manager.go:563,628` (drop "recyclable and" from the promote comments), `:1271` (the race is with dispose removing or re-statusing the record), `:1412` (keep the `Disposing` skip), `:1493-1495` (delete the skip)
- Modify: `internal/daemon/daemon.go:1665-1667` (keep the `Disposing` skip; reword the comment)
- Modify: `internal/workspace/vcs_sapling_test.go:259` comment ("reuse-friendly path")
- Modify: `docs/api.md:346,617,1127`, `docs/sessions.md:73`, `docs/overlays.md:49`, `docs/flights.md:269,274`, `docs/remote.md:213`, `docs/README.md:36`, `test/TESTING.md:38,40,41`
- Modify: `docs/workspaces.md`: fix the Tier 2 row (`:96`) to say it matches only workspaces whose status is not `running`; rename `## Recyclable Workspaces` to `## Disposal`, keeping "Shared full-workspace disposal", "Force purge", "Flight ownership", and "Crash recovery" (reworded: stuck `disposing` records are deleted on startup); reword `:323,754,761`
- Test: `internal/dashboard/handlers_sessions_test.go:13`, `internal/dashboard/handlers_spawn_test.go:318,391`, `internal/workspace/manager_test.go` (replace `TestUpdateAllVCSStatus_SkipsRecyclable`; add `TestDispose_LeftoverRecyclableRecordIsDeleted`), `internal/workspace/manager_integration_test.go:530`

**Interfaces:**

- Consumes: Task 2's `dispose` signature and `config.Config` without the field.
- Produces: nothing new; `state` exports no recyclable constant. Tests write the status as the literal `"recyclable"`.

- [x] **Step 1: Write the leftover-record tests**

Replace `TestBuildSessionsResponse_ExcludesRecyclableWorkspaces` with `TestBuildSessionsResponse_IncludesLeftoverRecyclableWorkspace`: same two workspaces, second has `Status: "recyclable"`; assert both `"active-001"` and `"recycled-001"` appear in `buildSessionsResponse()`, failing with `"leftover recyclable workspace %q missing from buildSessionsResponse"`.

Replace `TestHandleCheckBranchConflict_RecyclableWorkspaceNoConflict` with `TestHandleCheckBranchConflict_LeftoverRecyclableWorkspaceConflicts`: same setup with `Status: "recyclable"`; assert `result.Conflict == true` and `result.WorkspaceID == "repo-001"`.

Replace `TestUpdateAllVCSStatus_SkipsRecyclable` with `TestUpdateAllVCSStatus_PollsLeftoverRecyclable`: same setup with `Status: "recyclable"`, plus `os.WriteFile(filepath.Join(recyclablePath, "untracked.txt"), []byte("x"), 0o644)` before polling; after `m.UpdateAllVCSStatus(ctx)` assert `w.Dirty == true` (porcelain-non-empty → dirty, `manager.go:1331`), reporting `w.Dirty` and `w.Status` on failure.

Add `TestDispose_LeftoverRecyclableRecordIsDeleted` to `manager_test.go`, modeled on `TestDispose` (`:234`): `git init` a workspace dir, `AddWorkspace` with `Status: "recyclable"`, call `m.Dispose(ctx, id)`; assert `err == nil`, `os.Stat(path)` is `IsNotExist`, and `st.GetWorkspace(id)` returns `found == false`.

- [x] **Step 2: Run them to confirm the first three fail**

Run: `go test ./internal/dashboard/ ./internal/workspace/ -run 'TestBuildSessionsResponse_IncludesLeftoverRecyclableWorkspace|TestHandleCheckBranchConflict_LeftoverRecyclableWorkspaceConflicts|TestUpdateAllVCSStatus_PollsLeftoverRecyclable|TestDispose_LeftoverRecyclableRecordIsDeleted' -v`
Expected: the first three FAIL (filtered out / no conflict / not dirty); `TestDispose_LeftoverRecyclableRecordIsDeleted` PASSES (it pins behavior Task 2 already produced). Any other result: stop and report the observed output; do not adjust the assertions.

- [x] **Step 3: Rename and de-constant the kept integration test**

`TestGetOrCreate_BranchReuse_PromotesRecyclableStatus` → `TestGetOrCreate_BranchReuse_PromotesLeftoverRecyclableRecord`; `w.Status = "recyclable"`; replace the two setup comments with "A leftover record from before recycling was removed"; keep the assertions.

- [x] **Step 4: Delete `TestHandleSpawnPost_RecyclableBranchNotBlocked` (`handlers_spawn_test.go:391`).**

- [x] **Step 5: Delete the constant and every filter listed in Files.**

- [x] **Step 6: Run the tests**

Run: `go build ./... && go vet ./... && go test ./internal/dashboard/ ./internal/workspace/ ./internal/daemon/ ./internal/state/`
Expected: `ok` for each; the four Step 1 tests and `TestGetOrCreate_BranchReuse_PromotesLeftoverRecyclableRecord` pass.

- [x] **Step 7: Edit the docs listed in Files.**

- [x] **Step 8: Check the constant is gone**

Run: `grep -rn "WorkspaceStatusRecyclable" internal cmd`
Expected: no output.

---

### Task 4: Full gates and final sweep

**Files:** none new; fixes only where a gate points.

- [x] **Step 1: Final grep**

Run: `grep -rniE "recycl" --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git . | grep -vE "^./CHANGES.md|^./docs/superpowers/|^./internal/session/reaper.go"`
Expected: only the leftover-record tests from Tasks 2-3 (`config_test.go`, `handlers_sessions_test.go`, `handlers_spawn_test.go`, `manager_test.go`, `manager_integration_test.go`), each using the literal `recyclable`/`recycle_workspaces`.

- [x] **Step 2: Format**

Run: `./format.sh`
Expected: exit 0.

- [x] **Step 3: Full test suite**

Run: `./test.sh`
Expected: unit, frontend, E2E, scenarios all pass.

- [x] **Step 4: Static analysis**

Run: `./badcode.sh`
Expected: exit 0 (deadcode, staticcheck, knip, tsc clean — catches any orphaned helper).

- [x] **Step 5: Test rubric review**

Run the `test-rules-review` skill on the changed test files.
Expected: no-violations verdict.

- [x] **Step 6: Dashboard style check**

Run the `dashboard-style-check` skill on the `WorkspacesTab.tsx` and `HomePage.tsx` changes.
Expected: no findings (removal only).

- [x] **Step 7: Report to the user**

Paste the Step 1-6 outputs. The spec and this plan remain until the user runs `/finalize`; until then report "Not done: spec and plan files not yet finalized".
