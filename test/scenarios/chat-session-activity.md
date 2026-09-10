# Follow background work from a chat session

A user monitors several workers below the transcript, expands the activity list,
and jumps to a worker's launch while keeping an unsent draft. A task completing
after the parent turn closes updates its original transcript entry.

## Preconditions

- The isolated scenario daemon serves the real dashboard.
- Dashboard and chat WebSockets replay controlled harness records. The Claude
  agent/task/tool IDs and async launch envelopes follow the captured activity
  fixtures; the five-worker sequence and delayed completion are synthetic.
- Run in both light and dark themes with reduced motion enabled.

## Verifications

- Stop appears at the right edge of the activity header during a connected active
  turn, stays visible while the task list scrolls, and sends the interrupt frame.
  It remains available with no task rows and disappears when the turn ends.

- Completed foreground work with multiple captured heartbeat IDs produces no
  active rows. Heartbeats for a live task update its single row.
- Task descriptions, commands, reported runtime, and explicitly labeled update
  age are visible without opening Details.
- Five workers produce three initial rows; Show all reveals exactly five.
- The agent's launch ID and task ID produce one activity row.
- Jump moves keyboard focus to the correct transcript tool, brings it into view,
  and suspends automatic following so the next update does not undo navigation.
- An unsent composer draft survives navigation and incoming events.
- A late completion updates the original tool's status and result, retaining the
  launch response in Details, and its temporary activity row disappears immediately.
- Buttons for operations without a transcript target are absent.
- The transcript and composer remain usable with the activity list expanded.
- Activity occupies its own bounded row between transcript and composer. Their
  rectangles never overlap, including in a shorter window and with a growing draft.
- The final chat paragraph stays visible and pinned to the bottom when activity
  expands, collapses, shows details, or disappears when work completes.
- Once the user navigates to earlier chat, activity resizing preserves that
  reading position. Resume restores following through later layout changes.
