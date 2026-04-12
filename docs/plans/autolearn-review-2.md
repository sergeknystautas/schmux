VERDICT: NEEDS_REVISION

## Summary Assessment

The revision addressed most of the 12 critical issues from Review 1 (line references, missing files, frontend scope, commit workflow, etc.), but introduced a critical compile-gap ordering problem in Group 1 and has a dependency ordering error between Groups 6 and 7. The source code verification confirmed all four requested references exist, but also revealed that `handlers_emergence.go` cannot survive the Step 4 field renames.

## Critical Issues (must fix)

### 1. Compile gap: `handlers_emergence.go` breaks after Step 4c field renames

After Step 4b extracts the 11 spawn-related handlers into `handlers_spawn.go`, three functions remain in `handlers_emergence.go`: `handleEmergenceCurate` (line 310), `runEmergenceCuration` (line 354), and `TriggerEmergenceCuration` (line 431). These functions reference:

- `s.emergenceStore` (renamed to `s.spawnStore` in Step 4c)
- `s.emergenceMetadataStore` (renamed to `s.spawnMetadataStore` in Step 4c)
- `s.loreExecutor` (not renamed until Step 16a)
- `emergence.CollectIntentSignals`, `emergence.BuildEmergencePrompt`, `emergence.ParseEmergenceResponse`, `emergence.GenerateSkillFile` (the `emergence` package import is still needed)

Step 4c says to "Remove the curate route" and "Update import from `internal/emergence` to `internal/spawn`" on `server.go`, but `handlers_emergence.go` (a separate file in the same package) still needs the `emergence` import and uses the old field names. The build check in Step 4-final (`go build ./...`) will fail.

**Fix**: Either (a) leave `handlers_emergence.go` completely untouched in Step 4 -- do not rename any server fields or imports that it depends on, and defer all renames to Step 16/20 when the emergence file is about to be deleted, or (b) explicitly handle the remaining emergence functions in Step 4 by stubbing them out or moving them to a temporary compatibility shim.

### 2. Dependency ordering: Step 20 uses `cfg.GetAutolearnEnabled()` before Step 23 creates it

Step 20 (Group 6, "Daemon wiring") says: "Use `cfg.GetAutolearnEnabled()` (new config method, added in Step 23)". But Step 23 is in Group 7, which comes after Group 6. The daemon wiring cannot compile without the config accessor methods.

**Fix**: Move the config changes (Step 23) to Group 5 or early Group 6, before the daemon wiring step. Alternatively, have Step 20 continue using `cfg.GetLoreEnabled()` temporarily and rename in Step 23.

### 3. Spec divergence: `prompt-history` route missing from autolearn route group

The spec lists `GET /api/autolearn/{repo}/prompt-history` as an autolearn route (for "intent signal history") AND `GET /api/spawn/{repo}/prompt-history` as a spawn route (for "prompt autocomplete data"). These serve different purposes in the spec. The plan's Step 16c route table omits `prompt-history` from autolearn routes entirely, with the note "prompt-history moves to the spawn route group." This drops the intent signal history endpoint from the spec's API surface. Either the spec is wrong (and should be updated), or the plan needs to add the autolearn prompt-history route.

## Suggestions (nice to have)

### 1. Step 4f has a wrong claim about `handlers_features.go`

Step 4f says to "Remove `import 'internal/emergence'` (not needed after spawn extraction)" from `handlers_features.go`. Verified: this file does NOT import `internal/emergence` -- it imports `internal/lore` (line 12), which calls `lore.IsAvailable()` at line 37. The `emergence` removal claim is incorrect. The actual lore import update is correctly handled later in Step 22, so this sub-step is a no-op but is misleading.

### 2. `TriggerEmergenceCuration` is not mentioned in the handler mapping

Step 16b's handler mapping covers `handleEmergenceCurate` ("absorbed into `handleAutolearnCurate`") but never mentions `TriggerEmergenceCuration` (handlers_emergence.go line 431), which is called from `daemon.go` line 1297. Step 20 item 6 mentions replacing the call site but does not mention that the method definition itself must be ported or replaced. The new `handlers_autolearn.go` presumably needs an equivalent `TriggerAutolearnCuration` method, or the unified callback absorbs it entirely. This should be stated explicitly.

### 3. `docs/api.md` update timing and CI enforcement

The plan acknowledges in Step 26e that `docs/api.md` should "ideally be updated incrementally alongside handler changes" since CI enforces co-changes. However, the plan still places the full api.md update at Step 26e (end-to-end verification). If the `/commit` workflow runs CI checks at each step, every handler step (16, 17, 20, 22) will fail the `check-api-docs.sh` script. The plan should either (a) update api.md in Step 16f alongside the handler commit, or (b) note that api.md must be touched in each handler-changing commit to pass CI.

### 4. `handleLoreApplyMerge` removal could use more justification

Step 16b's "Removed handler" section explains that `handleLoreApplyMerge` is replaced by "the per-learning approve+apply flow and the unified merge+push flow." This is adequate but terse. The old handler applied a reviewed merge to a specific proposal (proposal-scoped). In the new model, the equivalent flow is: approve individual learnings -> trigger merge -> review pending merge -> push. It might help to add a one-line note mapping the old user workflow to the new one, for the implementer's clarity.

## Verified Claims (things you confirmed are correct)

### Review 1 issue resolution

1. **Issue 1 (loreStore line reference)**: Fixed. The plan now correctly states `loreStore` is at line 212 (Step 16a), not 243.
2. **Issue 2 (ensure/manager.go references)**: The ensure/manager references are correct throughout.
3. **Issue 3 (daemon.go instruction store line)**: Fixed. The plan now correctly references line 1125, not 498.
4. **Issue 4 (session/manager.go callback rename)**: Fixed. Step 20 items 7-10 explicitly rename `loreCallback` to `autolearnCallback`, `SetLoreCallback()` to `SetAutolearnCallback()`, and update the callback invocation at lines 1467-1473.
5. **Issue 5 (handlers_config.go refreshLoreExecutor)**: Fixed. Step 16d explicitly updates `s.refreshLoreExecutor(cfg)` at line 822 to `s.refreshAutolearnExecutor(cfg)`. Step 16b defines the `refreshAutolearnExecutor` method.
6. **Issue 6 (WebSocket event type rename)**: Fixed. Step 16b includes a "WebSocket event rename" section and Step 25f updates the frontend listener in `AutolearnPage.tsx`.
7. **Issue 7 (handleLoreApplyMerge)**: Fixed. Step 16b explicitly lists it as a "Removed handler" with justification.
8. **Issue 8 (handleLoreCurationsActive route placement)**: Fixed. Step 16c correctly places `curations/active` OUTSIDE the `{repo}` group, matching current API semantics.
9. **Issue 9 (frontend scope underestimate)**: Fixed. Step 25 now lists all 31 affected files, broken into 6 sub-steps (25a-25f).
10. **Issue 10 (ReadFileFromRepo source file)**: Fixed. Step 9 correctly states `ReadFileFromRepo()` is from `internal/lore/curator.go` (line 192), not `merge_curator.go`. Step 13 explicitly notes this.
11. **Issue 11 (SkillProposal cleanup in contracts)**: Fixed. Step 22 explicitly removes `SkillProposal` type from `contracts/emergence.go` and lists the spawn-related types to keep.
12. **Issue 12 (CollectPromptHistory extraction)**: Fixed. Step 4a explicitly extracts `CollectPromptHistory()` from `emergence/history.go` into `internal/spawn/history.go`.

### Source code verification

13. **`internal/session/manager.go`**: Confirmed `loreCallback` field at line 56, `SetLoreCallback()` method at line 188, callback invocation at lines 1467-1473. All line references in the plan are accurate.
14. **`internal/dashboard/handlers_config.go`**: Confirmed `s.refreshLoreExecutor(cfg)` call at line 822. Plan reference is accurate.
15. **`internal/emergence/history.go`**: Confirmed `CollectPromptHistory` exists at line 24 with signature `func CollectPromptHistory(workspacePaths []string, maxResults int) []contracts.PromptHistoryEntry`. Plan reference is accurate.
16. **`internal/api/contracts/emergence.go`**: Confirmed `SkillProposal` type exists at line 72. Plan reference is accurate.
17. **`handlers_emergence.go`**: Confirmed 12 functions total. The spawn handlers match Step 4b's extraction list. The remaining 3 functions (`handleEmergenceCurate`, `runEmergenceCuration`, `TriggerEmergenceCuration`) depend on `s.emergenceStore`, `s.loreExecutor`, and the `emergence` package import.
18. **`handlers_features.go`**: Confirmed it imports `internal/lore` (NOT `internal/emergence`). Step 4f's claim about removing an emergence import is wrong.
19. **`lore_merge_complete` WebSocket events**: Confirmed at handlers_lore.go lines 1011, 1024, 1041 (backend) and LorePage.tsx line 187 (frontend). Plan correctly identifies all locations.
20. **`nolore` build tag references in docs/CI**: Confirmed at `.claude/commands/commit.md` lines 119-120 and 146, `docs/dev/experimental-features.md` lines 55/63/66/69, and `docs/api.md` line 85. Plan's Step 22 correctly lists all locations.
