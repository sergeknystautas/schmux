# Flights: pilot and classifier

Status: design for review, 2026-10-18. Replaces the earlier supervised-planning
proposal in this file. Parent workflow: [flights.md](flights.md), especially
its planning section. This increment gets the pilot and classifier running
so the user can learn from the four backlog flights. It does not automate
planning or implement the full workflow reference.

For this increment, this spec supersedes the parent's stage-1 restriction on
corrective messages: the user has requested the monitor-to-pilot-to-planner
exchange. Other automatic workflow actions remain excluded.

## Evidence before design

Question: can monitoring observe planner responses and send chat messages
without requiring the planner to emit schmux events? Yes, at the integration
level: `chat.Runtime.Subscribe` supplies durable history followed by live
records (`internal/chat/runtime.go:697`), and `Runtime.Send` records and
queues input (`runtime.go:856`). These are harness conversation records,
not agent-authored schmux events. This read-only check establishes the
integration points; restart and delivery behavior still need implementation tests.

Question: is Jev a plausible classifier for completed planner responses?
Yes. The [existing spike](../../review/jev-spike.html) classified 21 of 22
real final assistant messages correctly for turn-end state (report line 218).
It used direct HTTP requests (line 150). It did not establish production
accuracy, validate Codex responses, or test whole turns containing tool output
(line 423). No new live spike was needed to establish this starting point.

## Scope

Build four connected capabilities:

1. Start a persistent pilot chat session for the flight workspace.
2. Monitor completed planner responses and run two Jev classifiers.
3. Send relevant classifications and their evidence into the pilot chat.
4. Deliver validated pilot decisions directly to the planner inside the daemon.

The user continues the conversation, answers questions and permissions in the
existing session UI, and manually advances the flight. No automatic permission
answers, execution, restart, wipe, scheduling, or completion enforcement.
The pilot may return ordinary planning guidance for direct daemon delivery;
there is no per-message user approval step in this increment. This permission
does not authorize destructive workflow actions.

## Pilot startup and responsibility

Takeoff starts one pilot and one planner in the same assigned workspace,
using the existing local chat spawn path. Record the pilot as another flight
session role, with the same assignment/outcome conventions as the planner.
The pilot is part of the classifier system, not something that varies per
flight. Its model is flight-manager configuration, chosen in Settings > Flights >
Pilot Supervision. Flights carry no pilot model, and the planner model is never
a fallback. Takeoff without a configured chat-capable pilot model is rejected
before allocating a workspace or spawning sessions.

Schmux never dictates where the spec and plan go. The planner prompt tells the
planner to let the brainstorming and writing-plans skills choose each location
and to end the response that completes them with `Spec: <absolute path>` and
`Plan: <absolute path>` lines. The daemon checks every report deterministically:
both reported, absolute, distinct, nonempty, inside the workspace, and not
ignored by git. A violation is retained on the turn and always notifies the
pilot; execution requires a verified report and rechecks it.

Start the pilot before the planner. Its initial prompt contains the original
flight request, workspace and planner role, how the spec and plan are reported, the
planning-only guidance from the parent spec, and the decision format below. It
explicitly excludes the parent's automatic disposal and wipe instructions.
The pilot waits for a monitor notification before sending guidance. Subscribe
the planner monitor from sequence zero once the planner assignment exists,
so startup cannot lose the first response. Persist partial spawn outcomes.
Takeoff is available only from backlog; a planning flight cannot repeat takeoff
after a spawn failure or loss of a session or workspace. The user explicitly
rejected the previously implemented takeoff-retry feature.

The pilot interprets the notification, reads more conversation or repository
evidence when necessary, and either sends guidance or leaves the planner alone.
It can help with brainstorming, request a coherence check, or point out missing
planning work. It does not implement the feature, modify planning documents,
or mark planning complete. The planner remains the author.

Pilot and planner remain ordinary visible chat sessions. Existing session
pages provide the conversations and manual intervention. Flight detail links
to both. No separate pilot dashboard or new conversational UI is required.

## Monitor input

Monitor only sessions assigned to a flight, selecting classifier definitions
by the assigned role. Start with `planner` only. Do not classify pilot replies:
that would create a feedback loop. No executor or committer classifiers ship
in this increment.

Use the durable chat subscription, not status nudges, terminal scraping,
or schmux events. Detect root turn completion from Claude `result` or Codex
`turn/completed`, independent of what the agent says. Extract all top-level
assistant response text belonging to that completed turn, in order. Preserve
multiple response blocks; exclude tool output, thinking, and child-agent
responses. This is the full turn response, not the full conversation history.
Use the same normalized input for both harnesses.

Send that text directly from Go to the Jev HTTP API. Neither Claude nor the
TypeSafe Claude skill sits in the production request path. The pilot receives
only notifications selected by the monitor. A response classified confidently
as routine with no claimed artifact does not consume its context.

Configure the TypeSafe API key through Settings > Flights > Pilot Supervision,
using schmux's standard provider-secret storage and password prompt. Read only
the configured provider key.
Changes apply without restart. Never return the key through the config API or
put it in the classifier file or retained evidence. A missing key is a visible
classification error, with a Flights-page warning linked to its setting.

Native question and permission requests may suspend a turn. They remain
manual in the existing UI and do not count as completed responses or classifier
failures. Once the user answers and the turn completes, classify it normally.
A failed or interrupted turn is recorded and produces a pilot notification
with the harness outcome, without asking Jev to infer that outcome. A successful
turn with no assistant text needs no Jev request.

## Editable classifier questions

Store the definitions in `~/.schmux/flight-monitor.yaml`. Ship an initial
template and create it only if absent. Use the existing YAML dependency
(`go.mod:23`). Questions, choices, notification choices, and confidence
thresholds are editable without rebuilding schmux. This is a small declarative
file, not executable rules or a scripting language.

Read and validate the file before each classification. An invalid file causes
a visible monitor error and leaves the turn pending; it does not silently use
old questions or treat the response as uninteresting. The next attempt uses
the corrected file. No settings editor is required.

Initial file:

```yaml
model: jev-latest
roles:
  planner:
    classifiers:
      - id: attention
        question: >-
          Does this planner response ask for an answer or decision, report
          a blocker or failure, or otherwise require intervention?
        choices:
          - id: needs_attention
            description: A question, decision, blocker, or failure needs attention.
          - id: routine
            description: Progress or explanation that needs no intervention.
        notify_on: [needs_attention]
        min_confidence: 0.7

      - id: planning_artifacts
        question: >-
          Which planning documents does this response explicitly claim are
          written or ready for review? Intent to write them later is not enough.
        choices:
          - id: spec
            description: The specification only.
          - id: plan
            description: The implementation plan only.
          - id: both
            description: Both the specification and implementation plan.
          - id: neither
            description: Neither document is claimed to be written or ready.
        notify_on: [spec, plan, both]
        min_confidence: 0.7
```

These are two independent Choice questions over the same response, sent in
one request. A response can require attention and claim an artifact is ready.
Do not force those observations into one exclusive classification.
Preserve the listed choice order when constructing requests; the spike found
answers could change when choices were reversed (report line 334). Validate
unique IDs, valid notification choices, and thresholds between zero and one.

Role names select question sets; classifier IDs identify results. The engine
supports this file's Choice questions and `notify_on` behavior only. Adding
another question of that shape needs no Go change. Adding a different action
or primitive is later work. Initial thresholds are tunable starting values,
not a demonstrated accuracy guarantee.

## What a ruling does

| Result                                                                        | Monitor behavior                                                                              |
| ----------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Confident `routine` and `neither`                                             | Record the results; do not wake the pilot.                                                    |
| Any confident choice listed in `notify_on`, or any answer below its threshold | Send one combined notification, including all rulings and any uncertainty.                    |
| API or configuration failure                                                  | Record a visible error and leave the turn pending for retry.                                  |
| Claimed spec or plan                                                          | Include the claim in the notification; the pilot checks the actual file before relying on it. |

A notification includes flight and planner IDs, source turn/record range,
classifier answers and confidence, and the full response that caused the
notification. Include the turn's spec/plan report with its check and the planner's durable conversation
file path, resolved through the existing chat storage layout. The pilot can
read that file with its ordinary shell tool when it needs surrounding context;
the full conversation is not injected automatically. A claimed artifact must
be checked in the flight workspace before the pilot relies on it.

Do not build file watchers, diff classifiers, skill-compliance enforcement,
or a planning-position state machine here. The pilot can inspect evidence
when notified. A file existing does not establish that the spec is coherent,
that a skill was followed, or that planning is complete.

## Pilot to planner

The flight worker subscribes to its assigned pilot's durable chat stream.
The pilot's final response is one JSON object containing `notification_id`
and `guidance`, where guidance is a nonblank string or null. Before receiving
a notification, the pilot acknowledges startup with `{"guidance":null}`.
Use the harness's final response, separate from intermediate commentary.
Ordinary prose, malformed JSON, unknown fields, missing guidance, and blank
guidance are visible decision errors and never trigger a send.

For guidance, the daemon checks current flight phase, cleanup state, pilot
and planner assignments, actual workspace membership, planner availability,
and a delivered notification belonging to the current planner. The source
session is established by the subscription; the payload cannot name a
recipient. The daemon persists the exchange and calls the planner chat runtime
directly. No CLI command, shell invocation, or public return endpoint is involved.

Persist source pilot turn, sender, recipient, source notification, message text, returned chat
message ID, and delivery/error state through the flight manager. Show pilot
attribution in the planner conversation. Queueing while the planner is busy
is acceptable; report it as queued rather than received or acted upon. The
existing terminal-oriented tell endpoint is not the chat delivery path.
Use a server-added `[Pilot]` prefix for visible chat attribution; the flight
record retains the sender and exact unprefixed text. No new chat record schema
is required for this increment.

This delivery sends ordinary chat input. It cannot answer native permission
or question cards. Those remain manual, so a pilot message may wait until the
user resolves the request. Retain failures visibly in flight history.

## Minimum persistence and recovery

The flight manager remains the sole writer of flight records. Retain:

- Pilot assignment alongside the existing planner assignment.
- Source session and completed-turn record range, exact classifier input,
  definition snapshot, answers, model, and errors.
- Pending/sent notification and its pilot chat message ID.
- Pilot-to-planner messages and delivery outcomes.

Persist these through the manager's existing persist-then-notify path
(`internal/flights/manager.go:389`). Chat runtimes continue to own conversation
records; the monitor owns no second copy of flight state. Dashboard summaries,
detail views, and retry responses read the same manager-owned results.

Keep record consumption separate from slow HTTP calls and pilot work. Process
classifications in order per planner. Resume from durable records when a
subscription closes or the daemon restarts, deduplicating recorded work by
source session and turn boundary. A pending classification is not lost just
because later chat records have been read.

Support explicit retry of failed classification or delivery, reusing the
recorded work item. Do not blindly resend an exchange whose delivery is
unknown after a crash; expose that outcome for inspection. Full automatic
recovery is outside this increment. Existing disposal must include the pilot
and retained flight communication resources.
On retry, read the current classifier definitions and save a new attempt with
that definition snapshot, preserving the earlier failed attempt for comparison.

## Review and acceptance

The existing flight detail page gains pilot/planner links and a compact
monitor history: source response, questions used, rulings, notification or
error, and outgoing pilot messages. Provide Retry for failed pending work.
Use the existing dashboard broadcast and session pages. This history is the
minimum evidence needed to tune the classifiers during the four flights.

Acceptance checks, following `docs/testing.md`:

1. Takeoff creates a visible pilot and planner; another takeoff during planning
   is rejected without allocating a workspace or spawning sessions.
2. Recorded Claude and Codex turns produce the full intended response text;
   tool output, mid-turn questions, and permissions do not corrupt extraction.
3. A routine response produces stored rulings without a pilot notification;
   either interesting ruling produces one combined notification.
4. Editing the YAML questions changes the next request without a rebuild;
   choice order is preserved and roles without definitions are not classified.
   Invalid YAML and a missing Jev API key are visible errors.
5. A completed structured pilot decision produces an attributed message in its
   assigned planner chat, with queueing and failure reported accurately. Malformed
   decisions and stale assignments are rejected; replay never resends an exchange.
6. Restart/subscription recovery does not lose recorded pending work or replay
   a completed notification. Ambiguous delivery is surfaced.
7. A manual live flight verifies direct Jev classification and the round trip
   through the pilot. No planner-emitted schmux event is needed.

Unit tests use a fake Jev server and chat fixtures. Validate the two initial
questions against real responses before calling their accuracy established;
the earlier spike is feasibility evidence, not acceptance for these wordings.

Implementation reaches flight assignments/startup and daemon wiring to attach
the pilot and monitor; the Jev client and YAML reader to classify responses;
direct chat delivery to provide the return path; API contracts and generated types
to expose monitor history; the Flights settings tab for the pilot model; and the
flight detail UI for links and inspection. Reuse chat delivery with a prefix rather than extending
the chat schema. Update `docs/flights.md`, `docs/chat-sessions.md`,
`docs/cli.md`, and `docs/api.md` with the implemented behavior. No broader
planner automation or implementation plan is part of this spec-writing task.
