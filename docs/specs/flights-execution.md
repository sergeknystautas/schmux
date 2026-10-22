# Flights: supervised execution

Status: draft for user review, 2026-10-20. Builds the Execution part of the
workflow in [`flights.md`](flights.md#execution) on the planning increment that
is already built and documented in [`docs/flights.md`](../flights.md).

## Intent

Planning is supervised: the monitor classifies each completed planner response
with Jev, wakes the pilot on interesting turns, and delivers the pilot's
guidance to the planner. Execution is not. Today **Enter execution** spawns an
executor that is told only to implement the plan (`ExecutorPrompt`,
`internal/flights/prompts.go:53`). Nothing watches it, nothing checks that it
used the execution skill, nothing knows when it has finished, and nothing
brings its work back to the planner to be checked.

This increment gives execution the same supervision, following the north
star's execution steps 1–4. The pilot becomes able to read either working
session and message either one, which is what lets it hand the executor's
report to the planner. Phase changes stay manual.

## Decisions

These are the user's, from the north star and this design conversation:

- **Execution starts from the existing button.** The user presses **Enter
  execution** once the planner has reported a verified spec and plan. The
  preconditions and the recheck of both documents stay as built
  (`internal/flights/transitions.go:308`).
- **The executor's instructions deliberately differ from the skill.** It uses
  `superpowers:executing-plans` but is told to use no subagents, create no
  worktrees, make no commits, and not run
  `superpowers:finishing-a-development-branch`, which the completion phase
  handles. These are instructions, not rules schmux polls for in this
  increment. Only actual use of the execution skill (step 1a) is checked.
- **The executor writes a report so its work can be checked.** The pilot takes
  the report to the planner, which checks the work against the spec and plan.
- **The pilot can read and message either working session.** It analyzes
  planner and executor turns and can send guidance to the planner or the
  executor.
- **Questions and permissions stay human-answered through MVP.** Execution
  steps 2a and 2b are not built. Schmux never answers a native question or a
  permission request.
- **The workflow is part of schmux, not something each user designs.** The
  classifier definitions, executor included, live in schmux's own
  `internal/flights/flight-monitor.yaml`, embedded in the binary. Nothing is
  installed into `~/.schmux`. (My draft put them in a per-user
  `~/.schmux/flight-monitor.yaml` that users had to migrate; that was my
  design, and it was wrong.)

## What the chat history shows

Searched the retained conversations under `~/.schmux/chat/`:

| Finding                                                                                                                                                                                                                                                                                                  | Consequence                                                                                                                                                                |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 36 Claude sessions invoked the skill. Each invocation is a structured tool call: `{"name":"Skill","input":{"skill":"superpowers:executing-plans"}}` (for example `bach-godot-002/bach-godot-002-15662dfa/conversation.jsonl` line 21).                                                                   | Step 1a can be checked deterministically from the record for Claude.                                                                                                       |
| Codex has no skill tool. It reads the skill file with a shell command, recorded as a `commandExecution` item whose command includes `executing-plans/SKILL.md` (for example `bach-godot-003/bach-godot-003-62aa07d6/conversation.jsonl` line 54, `bach-godot-009/bach-godot-009-70fc9a6a/...` line 125). | Step 1a for Codex can only show the skill file was read, which is weaker than invocation. The check says which kind of evidence it found.                                  |
| Of the 36 Claude sessions, 28 have exactly one `result` record; 2 have none; 6 have 2–19.                                                                                                                                                                                                                | A per-turn check usually runs once, at the end. Step 2's drift comparison is effectively a final review of the work against the plan, plus whatever turns a stop produces. |
| 1 of 36 ran `git commit` through Bash; 4 of 36 dispatched an `Agent`.                                                                                                                                                                                                                                    | The behavior the executor is told to avoid does happen. Stopping it as it happens is future work (see Non-goals).                                                          |

## Behavior

### 1. Starting the executor (north star step 1)

**Enter execution** keeps its preconditions. It additionally:

1. Records the execution start time on the flight (the operation's time is
   enough if the builder prefers).
2. Spawns the executor with a new prompt (below).
3. Records an execution notice: a `FlightMessage` of a new kind,
   `execution_notice`, addressed to the pilot and delivered through the
   existing pending-message path. It is a launch message: an executor has
   now been launched, its session, the report path, and that guidance can
   now go to `executor` or `planner`. What to do with each executor turn
   arrives in that turn's notification. It is not a monitor notification, so
   the pilot answers it with `{"guidance":null}` and cannot direct guidance
   from it.

The executor prompt says, in this order:

- You are the executor. Implement the plan with the
  `superpowers:executing-plans` skill. Run the skill; do not describe or
  simulate it.
- Use no subagents, create no worktrees (this workspace is already isolated),
  make no commits, and do not run `superpowers:finishing-a-development-branch`.
  Where the skill says to commit, dispatch a reviewer, or finish the branch,
  skip that step. A later phase commits.
- How to work, from what executors did on the first execution flight: run
  each test once, and rerun one only after ending the turn to ask the pilot
  for permission and getting its answer, never in a loop; a failing test is a result to report, so no hunting
  flakiness, no checking whether a failure predated the change, and no
  changing a test to make it pass unless the plan says to; start a long suite
  once and let it finish, never a second run, a kill, a sleep, or polling;
  never revert or discard changes unless a human asks; when a test fails or
  work is blocked, stop, say what ran, the exact output, and what is left, and
  wait for direction.
- When the work is finished, write the execution report to the report path
  (below). It covers what was built, every
  deviation from the plan and why (the skill's `Ruling:` lines), the tests and
  checks run with their results, and anything deferred or unresolved. The
  plan's author will check the work against it.
- Messages prefixed with `[Pilot]` are flight guidance. Native questions and
  permission prompts are answered by a human.
- The spec and plan paths, then the original prompt between the existing
  delimiters.

The report is named after the plan it implements and sits beside it: the
verified plan path with `.md` replaced by `-execution.md`. A plan at
`docs/superpowers/plans/2026-10-19-flight-names.md` gets the report
`docs/superpowers/plans/2026-10-19-flight-names-execution.md`. The plan's own
date prefix, chosen by the writing-plans skill, dates the report. Schmux derives
the absolute path at **Enter execution** and gives it to the executor and the
pilot. This replaces the north star's proposed `review/execution-report.md`.

### 2. Supervising the executor (step 2)

While the flight is `executing`, the monitor follows the pilot and every
assigned working session that is running: the executor, and the planner, which
stays in the conversation to check the work.

- **Executor turns:** every completed executor turn notifies the pilot.
  Classification adds answers but does not gate the notification, because the
  history shows most executions end in one turn and the pilot's comparison
  with the plan is the drift check.
- **Planner turns:** classified with the planner's classifiers and, like
  executor turns, always notify the pilot. During execution the planner speaks
  only when checking the work, so a "routine" answer must not hide its verdict
  from the pilot.
- The definitions must have an `executor:` role with at least one classifier,
  as they must have a `planner:` role. Validation names a missing role, and a
  test keeps the shipped file valid. The definitions gain the executor role and
  the planner classifier below.
- The executor role's `notify_on` and `min_confidence` are validated as for
  the planner but do not decide whether the pilot is woken.
- Classifiers. Executor: `attention` (matching the planner's) and
  `execution_state` (finished, stopped on a question or blocker, still
  working). Planner: `work_check` (the work matches the spec and plan,
  problems found, still checking), for the planner's response to the report.
  These answers tell the pilot what each response presumably says; the report
  check, not a classifier, is the proof that the report exists.
- Interrupted and failed executor turns notify the pilot with the harness
  outcome, as planner turns do.
- A failed executor classification (missing TypeSafe key, invalid YAML, Jev
  error) does not hold the notification back, unlike planner turns today
  (`monitor.go:258` skips failed turns). The notification goes out with the
  classification error in place of answers. A later retry records its answers
  in monitor history only; the pilot keeps one notification per source turn.
  (My call: otherwise a configuration problem leaves the one turn most
  executions produce unwatched.)

Each executor notification carries what a planner notification carries
(flight, session, record range, outcome, durable conversation path, questions
and answers, full response) plus:

- **Skill check:** `executing-plans invoked at record N` (Claude),
  `executing-plans skill file read at record N` (Codex), or
  `executing-plans not found`. Only the executor's records count.
- **Report check:** `not written yet`, `verified: <path>`, or the reason it
  failed.

### Pilot messages to either session

The pilot's decision gains a required `to` field naming a role:

```json
{ "notification_id": "msg-…", "to": "planner", "guidance": "…" }
```

- `to` is `planner` or `executor` and is required whenever guidance is a
  string. A null-guidance decision sends nothing, so any `to` with it is
  ignored. With string guidance, a missing `to` or an unknown role is a decision error
  shown in monitor history, like malformed JSON today, and nothing is
  delivered. There is no default recipient and no classifier guessing one.
  Planning decisions need `to: "planner"` too; the pilot prompt says so.
- Only a pilot response to a message schmux sent it (a notification, the
  execution notice, or a rejection) is a decision. A response to the user's
  own message is the pilot talking to the user: it is not parsed and sends
  nothing.
- A decision error from a completed pilot response goes back to the pilot as
  a `rejection` message carrying the error, so it can answer again. It goes
  back once: a decision error in the response after a rejection is not
  returned, and its error says so.
- The daemon resolves the role to the flight's currently assigned session. The
  response still cannot name a session ID.
- The referenced notification must be a delivered notification whose source
  turn belongs to a currently assigned planner or executor.
- The recipient must be assigned, running, and in the flight's workspace, as
  the planner must be today. During planning there is no executor, so `to:
"executor"` fails visibly.
- Delivery is unchanged: persisted first, `[Pilot]` prefix, the same outcome
  states. A pending message keeps its recipient across the change to
  `executing`.

The takeoff pilot prompt documents `to` as required. Each executor
notification tells the pilot its job during execution (the execution notice
only announces the launch): compare the work with the spec and plan, point out
substantial drift to the executor, check the record for actual use of the
execution skill, and bring the verified report to the planner (step 4). It
does not implement, commit, edit the spec or plan, change the phase, dispose
the flight, or wipe the workspace.

### 3. Verifying the report (step 3)

On each completed executor turn, the daemon checks the report path: a regular,
nonempty file inside the workspace, modified after the execution start. The
first turn whose check passes marks the report verified on the flight
(`report_path`). Later turns still carry the report check in their
notifications. Git-ignore status is not checked: the report is a working
document, not a committed one.

A verified report is how schmux knows execution has finished. No Jev answer
or claim in the response substitutes for it.

### 4. Bringing the report back to the planner (step 4)

The notification for the turn whose report check passes tells the pilot that
execution has finished and the report is verified. The pilot sends the
planner a message (`to: "planner"`) that says another agent implemented its
plan, gives the report path, and asks it to check everything against the
intended result. That is also review step 1 in the north star. The planner's
response notifies the pilot with its `work_check` answer, so the pilot sees
whether the planner presumably considers the work done.

The flight stays `executing` until the user advances it.

### Waiting on a human

The flight must show when its executor is waiting on a native question or
permission. The same fix covers a planner waiting on a question card.

What exists: a pending question or permission makes the chat session's nudge
state "Needs Input", ahead of "Working" (`internal/chat/nudge.go:158`). The
in-progress check is true only for "Working" and "Background"
(`assets/dashboard/src/lib/workspaceSort.ts:57`), so a waiting session is not
in progress, is not sorted down, and its list line already shows the nudge
summary. Two gaps remain:

- `flightActivity` (`assets/dashboard/src/lib/flights.ts:126`) shows the
  non-pilot session with the most recent output, so a waiting executor is
  hidden whenever the planner spoke more recently.
- The flight detail page shows no session state at all.

My calls on how:

- A non-pilot session whose nudge state is "Needs Input" wins the flight's
  line over a session with more recent output.
- The detail page shows the same activity line as the list.
- The change stays in the flights code. The shared in-progress check and the
  sidebar workspace sort are unchanged.

### Dashboard

- Flight detail shows the report path once verified, next to the spec and
  plan.
- Monitor history shows executor turns with their skill and report checks,
  the way it shows the spec/plan check for planner turns, and shows each
  pilot message's recipient role.
- No new buttons.

## Non-goals

- Answering questions or permissions (2a, 2b).
- Stopping forbidden behavior as it happens, such as a commit or a subagent
  dispatch mid-turn, including shutting the session down. The user wants this
  later; supervision in this increment runs at turn boundaries only.
- Restarting execution, wiping the workspace, or capturing a baseline. These
  exist only to support a restart, which this increment does not build.
- Automatic phase changes.
- Review and completion behavior beyond the report reaching the planner.

## Anti-goals

- A Jev answer or the executor's own claim being treated as proof that
  execution finished or the skill ran.
- A second pilot. The workspace's pilot supervises both roles.
- A pilot response naming a session ID instead of a role.

## Left to the builder

- How the skill check reads tool calls from the record. The turn collector
  excludes tool activity from the text sent to Jev; the check reads the
  records separately.
- Where the execution start time and `report_path` live in the model, and the
  contract and generated-type changes that expose them.
- Generalizing `recordPlannerTurn`, the monitor worker, and pilot validation
  and delivery over the role rather than copying them for the executor.
- Tests per `docs/testing.md`, and `docs/flights.md` and `docs/api.md`
  updates.

Scope: about a dozen files, because the supervision loop is planner-specific
in each layer it passes through: the monitor worker and turn recording
(`monitor.go:103`, `monitor_model.go:13`), classification
(`monitor_jev.go:118`), config validation (`monitor_config.go:81`), pilot
validation and delivery (`pilot.go:77`,
`pilot.go:159`), the prompts, the transition, the model and its contract, the
detail page, monitor history, the activity line, and the embedded YAML
template.
