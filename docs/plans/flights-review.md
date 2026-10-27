# Flights Review and Completion Implementation Plan

> **For agentic workers:** Use `superpowers:executing-plans` to execute this plan task by task. Keep the checkbox for each step updated. Do not invoke subagents.

**Goal:** Add the `reviewing` phase, fresh-reviewer convergence loop, pilot stop and next-reviewer decisions, supervised completion, explicit noninteractive finalize, and the flight dashboard/API views defined by `docs/specs/flights-review.md`.

**Architecture:** Keep `flights.Manager` as the sole writer of flight state. Reuse the existing local-target launcher, session assignments and prior-session history, monitor turn/message model, Jev Choice engine, chat delivery, dashboard WebSocket snapshot, and React flight pages. Schmux only launches agents, retains evidence, delivers pilot messages, and exposes user-controlled phase transitions; it never reads review artifacts, parses agent claims, runs repository checks, or gates a transition on those claims.

**Tech stack:** Go, embedded YAML, generated API contracts, React/TypeScript, Vitest/React Testing Library, Go HTTP tests, and the existing Playwright scenario runner.

**Spec:** `docs/specs/flights-review.md`

## Global constraints

- Run every command from the repository root.
- The only phase transitions are user-initiated API calls: **Enter review**, **Enter completion**, and **Mark done**. Nothing advances automatically.
- A reviewer always uses `planner_model`, is a fresh chat session, and receives no planner, executor, or prior-reviewer history.
- Execution ends when the report is verified. The planner does not check executed work once `reviewing` exists.
- A reviewer that changed anything causes another fresh reviewer. The loop ends only when a reviewer changed nothing and everything it checked passed.
- A failed commit gate sends the failure through the same reviewer convergence loop. The committer retries only after convergence.
- Pilot guidance is valid only for the current assigned session in the phase's allowed role set.
- Review and completion add no daemon-side file reading, artifact existence checks, report parsing, freshness computation, test execution, repository commands, or transition gating.
- `review/` is excluded from the work commit. If a commit gate fails, the committer leaves the workspace unchanged. Only after the work commit succeeds does it remove `review/`, finalize the exact spec and plan, and amend that work commit through the repository commit skill.
- Finalize is mandatory and explicit-path: ignore unrelated specs and plans, consolidate both supplied documents, make the standard destination choice without asking, and amend the supplied work commit.
- Do not edit `assets/dashboard/src/lib/types.generated.ts` by hand. Edit `internal/api/contracts/flights.go`, then run `go run ./cmd/gen-types`.
- Do not run frontend tests from `assets/dashboard/`. Use `./test.sh --quick` during development and `./test.sh` for final verification.
- Do not run E2E tests with direct Go tags. Use `./test.sh --e2e` if that gate is needed.
- Do not start, stop, or restart the user's daemon. Tests must use their own fixtures.
- Do not commit, merge, push, rebase, stash, or rewrite history. The user owns git operations.

---

## Task 1: Extend durable flight vocabulary and evidence

**Files:**

- Modify: `internal/flights/model.go`
- Modify: `internal/flights/store.go`
- Test: `internal/flights/model_test.go`
- Test: `internal/flights/store_test.go`
- Modify: `internal/api/contracts/flights.go`
- Regenerate: `assets/dashboard/src/lib/types.generated.ts`

**Interfaces to add:**

```go
StateReviewing WorkflowState = "reviewing"
ActionReview   OperationAction = "review"
RoleReviewer   SessionRole = "reviewer"
```

Add these durable fields to `Flight`:

```go
ReviewStartedAt *time.Time `json:"review_started_at,omitempty"`
PilotStop       *PilotStop `json:"pilot_stop,omitempty"`
```

Use one durable stop record:

```go
type PilotStop struct {
    WorkflowState        WorkflowState `json:"workflow_state"`
    Reason               string        `json:"reason"`
    SourceTurnID         string        `json:"source_turn_id"`
    SourceNotificationID string        `json:"source_notification_id"`
    CreatedAt            time.Time     `json:"created_at"`
}
```

Retain the stop after a phase change. The summary/detail conversion exposes it only while `WorkflowState` equals `PilotStop.WorkflowState`, so the UI says “until the phase changes” without deleting evidence.

Add review evidence to `MonitoredTurn`:

```go
Review *ReviewCheck `json:"review,omitempty"`
```

Model `ReviewCheck` after `ExecutionCheck`:

```go
type ReviewCheck struct {
    Skill string `json:"skill"`
}
```

Extend API contracts with `reviewing`, `review`, and `reviewer`; expose `review_started_at`, `pilot_stop`, and `review` on appropriate flight responses. Add a compact `FlightReviewStatus` contract for the list snapshot, derived from the latest successful reviewer classification rather than persisted separately:

```go
type FlightReviewStatus struct {
    ReviewerSessionID string `json:"reviewer_session_id"`
    State             string `json:"state"`
    Tests             string `json:"tests"`
    TestRulesReview   string `json:"test_rules_review"`
    ChangedWork       string `json:"changed_work"`
    ArtifactsNamed    string `json:"artifacts_named"`
    UpdatedAt         string `json:"updated_at"`
}
```

- [ ] Add failing model/store tests for the new state, action, role, review timestamp, stop record, and review-check cloning.
- [ ] Add snapshot-validation tests that reject an invalid reviewing state, reviewer role, blank stop reason, and invalid stop source IDs.
- [ ] Extend `clone()` so nested `PilotStop` and `ReviewCheck` values are copied.
- [ ] Run `go run ./cmd/gen-types`.
- [ ] Run:

```bash
go test ./internal/flights -run 'Test(Workflow|SessionRole|FlightClone|Snapshot|LoadSnapshot)'
```

Expected: all listed tests pass.

---

## Task 2: Add the review transition and worker prompts

**Files:**

- Modify: `internal/flights/prompts.go`
- Modify: `internal/flights/transitions.go`
- Test: `internal/flights/prompts_test.go`
- Test: `internal/flights/transitions_test.go`

**New functions:**

```go
func ReviewerPrompt(f Flight, nextReviewerContext string) string
func ReviewNotice(f Flight, reviewerSessionID string) string
func (m *Manager) EnterReview(ctx context.Context, flightID string) (Flight, error)
```

**Enter review behavior:**

1. Require `StateExecuting`, an idle cleanup state, and a workspace. Do not require a verified report.
2. Use the shared `Spawner.LocalTarget` path with `WorkspaceID`, `TargetName: PlannerModel`, `Nickname: "reviewer"`, chat session kind, and the existing fence calculation.
3. Use `ReviewerPrompt`. Include `SpecPath`, `PlanPath`, and `ReportPath` only when the report path has actually verified; otherwise say that no verified execution report exists.
4. On successful spawn, atomically record `ActionReview`, `ReviewStartedAt`, the reviewer assignment, prior reviewer IDs, and the transition to `StateReviewing`. A spawn failure leaves the flight `executing` and retryable, matching `EnterExecution`.
5. Queue a `review_notice` to the pilot naming the reviewer and saying null guidance is the launch response.
6. Kick the monitor after recording the notice.

**Reviewer prompt requirements:**

- Review the whole workspace as the plan's author would; fix every finding in place; preserve or correct plan deviations and explain each choice.
- Check reuse, repository patterns, review/style skills, matching docs, and behavioral tests.
- Verify every acceptance criterion with evidence.
- Run the repository's own tests and checks, rerun after the last change, and report actual command output.
- Run `test-rules-review` on changed tests, fix violations, and rerun after changes until clean.
- Put only acceptance-criteria artifacts and the self-contained HTML summary under `review/`.
- End with changed/not changed, summary path, and artifact paths.
- Forbid commits, pushes, rebases, finalize, and phase changes.
- Carry `executionDiscipline` and the `[Pilot]` attribution note.
- Append the pilot's next-reviewer paragraph only for later reviewers.

**Planning change:** add an acceptance-criteria step to `planningSteps` before the coherence check. The step must require checkable conditions, exact `review/` artifact paths, and the repository's own proof commands. Do not change the existing `planning_artifacts` classifier.

**Completion transition change:** `EnterCompletion` must require `StateReviewing`, not `StateExecuting`. It stays retryable on spawn failure and still uses `ExecutorModel` with the executor harness.

- [ ] Add prompt tests asserting reviewer model/path/report/no-report content, discipline, artifacts, and prohibitions.
- [ ] Add transition tests for happy path, wrong phase, missing workspace, spawn-failure retry, prior reviewer history, notice recipient, and completion requiring `reviewing`.
- [ ] Add a test proving an unverified report does not block Enter review and the prompt says no verified report exists.
- [ ] Run:

```bash
go test ./internal/flights -run 'Test(ReviewerPrompt|ReviewNotice|EnterReview|EnterCompletion|PlanningSteps|PilotPrompt)'
```

Expected: all listed tests pass.

---

## Task 3: Add review and completion supervision

**Files:**

- Modify: `internal/flights/flight-monitor.yaml`
- Modify: `internal/flights/monitor.go`
- Add: `internal/flights/monitor_review.go`
- Test: `internal/flights/monitor_config_test.go`
- Test: `internal/flights/monitor_test.go`
- Test: `internal/flights/monitor_review_test.go`

**Monitored sessions:**

- `planning`: pilot plus planner.
- `executing`: pilot plus executor only. Remove the planner from supervised execution.
- `reviewing`: pilot plus current reviewer.
- `completing`: pilot plus committer and current reviewer while a gate-failure reviewer is assigned.

**Every-turn notification:**

- Reviewer turns always notify in `reviewing` and during completion gate repair.
- Committer turns always notify in `completing`.
- Failed or interrupted turns notify with the harness outcome without calling Jev.

**Reviewer classifier IDs and choices:**

```yaml
review_state:
  - finished
  - fixing
  - blocked
tests:
  - run_passed
  - run_failed
  - not_run
test_rules_review:
  - compliant
  - violations
  - not_run
changed_work:
  - changed
  - unchanged
artifacts_named:
  - named
  - incomplete
```

Leave `notify_on` empty for these classifiers; every reviewer turn notifies anyway.

**Commit classifier IDs and choices:**

```yaml
attention:
  - needs_attention
  - routine
completion_state:
  - committed
  - gate_failed
  - working
  - blocked
```

**Evidence:**

- Extract the hard-coded skill-search loop in `monitor_execution.go` into a helper that accepts a skill suffix, or add a parallel helper in `monitor_review.go`.
- For each reviewer turn, derive evidence for `test-rules-review` and namespace-compatible variants such as `superpowers:test-rules-review`.
- Persist the same three readable outcomes as execution: invoked, skill file read, and not found.
- Never inspect workspace files or command output.

**Notifications:**

- Include the full reviewer response, classification answers, durable conversation path, and skill evidence.
- Reviewer notifications tell the pilot to inspect actual test/skill evidence, guide the reviewer, request another reviewer after any change, and stop if the chain has gone off the rails.
- Commit notifications include the full response and tell the pilot how to route a gate failure into `next_reviewer`.
- Remove the execution instruction that sends a verified report to the planner.

- [ ] Add config tests requiring planner, executor, reviewer, and commit definitions and preserving choice order.
- [ ] Add monitor-routing tests for all four phase/role combinations.
- [ ] Add every-turn tests for reviewer, committer, failed, and interrupted turns.
- [ ] Add review-evidence tests for invoked, file-read, and missing skills.
- [ ] Add a regression test proving planner turns after execution are no longer monitored as work checks.
- [ ] Run:

```bash
go test ./internal/flights -run 'Test(Monitor|Review|ParseMonitorConfig|ValidateMonitorConfig)'
```

Expected: all listed tests pass.

---

## Task 4: Add pilot decisions and reviewer spawning

**Files:**

- Modify: `internal/flights/pilot.go`
- Modify: `internal/flights/prompts.go`
- Modify: `internal/flights/transitions.go`
- Modify: `internal/flights/model.go`
- Test: `internal/flights/pilot_test.go`
- Test: `internal/flights/transitions_test.go`

**Replace the private guidance-only decision with three mutually exclusive forms:**

```json
{"notification_id":"msg-…","to":"reviewer","guidance":"…"}
{"notification_id":"msg-…","next_reviewer":"what changed and what to re-check"}
{"notification_id":"msg-…","stop":"why this flight has gone off the rails"}
```

Null guidance remains valid:

```json
{ "guidance": null }
```

**Decision rules:**

- Decode with unknown-field rejection and require exactly one decision object.
- Non-null guidance requires `to`.
- `next_reviewer` and `stop` require nonblank text and forbid `to`.
- More than one active field is a decision error.
- The source must be a completed pilot turn answering a delivered notification for a currently monitored working session.
- Guidance recipients by phase:
  - planning: planner
  - executing: executor
  - reviewing: reviewer
  - completing: committer or reviewer
- `next_reviewer` is valid in reviewing and completing. In reviewing its source is a reviewer notification; in completing its source is the committer gate-failure notification or a reviewer notification.
- `stop` is valid in every supervised phase and records the phase, reason, source turn, source notification, and time. It sends no chat message, cancels nothing, hides nothing, and disables no action.

**Next-reviewer operation:**

- Reuse the Manager operation pattern rather than embedding the spawn in message delivery.
- Add an operation action for a pilot-requested reviewer so launch success/failure is durable.
- Validate the current reviewer assignment and phase before spawning.
- Spawn a fresh reviewer with `PlannerModel`, `ReviewerPrompt`, and the pilot's paragraph.
- On success, move the old reviewer ID into `PriorSessionIDs`, record the new current session, retain the decision, queue a new review notice, and restart monitor following.
- On spawn failure, leave the current reviewer and phase unchanged and return a rejection with the actual spawn error.
- Never dispose, message, or interrupt the superseded reviewer.

**Pilot prompt update:** teach every newly launched pilot all decision forms and recipient roles. Per-turn notifications still restate the job for the current phase; this is not legacy-data migration.

- [ ] Add table-driven decision-validation tests for every valid and invalid combination.
- [ ] Add delivery tests for reviewer and committer guidance in the correct phases.
- [ ] Add next-reviewer tests for review convergence, completion gate repair, wrong source, spawn failure, prior IDs, and durable operation outcome.
- [ ] Add stop tests for each supervised phase and prove phase actions remain available.
- [ ] Add rejection-text tests so a rejected decision tells the pilot the exact valid shape.
- [ ] Run:

```bash
go test ./internal/flights -run 'TestPilot'
```

Expected: all pilot tests pass.

---

## Task 5: Implement the committer contract and explicit finalize mode

**Files:**

- Modify: `internal/flights/prompts.go`
- Modify: `internal/flights/transitions.go`
- Modify: `.agents/skills/finalize/SKILL.md`
- Modify: `.agents/skills/commit/SKILL.md`
- Test: `internal/flights/prompts_test.go`
- Test: `internal/flights/transitions_test.go`

**Committer prompt:**

1. Stage every change except `review/`, including the exact spec and plan.
2. Invoke the repository commit skill. Gates run once per commit attempt.
3. On gate failure, stop, report actual output, and change nothing.
4. Only after the work commit succeeds, remove `review/`.
5. Invoke finalize in explicit amend mode for exactly the flight's spec and plan.
6. Ignore unrelated specs and plans.
7. Make standard consolidation choices without asking.
8. Amend the work commit through the commit skill.
9. Report the final commit hash and files. Do not rebase or push.

The daemon does not enforce these steps against the workspace. The prompt, pilot evidence, and user inspection carry the contract.

**Skill invocation contract:**

- Teach finalize to recognize an explicit, noninteractive amend invocation that names the work commit, one spec, and one plan.
- In that mode, process both named documents, ignore unrelated specs/plans, choose or create the subsystem guide by its existing rule, and never ask whether to consolidate spec only, plan only, create a commit, or leave changes.
- Teach commit to recognize the amend request from finalize and amend the named work commit rather than creating a second commit.
- Preserve the existing interactive finalize behavior outside this mode.

- [ ] Add prompt tests for exact paths, `review/` exclusion, failed-gate behavior, cleanup ordering, finalize arguments, amend, and no rebase/push.
- [ ] Add transition tests proving completion uses the composed prompt and executor target.
- [ ] Update both skills' self-description and step order so an agent can follow them without asking the user.
- [ ] Run:

```bash
go test ./internal/flights -run 'Test(CommitterPrompt|EnterCompletion)'
```

Expected: all listed tests pass.

---

## Task 6: Expose the review API and dashboard controls

**Files:**

- Modify: `internal/dashboard/handlers_flights.go`
- Modify: `internal/api/contracts/flights.go`
- Regenerate: `assets/dashboard/src/lib/types.generated.ts`
- Modify: `assets/dashboard/src/lib/api.ts`
- Modify: `assets/dashboard/src/lib/flights.ts`
- Modify: `assets/dashboard/src/routes/flights/FlightActions.tsx`
- Modify: `assets/dashboard/src/routes/flights/FlightEditForm.tsx`
- Modify: `assets/dashboard/src/routes/flights/FlightDetailPage.tsx`
- Modify: `assets/dashboard/src/routes/flights/FlightActivityLine.tsx`
- Modify: `assets/dashboard/src/routes/flights/FlightMonitorHistory.tsx`
- Modify: `assets/dashboard/src/routes/flights/FlightAssignmentLinks.tsx`
- Test: `internal/dashboard/handlers_flights_test.go`
- Test: `assets/dashboard/src/lib/flights.test.ts`
- Test: `assets/dashboard/src/routes/flights/FlightActions.test.tsx`
- Test: `assets/dashboard/src/routes/flights/FlightDetailPage.test.tsx`
- Test: `assets/dashboard/src/routes/flights/FlightMonitorHistory.test.tsx`

**API:**

- Add `POST /api/flights/{flightID}/review`.
- Register it between execution and completion.
- Map action name `review` to `Manager.EnterReview`.
- Extend summary/detail conversion with `review_started_at`, active pilot stop, and derived review status.
- Include review evidence on monitored turns in detail responses.

**Dashboard:**

- Add `review` to `FlightAction`.
- Executing offers `review`; reviewing offers `completion`; completing offers `done`.
- Add the `enterFlightReview` API client.
- Add `reviewing` to the edit-form stage list and active workflow pill.
- Add the Reviewer role label.
- Keep activity selection role-priority consistent with the latest active supervised role.
- Detail page shows:
  - current and prior reviewer assignment links through the existing assignment table;
  - latest review reading in words;
  - active pilot stop with reason;
  - review skill evidence in monitor history;
  - reviewer, committer, launch, next-reviewer, and stop messages.
- Do not add a confirmation dialog, summary link, artifact-existence check, polling, or a new WebSocket.
- Use the existing cards, pills, buttons, tables, semantic markup, and design tokens.

- [ ] Add handler tests for the new route, action errors, summary fields, stop visibility, and review turn evidence.
- [ ] Add frontend helper tests for all phase/action combinations.
- [ ] Add component tests for Enter review, Enter completion visibility, reviewer label, review reading, stop display, and monitor evidence.
- [ ] Run code generation and formatting:

```bash
go run ./cmd/gen-types
./format.sh
```

- [ ] Run backend and frontend focused tests through the required wrapper:

```bash
./test.sh --quick
```

Expected: pass.

---

## Task 7: Add the user-facing scenario

**Files:**

- Add: `test/scenarios/flights-review-completion.md`
- Add generated test as produced by the repository scenario tooling.
- Modify helper files only if a deterministic flight fixture is genuinely missing.

**Scenario content:**

- Seed a local repository and noninteractive prompt fixture.
- Create a flight and drive it into execution through existing APIs.
- Assert executing exposes **Enter review**, not **Enter completion**.
- Enter review and assert the phase and reviewer assignment arrive through the dashboard snapshot.
- Assert reviewing exposes **Enter completion** even without a finished reviewer classification.
- Enter completion and assert the committer assignment and phase arrive through the same snapshot.

If the existing scenario helper cannot seed a flight without a real planner/executor, add the smallest deterministic manager/API fixture needed; do not add a broad new test framework. Wait for semantic dashboard state, never elapsed time.

- [ ] Write the plain-English scenario.
- [ ] Use the repository scenario generator rather than hand-writing a new Playwright structure.
- [ ] Review the generated test against `docs/testing.md`.
- [ ] Run:

```bash
./test.sh --scenarios --run "flights review completion"
```

Expected: pass.

---

## Task 8: Update authoritative documentation

**Files:**

- Modify: `docs/api.md`
- Modify: `docs/web.md`
- Modify: `docs/flights.md`
- Modify: `docs/specs/flights.md`
- Modify: `docs/specs/flights-review.md`

Documentation must describe implemented behavior, not proposal intent:

- `docs/api.md`: new state, role, route, request/response fields, review status, stop, review turn evidence, and transition preconditions.
- `docs/web.md`: review phase controls, reviewer/committer resources, monitor history, stop display, and explicit no-file-check behavior.
- `docs/flights.md`: authoritative review/completion behavior, prompt contracts, convergence rule, completion gate repair, and explicit finalize mode.
- `docs/specs/flights.md`: move current-implementation statements from “proposed” to built once code is complete.
- `docs/specs/flights-review.md`: remove resolved “my call” markers and record any implementation deviation. If none remains, mark the increment ready for finalization rather than changing its decisions.

- [ ] Update all five documents in the same task as the behavior they describe or immediately after Task 6.
- [ ] Check every named API field against generated TypeScript types.
- [ ] Run:

```bash
./scripts/check-api-docs.sh
```

Expected: pass.

---

## Task 9: Final verification and review gates

**Files:**

- No new source files unless a prior task discovered a concrete gap.

- [ ] Run formatting:

```bash
./format.sh
```

Expected: `FORMAT_RESULT=PASS`.

- [ ] Run the complete test gate:

```bash
./test.sh
```

Expected: pass.

- [ ] Run static analysis and typechecking:

```bash
./badcode.sh
```

Expected: pass.

- [ ] Run the mandatory changed-test audit:

```text
test-rules-review
```

Expected: compliant verdict, or `no changed test files` only if no test was actually changed.

- [ ] Run the dashboard UI compliance skill before presenting UI work:

```text
dashboard-style-check
```

Expected: no unresolved findings.

- [ ] Run one manual/API-level acceptance pass against a real repository for a Claude planner target and a Codex planner target, following criteria 1–15 in `docs/specs/flights-review.md`.

Expected: every criterion has a yes answer with session/workspace evidence. Record any failure verbatim; do not widen tolerance or substitute a quicker command.
