# Build Monitor

## What it does

Watches GitHub Actions workflows on each monitored repo's default branch, surfaces the newest run per workflow on a dashboard page (`/build-monitor`), and auto-launches a remediation session on the first `failure` of an episode. Owned end-to-end by `internal/buildmonitor`; the dashboard only consumes the package's outputs.

## Key files

| File                                                    | Purpose                                                                                                       |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| `internal/buildmonitor/monitor.go`                      | `Monitor` lifecycle and pass orchestration                                                                    |
| `internal/buildmonitor/pass.go`                         | Per-unit check pass: per-workflow fetch, two-view runs reconciliation, session unlink, `PassResult.StaleRuns` |
| `internal/buildmonitor/status.go`                       | `AggregateRuns` (commit chip), `AggregateWorkflows` (unit roll-up), shared `isFailureConclusion`              |
| `internal/buildmonitor/transitions.go`                  | `isFailing` (remediation trigger), `ApplyTransitions` (event emission, ledger carry, episode bookkeeping)     |
| `internal/buildmonitor/state.go`                        | `UnitState`, `WorkflowState`, `RemediationRecord` schemas and durable read/write                              |
| `internal/buildmonitor/launch.go`                       | Auto-remediation eligibility and `RecordManualLaunch` (manual "Launch workspace" button on the page)          |
| `internal/buildmonitor/check.go`                        | Public `CheckPass` adapter invoked by the dashboard                                                           |
| `internal/github/actions.go`                            | `ListRepoRuns`, `LatestWorkflowRun` (one-request-per-workflow with `per_page=1`), `Job` leases                |
| `internal/dashboard/handlers_buildmonitor.go`           | HTTP handlers, `BuildMonitorProvider` initialization, session-ID pass for row unlinking                       |
| `assets/dashboard/src/routes/BuildMonitorPage.tsx`      | Page UI: `CONCLUSION_BADGES` / `STATUS_BADGES`, `workflowBadge`, alert-driven failure UX                      |
| `assets/dashboard/src/contexts/BuildMonitorContext.tsx` | Client-side fetch+refetch driven by `buildMonitorUpdateCount` from `SessionsContext`                          |

## Architecture decisions

- **Workflows are independent rows.** A unit row is the newest run for that workflow, fetched with one `GET /actions/workflows/{id}/runs?branch=&per_page=1`. A scheduled or dispatched workflow's newest run is rarely on the head; the row carries whatever commit the run built and the page shows its short SHA. The unit roll-up (`AggregateWorkflows`) uses **failure > in_progress > queued > success** so a workflow still running never hides another workflow's failure.
- **Two views of the same runs.** Rows follow the newest run on the branch; the commit store for workspace chips follows the head commit. Both views reconcile the same `runs` payload (`runsWithEffectiveStatus`); one jobs fetch is shared when the same run is newest in both views.
- **Per-workflow fetch, not repo-wide listing.** `ListRepoRuns` (repo-wide, `per_page=100`) was observed serving stale snapshots (Aug–Jun window) or omitting a workflow's latest run, which launched agents for months-old failures and blanked rows. `LatestWorkflowRun` returns one run per workflow and a row never moves to an older run or blanks when a response has none; missing/stale responses land in `PassResult.StaleRuns` and the dashboard logs them.
- **Remediation trigger is narrower than failure conclusion.** `isFailing` (the auto-launch gate) is `failure` only. `isFailureConclusion` (shared by both aggregates) also covers `timed_out` and `startup_failure` so a red badge and a `failure` roll-up still fire — but no agent is launched. A row that flips to `timed_out` after a `failure` episode closes the episode (`TransitionRecovered`) without launching.
- **Sessions unlink when gone.** The dashboard passes the live session IDs in `UnitInput.SessionIDs`; the pass unlinks any row pointing at a session that no longer exists. The ledger keeps the record so the run is not auto-relaunched.
- **`AggregateRuns` precedence differs from `AggregateWorkflows`.** `AggregateRuns` answers "is this commit's CI finished and green": **in_progress > queued > failure > success**. `AggregateWorkflows` answers "is the unit red": **failure > in_progress > queued > success**. Both are deliberate; the chip and the page can disagree.
- **Watched non-default heads stay commit-scoped.** Non-default-branch workspace heads use `ListRepoRuns` and only update CI chips. They never emit remediation events; remediation only fires for monitored units on the default branch.

## Gotchas

- **`timed_out` and `startup_failure` are red, not remediation triggers.** The badge shows red, the unit roll-up is `failure`, but `isFailing` does not match. The page therefore shows the red badge without a "Launch workspace" button — `Launch` requires `conclusion === 'failure'` to match the backend gate. Widening the gate is a separate product decision.
- **A row with no `run_id` is "No runs", not "Queued".** Use `CONCLUSION_BADGES` / `STATUS_BADGES` for completed/unfinished states; do not collapse "no run" into "Queued". A row can also be blank because `LatestWorkflowRun` returned `nil` — the row keeps its prior run and `PassResult.StaleRuns` records the fetch.
- **A stale row stays stale until a newer run completes.** `isFailing && wasFailing` on the same `FirstFailureRunID` carries the episode forward without emitting a launch. The page shows the stale run's `head_sha` so the staleness is legible.
- **`PassResult.StaleRuns` is dashboard-visible.** When the API response is stale or incomplete, the pass collects the names and logs them so the user can see which workflows lost their newest run.
- **The unit pass and the `GET /api/build-monitor` read path must derive status identically.** Both call `AggregateWorkflows(rows)` from the rows they already have. Do not derive unit `status` from `Monitor.Status` — that answers a different question and will diverge.
- **The badge table is exhaustive.** Unknown conclusion/status values render verbatim (underscores → spaces, capitalised) in the neutral class. Add a mapping only when GitHub introduces a new value; do not invent labels.
- **The remediation ledger dedupes by `FirstFailureRunID`.** Re-running the same failing run via dispatch records the same episode. The session in the row is replaced when a new manual launch targets the same run.
- **`CheckPass` recomputes `remediationWorkspaceStillCurrent`** — a workspace left over from a previous head is not the new head's debugging episode. A new head with a new push-triggered failure joined to a stale workspace would branch from the old commit.
- **Build tag is `nobuildmonitor`.** A no-op stub in `internal/buildmonitor/disabled.go` mirrors the package surface so `nobuildmonitor`-tagged builds compile cleanly.

## Common modification patterns

- **Add a new badge label.** Add an entry to `CONCLUSION_BADGES` or `STATUS_BADGES` in `BuildMonitorPage.tsx`. Add a row to the `workflowBadge` table test in `BuildMonitorPage.test.tsx`. Update `docs/web.md` if it's user-visible behavior. Do not change the badge mapping in `AggregateWorkflows`; UI labels are frontend-only.
- **Change which conclusions count as failure.** Edit `isFailureConclusion` in `status.go` (shared by both aggregates). If you want the change to also gate auto-launch, also edit `isFailing` in `transitions.go` — the two are deliberately separable.
- **Add a new trigger for remediation.** Edit `ApplyTransitions` in `transitions.go`. The narrow `isFailing` (failure only) is the gate; widening it changes product behavior and needs spec coordination.
- **Watch a new branch.** A non-default branch head is passed via `HeadInput` to `CheckPass`. It only updates CI chips; remediation requires the unit to be enabled. PR tracking and unit state live separately.
- **Persist new state on a workflow row.** Add the field to `WorkflowState` in `state.go`, write it in `pass.go`'s row fill, carry it through `carryRecentRemediations` if it must survive fresh API snapshots, and round-trip it in `applyTransitions` if it changes between passes. Regenerate types with `go run ./cmd/gen-types`.
- **Add a dashboard-side derived field.** Consume it from the existing `/api/build-monitor` response; do not add a polling client (the context already drives `useBuildMonitor` from `/ws/dashboard`'s `buildMonitorUpdateCount`).
