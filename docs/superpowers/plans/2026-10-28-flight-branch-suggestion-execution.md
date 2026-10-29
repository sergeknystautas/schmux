# Execution report — flight branch suggestion

Plan: `docs/superpowers/plans/2026-10-28-flight-branch-suggestion.md`
Spec: `docs/superpowers/specs/2026-10-28-flight-branch-suggestion-design.md`
Executed with `superpowers:executing-plans`, inline, in this worktree.

## What was built

Taking off a flight no longer creates a `schmux/flight/<flight-id>` branch. The
first takeoff of a Git flight asks a new `flights.BranchSuggester` dependency
for a name derived from the flight prompt and hands that name to the existing
`workspace.Manager.GetOrCreateWithLabel`, exactly as the spawn form does. No
name is a takeoff failure; a retry reuses the branch the flight already
recorded. Flight disposal can now delete the branch the flight recorded, so
`CleanupFlightBranch` no longer requires a `schmux/flight/` prefix.

### Production changes

| File                                      | Change                                                                                                                                                                                                                     |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/flights/manager.go:64-68`       | New `BranchSuggester` interface: `SuggestBranch(ctx, prompt) (string, error)`.                                                                                                                                             |
| `internal/flights/manager.go:92-94`       | `Dependencies` gains `Branches BranchSuggester`; a nil value behaves as an unconfigured suggestion.                                                                                                                        |
| `internal/flights/transitions.go:202-211` | `Manager.suggestBranch` returns `oneshot.ErrDisabled` for a nil suggester, otherwise delegates.                                                                                                                            |
| `internal/flights/transitions.go:241-256` | Branch selection in `Takeoff`: Sapling keeps branch `""` and the display-name label; a recorded `RequestedBranch` is reused on retry; otherwise the suggester is asked and a failure returns `suggest branch name: <err>`. |
| `internal/flights/transitions.go:213-219` | `Takeoff` doc comment rewritten to describe suggested naming and retry reuse.                                                                                                                                              |
| `internal/daemon/flights.go:16-28`        | `flightBranchSuggester` adapts `branchsuggest.AskForPrompt` to the new interface.                                                                                                                                          |
| `internal/daemon/flights.go:38`           | `initFlights` wires `Branches: flightBranchSuggester{cfg: cfg}`.                                                                                                                                                           |
| `internal/workspace/manager.go:1589-1598` | `CleanupFlightBranch` drops the `schmux/flight/` prefix guard; doc comment reworded.                                                                                                                                       |
| `internal/workspace/manager.go:1626`      | Log line reworded from "deleted local flight branch" to "deleted local branch".                                                                                                                                            |

### Test changes

| File                                                    | Change                                                                                                                                                              |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/flights/fakes_test.go:132-147`                | `fakeBranches` fake: records every prompt, returns a scripted name or error.                                                                                        |
| `internal/flights/fakes_test.go:163,174`                | `fakes.branches` field and its `newFakes` default (`feature/build-the-thing`).                                                                                      |
| `internal/flights/manager_test.go:71`                   | `newManagerWith` supplies `Branches: fk.branches`.                                                                                                                  |
| `internal/flights/manager_test.go:525`                  | `TestBind_SuppliesCollaboratorsThatExistOnlyAfterTheServer` supplies a suggester (see ruling 1).                                                                    |
| `internal/flights/transitions_test.go:97-110`           | `TestTakeoff_AllocatesWorkspaceAndSpawnsPlanner` expects the suggested branch, the recorded requested branch, the manager-suffixed branch, and the prompt verbatim. |
| `internal/flights/transitions_test.go:141-175`          | `TestTakeoff_FailsWhenNoBranchName` — three cases (not configured, suggestion error, nil suggester).                                                                |
| `internal/flights/transitions_test.go:177-202`          | `TestTakeoff_RetryAfterNamingFailureAsksAgain`.                                                                                                                     |
| `internal/flights/transitions_test.go:204-262`          | `TestTakeoff_RetryReusesRecordedBranch` — cases "suggested branch" and "legacy flight branch".                                                                      |
| `internal/flights/transitions_test.go:310-312`          | `TestTakeoff_SaplingRepoDoesNotRequestABranch` asserts zero suggester calls.                                                                                        |
| `internal/flights/transitions_test.go:350-352`          | `TestTakeoff_WorkspaceFailureStaysInBacklogAndRestartsOnRetry` asserts two suggester calls.                                                                         |
| `internal/daemon/flights_test.go:22-31`                 | `TestFlightBranchSuggester_NotConfiguredReturnsErrDisabled`.                                                                                                        |
| `internal/workspace/force_purge_test.go:126-152`        | `TestCleanupFlightBranch_DeletesSuggestedBranch` replaces `TestCleanupFlightBranch_RejectsNonFlightBranch`.                                                         |
| `internal/workspace/force_purge_test.go:153-175`        | `TestCleanupFlightBranch_KeepsDefaultAndCheckedOutBranches` — default branch, checked-out branch.                                                                   |
| `internal/dashboard/handlers_flights_test.go:79-83,106` | The `flightResources` fake gained `SuggestBranch`; the flights manager under test gets one (see ruling 2).                                                          |

### Docs and artifacts

- `docs/api.md:3960,3963` — the example flight's `requested_branch` and
  `branch` are now `fix/password-reset`.
- `docs/api.md:4021,4023` — the takeoff section describes suggested naming,
  failure when no name is available, retry reuse of the recorded
  `requested_branch`, and the naming failure next to the workspace-creation
  failure.
- `docs/flights.md:44` — new "Branch name from the prompt" architecture bullet.
- `docs/workspaces.md:142-148` — `CleanupFlightBranch` deletes the branch the
  flight recorded at takeoff; the "only branches under `schmux/flight/`" claim
  is gone.
- `review/flight-branch-names.html` — the review artifact with the takeoff and
  disposal tables and the pasted `./test.sh` and `./badcode.sh` output.

## Deviations from the plan

### Ruling 1 — `internal/flights/manager_test.go:525` needed a suggester

`TestBind_SuppliesCollaboratorsThatExistOnlyAfterTheServer` builds a flights
manager directly instead of through `newManagerWith`, so it never picked up
`Branches` and its takeoff failed with `suggest branch name: oneshot target is
disabled`. The plan does not list this file.

**Decision:** added `Branches: fk.branches` to that `Dependencies` literal.
The spec makes a nil suggester a takeoff failure (criterion 4), so the test
needed a suggester rather than an assertion of failure. Its subject — `Bind`
supplying collaborators that exist only after the server — is unchanged.

**Cost if wrong:** the test would instead assert the naming failure; one
assertion either way.

### Ruling 2 — `internal/dashboard/handlers_flights_test.go` needed a suggester

The dashboard test env builds a flights manager at
`handlers_flights_test.go:100` with no `Branches`, so every dashboard takeoff
test would have failed with the naming error. The plan does not list this
file.

**Decision:** gave the existing `flightResources` fake a `SuggestBranch`
method — it already "stands in for every collaborator the flights manager
drives" — and wired `Branches: res`. No new stub type was introduced.

**Cost if wrong:** the dashboard tests would need their own stub type.

### Ruling 3 — `./badcode.sh` needed its provisioning step

`./badcode.sh` exited 127 before running a single check: `vendor/` and
`.cache/badcode/` are gitignored and absent from this worktree, so its
preflight failed with "missing local deadcode", "missing local
vendor/modules.txt", "vendor/ is stale", "missing local tsc", and "node_modules
is stale".

**Decision:** ran `./scripts/bootstrap-badcode.sh`, which fetches the pinned Go
tools, vendors the modules, and installs the npm dependencies, then re-ran
`./badcode.sh`. Provisioning is gitignored and stays inside this worktree.

**Cost if wrong:** none; nothing outside the worktree changed.

### Ruling 4 — the prettier dependency bump was reverted

`./format.sh` runs `npm install --save-dev prettier`, which bumped `prettier`
from `^3.9.9` to `^3.9.10` in `assets/dashboard/package.json` and
`assets/dashboard/package-lock.json`. That is a side effect of the prescribed
formatting command and is unrelated to this change.

**Decision:** the flight pilot asked for the bump to be removed, since the
spec limits this work to flight branch naming and disposal. Both files were
restored to their committed state (`git restore assets/dashboard/package.json
assets/dashboard/package-lock.json`), and the delivered diff no longer touches
any dependency file. `assets/dashboard/` shows no diff at all.

**Cost if wrong:** none — the change is a dev-tool version bump with no
bearing on the flight behavior, and the repository's committed lock already
pins `prettier@3.9.9`.

### Ruling 5 — no commits, no reviewer dispatch

The assignment says to make no commits, use no subagents, and skip the commit,
reviewer-dispatch, and branch-finishing steps. The ledger therefore records
test commands instead of commit ranges, and the final whole-branch review was
performed as a self-review against `code-reviewer.md` rather than by a
dispatched reviewer. A self-review by the author is weaker than a fresh
reviewer; the plan's author should decide whether that is enough.

## Tests and checks

| Command                                                                                           | Result                                                                                                               |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `go test ./internal/flights/ -run 'TestTakeoff' -v` (RED, before the implementation)              | build failure: `unknown field Branches in struct literal of type Dependencies`                                       |
| `go test ./internal/flights/ -run 'TestTakeoff' -v` (GREEN)                                       | 11 test functions / 13 subtests, all PASS                                                                            |
| `go test ./internal/flights/ ./internal/daemon/ ./internal/dashboard/`                            | `ok flights`, `ok daemon`, `ok dashboard`                                                                            |
| `grep -rn 'schmux/flight/' internal/flights --include='*.go' \| grep -v _test.go`                 | no output                                                                                                            |
| `go test ./internal/workspace/ -run 'TestCleanupFlightBranch' -v` (RED, before the guard removal) | `FAIL` — `refuse to delete non-flight branch "fix/password-reset"`, plus the same refusal for `main` and `feature/x` |
| `go test ./internal/workspace/ ./internal/disposal/`                                              | `ok workspace`, `ok disposal`                                                                                        |
| `go test ./internal/daemon/ -run TestFlightBranchSuggester -v` (RED, before the adapter)          | build failure: `undefined: flightBranchSuggester`                                                                    |
| `go test ./internal/daemon/ -run 'TestFlightBranchSuggester\|TestInitFlights' -v`                 | PASS                                                                                                                 |
| `./format.sh`                                                                                     | `FORMAT_RESULT=PASS`                                                                                                 |
| `./test.sh`                                                                                       | exit 0 — Frontend 2308, Backend 3100, E2e 55, Scenarios 237, **5700 passed**                                         |
| `./badcode.sh`                                                                                    | exit 0 — `PASS: No issues found.` (deadcode, staticcheck, govulncheck, knip, npm audit, tsc, bind-write guard)       |
| `./scripts/check-api-docs.sh`                                                                     | exit 0                                                                                                               |

After the prettier revert (ruling 4), `./format.sh` and `./badcode.sh` were re-run
against the delivered tree — both still pass, with identical output — and
`git status` shows no dependency file among the changes. `./test.sh` was not
re-run: no source file changed, and no dashboard source or test invokes the
prettier binary (the only `prettier` mentions in `assets/dashboard/src/` are
autolearn rule text in a fixture).

## Acceptance criteria

| #   | Criterion                                  | Evidence                                                                                                                                                                                                                                                                                                                                                                                                    |
| --- | ------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Suggested name used                        | `internal/flights/transitions_test.go:TestTakeoff_AllocatesWorkspaceAndSpawnsPlanner` — one `GetOrCreateWithLabel` call with branch `feature/build-the-thing` and an empty label; `RequestedBranch == "feature/build-the-thing"`; state `planning`                                                                                                                                                          |
| 2   | Suggester gets the flight prompt           | same test — `fk.branches.prompts` equals `[]string{"Build the thing"}`, the `goodCreate()` prompt verbatim, exactly one call                                                                                                                                                                                                                                                                                |
| 3   | Manager's branch recorded                  | same test — `ws.Branch == "feature/build-the-thing-2"` (the suffix the fake manager adds) while `RequestedBranch` stays the requested name                                                                                                                                                                                                                                                                  |
| 4   | Naming failure fails takeoff               | `internal/flights/transitions_test.go:TestTakeoff_FailsWhenNoBranchName` — cases `suggestion_not_configured` (`oneshot.ErrDisabled`), `suggestion_failed` (`timeout`), `no_suggester` (nil `Branches`). Each asserts the error contains `suggest branch name`, the flight stays in `backlog` with `Workspace == nil`, the last takeoff operation is `failed`, `len(workspaces.calls) == 0`, and zero spawns |
| 5   | Retry after a naming failure asks again    | `internal/flights/transitions_test.go:TestTakeoff_RetryAfterNamingFailureAsksAgain` — the retry requests `fix/password-reset`, reaches `planning`, and the suggester was asked twice                                                                                                                                                                                                                        |
| 6   | Retry reuses the recorded branch           | `internal/flights/transitions_test.go:TestTakeoff_RetryReusesRecordedBranch` — `suggested branch` reuses `fix/password-reset` while the suggester now offers `fix/other`; `legacy flight branch` loads a snapshot recording `schmux/flight/fl-1` and requests it. The prompt list does not grow in either case                                                                                              |
| 7   | Sapling unchanged                          | `internal/flights/transitions_test.go:TestTakeoff_SaplingRepoDoesNotRequestABranch` — branch `""`, label `Fix login`, zero suggester calls                                                                                                                                                                                                                                                                  |
| 8   | No new hard-coded branch                   | `grep -rn 'schmux/flight/' internal/flights --include='*.go' \| grep -v _test.go` prints nothing                                                                                                                                                                                                                                                                                                            |
| 9   | Disposal deletes a suggested flight branch | `internal/workspace/force_purge_test.go:TestCleanupFlightBranch_DeletesSuggestedBranch` — `fix/password-reset` is deleted locally, the remote keeps it, a retry returns nil                                                                                                                                                                                                                                 |
| 10  | Disposal guards remain                     | `internal/workspace/force_purge_test.go:TestCleanupFlightBranch_KeepsDefaultAndCheckedOutBranches` (`the default branch` → `main`, `a branch checked out in a worktree` → `feature/x`) — both return nil with the local branch still present. `TestCleanupFlightBranch_AfterWorkspaceRecordIsGone` covers the already-gone branch and passes unchanged                                                      |
| 11  | Wiring                                     | `internal/daemon/flights_test.go:TestFlightBranchSuggester_NotConfiguredReturnsErrDisabled` — `errors.Is(err, oneshot.ErrDisabled)` with no branch-suggest target configured                                                                                                                                                                                                                                |
| 12  | Docs match code                            | `docs/api.md:4021,4023` (suggested naming, failure, retry reuse), `docs/workspaces.md:142-148` (no `schmux/flight/` restriction), `docs/flights.md:44` (suggested branch at takeoff)                                                                                                                                                                                                                        |
| 13  | Repository checks pass                     | `./test.sh` exit 0, 5700 passed; `./badcode.sh` exit 0, `PASS: No issues found.`                                                                                                                                                                                                                                                                                                                            |

Review artifact: `review/flight-branch-names.html`.

## Deferred and unresolved

**Deferred minors (self-review, not fixed):**

1. `flights.BranchSuggester` may return an empty name with a nil error. The
   interface contract is "a branch name or an error"; an empty name would be
   passed to `GetOrCreateWithLabel` as branch `""`, which the workspace
   manager resolves to the repository's default branch, putting the flight's
   work on `main`. The production wiring cannot produce this:
   `branchsuggest.validateSuggestedBranch` (`internal/branchsuggest/branchsuggest.go:112-114`)
   rejects an empty name with `ErrInvalidBranch`. It is a latent hole for a
   future alternative `BranchSuggester`, not a live defect. The spec defines
   "no name" as "the suggester returns an error", so a guard here would add a
   failure mode the spec does not list.
2. `internal/workspace/manager.go:1624` still wraps a delete failure as
   `delete local flight branch: %w` while the log line beside it says
   `deleted local branch`. Cosmetic only; no test or doc references the
   string.

**Observations, not findings:**

- `CleanupFlightBranch`'s default-branch guard cannot fire for `local:`-prefixed
  repository URLs — `GetDefaultBranch` returns an error for them
  (`internal/workspace/origin_queries.go:19-21`) — so the worktree guard is
  what protects the checked-out branch there. This is pre-existing behavior
  and the spec says the other guards stay as they are. It has no practical
  effect here: the suggestion rejects `main` and `master`
  (`internal/branchsuggest/branchsuggest.go:113-115`), so a flight can never
  record the default branch.
- The `flights` package now imports `internal/oneshot` for `ErrDisabled`. No
  import cycle: `oneshot` does not import `flights`.

**Not done:** no commits were made, and no reviewer was dispatched — the
assignment assigns both to a later phase.
