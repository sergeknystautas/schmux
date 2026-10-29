# Flights: review and completion

A flight's executor has finished. The pilot advances the flight from planning
to executing and from executing to reviewing on its own, and the user reviews
the work and starts the commit from the flight detail page: an executing flight
offers **Enter review**, a reviewing flight offers **Review and commit**, and
each transition starts the next agent and records it on the flight.

## Preconditions

- The daemon is running.
- The flight API and the dashboard snapshot are delivered under test control:
  the scenario image has no chat harness, so it cannot start real reviewer or
  committer sessions, and it cannot run a pilot that decides to advance a
  phase. The daemon's own review and completion transitions, and the pilot's
  advance decisions, are covered by the Go manager and pilot tests.
- The controlled flight starts in `planning` with its pilot and planner
  sessions, and no reviewer classification.

## Verifications

- A flight in `planning` offers **Enter execution** and not **Review and
  commit**.
- When the dashboard snapshot carries the flight in `executing` with its
  executor session, the detail page follows: the state pill reads `executing`
  and the activity line names the Executor and links to its session. No phase
  button was clicked, so no transition request was sent.
- The next snapshot carries the flight in `reviewing` with its reviewer
  session. The page follows again, and no transition request was sent.
- The story shows the automatic transition as the pilot's: its entry names the
  deciding pilot turn, and its details carry that turn's ID.
- A flight in `reviewing` offers **Review and commit** and the action explains
  that the user reviews the work, artifacts, and running changes before the
  commit starts. Nothing requests completion until that action is clicked.
- Clicking **Review and commit** sends `POST /api/flights/{id}/completion`. The
  flight shows `completing`, and once the dashboard snapshot carries the
  committer session, the activity line names Commit and links to its session.
  No further transition is offered: completing is the last phase, and only
  **Dispose** remains.
- A pilot stop does not disable the manual actions: the phase actions stay
  available and a manual advance is still sent.
- A transition the backend refuses because another operation is unresolved is
  reported to the user in words, naming the operation.
- After a reconnect the dashboard snapshot may carry a flight in `completing`
  whose commit did not succeed. The page shows the failure and claims nothing
  about the work being ready to merge.
