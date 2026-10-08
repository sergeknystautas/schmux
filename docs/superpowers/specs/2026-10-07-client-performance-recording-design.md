# Client performance recording

## Goal

An agent can diagnose and fix schmux's client performance.

The slowness happens in the user's browser: keystrokes that lag, a sidebar that falls behind the daemon, a tab that gets heavier over an afternoon. The agent that could fix it runs in a terminal with no view of that browser, and the user cannot describe a stall in terms the agent can act on. The goal is met when an agent working in a schmux checkout can see what the user's browser was doing when it was slow, find the cause in the source, change it, and show with a second measurement that the change fixed it. The daemon analyzes nothing; the agent does the engineering.

## What the agent needs

Four things follow from the goal, and the rest of this spec provides them.

1. **A record of the browser's main thread during the slow minutes.** Not a summary. The agent has to see, second by second, how blocked the thread was and what was on it, with enough attribution to walk from a stall to a file in the source.
2. **A conversation with the user who felt it.** The record says what the browser did; only the user knows what they were doing and how it felt. The agent has to ask, and has to be able to ask for another recording with instructions.
3. **The schmux source in a workspace it can change.** Diagnosis ends in a branch with a fix on it.
4. **A second record, from the same browser, against the fix.** The agent proves the fix by comparing the two on the measures that named the cause, so the second file has to be comparable with the first and has to say which code it was recorded against.

## How the agent works

1. **Finds where the time goes.** The agent reads the recording's per-second timeline and picks the stalls, the seconds where the main thread was blocked for 100ms or more. For each stall it reads what was on the thread at that second: the long tasks and their attribution, the React commits and their Profiler ids, the WebSocket messages by path and type with their handler time, the fetches in flight, the terminal frames and bytes, the route, and whether the tab was visible.
2. **Names a cause.** The agent groups the stalls by what they share. The same `/ws/dashboard` message type before every stall points at that handler and what `SessionsContext` re-renders from it. Sidebar commits on every broadcast point at the whole list re-rendering for one changed row. Terminal frame bursts with handleOutput p99 over budget point at `terminalStream` and the xterm write path. A heap that climbs across the recording and never comes down points at retained terminals, sockets, or transcripts. Stalls only after a route change point at mount cost on that page. Stalls while the tab was hidden point at work that should pause.
3. **Confirms it in the source.** The agent reads the handler, component, or stream path it named and checks that the code does what the recording shows: what runs per message, what state it sets, what re-renders from that state, what happens per terminal frame. If the code cannot produce the stall, the cause is wrong and the agent goes back to step 2.
4. **Checks with the user.** The agent asks one or two questions: what they were doing, how long it felt, whether a reload clears it, whether it builds over time. If the answers and the recording disagree, the agent asks for another recording and says what to do while it runs.
5. **Fixes it** in its workspace, on its branch: batching the broadcast, memoizing the list rows, coalescing terminal writes, releasing what is retained, deferring work while hidden, whatever the cause calls for.
6. **Proves it.** The agent tells the user how to run its branch and record again. It compares the two files on the measures that named the cause: stall count, worst event loop delay, handler time per message type, commit time per Profiler id, heap slope. It reports before and after and says whether the cause is gone.

## The recording

The recording is one JSON file, `client-perf-<timestamp>-<browser id>.json`, built by the user's browser and written for the agent to read in step 1 and compare in step 6. The agent reads it top down.

**`build`** tells the agent which code it is looking at, which step 6 turns on. It holds the daemon version and `dev_mode` from `useVersionInfo`, `source_workspace` from `/api/dev/status`, which is the worktree whose code dev mode is serving, and `import.meta.env.DEV`, which says whether the React build is the Vite development build. `Profiler` only fires in that build. `./dev.sh` passes `--dev-mode` and `--dev-proxy` together, so under `./dev.sh` the flag is true; a daemon run with `--dev-mode` alone serves the embedded production bundle, and the agent reads an empty `commits` buffer as a consequence of that, not as evidence.

**`environment`** lets the agent rule the machine in or out: user agent, CPU count, device memory, viewport, pixel ratio, host, `isRemoteClient()`, which observers the browser lacked, and a random browser id kept in localStorage so the agent can match files from the same browser.

**`workload`** tells the agent how much the dashboard was holding when the file was built: workspace and session counts from `SessionsContext`, how many were running, how many were chats, mounted terminals, open sockets by path, the byte size of the last `/ws/dashboard` message, `ui.panels`, and the client-facing config flags.

**`timeline`** is where the agent starts step 1: one row per second, up to 3600, oldest dropped. Each row has the event loop delay, measured once a second as a MessageChannel round trip, the long task time, commit time, WebSocket bytes and handler time, fetch count, route, and whether the tab was hidden. A row is a **stall** when its event loop delay or long task total reached 100ms. The sidebar pane counts stalls by the same rule, so the number the user sees is the number the agent finds.

**The detail buffers** are where the agent goes for a stall second. Each is a ring with a fixed cap:

| Buffer         | What the agent reads it for                                                           | Source                                                                                                                                                                                                  | Cap                        |
| -------------- | ------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `longTasks`    | which script blocked the thread, when, for how long                                   | `PerformanceObserver` for `longtask` with attribution, where supported                                                                                                                                  | 2000                       |
| `interactions` | which inputs felt slow and where the time went: input delay, processing, presentation | `PerformanceObserver` for `event` with a 100ms threshold                                                                                                                                                | 2000                       |
| `commits`      | which React tree re-rendered and how long it took                                     | `Profiler` around the sidebar and the main area in `AppShell`, around each chat transcript, and the `SessionDetailPage` Profiler that already exists; commits over 16ms with id, phase, duration, route | 2000                       |
| `websocket`    | which message types arrived, how big, how long their handlers ran                     | every socket made through `transport.createWebSocket`, summed per second by path and type: count, bytes, handler time; messages over 5ms or 50KB kept individually                                      | 3600 rows, 2000 individual |
| `fetches`      | which API calls were slow or large                                                    | `PerformanceObserver` for `resource` on same-origin `/api/` requests, ids replaced in the path, duration, bytes                                                                                         | 2000                       |
| `terminals`    | whether terminal output was the load                                                  | per mounted terminal per second: frames and bytes from `StreamDiagnostics`, handleOutput p50 and p99 from `inputLatency`                                                                                | 3600                       |
| `chatLoads`    | how long chat transcripts took to load                                                | the samples `captureChatLoad` already posts                                                                                                                                                             | 200                        |
| `memory`       | whether the tab is growing                                                            | every 10s: heap size where available, DOM node count, mounted terminals, open sockets                                                                                                                   | 360                        |
| `navigation`   | whether stalls follow route changes or reloads                                        | route changes with time to first commit and to paint; visibility changes; reloads                                                                                                                       | 500                        |
| `errors`       | whether something broke rather than slowed                                            | window errors and unhandled rejections, message only                                                                                                                                                    | 100                        |

Every timestamp in the file is daemon time, the browser's clock corrected by the offset it measured from the `Date` header of `/api/healthz`, so the agent can line a stall up with daemon logs. Two files are comparable because they share rows, buffers, and thresholds, and because `build.source_workspace` says which code each came from. `docs/client-performance.md` writes the format up field by field, and the prompt points the agent at it.

## The conversation

The agent talks to the user in a chat session, the performance chat, and the user's messages carry recordings as file attachments. A chat session is the right vehicle because the agent has to ask questions and the user has to answer them, and because chat attachments already land files in the session's workspace, which is where the agent reads them.

The agent's workspace is a checkout of schmux on the branch `client-performance`. The branch is fixed so the agent lands in the same workspace every round, its commits from earlier rounds are still there, and the user's own workspaces on that repo are never adopted. The daemon refuses to start the chat in a workspace whose `go.mod` is not schmux's module, using the `isSchmuxWorkspace` check dev mode already makes, so the agent is never put in a checkout it cannot fix.

The agent starts with a fixed prompt as the first message of the conversation, which is how `Spawn` delivers a chat prompt. The prompt is a text file in the repo, embedded with `go:embed` like `cookbooks.json`. It tells the agent:

- This chat exists so the agent can diagnose and fix why the schmux dashboard is slow for the user it is chatting with. The workspace is a checkout of schmux, the dashboard's source. Recordings arrive as files attached to the user's messages; `docs/client-performance.md` describes the format.
- For each file: read `timeline` and find the stalls; for each stall read the detail buffers for that second; name a cause from what the stalls share; confirm it in the dashboard source before saying it.
- Ask the user one or two questions at a time, in plain language, and prefer a question to a guess. When another recording is needed, say exactly what the user should do while it runs.
- Make the fix on this branch. Then tell the user to switch dev mode to this workspace from the sidebar panel, reload, and record again. Compare the new file with the earlier one on the measures that named the cause, and say whether the cause is gone.
- Reply to this message briefly; the user's first message comes next.

## Proving the fix

The agent's fix is on its branch in its workspace. Dev mode's workspace switching rebuilds the daemon or restarts Vite from any worktree's source, from the sidebar panel, so the agent tells the user to switch to its workspace, reload, and record. The next file's `build.source_workspace` names that workspace, and the agent tells before from after without asking. The agent compares the two files on the measures that named the cause and reports the numbers.

## The recorder

The recorder runs in the user's browser and builds the file. It is `lib/clientPerf.ts`, a module singleton like `inputLatency`: `export const clientPerf = new ClientPerfCollector()`. Every method is a no-op while off.

It runs only while the daemon reports `dev_mode`, applied by `AppShell` from `useVersionInfo`, and when two switches are on: `config.client_performance.enabled`, applied from `ConfigContext` on every config response, and the per-browser `localStorage['schmux:client-perf']`, which only the recorder writes, through `clientPerf.start()` and `clientPerf.stop()`. The sidebar pane calls those and reads state back from the singleton.

It keeps the recording alive across reloads, because the agent may ask the user to reload as a test. Every 5 seconds and on `pagehide` the buffers go to IndexedDB; at page load with both switches on they come back and the reload itself is recorded. The recorder empties the buffers in three cases only: a message carrying the recording was sent successfully, the user pressed Stop, or the config switch went off.

It never breaks the dashboard it is measuring. Each observer is wrapped; an API the browser lacks is noted in `environment` and skipped. The provisioning socket in `ConnectionProgressModal` does not go through `transport` and is not counted. jsdom has no IndexedDB, so `fake-indexeddb` is a dev dependency loaded in `setupTests.ts`.

The recorder also keeps the performance chat's workspace id and session id, in the same IndexedDB store beside the buffers, from the response to the endpoint below. The pair survives a send and a Stop, since the chat outlives any one recording, and is cleared only when the config switch goes off. The daemon stores nothing about the chat; the recording browser is the only party that remembers which chat is the performance chat.

## What the user does

The user does four things, each one control in the dashboard.

**Start recording**, in a sidebar pane. `ClientPerformance.tsx` is a diag-pane like `TypingPerformance`: collapsed state in `localStorage['client-perf-collapsed']`, the `diag-pane__toggle` header reading `Client Performance`, or `Client Performance · REC` while recording. `AppShell` renders it with the other panels when `isDevMode && config.client_performance?.enabled`, the gate `SessionDetailPage` uses for `StreamMetricsPanel`. Off, the pane shows two sentences and **Start recording**. On, it shows `Recording 12 min · 3 stalls`, **Open performance chat**, and **Stop recording**. With repo or target unset it shows `Pick a repo and target on the Config page, Advanced tab.`, linking there, in place of the chat link.

**Open performance chat**, in the same pane. The pane calls `POST /api/client-performance/session` with `{ "workspace_id", "session_id" }` as the recorder last received them, both empty the first time, and the daemon returns the pair that is valid now. If the session is running, the same pair. Else the daemon settles the workspace first: the sent workspace id if it exists, is not disposing, and is on the configured repo and the branch `client-performance`; otherwise the workspace manager's `GetOrCreate` with the configured repo's URL and that branch. It then applies the `isSchmuxWorkspace` check and refuses with a 400 if it fails, leaving a worktree it created as an ordinary workspace. Then `Spawn` with `WorkspaceID` set to that workspace, the configured target, the prompt, and `Kind: chat`. The 400s before any of that, in order: `enabled` off; `repo` empty or not in config; `target` empty or not in config; `chat_sessions` off; the target's adapter has no chat mode. Errors appear in the pane. On success the pane navigates to `/sessions/<id>` after `waitForSession`, as the desync path does after its spawn, and the recorder keeps the pair.

**Send with the recording attached**, in the chat's composer. `ChatView` passes `Composer` a flag that is true when the open session's id matches the kept pair and this browser is recording; only then does the composer show a checkbox labelled `Recording since HH:MM · attach`, checked by default. On send with it checked the composer asks the recorder for the file, uploads it with `uploadWorkspaceAttachment` into the chat's workspace, appends the path with `withFileAttachments`, sends, and then tells the recorder the recording was sent, which is when the recorder empties the buffers. The upload lands in `<workspace>/<schmux data dir>/attachments/<upload id>/` behind `os.OpenRoot`, unchanged; the 50 MiB limit is far above a full recording. If the upload or the send fails, the message and the recording stay where they were.

**Stop recording**, in the pane. With unsent data, the pane asks first.

## Configuration

`config.json` gets `client_performance`, shaped like `desync` and `io_workspace_telemetry` with one more field:

```json
{
  "client_performance": { "enabled": false, "repo": "", "target": "" }
}
```

| Field                        | Default | Description                                                               |
| ---------------------------- | ------- | ------------------------------------------------------------------------- |
| `client_performance.enabled` | `false` | Turns on the recorder and the sidebar pane. Hot-reloadable.               |
| `client_performance.repo`    | `""`    | Name of the configured repo the chat works in. Must be a schmux checkout. |
| `client_performance.target`  | `""`    | Target the chat is spawned on. Must have a chat mode.                     |

Go: `ClientPerformanceConfig{Enabled *bool, Repo, Target string}` on `Config`, getters, the legacy-target migration hooks `Desync` has, `contracts.ClientPerformance` and `contracts.ClientPerformanceUpdate` with pointer fields, in the config response and PATCH beside `IOWorkspaceTelemetry`.

Dashboard: `clientPerformanceEnabled`, `clientPerformanceRepo`, `clientPerformanceTarget` in `useConfigForm` and `buildConfigUpdate`, following the desync fields. Advanced tab: a **Client Performance** section after IO Workspace Telemetry inside the `isDevMode` block: the enable checkbox, a repo select over the configured repos, and `TargetSelect` with `includeDisabledOption={false}` and `options={models}`.

## Documentation

- `docs/client-performance.md`: the file, section by section and field by field, the thresholds, and how to read one, written for the agent.
- `docs/telemetry.md`: a **Client Performance Recording** section between IO Workspace Telemetry and Chat Load Telemetry, with the same subsections as the IO one; Analysis workflow points at the chat.
- `docs/api.md`: the `client_performance` config entry beside `chat_load_profiling_enabled`, and the endpoint.
- `docs/settings.md`: the Dev-only row and the Advanced tab description.
- `docs/web.md`: the pane and the checkbox.
- `docs/dev-mode.md`: one line under Workspace switching, that this is how a recording is taken against an agent's fix.

## Failure cases

- An observer the browser lacks is noted in `environment` and skipped; the agent still has the rest of the file.
- A production React build leaves `commits` empty; `build` says so and the agent reads it that way.
- The endpoint reports configuration problems, and a non-schmux repo, in the pane before any chat exists.
- A failed upload or send leaves the recording and the message where they were.
- A disposed workspace or ended session is replaced by the endpoint, which returns the new pair for the recorder to keep.
- Config disabled while recording: the recorder stops, clears its browser switch, and empties the store once the config response arrives.

## Tests

Per `docs/testing.md`, each in the lowest gate that can make the assertion.

- Go, quick gate: config round trip and PATCH; the endpoint for disabled, repo unset, repo not in config, target unset, target without chat mode, repo that is not schmux, empty ids, session id running, session id ended with a live workspace id, workspace id disposed, workspace id on a different repo or branch.
- Vitest, quick gate, fake timers: ring caps and timeline rows; the stall rule; IndexedDB save and restore across a reload; the file's `build`, `environment`, and `workload` sections; the WebSocket wrapper's per-type sums and large-message capture through `setTransport`; recorder off until both switches are on; the pane's states, including the config hint; the checkbox shown only for the kept session while recording; the Advanced tab section saving through the config form.
- Scenario: like the other chat scenarios, daemon endpoints and the chat socket are Playwright routes. Start recording, open the performance chat, send with the checkbox on; assert the upload request body carries `build`, `environment`, and `timeline`, and the chat message text ends with the `File attachments:` block holding the path the upload route returned.

## Not included

- Any analysis in the daemon. No findings, no verdict, no report command.
- Sending recordings anywhere other than the user's own daemon.
- A production build change. Dev-only.
