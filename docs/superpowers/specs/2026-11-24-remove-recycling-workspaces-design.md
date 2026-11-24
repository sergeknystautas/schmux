# Remove Recycling Workspaces — Design

## Intent

Recycling (`recycle_workspaces`, opt-in, default off) makes dispose keep the workspace: the directory and the `state.json` record stay, the status becomes `"recyclable"` (`state.WorkspaceStatusRecyclable`), and a dozen call sites filter that status out of the dashboard, polling, overlay propagation, build monitoring, and branch-conflict checks. The next spawn for the same repo checks it out to a new branch. The cost is a parallel lifecycle that every workspace consumer must remember to filter. Removing it makes dispose mean delete, everywhere.

## Goals

- `Dispose` always deletes the directory and the state record.
- No code, config field, API endpoint, UI element, or doc refers to recyclable workspaces.

## Non-goals

- No migration of leftover records — workspace records in an existing `state.json` whose status is `"recyclable"`. They reappear as ordinary workspaces; the user disposes or reuses them as they choose, unless a spawn reuses one first (Approach 1).
- No change to the matching rules of reuse Tier 1 — Same branch (`manager.go:539-571`) or Tier 2 — Different branch (`manager.go:573-640`), named as in `docs/workspaces.md:88`. Only their recyclable-specific steps and comments go (see the inventory).
- No change to remote persistent-host workspace reuse (`internal/remote`), which is a separate mechanism.
- No change to `DisposeForcePurge` (flight dispose).
- `CHANGES.md` and past specs/plans under `docs/superpowers/` are history; leave them.

## Anti-goals

- A startup sweep, compatibility shim, or status rewrite for leftover records.
- Keeping `Purge`/`PurgeAll` "for cleanup" — normal `Dispose` already deletes a leftover record (spike below).

## Spike

Question: with recycling off, does a workspace whose status is `"recyclable"` dispose through the normal `Dispose` path?
Answer: yes. Throwaway test, deleted after the run:

```
=== RUN   TestSpike_LeftoverRecyclableDisposesNormally
    zz_spike_test.go:28: dispose err=<nil> dirGone=true inState=false
--- PASS: TestSpike_LeftoverRecyclableDisposesNormally (0.08s)
```

## Approaches

1. **Delete outright, no migration (chosen).** Leftover records resurface as workspaces whose status no code recognizes. Worst case a spawn for the same repo reuses one before the user acts on it: Tier 1 matches any status on same repo + branch (`manager.go:484`), Tier 2 matches `Status != running` (`manager.go:520`). `prepare()` then discards its uncommitted and untracked files (`manager.go:915,920`), as recycling did. Two differences from recycling: a leftover whose `prepare()` fails can fail the spawn (Tier 0 skipped it), and stale gitignored overlay files are not removed.
2. **Delete plus startup sweep** that force-deletes leftover `"recyclable"` records next to the stuck-`disposing` retry (`internal/daemon/daemon.go:1238`). Rejected: the user prefers resurrected workspaces they can act on over silent deletion.
3. **Delete but keep `Purge`/`PurgeAll` and their endpoints.** Rejected: the spike shows `Dispose` already handles leftovers.

## Behavior after the change

- Existing `"recycle_workspaces": true` in `config.json` is ignored on load (config parsing does not reject unknown fields).
- Leftover records appear on the home page and sidebar alike (both read the `/ws/dashboard` broadcast; `WorkspaceStatusBadge.tsx` does not read `status`, so they render like any workspace), are polled for git status, receive overlay propagation, count for spawn branch-conflict checks (a spawn on a branch a leftover holds is reported as a conflict until the user disposes it), and dispose normally.
- `DELETE /api/workspaces/{id}/purge`, `DELETE /api/workspaces/purge`, and `GET /api/workspaces/recyclable` return 404 (routes gone).

## Change inventory

### Backend

| Location                                              | Change                                                                                                                             |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `internal/config/config.go:127`                       | Remove `RecycleWorkspaces`.                                                                                                        |
| `internal/api/contracts/config.go:217,439`            | Remove `RecycleWorkspaces` from response and update request; regenerate `types.generated.ts` with `go run ./cmd/gen-types`.        |
| `internal/dashboard/handlers_config.go:290,1045-1046` | Remove read and write of the field.                                                                                                |
| `internal/state/state.go:93`                          | Remove `WorkspaceStatusRecyclable`.                                                                                                |
| `internal/workspace/manager.go:471-531`               | Remove Tier 0. Rewrite the comment at `:533` (local repos no longer "moved after Tier 0"). Keep the per-repo lock.                 |
| `internal/workspace/manager.go:563,628`               | Drop "recyclable" from the "Promote to running" comments.                                                                          |
| `internal/workspace/manager.go:597-606`               | Remove step 2 (`purgeRecyclableWithBranch`); keep stale-worktree prune.                                                            |
| `internal/workspace/manager.go:645-652`               | Remove purge-and-retry after `create()` failure.                                                                                   |
| `internal/workspace/manager.go:1271`                  | Reword the re-read comment (the race is with dispose removing the record, not with recyclable).                                    |
| `internal/workspace/manager.go:1412,1493`             | Drop the recyclable status checks (keep the `Disposing` skip at `:1412`).                                                          |
| `internal/workspace/manager.go:1562-1577`             | Remove `purgeRecyclableWithBranch`.                                                                                                |
| `internal/workspace/manager.go:1599-1639`             | Remove `Purge` and `PurgeAll`.                                                                                                     |
| `internal/workspace/manager.go:1641-1645` and callers | Drop the `skipRecycling` parameter from `dispose()`; update `Dispose`, `DisposeForce`, `DisposeForcePurge` and their doc comments. |
| `internal/workspace/manager.go:1686-1746`             | Remove the recycle branch; reword the zombie comment at `:1688`.                                                                   |
| `internal/workspace/interfaces.go:100-101`            | Remove `Purge`, `PurgeAll` from the interface.                                                                                     |
| `internal/workspace/overlay.go:253`                   | Reword the comment that cites recycling.                                                                                           |
| `internal/dashboard/server.go:1041,1042,1171`         | Remove the three routes.                                                                                                           |
| `internal/dashboard/handlers_dispose.go:220-287`      | Remove `handlePurgeWorkspace`, `handlePurgeAll`, `handleGetRecyclableWorkspaces`.                                                  |
| `internal/dashboard/handlers_sessions.go:88-91`       | Remove the hide filter.                                                                                                            |
| `internal/dashboard/handlers_spawn.go:930-932`        | Remove the recyclable exclusion from the branch-conflict check.                                                                    |
| `internal/dashboard/handlers_buildmonitor.go:454,460` | Remove the recyclable exclusion and its comment mention.                                                                           |
| `internal/daemon/daemon.go:1238-1242`                 | Rewrite the comment: `DisposeForce` deletes.                                                                                       |
| `internal/daemon/daemon.go:1665-1667`                 | Keep the `Disposing` skip; drop recyclable.                                                                                        |
| `internal/disposal/service.go:37`                     | Reword `ForcePurge` comment ("without recycling" no longer meaningful).                                                            |

### Dashboard

| Location                                                        | Change                                                                                                          |
| --------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/components/RecyclableIndicator.tsx`       | Delete file.                                                                                                    |
| `assets/dashboard/src/routes/HomePage.tsx:44,724,1302`          | Remove import and both usages.                                                                                  |
| `assets/dashboard/src/lib/api.ts:1788-1814`                     | Remove `RecyclableWorkspacesResponse`, `PurgeWorkspacesResponse`, `getRecyclableWorkspaces`, `purgeWorkspaces`. |
| `assets/dashboard/src/routes/config/WorkspacesTab.tsx:10,24,77` | Remove prop and the checkbox `form-group` (label plus hint).                                                    |
| `assets/dashboard/src/routes/config/useConfigForm.ts:119,335`   | Remove form field.                                                                                              |
| `assets/dashboard/src/routes/config/buildConfigUpdate.ts:14`    | Remove field.                                                                                                   |
| `assets/dashboard/src/routes/ConfigPage.tsx:146,1224`           | Remove load and prop pass.                                                                                      |

`RecyclableIndicator.tsx` has no `className` attributes, so no CSS goes with it.

### Tests

Delete tests that exist only for recycling; edit tests that set the field or status incidentally.

- Pin leftover-record behavior (replace the old recyclable tests with their inverses): `TestRecycleWorkspaces_ParsesFromJSON` becomes a test that `Load` accepts a config still carrying `"recycle_workspaces": true`; `TestBuildSessionsResponse_ExcludesRecyclableWorkspaces` becomes includes; `TestHandleCheckBranchConflict_RecyclableWorkspaceNoConflict` becomes conflicts; `TestUpdateAllVCSStatus_SkipsRecyclable` becomes polls; `TestGetOrCreate_BranchReuse_PromotesRecyclableStatus` is kept (status written as the literal `"recyclable"`) because it is exactly the spec's worst case; a new test disposes a leftover record (the spike).
- Delete: `TestRecycleWorkspaces_DefaultFalse` (`config_test.go:2895`); `TestDispose_RecycleWorkspaces_KeepsDirectory`, `TestDispose_RecycleWorkspaces_ForceStillRecycles`, `TestDispose_RecycleDisabled_StillDeletesDirectory` (duplicates `TestDispose`, `manager_test.go:234`, once the field is gone), `TestPurge_*`, `TestPurgeAll_*` (`manager_test.go`); the six other `TestGetOrCreate_Recycl*`/`*Recyclable*` tests (`manager_integration_test.go:361-755`); `TestDeleteRemoteBranch_RecyclingKeepsWorkspace`; `TestAPIContract_ConfigUpdatePersistsRecycleWorkspaces`; `TestHandleSpawnPost_RecyclableBranchNotBlocked` (spawn POST never blocks on branch, whatever the status); `TestHandlePurgeWorkspace`; `TestHandlePurgeAll`.
- Edit incidental references in `force_purge_test.go` (drop the `recycle` table column), `delete_remote_branch_test.go:207-209`, `vcs_sapling_test.go:259` (comment), `handlers_config_authenable_test.go:111-114` and `disable_auth_raw_test.go` (swap `recycle_workspaces` for the unrelated `local_echo_remote`), `WorkspacesTab.test.tsx:12`. `flights/dispose_test.go` and `disposal/service_test.go` mention only `ForcePurge`, which stays.
- `test/TESTING.md:38,40,41`: remove rows for deleted tests.

### Docs

| Location                             | Change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/workspaces.md`                 | Remove Tier 0 row (`:94`). Correct the Tier 2 row (`:96`): it matches only workspaces whose status is not `running` (`manager.go:579`), which the row omits today. Under `## Recyclable Workspaces` (`:103`), remove "Why they exist", "How it works", "Dispose path", "Purge API", "Dashboard behavior"; keep "Shared full-workspace disposal" (`:123`), "Force purge" (`:137`), "Flight ownership" (`:141`), and "Crash recovery" (`:163`, reworded: stuck `disposing` records are deleted) under a renamed `## Disposal` heading. Remove the gotchas at `:740-742`; reword `:323,334,754,761`. |
| `docs/api.md:346,617,1127,1291,1572` | Remove `recyclable` status and `recycle_workspaces` from config examples; reword "recycled" mentions.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| `docs/api.md:1166-1189,1232-1241`    | Remove the `DELETE .../{workspaceId}/purge`, `DELETE /api/workspaces/purge`, and `GET /api/workspaces/recyclable` sections. Keep `backburner` (`:1190`) and `share-intent` (`:1211`) between them.                                                                                                                                                                                                                                                                                                                                                                                                |
| `docs/web.md:289`                    | Remove the checkbox description.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `docs/sessions.md:73`                | Remove the `recyclable` row.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `docs/overlays.md:49`                | Drop `Recyclable`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `docs/flights.md:269,274`            | Drop recycling mentions.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `docs/remote.md:213`                 | Drop "(identical to local workspace recycling)".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `docs/architecture.md:266`           | Drop `recycle_workspaces` from key areas if listed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `docs/README.md:36`                  | Drop "recyclable workspaces".                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

`docs/api.md` must change in the same diff (CI-enforced by `scripts/check-api-docs.sh`).

## Left to the builder

- Order of edits and how to split into plan tasks.
- Exact rewording of comments and docs listed above.
- Finding any remaining reference: the final check is `grep -rniE "recycl" --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=.git .` returning only `CHANGES.md`, `docs/superpowers/` files, the leftover-record tests and the `test/TESTING.md` row naming one of them, and the unrelated PGID comments in `internal/session/reaper.go:229-232`.

## Verification

- `./test.sh` passes.
- `./badcode.sh` passes (deadcode, knip, tsc catch orphaned helpers and CSS).
- The grep above returns only the allowed hits.
