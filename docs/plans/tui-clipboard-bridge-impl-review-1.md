VERDICT: NEEDS_REVISION

## Summary Assessment

The plan is detailed, well-structured, and faithful to the spec, but it contains several concrete inaccuracies that will block an executor: (1) it invents `SetGlobalOption`/`Setenv` methods on `*controlmode.Client` that do not exist, (2) it uses Go 1.22 stdlib mux pattern (`mux.HandleFunc("POST /api/...")`) when the codebase uses chi, (3) it references a generic `cs.broadcaster.Broadcast(event)` API that doesn't exist (broadcast is via per-purpose helpers using `s.broadcastToAllDashboardConns(payload)`), and (4) the dispose ordering for `clipboardCh` interacts unsafely with the existing `Stop()` sequence.

## Critical Issues (must fix)

### 1. `*controlmode.Client` has neither `SetGlobalOption` nor `Setenv` — Step 4 invents API

`internal/remote/controlmode/client.go:570-574` shows the entire surface that exists:

```go
func (c *Client) SetOption(ctx context.Context, option, value string) error {
    _, _, err := c.Execute(ctx, fmt.Sprintf("set-option %s %s", option, value))
    return err
}
```

There is no `SetGlobalOption` and no `Setenv`. The actual code in `connection.go:736-746` uses `c.client.SetOption(ctx, "window-size", "manual")` (which produces `set-option window-size manual` — session scope, no `-g`) and `c.client.Execute(ctx, "setenv -g DISPLAY :99")` (raw command). Plan Step 4 declares a fake interface:

```go
type remoteTmuxOptionSetter interface {
    SetServerOption(ctx context.Context, option, value string) error
    SetGlobalOption(ctx context.Context, option, value string) error
    Setenv(ctx context.Context, name, value string) error
}
```

…and the test fakes those methods. To make the plan executable, the executor must EITHER (a) add `SetGlobalOption` and `Setenv` to `*Client` as part of Group A (the plan says Step 4 should "verify by reading … and adapt the interface if the names differ" — but the names don't just differ, the methods don't exist), OR (b) drop those methods from the interface and have `applyRemoteTmuxDefaults` call `c.SetOption(ctx, "window-size", "manual")` and `c.Execute(ctx, "setenv -g DISPLAY :99")` to mirror current behavior. The plan should pick one and make it explicit; right now Step 4 says "must already exist on Client; verify during implementation" which is wrong.

Note also the existing `c.client.SetOption(ctx, "window-size", "manual")` does not pass `-g` — but the plan's pseudocode says `set-option -g window-size manual` is current behavior ("the lines that set `set-option window-size manual` and `setenv -g DISPLAY :99`"). The current behavior is `set-option window-size manual` (no `-g`). The semantics (session vs global vs server) for `window-size` differ — Step 4 needs to either preserve the bug or fix it deliberately, not silently introduce `-g` via a phantom `SetGlobalOption`.

### 2. Routing — codebase uses chi, not Go 1.22 stdlib mux

Plan Step 19b suggests:

```go
mux.HandleFunc("POST /api/sessions/{id}/clipboard", s.handleClipboardAck)
```

`internal/dashboard/server.go:621` initializes `r := chi.NewRouter()`. Routes look like `r.Post("/sessions/...", ...)` and `r.Get("/...", ...)`. The plan parenthetically notes "Verify the routing pattern this server uses" but should specify the chi pattern outright. Also: `chi.URLParam(r, "id")` is the URL-extraction pattern (not `r.PathValue("id")`).

### 3. No generic broadcaster surface — plan's `cs.broadcaster.Broadcast(event)` does not exist

Plan Step 18b's pseudocode calls `cs.broadcaster.Broadcast(event)`. There is no such method. The actual surface (visible in `internal/dashboard/server.go:1722-1736`) is:

```go
func (s *Server) BroadcastCuratorEvent(event CuratorEvent) {
    msg := struct { Type string; Event CuratorEvent }{...}
    payload, err := json.Marshal(msg)
    s.broadcastToAllDashboardConns(payload)
}
```

`broadcastToAllDashboardConns([]byte)` is the actual lower-level helper (used by `BroadcastCuratorEvent` and `BroadcastEvent`). Per-event broadcast functions are hand-written. Plan should:

- Drop the `broadcaster` field on `clipboardState`.
- Have the broadcast helpers (`fireBroadcast`, `clear`, snapshot send) directly marshal JSON and call `s.broadcastToAllDashboardConns(payload)`.
- Add `BroadcastClipboardRequest(...)` and `BroadcastClipboardCleared(...)` methods on `*Server` mirroring `BroadcastCuratorEvent` precedent.

### 4. Dispose ordering interacts dangerously with existing Stop()

`internal/session/tracker.go:165-185` shows that `Stop()` already closes subscriber channels at lines 173-178 BEFORE waiting on `<-doneCh`. The fanOut iterates over a snapshot of subs (lines 233-236) so this works because closes happen after `t.source.Close()` at line 168 (which prevents new fanOut events). However, plan Step 16c says:

> Add `close(t.clipboardCh)` after the existing `<-t.doneCh` wait.

That works — but the plan also doesn't address the existing 5-second timeout at line 182-183. If `<-doneCh` times out (run() stuck), then `close(t.clipboardCh)` runs anyway, and there is then a possible race: `run()` is still alive and may call `fanOut → e.process → emit on closed channel`. Plan needs to either (a) acknowledge this is the same race as exists today and document it as out-of-scope, or (b) use a `select { case <-doneCh: close(clipboardCh); case <-time.After(...): /* leak */ }` pattern that does not close on timeout.

### 5. Step 16 is too large for the spec's "2-5 minute" granularity

Per the user's explicit granularity check: Step 16 ("Add clipboardCh and extractor to SessionRuntime") includes:

- Failing test for fanOut+extractor integration
- Add struct fields (`clipboardCh`, `extractor`)
- Constructor wiring (`make(chan, 1)` + `newOSC52Extractor`)
- fanOut modification (extractor.process + drop-on-overflow + ClipboardDrops counter add)
- TrackerCounters struct field addition (in `tracker.go:48-55`, currently `TrackerCounters`, not `Counters`)
- DiagnosticCounters export update (line 305-308 already references each field)
- Dispose ordering in Stop()

That's 5-6 distinct concerns, easily 15+ minutes. Should be split into:

- 16a: Add `ClipboardDrops` to `TrackerCounters` + DiagnosticCounters export + test
- 16b: Add `clipboardCh` field + extractor field + constructor wiring (with `cap(t.clipboardCh) == 1` invariant test)
- 16c: Wire extractor into fanOut (test: stripped output + ClipboardRequest emit + drop-on-full-channel)
- 16d: Dispose ordering update

### 6. Step 17 references non-existent `internal/api/contracts/dashboard.go`

`internal/api/contracts/` does not contain `dashboard.go`. Existing files: `commit_detail.go`, `commit_graph.go`, `config.go`, `detection.go`, `diff.go`, `environment.go`, `features.go`, `github.go`, `persona.go`, `pr.go`, `preview.go`, `remote.go`, `resolve_conflict.go`, `sessions.go`, `spawn.go`, `spawn_request.go`, `style.go`, `tab.go`. Plan should either pick `sessions.go` (most relevant) or instruct creating a new `clipboard.go` contract file.

### 7. `internal/dashboard/clipboard_test.go` already exists — naming collision

`internal/dashboard/clipboard_test.go` is in the file listing. Plan Step 18a creates `internal/dashboard/clipboard_state_test.go` (different name, OK), but Step 19 talks about `internal/dashboard/clipboard.go` — verify whether the existing `clipboard.go` (likely the image-paste handler the spec mentions) clashes. The plan should explicitly name the new file something disambiguating like `clipboard_ack.go` or `clipboard_state.go` and verify the existing file's purpose.

### 8. Step 5b/5d test guidance is hand-wavy and may not be implementable

`internal/dashboard/websocket_test.go` (1,215 lines) does not contain any harness that exercises `handleTerminalWebSocket`'s CR or FM event-streaming loop end-to-end. Existing tests cover `bootstrapFrameSeq`, `appendSequencedFrame`, status-event-handler integration — but not a CR/FM subscriber loop with synthesized output events. The plan asks the executor to "Use whatever subscriber-mocking pattern the existing tests use; if none exists, you may need to construct a minimal harness — keep it small."

That's hand-wavy. The CR handler (lines 895-936) is a tight loop reading from `outputCh` and writing to a websocket; testing it requires either a full WS round-trip (heavy) or refactoring the inner loop into a testable function. Plan should either (a) explicitly call for the refactor, (b) pick a higher-level integration test approach (perhaps via the existing `broadcast_test.go` infrastructure), or (c) accept covering this only via E2E and remove the unit-test step.

### 9. Step 4 fake test omits assertions on global/setenv but still asserts on them

The Step 4 test (lines 314-327) constructs `wantGlobal` and `wantEnv` slices but the visible code stops at `// ... assert global + setenv` — incomplete. Plus, with critical issue #1, those calls will not exist. Step needs full rewrite.

### 10. Group dependency table inconsistency

Plan's table (lines 38-47):

```
| D | 7, 8 | Yes — both depend on extractor wiring; CR/FM fix is independent of remote-side helpers |
```

But Steps 7 and 8 in the actual plan body are "Define ClipboardRequest type" and "Plain-bytes pass-through" — both within the OSC 52 extractor package (Group D). The table appears to describe a different numbering. The dependency table seems to use group/step numbers that don't match the body — the body has 27 sequential steps grouped A-H, but the table refers to "Group D: Steps 7, 8" which doesn't match (Group D body has Steps 7-15, all extractor work).

Also the table says "Group D depends on Groups A+B (helpers + extractor scaffolding)" but Group D IS the extractor package — it depends on nothing in A or B except possibly the `google/uuid` import. The dependencies listed are wrong; D is purely additive and could in principle be done first. Fix the table or delete it.

## Suggestions (nice to have)

### S1. `Counters` vs `TrackerCounters`

Plan refers to "Counters struct" — actual name is `TrackerCounters` (`internal/session/tracker.go:48`). The struct is reachable via `t.Counters` field on SessionRuntime. Tighten language for clarity.

### S2. Default-socket StartServer happens at line 217, not 213

Plan says "Spec v2 placed it at `Start()`'s `StartServer` call (line 217)". Verified at `daemon.go:217` (`_ = startupServer.StartServer(ctx)`). Plan also says "tmuxServer constructed in the daemonInit returned by initConfigAndState at daemon.go:578" — verified at line 578 (`tmuxServer := tmux.NewTmuxServer(...)`). Good.

But the early-Run() insertion point should be line 389 area where `tmuxServer := di.tmuxServer` is unpacked, not "early in Run()" generically. Plan's Step 6a should pin the insertion point precisely.

### S3. Existing controlmode test pattern is weak

`internal/remote/controlmode/client_test.go` has no precedent for testing `SetOption`-style command issuance. Plan Step 2b says "Use whatever fake-tmux helper the existing tests use" — there is none. Suggest the executor wrap `c.Execute` behind an interface for the test, or add a minimal capture in the test setup. Be explicit; otherwise this step blocks.

### S4. CSS approach for ClipboardBanner

Plan says "Add styles in the project's CSS approach (CSS modules or `global.css` — verify which is used)." `assets/dashboard/src/styles/global.css` is flagged in CLAUDE.md as too large to read in full. Plan should give the executor a heuristic ("look for `.clipboard-paste-banner` or similar in `global.css`; if BEM-style class names are convention, follow that").

### S5. `bootstrapFrameSeq` precedent — narrow seq comment

Plan says "this matches the existing precedent at `internal/dashboard/websocket.go:91` (`bootstrapFrameSeq` calls `Append(nil)` to reserve a seq)". Verified at line 91. Good.

### S6. Snapshot wire to handleDashboardWebSocket — line numbers

Plan says "around `handleDashboardWebSocket:1810-1834`". Function starts at line 1758; the snapshot block runs from 1810 (linear sync) to 1850 (recent curations). Plan's range is approximately correct but the executor should insert the clipboard snapshot AFTER 1850 to keep "current" state grouped together, or after 1822 (curationTracker.Active) to keep "currently active" state together. Pin a location.

### S7. Step 1b's test is misleading

Step 1b's test (lines 67-76) constructs `srv.cmd(...)` directly rather than calling `srv.SetServerOption(...)`. That's the same pattern as the existing `TestTmuxServerSetOptionArgs` at line 221, so it's idiomatic — but the test at Step 1b is named `TestTmuxServerSetServerOptionArgs` which implies it tests the new method. Either rename or have it actually call `SetServerOption` (which would require the implementation to expose its arg construction; see how the existing test does it). Minor consistency issue.

## Verified Claims

The following plan claims were verified against the codebase and are correct:

- `internal/tmux/tmux.go:195-206` SetOption — confirmed (slight off: spans 195-206; SetOption proper is 196-206).
- `internal/tmux/tmux_test.go:218-226` test pattern — actual test at lines 221-229; pattern is `srv.cmd(...) → reflect.DeepEqual(cmd.Args[1:], want)`. Plan's Step 1b mimics it correctly.
- `internal/remote/controlmode/client.go:571` SetOption — confirmed at exactly that line.
- `internal/remote/connection.go:643-755` waitForControlMode — confirmed; `waitForControlMode` starts at line 643 and the inline option calls are at lines 736-746 (set-option window-size manual + setenv -g DISPLAY :99). Plan's range and contents are accurate.
- `internal/dashboard/websocket.go:920` and `:1013` CR/FM zero-length skip — confirmed exactly. Both have `if len(event.Data) == 0 { continue }`.
- `internal/dashboard/websocket.go:558-563` main handler precedent — confirmed; the comment at lines 558-562 explicitly warns about phantom-gap detection.
- `internal/daemon/daemon.go:213-230` StartServer call sites — confirmed `_ = startupServer.StartServer(ctx)` at line 217 in `Start()`.
- `internal/daemon/daemon.go:578` tmuxServer construction — confirmed.
- `internal/daemon/daemon.go:1003-1011` restored-session socket loop — confirmed; loop iterates `activeSocketSet`, skips default, calls `srv.StartServer(d.shutdownCtx)`.
- `internal/session/tracker.go:222-246` fanOut chokepoint — confirmed; fanOut at 222, calls `t.outputLog.Append([]byte(event.Data))` at 229, fans out to subs at 238-245.
- `outputLog.Append` returns a seq (`uint64`) — confirmed at `internal/session/outputlog.go:39`.
- `OutputEvent` is in `controlmode` package at `internal/remote/controlmode/parser.go:23` — confirmed; `internal/session/tracker.go:13` imports it.
- TrackerCounters has `EventsDelivered`, `BytesDelivered`, `FanOutDrops` — confirmed at `internal/session/tracker.go:48-55`. Adding `ClipboardDrops` is a clean one-liner. Note: struct is named `TrackerCounters`, not `Counters`.
- `csrfHeaders()` exists at `assets/dashboard/src/lib/csrf.ts:17` — confirmed.
- Vitest + React Testing Library — confirmed; `assets/dashboard/src/contexts/SessionsContext.test.tsx` imports from `vitest` and `@testing-library/react`. Plan's frontend test header is accurate.
- `linearSyncResolveConflictStates` and `curationTracker.Active()` initial-state sends in `handleDashboardWebSocket` — confirmed at server.go:1810-1819 (linear) and 1821-1834 (curations).
- WS broadcast precedent — `BroadcastCuratorEvent` at `server.go:1722` and `BroadcastEvent` at `server.go:1740` use `s.broadcastToAllDashboardConns(payload)`. This is the actual lower-level helper to use (NOT the plan's invented `cs.broadcaster.Broadcast(event)`).
- Step 5 (CR/FM fix) is independent of Steps 3 (`applyTmuxServerDefaults`) and 4 (`applyRemoteTmuxDefaults`) — confirmed; the files are entirely separate (`internal/dashboard/websocket.go` vs `internal/tmux/defaults.go` vs `internal/remote/defaults.go`). Parallelizable as the table claims.
- `internal/dashboard/websocket_test.go` exists at the expected path — confirmed (1,215 lines).
- `reflect.DeepEqual` is the assertion idiom in `internal/tmux/tmux_test.go` — confirmed (line 226 and several others). No assertion library.
- chi router in use — confirmed via `go.mod:37` (`github.com/go-chi/chi/v5 v5.2.5`) and `server.go:22, 621`.
- Sessions/curator broadcast pattern uses `s.broadcastToAllDashboardConns(payload)` after marshaling JSON with a `"type"` field — confirmed.

## TDD spot-check

Sampled steps:

- **Step 8** (Plain-bytes pass-through): has failing test (8a) → minimal impl (8b) → run command (`go test … -run TestExtractor_PlainBytesPassThrough`). Complete cycle.
- **Step 9** (Single-event OSC 52 BEL): has failing test (9a) → impl (9b) → run command. Complete.
- **Step 10** (ST + surrounding): has failing tests (10a) but explicitly says "These should pass on the existing implementation from Step 9" — so it's not truly TDD failing→implement, it's "verify Step 9 implementation also covers this". OK conceptually, but should be clearer that this is a regression-coverage step, not a fresh TDD cycle.
- **Step 12** (Validation rejections): same pattern as Step 10 — "These should pass against the Step 9 implementation". Same minor issue.
- **Step 19** (HTTP endpoint POST): has failing tests (19a) → impl (19b). The test pseudocode is high-level pseudo-Go ("Set up state with a pending entry; POST approve…") — needs more concreteness for the executor.
- **Step 23** (ClipboardBanner): has failing test (23a) — six test cases — → impl (23b). Test cases are concrete and well-scoped.

Overall TDD discipline is good but Steps 10 and 12 should be re-framed as "regression checks against Step 9's implementation" rather than fresh TDD cycles, to avoid confusion when those tests pass on first run.
