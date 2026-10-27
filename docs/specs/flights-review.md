# Flights: review and completion

Status: built, 2026-10-26, with the deviations listed under "Implementation
deviations"; not yet finalized into [`docs/flights.md`](../flights.md), and
the acceptance criteria below have not been checked on a real flight. Builds
the Review and Completion parts of
the workflow in [`flights.md`](flights.md#review) on the planning and
execution increments documented there. Every item under "Decisions" is the
user's, with the conversation it came from. The proposals this document first
marked as the author's own calls are built as written; where the build
differs from this text, "Implementation deviations" says so.

## Intent

A flight today ends at `executing`. Once the executor's report is verified,
the user takes over by hand: tell an agent to fix what it found, run
`/test-rules-review`, say "fix", ask for the summary, ask for the screenshots
to be moved into `review/`, type `/commit`, watch the commit gates, type
`/finalize`, type `/rebase`. The retained conversations (below) show that this
stretch, from "done" to commit, is where most of the user's time and nearly all
of the anger goes.

This increment adds two phases, each with its own boundary, worker, and
supervision:

| Phase        | Starts when                         | Who works                                                  | What it covers from `flights.md`                                                                               | Ends when                        |
| ------------ | ----------------------------------- | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| `executing`  | (built) user clicks Enter execution | executor                                                   | Execution 1-4, ending when the daemon verifies the execution report                                            | user clicks **Enter review**     |
| `reviewing`  | user clicks **Enter review**        | one or more **reviewer** sessions, supervised by the pilot | Review 1-5: fix and settle, verify, tests, `test-rules-review`, artifacts and summary, handoff to the user     | user clicks **Enter completion** |
| `completing` | user clicks **Enter completion**    | the **committer**, supervised by the pilot                 | Completion 1, 2, 4: commit skill, cleanup, explicit finalize amended; 3 and 3a stay future; 5-7 are the user's | user clicks **Mark done**        |

The user still advances every phase. In review and completion, Schmux does not
run tests, read the spec or test files, parse report lines, check whether a
file exists, or block a transition on what an agent claimed.

## What the chat history shows

214 retained conversations in `~/.schmux/chat` (bach-godot, bach-godot-engine,
schmux; 2026-09-23 to 2026-10-26; 3,746 human messages after removing daemon
notifications and pilot guidance; restarted sessions seeded from the same
conversation counted once). The user's messages between the agent's claim of
completion and the commit fall into a short list of chores, the same in every
repository. The full tables are in
`review/flight-review-chat-analysis.html`.

**Between "done" and Enter completion** (the review phase's territory):

| Chore                                                     | Sessions                      | Example                                                                                                                                                                                                                                            |
| --------------------------------------------------------- | ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tell the agent to fix what its own review found           | 24                            | "please fix everything you identified" (`bach-godot-002-7948ec2e`); "fix the 6 issues you identified" (`schmux-003-fa0af6b2`); "1. fix 2. fix" (`bach-godot-002-e1525e39`)                                                                         |
| Type `/test-rules-review`, then "fix", then type it again | 17, 6 with two or more rounds | "/test-rules-review" four times in `bach-godot-002-5601e738`; "fix the violations, sorry about the rebase. that was the wrong order" (`bach-godot-002-a883a16e`)                                                                                   |
| Ask whether the tests actually ran, or stop reruns        | 9                             | "please stop inventing new ways to test. run our normal fucking process." (`schmux-003-5b1f9580`); "how many times are you going to rerun the same tests?" (`schmux-003-5b1f9580`)                                                                 |
| Ask for the handoff                                       | 13                            | "ok, can you summarize what was changed?", "any open questions?" (`bach-godot-006-90245949`); "ok, what's left to do" (`schmux-003-fa0af6b2`); "so are we ready for me to commit and then put in a remote branch?" (`bach-godot-002-5601e738`)     |
| Ask for artifacts, or for them to be moved into `review/` | 8                             | "please put files you want me to see in review/ as you've instructed to" (`bach-godot-002-6433ec7c`); "please move .screenshots/recital_corners.png to review/" (`bach-godot-002-ca32ef41`); "please put it in review" (`bach-godot-006-90245949`) |
| Catch a claim that is not true                            | 27                            | "can you NEVER FUCKING SAY ANYTHING WITHOUT EVIDENCE" (`bach-godot-002-a883a16e`); "I'm trying to figure out where you're lying" (`schmux-003-804336cb`)                                                                                           |
| Catch work that was not asked for                         | 31                            | "why the fuck is there a button to 'retry takeoff'?" (`schmux-003-5b1f9580`); "why the fuck would there be a native 'sample' command running in our tests?" (`bach-godot-002-7948ec2e`)                                                            |
| Try the change by hand and report back                    | 26                            | "ok, it is a revert. You've completely removed the burn effect." (`bach-godot-002-7948ec2e`); "ok, I stopped hovering and it never stopped. something is fucked" (`bach-godot-008-8227cdd2`)                                                       |

The agents' own reports confirm the test gap the user described: 71 responses
begin "Not done:", and 143 lines in 10 sessions admit the tests were not run
("Not done: I haven't run the full `just test` and `just check`",
`bach-godot-002-6433ec7c`; "I haven't run `test-rules-review` on the new
test", `bach-godot-002-5601e738`). The session that reviews is the session
that wrote the plan, and it reads its own work kindly: "The implementation
does what the spec says and the tests pass" (`schmux-003-804336cb`) preceded
"there's one real bug" and then two rounds of user-found defects.

**Between Enter completion and the commit** (the completion phase's
territory). The user typed `/commit` 53 times. Of 71 distinct work sessions
with a completion claim or a review, 34 reached a commit. Between the claim of
completion and the commit the user sent from 0 to about 49 messages (schmux:
0, 1, 1, 3, 26, 35, 49; bach-godot-001/002: 0, 0, 0, 1, 1, 22; bach-godot-003
to 009: 1 to 29, most between 3 and 15). The `/commit` that took zero messages
was always in a fresh session started for the commit alone, which is what the
flight's commit session already is. Three sessions needed two or three
`/commit` sessions before one landed.

| Failure                                                               | Sessions | Example                                                                                                                                                                                     |
| --------------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The committer reruns the gates or tests, or restarts a running suite  | 6        | "NEVER FUCKING MINDLESSLY RERUN TESTS" (`bach-godot-002-02f79e5f`); "DO NOT FUCKING RERUN THE COMMIT GATES WITHOUT DISCUSSION" (`bach-godot-002-ad14c7e6`)                                  |
| The gates hang or take far too long, and the user has to find out why | 5        | "yeah, running the commit-gates, the app just fucking hangs." (`bach-godot-002-7948ec2e`); "./test.sh did not pass. It ran for almost an hour before I intervened." (`schmux-003-804336cb`) |
| A gate fails on the environment and the user fixes it by hand         | 5        | "try now. go is updated" (`schmux-003-5235d761`); "badcode.sh issue is addressed" (`schmux-003-fa0af6b2`)                                                                                   |
| A failing test is found only by the commit gates                      | 2        | "we can't commit until you fix this" (`bach-godot-006-223617fd`); "please fix. I know that test is failing and the last agent refused to fix it" (`bach-godot-002-b0be427e`)                |
| The committer changes the work                                        | 2        | "the commit agent did some changes because of a review. can you please check the commit to see if there's anything you didn't want" (`bach-godot-002-5601e738`)                             |
| The commit is incomplete or has unrelated files                       | 3        | "ok, I removed that commit. Please do it again. Everything this time" (`schmux-004-3639e08d`); "the commit had files changed unrelated to the change" (`bach-godot-007-4d55f2b6`)           |
| The review scratch is cleared by hand                                 | 2        | "ok, I've wiped the review/ and committing the change." (`bach-godot-002-7948ec2e`)                                                                                                         |
| Finalize and rebase are separate chores                               | 15 / 19  | `/finalize` and `/rebase` typed, usually as "/commit", "/finalize", "/rebase" (`bach-godot-006-a6e1fdf8`, `bach-godot-009-3d67eeed`)                                                        |
| The commit message is written by hand                                 | 8        | "please stage the changes related to deleting that hallucination, and give me a commit message" (`bach-godot-006-7ae2a3c5`)                                                                 |

**The one automated flight that reached review** (`fl-29a54794`, bach-godot,
2026-10-24, on a build since removed): the planner, instructed by schmux and
supervised by the pilot, fixed its own findings, ran `just check` and
`just test`, ran `test-rules-review` three times until compliant, built the
web export, ran the three web specs, and wrote
`review/2026-10-21-solve-ceremony-review.html`, with the pilot sending one
correction. The user's part was one click and one message. The build was wiped
because its daemon parsed `Test:` lines out of the planner's report, computed
"freshness" from file edits, and blocked Enter review when the lines were
missing. The review workflow worked; the enforcement around it did not.

The review phase removes the first five rows of the first table; the
completion phase removes every row of the second. Catching untrue claims and
unasked-for work stays the pilot's and the user's judgment, with evidence.

## Decisions

The user's, with the conversation each came from.

Review:

- **Review is a phase like planning and execution.** "planning is getting it
  to plan. execution is getting it to execute. review is getting it to
  review." The user starts it and moves the flight on. Nothing advances on its
  own. (`schmux-003-fa0af6b2`, 2026-10-24)
- **A fresh reviewer agent does the review, not the planner.** It uses the
  planner's model. The pilot coordinates with the reviewer. The planner's
  conversation is biased toward the work being right. After a reviewer has
  fixed anything, another reviewer is started to re-check that the changes
  are correct; a flight may have several reviewers in turn. This replaces the
  2026-10-24 decision that the planner reviews. (this conversation,
  2026-10-26)
- **Fix in place.** No follow-up plan, no second executor, no wipe or redo in
  this increment. (`schmux-003-fa0af6b2`, 2026-10-24)
- **The system can say the work has gone off the rails and tell the user.**
  The pilot is the one placed to see it. "stop means stop"; on a stop schmux
  does nothing further. There is no interrupt, cancellation, or inert-monitor
  mechanism. (`schmux-003-fa0af6b2`, 2026-10-24 and 2026-10-25)
- **Nothing blocks the user.** The system may report that the work is dead
  and recommend a wipe; the user may still force a phase.
  (`schmux-003-fa0af6b2`, 2026-10-25)
- **The agents prove they did their job; the user does not check the
  basics.** Tests run, test-rules-review runs, and after fixes they run again.
  (`schmux-003-fa0af6b2`, 2026-10-24)
- **`test-rules-review` is part of review. `spec-coherence-check` is not.**
  (`schmux-003-fa0af6b2`, 2026-10-24)
- **The planner does not check executed work.** Execution ends when the
  daemon verifies the executor's report; the first fresh reviewer checks the
  work. This removes the planner's built-in `work_check` handoff once review
  exists. (this conversation, 2026-10-26)
- **Acceptance criteria are written in the spec during planning**, in the
  industry's ordinary sense: the conditions the change must satisfy to be
  accepted, stated so that each can be checked. They anticipate what the user
  needs in order to review: screenshots for visual changes; for architectural
  changes, who owns what and the tradeoffs made; and where each artifact goes.
  Diffs are always available from schmux and are not an artifact.
  (`schmux-003-fa0af6b2`, 2026-10-24; `schmux-003-894db9a1`, 2026-10-26)
- **Review and completion add no daemon reading of spec or test files, report
  parsing, or freshness computation.** (`schmux-003-fa0af6b2`, 2026-10-24;
  `schmux-003-116d72a1`, 2026-10-25)
- **No confirmation dialog on Enter review, and no file-existence checks or
  summary links on the flight page.** (this conversation, 2026-10-26)
- **Schmux does not run the tests.** The agents run them in the workspace.
  (`schmux-003-894db9a1`, 2026-10-26)
- **Artifacts need not be fresh.** (`schmux-003-fa0af6b2`, 2026-10-24)
- **How complete testing is defined is the repository's convention, not
  schmux's.** Either the agents follow the repository's own instructions, or
  an optional per-repository config says so later. (`schmux-003-894db9a1`,
  2026-10-25)
- **Jev reads the review.** (`schmux-003-894db9a1`, 2026-10-26)
- **`review/` is where artifacts go, and it is not git-ignored**, so the user
  can open them from the dashboard. (`schmux-003-894db9a1`, 2026-10-25)
- **Fix loops should not spiral.** The user wants them bounded but does not
  know how. (`schmux-003-894db9a1`, 2026-10-25)
- **Legacy flights are not a concern.** No migration work for flights
  recorded by earlier builds. (this conversation, 2026-10-26)

Completion:

- **The commit must not contain `review/`; when the work is committed,
  `review/` is cleaned up.** (`schmux-003-894db9a1`, 2026-10-25; `flights.md`
  Completion step 1)
- **Cleanup waits for commit success.** A failed gate leaves `review/` and
  every other workspace change untouched. (this conversation, 2026-10-26)
- **Finalize changes are amended onto the work commit**, not a separate
  commit. Finalize is mandatory, receives the flight's exact spec and plan
  paths, ignores unrelated specs, runs without interactive choices, and amends
  the work commit. (`schmux-003-894db9a1`, 2026-10-25; this conversation,
  2026-10-26)
- **Rebase is exclusively a user action.** (`schmux-003-894db9a1`,
  2026-10-25)

Out of scope: what happens to the user's feedback after hands-on testing;
wiping and retrying. "I want to get from soup to nuts for a flight, and then
we can start to figure out nuance and complexity." (`schmux-003-fa0af6b2`,
2026-10-24)

## Planning: acceptance criteria

The planning steps in the planner and pilot prompts (`planningSteps`,
`internal/flights/prompts.go`) gain one step, before the coherence check: the
spec has an **Acceptance criteria** section. Each criterion is a condition the
change must satisfy to be accepted, written so a reader can check it. The
section also names the artifacts the user will need to review the change and
the exact path of each under `review/`, and the checks that prove the change,
in the repository's own terms.

Schmux does not read the section. The pilot holds the planner to writing it, as
it holds the planner to the other steps; the existing `planning_artifacts`
classifier is unchanged.

## Review phase

### Roles

- **Reviewer:** a chat session spawned by schmux in the flight's workspace
  with the planner's model (`planner_model`), under a new session role
  `reviewer`. It reviews and fixes. Each reviewer is a fresh session with no
  history from the planner, the executor, or an earlier reviewer; it reads the
  spec, plan, report, and workspace for itself. The flight keeps the current
  reviewer and every earlier one, as it keeps prior session IDs today.
- **Pilot:** unchanged session. It receives every reviewer turn, sends
  guidance to the reviewer, asks for the next reviewer, and may stop.
- **Planner:** not part of the review phase. It stays assigned and idle; the
  user may still talk to it.

### Enter review

**Enter review** is offered on an `executing` flight with a workspace. That is
the only precondition. There is no confirmation dialog. The execution report
is named in the reviewer's prompt if the daemon verified one, and the prompt
says so if not.

Entering review records the review start, moves the flight to `reviewing`,
and spawns the first reviewer with the prompt below, the way Enter execution
spawns the executor. The pilot learns of it the way it learns of the executor:
a notice naming the reviewer session, answered with null guidance (the same
pattern as `ExecutionNotice`).

### The reviewer's prompt

In this order, in plain prose:

1. You are the reviewer for this work. One agent planned it (spec and plan at
   these paths) and another implemented it (execution report at this path, or:
   no verified report exists). Review the work in this workspace as the plan's
   author would, and fix, yourself and in place, every problem you find. Keep
   or correct every deviation from the plan and say which and why. Do not
   start over.
2. Check what a reviewer checks: reuse of existing code instead of copies, the
   repository's patterns and its style or review skills, docs that match the
   code, and tests that test behavior.
3. Verify every acceptance criterion in the spec against the workspace and say
   which are met, with the evidence, and which are not.
4. Run the repository's tests and checks as the repository's own instructions
   define them. After your last change, run them again. Report each command
   with its actual result; never report a run you did not do.
5. Run the `test-rules-review` skill on the changed tests. Fix every violation
   and run it again until it is clean. Run the skill; do not describe or
   simulate it.
6. Put every artifact the acceptance criteria name at its path under
   `review/`, and nothing for the user anywhere else. Write a self-contained
   HTML account of the result at `review/<plan file stem>-review.html`: what
   changed and why, how each deviation was settled,
   each acceptance criterion with its evidence, the test and test-rules-review
   results, how to run and try the change, and open questions for the user.
7. End the response that completes your review by saying whether you changed
   anything, and listing the summary path and the artifacts. Do not commit,
   push, rebase, or finalize; do not change the flight phase.

It also carries the working rules the executor already has
(`executionDiscipline`, `internal/flights/prompts.go`): run each check once
per change and never in a loop, no hunting flakiness, no checking whether a
failure predates the change, start a long suite once and never restart, kill,
sleep on, or poll it, never revert without a human asking, and stop and say so
when blocked. The `[Pilot]` attribution note is included.

A later reviewer gets the same prompt plus one paragraph from the pilot: what
the previous reviewer changed and what to re-check. It reviews the whole
change again, not only the previous fix.

### Supervising review

While the flight is `reviewing`, the monitor follows the pilot and the current
reviewer (`monitoredSessions`, `internal/flights/monitor.go`). Every completed
reviewer turn notifies the pilot (`notifiesEveryTurn` extended to
`reviewing`); interrupted or failed turns carry the harness outcome, as today.

Jev reads each reviewer response with Choice questions in a new `reviewer`
role of `internal/flights/flight-monitor.yaml`: whether the review is finished,
still fixing, or blocked; whether the response reports the repository's tests
run after the latest change and their result, or says they were not run;
whether it reports test-rules-review compliant, with violations, or not run;
whether it reports that it changed anything; and whether it names the summary
and artifacts as written. None of these gates a notification; every reviewer
turn notifies anyway. The answers go to the pilot with the response and into
monitor history, the way `execution_state` does. The
notification also carries the daemon's existing skill evidence from the
reviewer's record, as executor notifications carry `executing-plans`:
`test-rules-review invoked at record N`, the Codex skill-file read, or `not
found` (the same mechanism, with no gating).

Each notification carries the pilot's review job: hold the reviewer to its
prompt; check the reviewer's conversation for evidence rather than its claim,
such as the test commands actually run with their output after the last change
and the skill actually invoked; send guidance to the reviewer; ask for the
next reviewer when this one has changed anything; and declare a stop if the
work has gone off the rails. The pilot does not implement, commit, edit the
spec or plan, change the phase, dispose the flight, or wipe the workspace.

### The next reviewer

When a reviewer's completed review says it changed something, the pilot asks
for a fresh reviewer with a third decision form:

```json
{
  "notification_id": "msg-…",
  "next_reviewer": "what the last reviewer changed and what to re-check"
}
```

The daemon validates it like guidance, records it, spawns a new reviewer
session with the reviewer's prompt plus the pilot's paragraph, moves the
`reviewer` assignment to it (the previous session ID joins the prior IDs), and
follows the new session. The previous reviewer is left running; nothing is
sent to it or done to it. The review is finished when a reviewer reports that
it changed nothing and everything it checked passed; the pilot then sends no
`next_reviewer`.

A chain that does not converge is one of the things the pilot can see from its
notification history, and a stop is how it reports one. There is no numeric
cap, and nothing is enforced.

### Pilot stop

The pilot's decision gains a stop form:

```json
{ "notification_id": "msg-…", "stop": "why the flight has gone off the rails" }
```

`stop`, `guidance`, and `next_reviewer` are mutually exclusive; more than one
present is a decision error. A stop takes no `to`. It is valid under the same
rules as guidance: a completed pilot response to a delivered notification
about one of the flight's current working sessions, in any supervised phase.
There is one decision shape and no phase special case.

On a valid stop the daemon records it on the flight (reason, time, source turn,
notification) and shows it: the list's activity line and the detail page lead
with "Pilot stopped: <reason>" until the phase changes. That is all. Nothing is
cancelled, delivered, interrupted, hidden, or disabled. The sessions keep
running; later turns keep notifying; the phase buttons stay. The user reads
the reason and decides: talk to the sessions, dispose, or advance anyway.

### The handoff to the user

The reviewer's final response names the summary and the artifacts; schmux
already rewrites workspace paths in chat to the dashboard's viewer, so the
user opens them from the reviewer's session. The flight list and detail page
show the latest reviewer turn's classification in words ("Reviewer reports:
finished, tests run, test-rules-review compliant, nothing changed") and the
pilot's latest guidance. The detail page links the reviewer sessions, current
and prior, and the workspace diff, as it links sessions today. The flight page
does not check whether any file exists and does not link the summary.

The user reviews as they choose: read the summary, read the code, open the
artifacts, try the change. Iterating on the result stays outside this
increment.

The review phase ends when the user clicks **Enter completion**. That click is
the user's approval of the review. It is not gated on the classification, on
whether a reviewer reported finished, or on the stop record.

## Completion phase

### Enter completion

**Enter completion** takes `reviewing` as its starting phase instead of
`executing`. An `executing` flight no longer offers it; the edit form's direct
`workflow_state` write remains the escape hatch (`docs/flights.md`, "Stage is
editable directly").

### The committer

Entering completion spawns the commit session (the `commit` role, called the
committer in `flights.md`) as today (`EnterCompletion`,
`internal/flights/transitions.go`), with the executor's harness, but with a
composed prompt instead of the bare `/commit`:

1. The user has reviewed and approved this work. Stage every change except
   `review/`, including the spec and plan at their exact paths, and run the
   repository's commit skill. Run its gates once per commit attempt. If a gate
   fails, stop and report the actual output; do not rerun the gates, do not
   change the work to make a gate pass, do not revert anything, and do not
   start or kill a running suite.
2. After the work commit succeeds, remove `review/` from the workspace; it is
   finished with and must not be committed.
3. Run the repository's finalize skill on exactly the flight's spec and plan
   paths, ignore unrelated specs, accept its default choices rather than
   asking, and amend the work commit with the finalize changes. The finalize
   output is documentation only, so the commit skill's gates do not run again.
4. End with the commit hash and the files committed. Do not rebase or push.
5. `[Pilot]` messages are flight guidance.

The prompt names the `commit` and `finalize` skills, as the reviewer's prompt
names `test-rules-review`, because `flights.md` names them: they are the
workflow, present in every repository the user runs flights in. What is not
named anywhere is a test command, gate, or tool; those come from the
repository's own instructions.

### Supervising completion

While the flight is `completing`, the monitor follows the pilot, the
committer, and any reviewer spawned after a failed gate. Every committer and
reviewer turn notifies the pilot. The `commit` role gets its own questions in
`flight-monitor.yaml`: whether the response needs attention, and whether it
reports the commit made, a gate failed, work continuing, or a block. Pilot
guidance may go `"to": "commit"` or `"to": "reviewer"`.

When a gate fails, the committer stops and its notification carries the
output. The pilot uses the same `next_reviewer` convergence loop as review,
carrying the gate failure, and the committer runs the commit skill again only
after a reviewer changes nothing and everything it checked passes. Nothing is
terminated or unstaged by the daemon; `flights.md` steps 3 and 3a stay future
work.

**Mark done** stays the user's decision on a `completing` flight. Rebase and
push (`flights.md` steps 5-7) are the user's actions and stay outside the
flight.

## Dashboard

- A `reviewing` phase pill and activity line, from the same flight state as
  the others.
- Flight actions: **Enter review** on `executing`; **Enter completion** on
  `reviewing`; **Mark done** on `completing`; dispose everywhere, as today.
- Detail page: the reviewer sessions (current and prior) among the resources,
  the latest reviewer classification in words, the pilot stop if any, and the
  existing session and diff links. Monitor history shows reviewer and
  committer turns with their answers and skill evidence, reviewer launches,
  next-reviewer decisions, and stops.

All of it is flight state written only by `flights.Manager`: the review start,
reviewer assignments, stop record, and the retained classifications are
persisted in the same edits that record the transition, the pilot's decision,
and the turn, and the list and detail page read them from the same
`/ws/dashboard` payload.

## Capabilities needed

| Capability                                                        | New or existing                                                                                                                                                                                                 |
| ----------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `reviewing` phase; Enter review; completion from review           | New state and transition, same pattern as `EnterExecution` (`internal/flights/transitions.go`, `model.go`); dashboard actions (`assets/dashboard/src/lib/flights.ts`, `FlightActions.tsx`).                     |
| `reviewer` session role, spawned at Enter review                  | New `SessionRole`; spawn through the shared launcher as the executor is; prompt in `prompts.go`; label in `ROLE_LABEL` (`docs/flights.md`, "Supervise another role").                                           |
| Pilot-requested next reviewer                                     | New decision form in `recordPilotGuidance` (`internal/flights/pilot.go`) that spawns a session: the first pilot decision with an effect beyond a message.                                                       |
| Acceptance-criteria planning step                                 | Prompt text only (`planningSteps`, `internal/flights/prompts.go`).                                                                                                                                              |
| Review and commit notifications every turn                        | `monitoredSessions` and `notifiesEveryTurn` (`internal/flights/monitor.go`) extended to `reviewing` and `completing`, following the reviewer and committer (and a gate-failure reviewer while one is assigned). |
| Jev questions for reviewer and commit                             | `reviewer` and `commit` roles in `internal/flights/flight-monitor.yaml`; the Go engine needs no change for Choice questions (`docs/flights.md`, "Add or change a classifier").                                  |
| `test-rules-review` skill evidence                                | The existing `SkillUse` collection (`monitor_turns.go`) and the executor's skill check (`monitor_execution.go`) applied to reviewer turns.                                                                      |
| Pilot stop                                                        | Decision validation in `recordPilotGuidance`; a stop record on the flight; list and detail display.                                                                                                             |
| Guidance to the reviewer and the committer                        | `RoleReviewer` and `RoleCommit` added to the roles `recordPilotGuidance` and `deliverFlightMessageLocked` accept.                                                                                               |
| Committer prompt                                                  | Replaces the `CommitPrompt` constant (`internal/flights/transitions.go`).                                                                                                                                       |
| Noninteractive, explicit-path finalize                            | The repository finalize skill accepts the exact spec and plan, makes its standard consolidation choices without asking, and amends the supplied work commit.                                                    |
| Review start, reviewer assignments, stop, reading on the contract | `internal/api/contracts/flights.go`, regenerated types, detail and list pieces under `assets/dashboard/src/routes/flights/`.                                                                                    |

Not needed in review or completion: reading or parsing workspace files;
checking whether a file exists; running commands; computing freshness;
per-repository command configuration; cancelling, interrupting, or disposing
anything automatically; a retry or wipe path; automatic phase advancement;
answering native questions or permissions; or migrating flights from earlier
builds.

## Non-goals

- Wiping, restoring, or restarting execution; a follow-up plan; a second
  executor.
- Routing the user's hands-on feedback back into the flight.
- Automatic advancement into or out of review or completion.
- Rebase and push.
- Terminating or unstaging a failed committer (`flights.md` steps 3, 3a).
- Disposing or messaging a superseded reviewer.
- Counting or reporting on stops or reviewers across flights.
- Checking that brainstorming ran during planning.
- Flights recorded by earlier daemon builds.

## Anti-goals

- In review or completion, the daemon reading the spec, the plan, the report,
  or test files, checking that a file exists, or parsing structured lines out
  of an agent's response.
- Schmux running tests or gates itself.
- A transition blocked by a classifier answer, a missing artifact, or a stop.
- An agent's claim that tests passed or a skill ran accepted as proof by the
  pilot; a claim counted as proof by the daemon.
- Any automatic action against a session: cancel, interrupt, dispose, mute.
- Hardcoding one repository's test commands, gates, or tools. (The `commit`,
  `finalize`, and `test-rules-review` skills are named by the workflow itself.)
- A reviewer that inherits the planner's or an earlier reviewer's
  conversation.

## Acceptance criteria

Each is one condition a person can check on a flight run against a real
repository, with a yes or no answer, by looking at the flight page, the
sessions, and the workspace.

Review:

1. An `executing` flight offers **Enter review**. After the click, the flight
   shows `reviewing`, and a new session using the planner's model is running
   in the flight's workspace, with the user having sent it nothing.
2. That session's first message gives it the spec path, the plan path, the
   execution report path (or says none was verified), and tells it to: fix
   its findings in place; verify each acceptance criterion in the spec; run
   the repository's tests and rerun them after its last change; run
   `test-rules-review` until clean; put the named artifacts under `review/`;
   write the summary under `review/`.
3. When the reviewer says it is finished, the workspace contains every
   artifact the spec's acceptance criteria named, at the named path under
   `review/`, and a self-contained HTML summary under `review/` that states:
   what changed and why; each deviation from the plan and how it was settled;
   each acceptance criterion and whether it is met, with evidence; the test
   results; the test-rules-review result; how to try the change; open
   questions.
4. The reviewer's session shows the repository's test commands run after its
   last change, and the `test-rules-review` skill invoked after its last test
   change, with their output. The pilot's session shows a message for each
   reviewer turn.
5. When a reviewer reports that it changed something, a second reviewer
   session starts, with the planner's model and no shared history, and
   reviews the whole change again; the flight lists both. When a reviewer
   reports that it changed nothing and everything it checked passed, no further
   reviewer starts.
6. Pilot guidance to a reviewer appears in that reviewer's chat with the
   `[Pilot]` prefix, with the user having relayed nothing.
7. A pilot stop appears on the flight list and the detail page with the
   pilot's reason. After it, every phase action is still offered, and every
   session is still running.
8. A `reviewing` flight offers **Enter completion** whether the reviewer
   reported finished or not, whether the summary exists or not, and whether a
   stop is recorded or not. An `executing` flight does not offer it.

Completion:

9. After Enter completion, the flight shows `completing`, and a commit session
   is running in the workspace with the user having sent it nothing.
10. When the committer reports success: the flight branch has exactly one
    commit on top of its base; that commit contains no path under `review/`,
    no spec file, and no plan file; it contains the finalize changes to the
    subsystem guide; the workspace has no `review/` directory.
11. When a commit gate fails: the committer's response contains that gate's
    output, the gate was run once, and the work in the workspace is unchanged
    by the committer.
12. Pilot guidance to the committer and any gate-failure reviewer appears in
    that session's chat with the `[Pilot]` prefix. The pilot's session shows a
    message for each committer and gate-failure reviewer turn.
13. After a commit gate fails, the pilot starts a fresh gate-failure reviewer;
    while a reviewer changes something, another fresh reviewer starts; the
    committer retries the commit skill only after a reviewer changes nothing
    and everything it checked passes.
14. A `completing` flight offers **Mark done** whether the commit succeeded or
    not.

Both:

15. Criteria 1 to 14 pass on a flight whose planner model is a Claude target,
    and on a flight whose planner model is a Codex target.

## Implementation deviations

- **The reviewer current at Enter completion is recorded.** The completion
  operation carries a `reviewer` resource naming it, so the monitor can tell
  a gate-failure reviewer (started later, followed during completion) from the
  reviewer that reviewed the work (not followed during completion). This
  document asks for the distinction without saying where it lives; the
  operation history is the durable place.
- **The planner's `work_check` classifier is removed**, along with the
  notification text that sent the verified report to the planner. The planner
  is not followed while executing and the pilot cannot address it then; the
  execution notice now names only the executor.
- **The review reading spans superseded reviewers.** When the pilot starts the
  next reviewer, the reading stays the previous reviewer's last
  classification (the one that explains why) until the new reviewer has a
  classified turn.
- **The pilot's latest guidance is the latest delivered or queued pilot
  message**, to any working session, shown as "Pilot to <Role>: <text>" in its
  own line under the list's status and in its own row on the detail page.
  Monitor history keeps every message.
- **The pilot stop on the detail page is a "Pilot stopped" status pill with
  the reason**, not an error paragraph, so it does not read as a failed
  operation. The list's activity line leads with "Pilot stopped: <reason>" as
  written.

## Left to the builder

- Field names and placement of the review start, reviewer assignments, stop
  record, and the review reading on the flight and on the API contract.
- The exact prose of the reviewer's prompt, the committer prompt, the
  pilot's review and commit job text, and the reviewer launch notice, within
  the content above and the existing "no bloat" preference for pilot
  messages.
- Question and choice wording in `flight-monitor.yaml`.
- Tests per `docs/testing.md`; updates to `docs/flights.md`, `docs/api.md`,
  `docs/web.md`, and `docs/specs/flights.md`.
