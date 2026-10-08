# Client Performance Recording

## What it is

A recording of what the user's browser main thread was doing while the schmux dashboard felt slow: keystrokes that lag, a sidebar that falls behind the daemon, a tab that gets heavier over an afternoon. An agent in a schmux checkout reads the recording, finds the cause in the source, fixes it, and proves the fix with a second recording.

The recorder is dev-only: it runs only while the daemon reports `dev_mode`, `client_performance.enabled` is set, and the user has pressed **Start recording** in this browser. The file reaches the agent as a workspace attachment. In the dashboard's performance chat (the sidebar pane **Open performance chat** opens it), the composer uploads the recording into the chat's workspace and appends its path to the message. The agent reads the file from the workspace. The daemon analyzes nothing.

## Reading a recording

1. Read `timeline` and find the stalls: rows where `loop` (event loop delay) or `longTask` reached 100ms. The `stalls` array lists their indexes into `timeline`.
2. For each stall, read the detail buffers for that second — `longTasks`, `commits`, `websocket.perSecond`, `websocket.individual`, `fetches`, `terminals`, `interactions` — and note the row's `route` and `hidden`.
3. Group the stalls by what they share and name a cause: the same WebSocket message type before every stall points at that handler and what re-renders from it; sidebar commits on every broadcast point at the whole list re-rendering for one changed row; terminal frame bursts with high `handleOutputP99` point at the xterm write path; a heap that climbs and never comes down points at retained terminals, sockets, or transcripts; stalls only after a route change point at mount cost; stalls while `hidden` is true point at work that should pause.
4. Confirm the cause in the dashboard source under `assets/dashboard/src` before saying it. If the code cannot produce the stall, the cause is wrong; go back to step 3.
5. Check `build`. If `viteDev` is false, `commits` is empty because React's `Profiler` only fires in the development build — say so instead of reading it as evidence.
6. Fix the cause, then have the user switch dev mode to your workspace, reload, and record again. Compare the new file with this one on the measures that named the cause — stall count, worst `loop`, handler time per message type, commit time per Profiler id, heap slope — and say whether the cause is gone.

## The file

One JSON file, `client-perf-<timestamp>-<browser id>.json`. `version` is 1. Every `t` is a millisecond timestamp in daemon time (see **Time**).

### `build`

Which code the recording was taken against.

- `version` — daemon version from `/api/healthz`.
- `devMode` — the daemon's dev mode flag.
- `sourceWorkspace` — the worktree whose code dev mode is serving, from `/api/dev/status`. After the user switches dev mode to your workspace, this names your workspace; that is how a second recording is matched to your fix.
- `viteDev` — true when the React bundle is Vite's development build. `commits` is only populated when this is true.

### `environment`

Rules the machine in or out.

- `userAgent`, `cpus` (`hardwareConcurrency`), `deviceMemoryGb` (null where unsupported).
- `viewport` (`w`, `h`), `pixelRatio`, `host`.
- `remoteClient` — true when the dashboard is a remote client.
- `unsupported` — observers and APIs the browser lacked (for example `longtask`, `event`, `performance.memory`). The recorder notes each and keeps recording everything else.
- `clockOffsetMs` — see **Time**.
- `persistMaxMs` — the worst main-thread cost of one of the recorder's own snapshot writes (building the snapshot plus the IndexedDB structured clone). A long task of about this size every 5 seconds is the recorder, not the dashboard.

### `workload`

How much the dashboard was holding when the file was built.

- `workspaces`, `sessions`, `running` — counts from the sessions snapshot.
- `chats` — sessions of kind chat; `terminals` — the rest.
- `mountedTerminals`, `socketsByPath` — open socket count per normalized path.
- `lastDashboardMessageBytes` — byte size of the most recent `/ws/dashboard` message.
- `panels` — `ui.panels` flags; `flags` — client-facing config flags (`chatSessions`, `chatLoadProfiling`, `desync`, `ioWorkspaceTelemetry`).

### `timeline`

One `PerfRow` per second, oldest dropped past the cap. This is where step 1 starts.

- `t` — the second's timestamp (daemon time).
- `loop` — event loop delay, measured once a second as a `MessageChannel` round trip. How late the main thread was.
- `longTask` — total long-task time in the second, from the `longtask` observer.
- `commit` — total React commit time in the second, all Profiler ids.
- `wsBytes`, `wsHandler` — total WebSocket bytes received and total handler time in the second.
- `fetches` — count of same-origin `/api/` fetches completed in the second.
- `route` — `location.pathname` during the second.
- `hidden` — whether the tab was hidden.

A row is a **stall** when `loop >= 100` or `longTask >= 100`. The `stalls` array lists the indexes of stall rows into `timeline`; the sidebar pane counts stalls by the same rule, so the number the user sees is the number you find.

### `longTasks`

`{ t, duration, attribution }` — each long task: when it started, how long it ran, and the attribution container type where supported. Which script blocked the thread.

### `interactions`

`{ t, type, target, inputDelay, processing, presentation }` — slow inputs (total at least 100ms) from the `event` timing observer: when, the event type and target tag, and where the time went.

### `commits`

`{ t, id, phase, duration, route }` — React commits over 16ms, by Profiler id (`sidebar`, `main`, `chat-transcript`, `SessionDetailPage`). Which tree re-rendered and how long it took. Empty in a production React build.

### `websocket`

`{ perSecond, individual }` — every socket created through `transport.createWebSocket`.

- `perSecond`: `{ t, path, type, count, bytes, handlerMs }` — one row per second per path-and-type: how many messages arrived, their total bytes, and their total handler time. `path` is normalized: `/ws/terminal/<id>` becomes `/ws/terminal/:id` (likewise `/ws/chat/:id`, `/ws/logs/fence/:id`), so one row serves every session.
- `individual`: `{ t, path, type, bytes, handlerMs }` — messages whose handler took over 5ms or that were over 50KB, kept one by one.
- `type` is the message's JSON `"type"` field; `binary` for non-text frames (counted by `byteLength`, never parsed); `untyped` for text frames without a type field in the first 200 bytes.

### `fetches`

`{ t, endpoint, duration, bytes }` — same-origin `/api/` fetches from the `resource` observer, ids replaced with `:id` in the endpoint (so `/api/sessions/abc-123/output` is `/api/sessions/:id/output`), with duration and transfer size.

### `terminals`

`{ t, id, frames, bytes, handleOutputP50, handleOutputP99 }` — per mounted terminal per second: frames and bytes received (delta since the previous second) and the global handle-output latency samples from `inputLatency`. Whether terminal output was the load.

### `chatLoads`

The chat load samples `captureChatLoad` already collects (`sessionId`, `loadId`, `at`, `start`, `frameChars`, `records`, resume fields). How long chat transcripts took to load.

### `memory`

`{ t, heapBytes, domNodes, terminals, sockets }` — every 10 seconds: JS heap size where `performance.memory` exists (null otherwise), DOM node count, mounted terminals, open sockets. A heap that climbs across the recording and never comes down points at retained terminals, sockets, or transcripts.

### `navigation`

`{ t, kind, route, firstCommitMs?, paintMs? }` — `route` changes (with time from navigation to first React commit and to paint), `visible`/`hidden` visibility changes, and `reload` when the tab restored a recording after a reload. Stalls that follow route changes point at mount cost on that page; stalls while hidden point at work that should pause.

### `errors`

`{ t, message }` — window errors and unhandled rejections, message only. Whether something broke rather than slowed.

## Caps and thresholds

| Buffer                 | Cap                   | Threshold                           |
| ---------------------- | --------------------- | ----------------------------------- |
| `timeline`             | 3600 rows (1 hour)    | stall at 100ms `loop` or `longTask` |
| `stalls`               | indexes of stall rows | same rule                           |
| `longTasks`            | 2000                  | —                                   |
| `interactions`         | 2000                  | kept when total ≥ 100ms             |
| `commits`              | 2000                  | kept when over 16ms                 |
| `websocket.perSecond`  | 3600 rows             | —                                   |
| `websocket.individual` | 2000                  | handler over 5ms or over 50KB       |
| `fetches`              | 2000                  | —                                   |
| `terminals`            | 3600 rows             | —                                   |
| `chatLoads`            | 200                   | —                                   |
| `memory`               | 360                   | every 10s                           |
| `navigation`           | 500                   | —                                   |
| `errors`               | 100                   | —                                   |

When a ring hits its cap the oldest rows drop; counters such as the stall count keep counting from Start.

## Time

Every `t` in the file is daemon time: the browser's clock corrected by the offset it measured from the `Date` header of `/api/healthz` (`environment.clockOffsetMs`), so a stall can be lined up with daemon logs. Two files are comparable because they share rows, buffers, and thresholds, and because `build.sourceWorkspace` says which code each came from.

## The ids the browser keeps

The pane calls `POST /api/client-performance/session` with `{ "workspace_id", "session_id" }` — the pair the recorder kept from the last call, both empty the first time. The daemon returns the pair that is valid now: the same pair if the session is still running; otherwise it settles the workspace (the sent one if it exists, is not disposing, and is on the configured repo and the branch `client-performance`, else the workspace manager finds or creates one) and spawns a new chat session there with the configured target and the embedded prompt. A workspace whose `go.mod` is not schmux's module is refused with 400. The daemon stores nothing about the chat; the recording browser is the only party that remembers which chat is the performance chat. The pair survives a send and a Stop; it is cleared only when the config switch goes off. If the workspace is disposed or the session ends, the next call returns a fresh pair for the recorder to keep.

## Key files

- `assets/dashboard/src/lib/clientPerf.ts` — the recorder: rings, stall rule, file builder, persistence.
- `assets/dashboard/src/lib/clientPerfObservers.ts` — PerformanceObservers, event-loop probe, visibility, errors, clock offset.
- `assets/dashboard/src/lib/clientPerfStore.ts` — IndexedDB snapshots, one per tab.
- `assets/dashboard/src/lib/transport.ts` — wraps every `createWebSocket` in the recorder.
- `assets/dashboard/src/components/ClientPerformance.tsx` — the sidebar pane.
- `assets/dashboard/src/components/chat/Composer.tsx` — the attach checkbox and upload-on-send.
- `internal/dashboard/handlers_client_performance.go` — the ensure-session endpoint.
- `internal/dashboard/client_performance_prompt.md` — the prompt the chat starts with.
