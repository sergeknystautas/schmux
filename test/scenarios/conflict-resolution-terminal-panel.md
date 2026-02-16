# Conflict resolution progress with terminal panel

A user triggers conflict resolution on a workspace that has merge conflicts.
While the agent is working, the dashboard broadcasts step-by-step progress.
The resolve-conflict page shows the progress steps with status indicators, and
the resolution ultimately completes (or fails). If the agent runs in a tmux
session, a `tmux_session` field is broadcast so the UI can display an embedded
terminal panel.

The user navigates to the resolve-conflict page for the workspace. While
resolution is in progress, the dashboard WebSocket should broadcast
`linear_sync_resolve_conflict` messages with steps. After resolution finishes,
the final status (done or failed) should be reflected. The resolve-conflict
page should show the header, steps, and final status message.

Note: In the test environment, the conflict resolution target is a user-defined
command (no real LLM), so `ExecuteTargetStreamed` falls back to non-streamed
execution and no `tmux_session` field is emitted. The terminal panel rendering
is tested separately via React unit tests.

## Preconditions

- The daemon is running with a spawned session in a workspace
- The workspace has a conflicting commit on main (diverged branch with
  conflicting changes to the same file)
- A conflict resolution target is configured as a user-defined promptable
  command that outputs valid JSON

## Verifications

- Triggering conflict resolution via POST returns 202 (accepted)
- A second trigger while in progress returns 409 (conflict)
- The dashboard WebSocket broadcasts a `linear_sync_resolve_conflict` message
  with status `in_progress` and steps array
- The resolve-conflict page shows "Resolving conflicts..." heading while active
- After resolution finishes, the dashboard WebSocket broadcasts a final
  message with status "done" or "failed"
- The resolve-conflict page shows the final status message
- The conflict resolution state includes the conflicting commit hash
- After dismissing a completed/failed state, re-triggering returns 202
