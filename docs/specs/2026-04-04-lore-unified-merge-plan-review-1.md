VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is well-structured with correct function references and a sound architectural approach, but has several code-level bugs that will cause compilation failures, a missing test import, an incomplete mutex strategy, a significant gap in invalidation logic, and Step 8 (740-line LorePage rewrite) is too large to be a single task.

## Critical Issues (must fix)

### 1. Step 6 push handler has a compilation error: `instrFiles` redeclared

In `handleLorePush` (Step 6), the variable `instrFiles` is declared twice in the same function scope:

- Line 832 (inside the freshness check block): `instrFiles := s.config.GetLoreInstructionFiles()`
- Line 870 (after the freshness check): `instrFiles := s.config.GetLoreInstructionFiles()`

The first declaration is inside an `if` block so it is technically a different scope, but the second declaration at line 870 is at function level alongside the first usage of `targetFile` at line 833. This will either shadow or redeclare depending on exact block nesting. More importantly, the freshness-check block (lines 828-849) declares its own `targetFile` as well as `instrFiles`, while the outer scope (lines 870-871) re-declares both. The code should compute `instrFiles` and `targetFile` once at the top of the function and reuse them. As written, a Go compiler with strict unused-variable checking or a linter will flag this, and even if it compiles, the duplication is a maintenance hazard.

### 2. Step 6 test (`TestHandleLorePush_Success`) requires `time` import but test file does not import it

The existing test file (`internal/dashboard/handlers_lore_test.go`) imports: `bytes`, `context`, `encoding/json`, `io`, `net/http`, `net/http/httptest`, `os`, `os/exec`, `path/filepath`, `strings`, `testing`. It does **not** import `"time"`. Step 6's test uses `time.Now().UTC()` on line 718 (`CreatedAt: time.Now().UTC()`). The plan never mentions adding this import. This will cause a compilation error.

### 3. Step 1 `PendingMergeStore` has inconsistent mutex usage

Only `UpdateEditedContent` acquires the mutex. `Save`, `Get`, and `Delete` do not. This means:

- The background merge goroutine (Step 5) calling `Save` races with a user calling `UpdateEditedContent` (which does `Get` then `Save` under the mutex).
- Two concurrent `Save` calls (e.g., from two browser tabs triggering merge) can corrupt the file.

The design document explicitly says "PendingMerge operations are protected by a sync.Mutex" and warns about "background merge goroutine writing results" and "concurrent access from multiple browser tabs." The implementation only protects one method. Either all public methods should hold the mutex, or the store should document that callers are responsible for serialization (which they currently are not).

### 4. Step 8 (LorePage rewrite) is too large for a single 2-5 minute task

`LorePage.tsx` is 740 lines. Step 8 requires:

- Removing `mergePreviews`, `editedPreviews` state
- Adding `pendingMerges` state with fetch logic
- Rewriting phase derivation
- Rewriting `handleApply` to call the unified merge endpoint
- Rewriting `handleCommitAndPush` to call the push endpoint with error handling
- Adding Diff/Edit toggle with debounced PATCH
- Adding `lore_merge_complete` WebSocket listener
- Removing `MergeReviewItem` type and per-proposal merge review rendering

This is easily 20-30 minutes of focused work, not 2-5. It should be broken into at least 3 sub-steps:

- 8a: Add pending merge loading and phase derivation
- 8b: Rewrite handleApply and handleCommitAndPush
- 8c: Add Diff/Edit toggle with debounced PATCH and WebSocket listener

### 5. Missing invalidation when rules change after merge

The design document explicitly requires: "The PendingMerge is invalidated (deleted) when... the included rules change (a rule is unapproved, edited, or dismissed after the merge was computed)."

The plan handles this at push time (Step 6, server-side rule validation before push) but does **not** implement proactive invalidation. Specifically:

- The `handleLoreRuleUpdate` endpoint (which handles approve/dismiss/edit of individual rules) should check whether the modified rule is referenced by an active `PendingMerge` and, if so, delete or mark the `PendingMerge` as stale.
- Without this, the user can dismiss a rule, the PendingMerge still shows as "ready" with a diff that includes the dismissed rule's content, and only gets rejected at push time. This is a confusing UX.

The plan should add a task (after Step 4, before Step 8) to wire invalidation into `handleLoreRuleUpdate` and `handleLoreDismiss`.

### 6. Step 4 (DELETE and PATCH endpoints) skips the TDD cycle

Step 4 says "4a. Write implementation" with no tests. The plan's own convention is failing test, then implementation, then verify. Steps 1, 3, and 6 follow TDD, but Step 4 skips it entirely. At minimum, the PATCH endpoint (which has non-trivial semantics: editing a pending merge that might not exist) should have a test.

### 7. Frontend API functions use wrong `parseErrorResponse` signature

In Step 7, every API function calls `parseErrorResponse(resp)` with one argument:

```typescript
if (!resp.ok) throw await parseErrorResponse(resp);
```

But the actual function signature is:

```typescript
export async function parseErrorResponse(response: Response, fallback: string): Promise<never>;
```

It requires two arguments: `response` and `fallback`. Every call in Step 7 is missing the fallback string. This will cause TypeScript compilation errors. Each call needs a fallback like `parseErrorResponse(resp, 'Failed to fetch pending merge')`.

## Suggestions (nice to have)

### S1. Step 5 has no test at all

Step 5 (`handleLoreUnifiedMerge`) is the core merge orchestration endpoint and has zero tests. The plan says "5b. Verify build" but no test. While it is a complex handler with a background goroutine, at least a test that verifies the 400/409 error paths and the initial "merging" PendingMerge creation would catch regressions. The happy path test is harder because of the LLM executor mock, but the error paths are straightforward.

### S2. Consider using `:=` consistently for `instrFiles` / `targetFile` in Step 6

The push handler code should compute `instrFiles` and `targetFile` once near the top of the function and reuse them in both the freshness check and the write section. This avoids the redeclaration issue and makes the code clearer.

### S3. Step 10 location is vague

The plan says "Modify `internal/daemon/daemon.go` (or wherever daemon startup runs)" and proposes calling `cleanupOrphanedLoreWorktrees()` from "the daemon's `Start` or `Run` method." The `Start()` function (line 161) only forks the daemon process; the actual initialization is in `Daemon.Run()` (line 333). The cleanup should go in `Run`, early in the function, after the home directory is resolved (around line 360).

### S4. Step 9 test updates are underspecified

Step 9 says "Remove or rewrite `TestHandleLoreApplyMerge_RepoPublic_WorkspaceBased`" and similar, but does not specify what the rewritten tests should look like. Since these tests verify important behavior (push flow, conflict detection), the plan should specify what replaces them.

### S5. The plan does not address `LoreMergePreview` TypeScript type removal

The frontend imports `LoreMergePreview` from `types.ts` (line 27 of LorePage.tsx). Step 9 removes `MergePreview` from the Go side and the `MergeReviewItem` interface from LorePage, but does not mention removing or updating the `LoreMergePreview` TypeScript type definition. This could leave dead types in the codebase.

### S6. PendingMerge should be a TypeScript type

The plan adds a `PendingMerge` Go struct but never adds a corresponding TypeScript type. The frontend code in Step 8 uses `Record<string, any>` (`pendingMerges: Record<string, any>`). A proper `PendingMerge` TypeScript interface should be defined in `assets/dashboard/src/lib/types.ts` (for manual types) or the struct should be added to `internal/api/contracts/` and generated via `go run ./cmd/gen-types`.

## Verified Claims (things you confirmed are correct)

1. **Function signatures exist and match**: `BuildMergePrompt(currentContent string, rules []Rule)` at `internal/lore/merge_curator.go:21`, `ReadFileFromRepo(ctx, repoDir, relPath)` at `internal/lore/curator.go:182`, `ParseMergeResponse(response string)` at `internal/lore/merge_curator.go:56` -- all match the plan's usage.

2. **Daemon startup code location**: `SetLoreStore`, `SetLoreInstructionStore`, and `SetLoreExecutor` are called at lines 1071, 1075, and 1085 of `internal/daemon/daemon.go`, inside `Daemon.Run()`. The plan correctly identifies this as the wiring location for Step 2.

3. **Route registration pattern**: The plan correctly follows the existing pattern. Routes are registered inside `r.Route("/lore/{repo}", func(r chi.Router) { ... })` at line 743 of `server.go`, using `r.Get(...)`, `r.Post(...)`, etc. The `validateLoreRepo` middleware is already applied.

4. **Server struct field placement**: The plan correctly identifies line 199 for `loreInstructionStore` and proposes adding `lorePendingMergeStore` nearby. The setter method pattern (`SetLorePendingMergeStore`) matches the existing `SetLoreStore`, `SetLoreInstructionStore`, and `SetLoreExecutor` patterns.

5. **`validateRepoName` is accessible**: It is defined at `internal/lore/proposals.go:200` in the `lore` package, so it is accessible from `pending_merge.go` in the same package.

6. **`MergePreviews` and `MergeError` fields exist**: Confirmed at `internal/lore/proposals.go:127-128`, and the `MergePreview` type at line 143. Step 9's removal targets are accurate.

7. **Frontend `apiFetch` and `csrfHeaders` exist**: `apiFetch` at `assets/dashboard/src/lib/api.ts:63`, `csrfHeaders` imported from `./csrf` at line 58. The plan's API function patterns are consistent with existing usage.

8. **`BroadcastCuratorEvent` and `CuratorEvent` exist**: Confirmed at `server.go:1507-1508` and `curation_state.go:22`. The plan's usage of these for WebSocket broadcast is correct.

9. **Test boilerplate matches**: The plan's test setup (NewServer constructor, SetModelManager, chi route context, httptest.NewRecorder) matches the existing test patterns in `handlers_lore_test.go`.

10. **`AllRulesResolved`, `ProposalApplied`, `RuleApproved`, `LayerRepoPublic`, `MergedAt`, `EffectiveLayer` all exist** at the referenced locations in `internal/lore/proposals.go`.
