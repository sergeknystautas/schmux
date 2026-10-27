# Flights: review and completion

A flight's executor has finished. The user now drives the flight through the
review phase and into the completion phase from the flight detail page: an
executing flight offers **Enter review**, a reviewing flight offers **Enter
completion**, and each transition starts the next agent and records it on the
flight.

## Preconditions

- The daemon is running.
- The flight API and the dashboard snapshot are delivered under test control:
  the scenario image has no chat harness, so it cannot start real reviewer or
  committer sessions. The daemon's own review and completion transitions are
  covered by the Go handler and manager tests.
- The controlled flight starts in `executing` with its pilot, planner, and
  executor sessions, and no reviewer classification.

## Verifications

- A flight in `executing` offers **Enter review** and does not offer **Enter
  completion**.
- Clicking **Enter review** sends `POST /api/flights/{id}/review`. The flight
  shows `reviewing`, and once the dashboard snapshot carries the reviewer
  session, the activity line names the Reviewer and links to its session.
- A flight in `reviewing` offers **Enter completion** even though no reviewer
  has reported, and shows no review reading.
- Clicking **Enter completion** sends `POST /api/flights/{id}/completion`. The
  flight shows `completing`, and once the dashboard snapshot carries the
  committer session, the activity line names Commit and links to its session.
  **Mark done** is offered.
