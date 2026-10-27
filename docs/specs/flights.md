# Flights

Status: project north star, updated 2026-10-20. This document holds the
project's terminology, the user's decisions, the full workflow reference, and
the questions that remain before more of the workflow is built. What is already
built is documented in the subsystem guide, [`docs/flights.md`](../flights.md);
that guide is authoritative for current behavior. Proposals that never received
a user decision stay labelled as proposals.

The earlier focused specs (stage 1 foundation, list overhaul, and the pilot and
classifier increment) were implemented and consolidated into `docs/flights.md`.
They no longer exist as separate files.

## Terminology and relationships

- **Flight:** the unit of work. It exists in the backlog before any workspace
  is created and retains its identity through the workflow.
- **Pilot:** the supervisor for a workspace, responsible for understanding the
  flight and the sessions working in that workspace. The pilot is part of the
  classifier system. Its model is flight-manager configuration (Settings >
  Flights > Pilot Supervision), never something chosen per flight.
- **Air traffic controller**, also **traffic controller** or **controller:**
  coordinates the list of flights and their pilots.

A flight has zero or more workspaces. When the controller starts the flight,
it creates a workspace for it. Initially it creates one; the relationship must
support multiple workspaces in the future. Each workspace assigned to a flight
has a pilot and uses the existing workspace-to-session relationship for its
working sessions. This does not require pilots for unrelated schmux workspaces.

A flight's identity must not be its workspace ID or its pilot session ID.
Backlog flights have neither. Pilot replacement and workspace disposal must
not erase the flight's identity or recorded history.

**Delivery stage** means stage 1 or stage 2 of this project. **Flight phase**
means progress through the work. The built phases are `backlog`, `planning`,
`executing`, `reviewing`, `completing`, and `done` (review and completion per
[`flights-review.md`](flights-review.md)). Live
session activity and a pilot's assessment are separate from the declared phase.

The phase control exists only to break the work into manually advanced chunks
while the workflow is being learned. Users should not eventually need to see or
set fine-grained workflow steps. How workflow position is represented later is
open (see [Questions before future automation](#questions-before-future-automation)).

The workflow reference below names roles by responsibility. The built sessions
map onto them as follows: the **planner** session is the author during planning,
the **executor** is the implementer, each fresh **reviewer** session is the
author during review, and the **commit** session is the committer.

## Delivery stage 1: manually operated flights

Goal: build the flight orchestration subsystem so the user can see what each
flight is doing and practice the workflow. Operations are explicitly initiated
by the user.

| Scope item                                                                | Status                                                                                                                                                                                                                                                 |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Establish a flight and manage the list of flights, including the backlog. | Built: create, edit, manual backlog order, daemon-generated names.                                                                                                                                                                                     |
| Represent workspace relationships and track a pilot per workspace.        | Partly built: one workspace assignment per flight, with one pilot and one planner. The zero-to-many workspace relationship is not built.                                                                                                               |
| Establish the controller and how the user interacts with it.              | Partly built: `flights.Manager` is the single writer of flight state, and the user drives it through the flights UI. There is no conversational controller.                                                                                            |
| Support pilot communication with sessions in its workspace.               | Built for the pilot, planner, executor, reviewer, and committer: classifier notifications to the pilot, validated pilot guidance delivered to a working session with `[Pilot]` attribution, pilot-requested next reviewers, and a recorded pilot stop. |
| Persist components, relationships, communications, and manual decisions.  | Built for assignments, operations, monitor turns, classifications, and messages. General decision records are not built.                                                                                                                               |
| Represent workflow progress without enforcing automatic transitions.      | Built: the user advances every phase. The only automatic exchanges are monitor notifications to the pilot, pilot guidance to the phase's working session, and a pilot-requested next reviewer during review and completion.                            |
| Build a simple flights UI alongside the data and communication model.     | Built: flight list, detail page with resources and monitor history, Settings > Flights.                                                                                                                                                                |
| Establish infrastructure for known decision points and their evidence.    | Partly built: monitor evidence (source turn, record range, questions, answers, spec/plan check). No general decision interface.                                                                                                                        |

This stage does not implement automatic scheduling, phase advancement,
permission or question responses, retry loops, wipes, or completion actions.
The monitor-to-pilot notifications and pilot-to-planner guidance during
planning are the one scoped exception to the manual-message restriction. Native questions and permissions remain manual, and
so do phase changes and destructive actions. A reported assessment is distinct
from an applied workflow change.

The flight is a shared record, not another conversational agent. A pilot reads
its intent and the planner's conversation; communication is between the user,
the daemon, the pilot, and the working sessions.

### What three manual flights showed

Three flights ran the same request through planning on 2026-10-19:

1. The first used a fixed planner prompt that named a hidden `.schmux` plan path
   and asked for no spec. The classifier, pilot, and planner exchange worked:
   three notifications, two pieces of pilot guidance, and two plan revisions. The
   planner wrote only `plan.md`, in a git-ignored directory.
2. The second was deleted after daemon naming failed with an unexplained
   "invalid flight name". Naming errors now report the broken rule and the
   rejected name.
3. The third used the planning steps below in the planner prompt. Its record
   shows brainstorming, question cards answered by the user, a spec, the
   coherence check, a second coherence check, and then the plan. The skills
   placed both documents. The planner reported them and the daemon verified
   them. The pilot confirmed no step was skipped. Its first name attempt, a
   reasonable 10-word name, was rejected by the fewer-than-10-words rule; a
   retry produced a shorter one.

The third flight's planner paused on a native question card. The flight showed
nothing while the planner waited; only the session's status said it needed
input.

## Delivery stage 2: workflow interpretation

Use real chat transcripts and available artifacts to evaluate how the workflow
holds up. Add scaffolding to identify the workflow position, satisfied and
unresolved requirements, missing evidence, and points requiring intervention.
Record the supporting evidence and proposed decision so the user can assess it.

Supervision needs two kinds of judgment. Some points need a response that
redirects or corrects the work. Others are checkpoints that only confirm the
flight is where it should be. They may share a delivery path to the pilot, but
their input differs: a response is judged on its own text, while a checkpoint
depends on everything the session has done so far, including tool activity.

Replay is not a full simulation. A correction, termination, or restart would
change subsequent conversation. Mark that intervention boundary; later
historical messages cannot validate the hypothetical action. If historical
workspace state is absent, expose the missing evidence rather than inventing
a verdict. Capture evidence during manual flights to improve future evaluation.

Jev is in production use for fast, structured judgements. The monitor sends
each completed planner and executor response to Jev with the questions in
schmux's own `internal/flights/flight-monitor.yaml`, which is part of the
workflow, not per-user configuration, and wakes the pilot on interesting or uncertain
answers. The original feasibility spike found promising results for turn state
and specific process checks, weaker question routing, and failures on raw diffs.
Its small, partly synthetic sample did not establish production thresholds or
prove skill compliance, and live accuracy has not been measured.

Neither stage connects an evaluation result to an automatic phase change.

## Architecture

`flights.Manager` is the controller service: the single writer of flight
records, assignments, operations, monitor evidence, and messages. It persists
each change before reporting success and before publishing the dashboard update.
The UI and the daemon's monitor submit changes through it. The pilot contributes
assessments and guidance; it does not change a flight's declared phase.

A pilot runs as an ordinary chat session through the existing runtime; its
session does not own the flight or the pilot assignment. Existing
workspace/session lifecycle and chat delivery stay behind their APIs. Flight
assignments reference those resources without owning their live state. The
flight list, flight detail, and workspace header read the same manager-owned
state. Every surface that shows them must keep declared phase, live session
activity, and assessments distinct.

Integration points in use:

- `chat.Runtime.Subscribe`: durable history followed by live records. The
  monitor detects turn completion from protocol records (Claude `result`, Codex
  `turn/completed`), not from sidebar nudges.
- `chat.Runtime.Send`: pilot guidance and pilot notifications. A persisted
  message is not proof of delivery; the monitor records queued, accepted,
  failed, and unknown outcomes.
- The shared spawn launcher and disposal service, which flights share with the
  dashboard.

Chat records carry no sender attribution. Pilot guidance uses a server-added
`[Pilot]` prefix in the planner's chat while the flight retains the exact text,
sender, and delivery outcome. The floor manager's terminal tell endpoint is not
the chat communication path.

Resolved during stage 1, documented in `docs/flights.md`: creation fields and
list organization; flights navigation; behavior when a workspace or session
fails during creation or is missing afterwards (shown as unavailable history);
pilot and planner replacement through dashboard Restart; message
attribution, queued and interrupted delivery, and manual retry without duplicate
sends; evidence retained with each monitored turn; the phase vocabulary and
manual controls.

Still open from stage 1:

- More than one workspace per flight.
- The controller's interaction surface beyond the flights UI.
- Session replacement other than dashboard Restart. A pilot or planner that dies
  or is replaced by hand leaves the flight pointing at the old session, and
  takeoff cannot be repeated.
- Evidence expiry: ended transcripts can expire, so a transcript link alone does
  not retain evidence.
- The smallest decision interface needed for stage 2 evaluation.

## Workflow reference for future evaluation and automation

The remainder preserves the original planning, execution, review, and completion
workflow. Its automatic actions describe the eventual behavior to evaluate;
they are not requirements of either delivery stage. "Current implementation"
notes record how far the built system follows each part.

The original source conversation is
`~/.schmux/chat/schmux-005/schmux-005-be5cc4cb/conversation.jsonl`.
Source references below are its physical line numbers. That transcript can
expire.

### Integration and visibility

Both Claude Code and Codex chat harnesses are required. Use existing workspace
creation, session creation, chat history, message delivery, permission/question
responses, and disposal infrastructure wherever possible. Do not make the
workflow depend on a separate hook-based reporting system.

The working sessions remain ordinary schmux sessions in ordinary workspaces.
The user can open them, watch the full exchange, and intervene. The orchestration
view provides a backlog, each entry's current phase and waiting reason, and links
to its workspace and sessions. Disposing an entry cleans up its sessions and
workspace while bypassing the usual manual workspace protections.

Evaluate planning compliance at agent turn boundaries. Mid-turn dirtiness is
allowed. Do not terminate a planning session because a git watcher sees a
temporary edit while the agent is still working.

Source: lines 1, 156, 199, 232.

Current implementation: the flights UI shows the phase and links, but not a
waiting reason. A planner paused on a native question shows "Needs Input" only
on its session.

### Roles

- **Author:** conducts interactive planning and retains its conversation for
  execution questions, review, corrective planning, and rebase.
- **Implementer:** a separate session that executes the plan in the workspace
  and produces a report for the author.
- **Committer:** another session started during completion to run the commit
  skill, then finalize after the commit succeeds.
- **Controller and pilots:** coordinate the flight and workspace work.

Source: lines 93, 309, 405, 447. The subsequent decision is zero-to-many
workspaces per flight, with one workspace created initially and a pilot per
workspace.

### Planning

Interactive planning is required. Preserve the user's original numbering:

1. Verify that the author actually uses the brainstorming skill. Reading the
   instructions and claiming to have used it is insufficient.
2. Work through all questions with the user.
3. If implementation remains in the codebase when the turn returns, terminate
   the session, wipe the workspace, and restart planning.
   - **3a.** Spikes are allowed to answer feasibility questions or produce
     screenshots and other review artifacts. Spike code is throwaway.
   - **3b.** Schmux directs and checks where spikes and artifacts go. Reviewable
     artifacts belong under `review/`.
4. Once questions are resolved, run `/spec-coherence-check`.
5. Verify that this check actually ran; a simulated check is insufficient.
6. Unresolved issues return to questions or to the coherence check, depending
   on whether user input or agent correction is needed.
7. A coherent spec automatically leads to writing the plan.
8. Planning finishes when the spec and plan exist and the planning turn has no
   unresolved questions, issues, or other abnormal outcome.
   - **8a.** Generated review artifacts require user approval before planning
     can be considered complete.

A missed skill gets one corrective message. If the next turn still fails the
same check, terminate and restart. This is distinct from the immediate restart
for implementation during planning.

Source: lines 106, 199, 232, 294, 297.

Current implementation: the planner prompt lists steps 1-8a in order
(`planningSteps`, `internal/flights/prompts.go`), and the pilot prompt carries
the same steps so the pilot can flag a skipped or out-of-order step. Nothing
enforces them. Steps 1 and 5 are not verified by the daemon, step 3's restart
and the one-corrective-message rule are not built, and the pilot's confirmation
is a judgment, not a check. The record shows the needed evidence
deterministically for Claude: skill invocations, question cards and answers, and
document writes appear with their record positions. Codex has not been checked.

#### Spec and plan placement

The skills choose where the spec and plan go. Schmux does not dictate or
configure a location. The planner prompt tells the planner to let the
brainstorming and writing-plans skills place each document, which follow any
repository preference, and to end the response that completes them with
`Spec: <absolute path>` and `Plan: <absolute path>` lines. The daemon then
determines whether that was violated: both reported, absolute, distinct,
nonempty, inside the workspace, and not ignored by git. A violation is retained
on the turn and always notifies the pilot. Execution requires a verified report
and rechecks it.

#### Spike placement and checks

The previous agent proposed `review/spikes/<name>/` for spike code and
`review/` for rendered artifacts. The user delegated spike placement design but
required detection of codebase changes. Retain this directory proposal for
validation, rather than treating every detail of the earlier git predicate as
approved.

The final check must account for committed changes as well as working-tree
changes. A clean git status alone does not prove that implementation did not
occur. The distinction between a misplaced artifact that should be moved and
implementation that requires restart needs an explicit classification order;
the previous tables gave overlapping rules for these cases.

Source: lines 238, 247, 294.

### Execution

1. Start a separate implementer when planning is complete. Instruct it to run
   the execute-plan skill, use no subagents, create no worktrees, make no
   commits, and produce a report when it finishes. Include the report location
   in this initial instruction.
   - **1a.** Verify actual use of the execute-plan skill.
   - The other restrictions are instructions to the implementer, not a new
     set of rules Schmux continually polls for.
2. Periodically compare the work with the plan to detect substantial drift.
   The exact cadence is open: every turn or every few turns were suggested.
   - **2a.** A question may be answered tactically, relayed to the author, or
     expose a gap requiring the author to revise the spec/plan and execution
     to restart. Preserve room for nuanced handling.
   - **2b.** Schmux decides permission requests. Automatically relaying that
     authority to the author was explicitly corrected by the user. The
     decision mechanism and restart policy remain open.
3. When execution finishes, verify that the report exists. The proposed path
   was `review/execution-report.md`.
4. Bring the report back to the author. That ends the execution phase.

“Wipe” means restoring exactly what existed before that execution attempt,
including the spec, plan, planning artifacts, and any other pre-existing files.
It does not mean deleting all untracked files indiscriminately. Capture the
baseline before each attempt. The earlier suggestion to copy files under
`~/.schmux/pipeline/` is an implementation proposal, not an approved storage
design.

Source: lines 309, 343, 361, 364.

Open detail: when an author revises the plan before a wipe, the restoration
sequence must preserve the revised documents. The earlier relay proposal did
not resolve this ordering. Define and verify it before implementing recovery.

Current implementation (see [`docs/flights.md`](../flights.md)): the user starts
execution by hand once a verified spec and plan exist. The executor prompt
carries step 1's
instructions (run `superpowers:executing-plans`; no subagents, worktrees, or
commits; no `finishing-a-development-branch`) and the report location, the plan
path with `-execution.md`. The pilot supervises the executor: every completed
executor turn notifies it, with a daemon check of skill use (step 1a) and of the
report (step 3), and it can message the executor. A verified report is how the
daemon knows execution finished. Step 4 is replaced: the planner does not check
executed work, and the first fresh reviewer does (see Review). Steps 2a and 2b
are not built and the phase never advances
automatically. Native questions and permission requests pause the session until
a human answers them in the session UI; the flight's activity line now leads
with a session waiting on one.

### Review

1. Tell the author that another agent implemented its plan and provide the
   report. Ask it to check everything against the intended result.
2. Evaluate the author's assessment, tolerated and intolerable deviations, and
   test results. The accepted review proposal has Schmux run tests itself and
   classify the outcome as successful, partial, or failed.
   - **2a. Partial:** retain useful work, have the author produce a follow-up
     plan, repeat the relevant coherence/planning gates, and start another
     implementer. Capture a new baseline for that attempt.
   - **2a. Failed:** restore the appropriate pre-execution baseline, have the
     author improve the spec and plan, repeat planning validation, and retry.
   - The accepted proposal allows one follow-up and one full redo before
     parking for user input. Other retry limits were not established.
3. After a successful assessment, ask the author to run `/test-rules-review`
   and verify that it actually ran.
   - **3a.** The author fixes violations and repeats the review until clean
     or until the workflow needs user intervention.
4. Ask the author to prepare screenshots and a user-facing account of the
   result under `review/`.
5. Notify the user that the result is ready for review. Provide the report,
   screenshots, code/diff access, and session links so the user can inspect
   and exercise the app before completion proceeds.

Source: lines 405, 427, 429.

The previous agent proposed `review/summary.md`. This repository's current
artifact contract requires user-facing reports to be self-contained HTML under
`review/`; use that format here. Do not confuse that presentation artifact with
the implementer's machine-consumed execution report.

The earlier claim that a repository config already supplies test/build/lint
commands was not established. Resolve how verification commands are selected;
do not invent a config field or infer a test command from arbitrary quick-launch
entries.

Decision, 2026-10-25 (conversation `schmux-003-894db9a1`): Schmux does not run
the tests itself, and the daemon does not read the spec, the plan, or test
files. The planner runs the repository's tests inside the workspace; the
outcome is read from the planner's response, not computed by the daemon.
That supersedes the "Schmux run tests itself" wording in step 2 above.

Current implementation (see [`docs/flights.md`](../flights.md), "Supervising
review"): the user clicks **Enter review** on an executing flight, which starts
a fresh reviewer with the planner's model. The reviewer fixes in place, verifies
the spec's acceptance criteria, runs the repository's tests and reruns them
after its last change (step 2, by the agent, not by Schmux), runs
`test-rules-review` until clean (step 3), and writes its artifacts and an HTML
summary under `review/` (step 4). Every reviewer turn notifies the pilot, which
asks for another fresh reviewer whenever one changed something. The user reads
the result and clicks **Enter completion** (step 5); nothing gates that click.

Decision, 2026-10-26 (this conversation): once review exists, the planner no
longer checks the execution report. Execution ends when the daemon verifies
that report; **Enter review** starts the first fresh reviewer, and reviewer
convergence replaces the planner's `work_check` pass.

### Completion

After user review allows completion:

1. Inspect what will be committed. Exclude `review/` from the work commit;
   after that commit succeeds, remove `review/` before finalize amends the
   commit.
2. Start a separate agent to run the repository's commit skill.
3. If it takes too long, terminate that session and send the failure back to
   the author to investigate and fix.
   - **3a.** On commit errors, terminate the committer, unstage changes, and
     send the actual errors to the author.
4. After a successful commit, ask that same committer to finalize the exact
   spec and plan without interactive choices, then amend the work commit.
   - **4a.** Guarded-file restrictions are explicitly outside the MVP.
5. Have the author run `/rebase` after commit and finalization.
   - **5a.** Detect rebase failures, unresolved conflicts, and successful
     rebases that leave broken behavior. Escalate where user intervention is
     required and revalidate the result as in review.
6. Once committed, finalized, and rebased, push to a remote branch.
7. The user decides whether to merge into main and close the workspace.

Source: lines 447, 480, 482.

Unresolved details include timeout values, whether completion artifacts are
archived before removal, which role pushes, and the exact post-rebase
verification and repair sequence. The prior agent proposed answers to these;
they were not separately approved. An initial commit alone must not be mistaken
for a fully committed finalized result.

Decisions, 2026-10-25 (conversation `schmux-003-894db9a1`): `review/` is not
git-ignored, so the user can open its artifacts from the dashboard; the commit
must not contain `review/`, and `review/` is cleaned up when the work is
committed. Finalize changes are amended onto the work commit, not a separate
commit. Rebase (step 5) is exclusively a user action.

Decision, 2026-10-26 (this conversation): cleanup waits for work-commit
success, so a failed gate leaves the workspace unchanged. Finalize is
mandatory and noninteractive: it receives the flight's exact spec and plan
paths, ignores unrelated specs, accepts predetermined choices, and amends the
work commit.

Current implementation (see [`docs/flights.md`](../flights.md), "Completion"):
entering completion, from `reviewing`, starts a committer with the executor's
harness whose prompt covers steps 1, 2, and 4: stage everything except
`review/`, run the commit skill with its gates once, remove `review/` only
after the work commit succeeds, then run the explicit amend mode of finalize on
the flight's spec and plan and amend the work commit. The pilot supervises the
committer and routes a gate failure to a fresh reviewer. Steps 3, 3a, and 5-7
are not built; the user marks the flight done by hand.

### Architecture discussion

The user proposed two directions after clarifying that Schmux must own permission
decisions:

1. A supervisor per workspace with full context, repository access, and authority
   over repository-specific questions; potentially a cheap model such as Haiku.
2. Jev for decisions and non-language judgements. After the feasibility spike,
   the user selected Jev for the decision points where it is useful. The original
   reference to a forthcoming OpenAI decisions API is not an implementation
   dependency.

Both directions are now in use for planning supervision. The pilot is the
supervisor: a persistent chat session in the flight's workspace with repository
access and the planner's conversation. Jev classifies completed planner
responses and decides when the pilot is woken. Neither yet answers questions or
permissions; that authority remains with the user.

The earlier agent's Go state machine, stateless oneshot judges, and decision
ledger were a separate proposal, never selected by the user. Nor was a command
allowlist/denylist ladder approved.

Before more automation, the existing philosophy's orchestration non-goals need
reconciliation with the requested behavior; they must not be used to silently
eliminate the user's pilot proposal.

Source: lines 364, 388, 390, 403; `docs/PHILOSOPHY.md:25`.

### Historical backlog and scheduling proposals

The initial request called for queued entries with controls such as models,
session count, and workspace choice, plus some automatic kickoff behavior.

The previous agent proposed request/context fields, repository/base branch,
harness and model per role, priority, a concurrency limit defaulting to one,
hold, and start-now. It also proposed excluding multi-session roles and excluding
entries waiting on the user from the concurrency count. None received a user
decision before the naming discussion failed.

These remain proposals. Flights currently use explicit operations on the list;
automatic scheduling is outside both delivery stages.

Source: lines 1, 497–500.

## Questions before future automation

- **Questions and permissions.** Native question and permission requests pause
  a session until a human answers. Flights were discussed as needing an answer
  path so they do not stall, and the user has said Schmux owns permission
  decisions. Who answers which requests, with what authority, is undecided;
  some may stay human-only. At minimum, a waiting session should be visible on
  its flight.
- **Workflow position.** Decide how a flight's position within a phase is
  represented: named workflow steps, or the phases plus derived state. Users
  should not have to manage fine-grained steps.
- **Checkpoint evidence.** Spike skill-compliance evidence for both harnesses.
  Claude records show skill invocations, question answers, and file writes with
  their positions; Codex is unverified. Invocation alone cannot establish that
  brainstorming or coherence review was performed properly.
- **Decision architecture.** Validate it against real flights and transcript
  replay, including when a checkpoint result should reach the pilot.
- **Session recovery.** Define how a flight recovers a pilot or planner that
  dies or is replaced outside dashboard Restart. Restarting mid-turn may merge
  an interrupted turn into the next one; this has not been tested.
- **Snapshot and restore.** Define snapshot/restore ordering and preservation of
  revised plans.
- **Backlog automation.** Specify backlog controls, kickoff, concurrency, retry
  limits, and timeouts.
- **Repository workflow bindings.** Specify how skills and verification commands
  are selected per repository. Spec and plan locations are not bindings; the
  skills choose them.
- **Restart safety for automation.** Flight state already survives daemon
  restart; future automatic actions must ensure a repeated event cannot spawn
  duplicate implementers or repeat completion actions.
- **Completion.** Resolve the remaining completion details and reconcile affected
  product docs.
- **Review before building.** Review the resulting coherent automation spec with
  the user before writing an automation implementation plan.
