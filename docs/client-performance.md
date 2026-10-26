# Client Performance Recording

## What it is

A recording of what the user's browser main thread was doing while the schmux dashboard felt slow: keystrokes that lag, a sidebar that falls behind the daemon, a tab that gets heavier over an afternoon. An agent in a schmux checkout uses the recording, source, and conversation with the user to diagnose and fix the cause.

The recorder is dev-only: it runs only while the daemon reports `dev_mode`, `client_performance.enabled` is set, and the user has pressed **Start recording** in this browser (see **Recorder lifecycle**). The file reaches the agent as a workspace attachment. The sidebar sends the recording directly to the agent in its dedicated workspace as an ordinary chat attachment. The daemon analyzes nothing.

## Architecture decisions

- The daemon deliberately performs no analysis. A recording is evidence for an agent that can read the schmux source, ask the user what felt slow, request another recording with instructions, and develop a fix; the daemon cannot make those judgments.
- Capture is a page-memory singleton. The browser retains no recording snapshots or history, so reloading discards an unsent capture instead of accumulating browser-local data.
- Instrumentation wraps the shared WebSocket transport and terminal stream rather than individual call sites. Every dashboard socket and mounted terminal is therefore counted without duplicating observer code at each consumer.
- Recording timestamps use the clock offset from `/api/healthz`. This aligns browser stalls with daemon logs even when the browser and daemon clocks differ.
- The performance agent is launched through ordinary spawn orchestration into a dedicated schmux workspace. The daemon stores only its workspace/session association, reuses the workspace for replacement agents, and leaves source control to the normal spawn path.
- Stop sends directly to the session bound when recording began. The daemon saves the JSON as an ordinary workspace attachment and sends its path through the existing chat runtime, so the conversation and workspace retain one consistent delivery mechanism.
- Recording is separate from panel visibility: `client_performance.enabled` permits capture, while `ui.panels.clientPerformance` only controls whether the sidebar pane is shown.

## Reading a recording

Investigate the slowness the user describes. Read the recording for relevant evidence: input delays, React work, WebSocket handlers, terminal traffic, chat loading, or memory growth. The `stalls` array indexes `timeline` rows where `loop` or `longTask` reached 100ms; use it to locate substantial blocking without excluding problems below that threshold. Read the relevant detail buffers and note the route and whether the tab was hidden.

Follow the evidence into the dashboard source under `assets/dashboard/src`. Attribution provides useful leads; it does not identify the responsible function for every stall. Distinguish an observed symptom from a suspected cause. Finding code that could produce a symptom does not establish that it did. When the evidence is insufficient, ask a focused question or instrument the suspected code path.

Check `build`. If `viteDev` is false, `commits` is empty because React's `Profiler` only fires in the development build — say so instead of reading it as evidence.

Develop and validate changes as the investigation requires. Ask for more recordings when useful.

## The file

One JSON file, `client-perf-<timestamp>-<browser id>.json`. Every `t` is a millisecond timestamp in daemon time (see **Time**).

### Envelope

Four top-level fields come before `build`:

- `version` — always 1.
- `browserId` — a random id kept in this browser's localStorage, the same in every file from this browser. Files with the same `browserId` came from the same machine and browser profile.
- `startedAt` — daemon time when recording actually began after clock synchronization. It is kept until send/discard and across dev-mode suspension within the current page.
- `builtAt` — daemon time when this file was built for upload. A failed delivery leaves the stopped recording ready for another send attempt.

### `build`

Context about the running dashboard when the file was built.

- `version` — daemon version from `/api/healthz`.
- `devMode` — the daemon's dev mode flag.
- `sourceWorkspace` — the worktree whose code dev mode is serving, from `/api/dev/status`.
- `viteDev` — true when the React bundle is Vite's development build. `commits` is only populated when this is true.

### `environment`

Rules the machine in or out.

- `userAgent`, `cpus` (`hardwareConcurrency`), `deviceMemoryGb` (null where unsupported).
- `viewport` (`w`, `h`), `pixelRatio`, `host`.
- `remoteClient` — true when the dashboard is a remote client.
- `unsupported` — observers and APIs the browser lacked (for example `longtask`, `event`, `performance.memory`, `MessageChannel`), plus `healthz-date` when the clock offset could not be measured. The recorder notes each and keeps recording everything else.
- `clockOffsetMs` — see **Time**.

### `workload`

How much the dashboard was holding when the file was built.

- `workspaces`, `sessions`, `running` — counts from the sessions snapshot.
- `chats` — sessions of kind chat; `terminals` — the rest.
- `mountedTerminals` — terminals mounted in this tab; `socketsByPath` — open socket count per normalized path.
- `lastDashboardMessageBytes` — byte size of the most recent `/ws/dashboard` message.

`mountedTerminals`, `socketsByPath`, and `lastDashboardMessageBytes` are read when the file is built, so they describe the uploaded file. A later retry builds another file from the retained recording. The other fields are updated whenever the sessions snapshot or the config changes.

- `panels` — `ui.panels` flags; `flags` — client-facing config flags (`chatSessions`, `chatLoadProfiling`, `desync`, `ioWorkspaceTelemetry`).

### `timeline`

One `PerfRow` per second, oldest dropped past the cap. The exported file also includes an unfinished final second when measurements have accumulated since the last tick. Building the file does not close the live second or change the tick cadence. Timeline rows and detail buffers are collected whether or not a stall occurs.

- `t` — the second's timestamp (daemon time).
- `loop` — event loop delay, measured once a second as a `MessageChannel` round trip. How late the main thread was.
- `longTask` — total long-task time in the second, from the `longtask` observer.
- `commit` — total React commit time in the second, all Profiler ids.
- `wsBytes`, `wsHandler` — total WebSocket bytes received and total handler time in the second.
- `fetches` — count of same-origin `/api/` fetches completed in the second.
- `route` — `location.pathname` during the second.
- `hidden` — whether the tab was hidden.

A row is a **stall** when `loop >= 100` or `longTask >= 100`. The `stalls` array lists the indexes of stall rows in the retained `timeline`.

### `longTasks`

`{ t, duration, attribution }` — each long task: when it started, how long it ran, and the first attribution entry's container type where supported. This field provides container attribution rather than a script URL or stack trace.

### `interactions`

`{ t, type, target, inputDelay, processing, presentation }` — inputs reported by the `event` timing observer, registered with its minimum `durationThreshold: 16`: when, the event type and target tag, and where the time went. Every delivered entry is retained without a second duration filter; input lag below the 100ms stall threshold remains available to the agent. The browser controls which event types and durations this API exposes.

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

`{ t, id, frames, bytes, handleOutputP50, handleOutputP99 }` — per mounted terminal per second: frames and bytes received (delta since the previous second) and the p50 and p99 of the global handle-output time from `inputLatency` (how long `handleOutput` took per frame). Whether terminal output was the load.

Frames and bytes are counted where the terminal stream receives output, whether or not desync diagnostics are on: a binary frame counts its `byteLength`; a remote session's JSON output frame and a legacy non-JSON text frame count the string's `length`, matching how `websocket` measures text. JSON control messages (`stats`, `controlMode`, and the like) are not counted.

### `chatLoads`

The chat load samples `captureChatLoad` already collects, one per transcript load. How long chat transcripts took to load and where the time went.

- `sessionId`; `loadId` — the socket attempt the load belongs to.
- `at` — ISO timestamp of the load, corrected to daemon time like every `t`. (The copy uploaded to chat telemetry keeps the browser's clock.)
- `start` — what began the load: `click` (navigation from the sidebar), `view` (the page opened directly), or `reconnect`.
- `frameChars`, `records` — size of the history frame and how many records it carried.
- Resume fields: `cacheHit` (the frame extended the cached conversation), `since` and `lastSeq` (the sequence requested and reached), `durableRecords` (durable records in the frame).
- Timing, in milliseconds: `routeToSocketMs` (navigation to socket creation), `socketOpenMs`, `historyWaitMs` (open to history frame), `parseMs`, `resolveMs` (optional), `reduceMs`, `commitMs`, `afterPaintMs`, and `totalMs`.
- `reduction` — present when chat load profiling is on: `categories` (`category`, `records`, `durationMs`, `maxMs` per record category), `probeOverheadMs`, and counts of `items`, `turns`, `segments`, `images`, and `operations`.

### `memory`

`{ t, heapBytes, domNodes, terminals, sockets }` — every 10 seconds: JS heap size where `performance.memory` exists (null otherwise), DOM node count, mounted terminals, open sockets. A heap that climbs across the recording and never comes down points at retained terminals, sockets, or transcripts.

### `navigation`

`{ t, kind, route, firstCommitMs?, paintMs? }` — `route` changes (with time from navigation to first React commit and to paint) and `visible`/`hidden` visibility changes. A route is recorded before the commit that renders it, so `firstCommitMs` times that commit. Stalls that follow route changes point at mount cost on that page; stalls while hidden point at work that should pause.

### `errors`

`{ t, message }` — window errors and unhandled rejections, message only. Whether something broke rather than slowed.

## Caps and thresholds

| Buffer                 | Cap                   | Threshold                           |
| ---------------------- | --------------------- | ----------------------------------- |
| `timeline`             | 3600 rows (1 hour)    | stall at 100ms `loop` or `longTask` |
| `stalls`               | indexes of stall rows | same rule                           |
| `longTasks`            | 2000                  | —                                   |
| `interactions`         | 2000                  | observer threshold 16ms             |
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

Timestamps in the file — every `t`, `startedAt`, `builtAt`, and `chatLoads[].at` — use the browser's clock corrected by the offset measured from the `Date` header of `/api/healthz` (`environment.clockOffsetMs`), so a stall can be lined up with daemon logs. Start and resuming a suspended in-memory recording wait for that response before recording anything. Every failed synchronization, including an invalid or missing `Date` header, uses a zero offset and adds `healthz-date` to `environment.unsupported`; in that case timestamps use the uncorrected browser clock.

## Gotchas

- React `Profiler` data exists only in the Vite development build. A daemon run with `--dev-mode` but without the Vite proxy serves the embedded production bundle and produces an empty `commits` buffer; check `build.viteDev` before interpreting it.
- `stalls` contains indexes into the retained `timeline` ring. When the ring drops older rows, the sidebar's stall count can still include those dropped seconds because it counts from Start rather than rebuilding the count from the file.
- The destination is bound at Record. Stop remains available if live session status or the daemon association changes, and a failed send keeps the completed capture for retry instead of silently substituting another agent.
- The dashboard WebSocket carries `client_performance` only in dev mode with recording enabled. Enabling the panel alone performs no workspace lookup or launch.
- Local workspaces may be reached through symlinked paths. Harness launch and attachment saving resolve those links first so the agent's working directory and the physical attachment path agree.
- Dev-mode routes are built into the daemon binary. A stale daemon can serve a dashboard that shows the panel while lacking the current recording endpoint; rebuild the backend before diagnosing a 404 as an application error.

## Workspace and recording controls

Enable the Client Performance panel through the shared sidebar panel preferences in Settings → Advanced. The recorder also requires dev mode and client_performance.enabled.

**Launch** follows Build Monitor: the ordinary spawn orchestration creates a dedicated workspace on a unique `fix/client-performance-<id>` branch in the configured schmux repo and starts its agent. Once the agent is running, the sidebar shows View and Record. The daemon stores the returned workspace/session IDs in `~/.schmux/client-performance.json`; the sidebar loads them after reload and reads live status from the dashboard WebSocket; recording contents are not persisted to browser storage. If the agent ends while no recording is pending, Launch starts a replacement in the tracked workspace.

The launch prompt contains the recording format so a checkout without this feature's documentation does not need another workspace. Local launches resolve symlinked workspace paths before supplying the working directory to the harness; recording attachment paths use the same physical workspace.

Enabling the panel does not require a workspace, fetch a workspace, or launch an agent. Existing agent associations arrive in the ordinary dashboard WebSocket snapshot. State-reading errors from that snapshot remain visible, as do errors from explicit Launch and Send actions.

- **Launch** is the initial action. Recording controls appear only after the agent is running.
- **View** returns to the agent conversation.
- **Record** binds the capture to that agent session and captures browser activity. The destination remains in page memory with the recording.
- **Stop** remains available throughout capture, even if the agent's live status or association changes. It finishes capture and sends to the bound session. Successful chat-runtime acceptance clears the capture and opens that agent. Failed delivery displays the error and offers Send recording to retry the stopped capture; it never silently substitutes Launch or another destination.
- **Discard** confirms before clearing the recording.
- Reload or page close discards the recording. Config-off discards it too. Losing dev mode pauses observation; a completed recording does not resume.

The ordinary chat composer handles the user's conversation without recording-specific controls.

## Key files

| File                                                    | Purpose                                                                               |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/clientPerf.ts`                | In-memory recorder: rings, stall rule, lifecycle, and file builder                    |
| `assets/dashboard/src/lib/clientPerfObservers.ts`       | PerformanceObservers, event-loop probe, visibility, errors, and clock synchronization |
| `assets/dashboard/src/hooks/useClientPerf.ts`           | Pane state and route navigation timing                                                |
| `assets/dashboard/src/lib/terminalStream.ts`            | Terminal frame and byte counters                                                      |
| `assets/dashboard/src/lib/transport.ts`                 | Wraps every dashboard WebSocket for recording                                         |
| `assets/dashboard/src/components/ClientPerformance.tsx` | Sidebar pane and recording workflow                                                   |
| `internal/dashboard/handlers_client_performance.go`     | Performance workspace launch, association, and recording delivery                     |
| `internal/dashboard/client_performance_prompt.md`       | Embedded prompt sent to the performance agent                                         |
| `internal/api/contracts/client_performance.go`          | API request/response contracts                                                        |

## Common modification patterns

- To add captured data, extend the recorder's buffers and file builder in `clientPerf.ts`, feed it from the relevant observer or call site, describe the field above, and cover retention/build behavior in the nearby tests. Update the embedded prompt only when an agent needs new interpretation guidance.
- To change the sidebar workflow, start at `ClientPerformance.tsx` and preserve the Launch → View/Record → Stop sequence. Update the component test and `test/scenarios/client-performance-recording.md`, regenerate or adjust its Playwright test, and update `docs/web.md` with the user-visible controls.
- To change daemon delivery, edit `handlers_client_performance.go`, its handler tests, and `docs/api.md`. Keep the file inside the performance workspace and send it through the existing chat runtime.
- To change configuration, update the Go contract and config structures, run `go run ./cmd/gen-types`, wire the fields through `useConfigForm` and `buildConfigUpdate`, and update the Advanced tab plus `docs/api.md`. Never edit `types.generated.ts` directly.
