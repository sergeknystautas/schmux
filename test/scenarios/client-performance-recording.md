# Record client performance and send it to the performance chat

A schmux developer in dev mode starts the client performance recorder from the
sidebar, opens the performance chat, and sends a message with the recording
attached. The daemon endpoints and the chat socket are controlled fixtures.

## Preconditions

- The dashboard is running with `client_performance` enabled, a repo named `schmux`, and a chat target.
- `/api/healthz` reports dev mode; `/api/dev/status`, `/api/client-performance/session`, the attachment upload, `/ws/dashboard`, and `/ws/chat/perf-session` are Playwright routes.

## Verifications

- The sidebar shows a Client Performance pane with Start recording.
- After Start recording the header reads `Client Performance · REC` and the status line shows minutes and stalls.
- Open performance chat posts empty ids, receives `perf-workspace`/`perf-session`, and navigates to `/sessions/perf-session`.
- The composer shows a checked `Recording since HH:MM · attach` checkbox.
- Sending uploads one JSON attachment to `perf-workspace` whose body has `build`, `environment`, and `timeline` keys and `build.sourceWorkspace` equal to the dev status source workspace.
- The sent chat message text ends with a `File attachments:` block holding the path the upload route returned.
