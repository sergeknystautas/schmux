VERDICT: NEEDS_REVISION

## Summary Assessment

V2 fixes most round-1 criticals well (chi router, file-naming, broadcast precedent, dispose race documentation), but introduces a fresh critical bug in Step 4 (`Execute` is documented as `(string, string, error)` everywhere — the actual signature is `(string, time.Duration, error)`), and Steps 5/19/20 ship pseudocode that doesn't match the real APIs at the cited insertion points (no `WriteJSON` on `wsConn`, CR/FM uses `escbuf.SplitClean` + `appendSequencedFrame`, route URL params use `{sessionID}` and live inside an `r.Route("/api", …)` block). The plan also omits the required edit to `cmd/gen-types/main.go` to register the new contract types.

## Critical Issues (must fix)

### C1. `controlmode.Client.Execute` signature is wrong throughout the plan (NEW BUG)

`internal/remote/controlmode/client.go:173`:

```go
func (c *Client) Execute(ctx context.Context, cmd string) (string, time.Duration, error)
```

The second return value is `time.Duration`, not `string`. The plan claims `(string, string, error)` in three places:

- Step 2a (line 125): "`Execute` returns `(string, string, error)` (stdout, stderr, err)" — flat-out wrong. There's no stderr return.
- Step 2c (line 147): `func (r *recordingExec) Execute(_ context.Context, cmd string) (string, string, error)` — fake signature won't satisfy the interface.
- Step 4a (line 344): `func (f *fakeRemoteClient) Execute(_ context.Context, cmd string) (string, string, error)` — same.
- Step 4b (line 390): `Execute(ctx context.Context, cmd string) (string, string, error)` — interface declaration wrong.
- Step 4b (line 416): `if _, _, err := c.Execute(...)` happens to work syntactically when the second value is duration, but the interface won't be satisfied by `*controlmode.Client`.

This is a fresh round-1→round-2 regression. Round-1's review (lines 13-32) explicitly mentioned that the methods didn't exist; v2 corrected the methods but introduced an incorrect Execute signature.

Fix: change every occurrence to `(string, time.Duration, error)` and import `time` in the test file.

### C2. Step 5 pseudocode targets the wrong API at the CR/FM call sites

The plan invents a `crFrame` struct with `{Seq, Data}` fields and a `wsConn.WriteJSON(frame)` call. Neither exists in the actual code path.

Verified at `internal/dashboard/websocket.go:910-936` (CR) and `:1003-1025` (FM):

- The variable in scope is `conn` (a `*wsConn`), not `wsConn`.
- `*wsConn` has only `WriteMessage(messageType int, data []byte) error` (server.go:74); there is no `WriteJSON`.
- The actual sending code is `escbuf.SplitClean(escScratch, escHoldback, []byte(event.Data))` followed by `frameBuf = appendSequencedFrame(frameBuf, event.Seq, send)` then `conn.WriteMessage(websocket.BinaryMessage, frameBuf)`.
- The "fix" must mirror the main handler at `:549-567` which keeps the `escbuf.SplitClean` call but calls `appendSequencedFrame(frameBuf, event.Seq, send)` even when `len(send) == 0` (the comment at lines 558-562 explains exactly why).

The Step 5 helper `frameForCROutput(event) → *crFrame` cannot encapsulate this work meaningfully because:

1. The `escbuf` state (escHoldback, escScratch) is per-handler-instance and must persist across events — it can't be captured by a per-event helper.
2. The output is byte-level (`appendSequencedFrame` builds a wire frame from `seq + bytes`), not a JSON `{Seq, Data}` struct.
3. `lastSeq` must be updated even when the input is zero-length (so the deferred-flush at handler exit at lines 915-917 has the right seq).

The actual CR/FM fix is a 1-2 line change in each handler: remove the `if len(event.Data) == 0 { continue }` early-out and run `event.Data` through `escbuf.SplitClean` (which handles empty input correctly) plus `appendSequencedFrame` unconditionally — same as the main handler. There's no need for an extracted helper, and pretending there is leads the executor astray.

Recommendation: rewrite Step 5b–5e to mirror the main-handler fix verbatim. If you want testability, extract `escbuf.SplitClean + appendSequencedFrame` into a helper that takes `(escbuf state, seq, bytes) → (newState, frameBuf)` — but that's a meaningful refactor, not the trivial JSON-frame helper currently in the plan.

### C3. Step 19 route registration: wrong URL path and wrong URL param name

The plan registers `r.Post("/api/sessions/{id}/clipboard", makeClipboardAckHandler(s.clipboardState))` and uses `chi.URLParam(r, "id")`.

Two problems verified against `internal/dashboard/server.go`:

1. **Routes live inside `r.Route("/api", func(r chi.Router) { ... })`** at server.go:661. Existing session POSTs at lines 860-864 are written as `r.Post("/sessions/{sessionID}/dispose", ...)` (no `/api` prefix in the path string). The plan's `/api/sessions/...` would produce `/api/api/sessions/...`. Fix: register as `r.Post("/sessions/{sessionID}/clipboard", ...)` inside the `/api` block, alongside the other session POSTs at line 860+.

2. **URL param convention is `{sessionID}`, not `{id}`** (server.go:860-864). Plan needs `chi.URLParam(r, "sessionID")`.

The Step 19a test helper `dispatch` builds its own router, so the test will pass even with `{id}` — but production code will not match the route convention and might break test-suite consistency tooling.

### C4. Step 17 omits the required edit to `cmd/gen-types/main.go`

`cmd/gen-types/main.go:27-62` requires every root contract type to be registered explicitly in the `rootTypes` slice. Adding `ClipboardRequestEvent`, `ClipboardClearedEvent`, `ClipboardAckRequest`, `ClipboardAckResponse` to `internal/api/contracts/clipboard.go` will not produce TS types unless the executor also adds:

```go
reflect.TypeOf(contracts.ClipboardRequestEvent{}),
reflect.TypeOf(contracts.ClipboardClearedEvent{}),
reflect.TypeOf(contracts.ClipboardAckRequest{}),
reflect.TypeOf(contracts.ClipboardAckResponse{}),
```

to the `rootTypes` slice in `cmd/gen-types/main.go`. The plan says only "Run: `go run ./cmd/gen-types`" and "Verify `assets/dashboard/src/lib/types.generated.ts` updated" — the executor will run gen-types, see no diff, and either silently move on with manual TS types or get stuck. Add an explicit instruction in Step 17b to register the new types in `cmd/gen-types/main.go`.

### C5. Step 20 test pseudocode and integration insertion don't match real `handleDashboardWebSocket`

Verified against `internal/dashboard/server.go:1810-1850`:

- The variable is `conn` (a `*wsConn`), not `wsConn`.
- Existing snapshot blocks call `conn.WriteMessage(websocket.TextMessage, payload)` after marshaling JSON to bytes manually; they do NOT use `WriteJSON`.
- There is no `wsWriter` interface in the codebase; the plan invents it (line 1867) without grounding.
- `*wsConn` has no `WriteJSON` method (server.go:74); the test fake `capturingWriter` with `WriteJSON(v any)` can't be substituted for the real conn.

Fix: either (a) add a `WriteJSON` helper method to `*wsConn` first (small but non-trivial — must not race with the underlying gorilla writer; CLAUDE.md notes "Always use the wsConn wrapper which has a mutex"), or (b) write the snapshot loop to marshal manually and call `conn.WriteMessage(websocket.TextMessage, payload)` like the surrounding code, with the test using a small interface that wraps `WriteMessage` for capture.

Option (b) is the lower-risk path and matches the codebase. Either way the plan must pin a concrete approach.

### C6. Step 16c's test uses `sr.outputLog.Entries()` which doesn't exist

`internal/session/outputlog.go` exposes `ReplayAll() []LogEntry` and `ReplayFrom(fromSeq uint64) []LogEntry` — there is no `Entries()` method. Step 16c.i (`TestFanOut_StripsOSC52FromOutputLog`) calls `sr.outputLog.Entries()`. Fix: use `sr.outputLog.ReplayAll()`.

### C7. `newTestSessionRuntime` helper does not exist; plan invents it

Used in Step 16b.i, 16c.i, 16d.ii, 18a, 19a (via `stateWithEntry`), 20a. Real codebase pattern (see `internal/session/tracker_test.go:20, 31, 169, 408, 480`) is to call `NewSessionRuntime("s1", source, st, "", nil, nil, nil)` directly with a constructed mock source.

Either: (a) introduce the helper in Step 16 explicitly with its constructor, or (b) inline `NewSessionRuntime(...)` calls in each test. The plan needs to pick one and provide the helper signature. Otherwise the executor wastes time hunting for it.

Note: this matters for the Step 16c "drops on full channel" test concern raised in the prompt. If a helper auto-wires the dashboard subscriber from Step 18c, the test races. The plan must make explicit that the test uses an unwired SessionRuntime (the production wiring happens in `manager.go`, not in `NewSessionRuntime`).

## Suggestions (nice to have)

### S1. ClipboardRequestEvent broadcast wire format breaks from `BroadcastCuratorEvent` precedent

`BroadcastCuratorEvent` (server.go:1722-1736) uses a nested `{Type, Event}` envelope:

```json
{"type": "curator_event", "event": {...}}
```

The plan's `BroadcastClipboardRequest` marshals the event directly with an inline `Type` field, producing a flat:

```json
{"type": "clipboardRequest", "sessionId": "...", ...}
```

Both patterns exist in the codebase (`BroadcastCatalogUpdated` at server.go:432 uses the flat pattern). It's a coin toss; the flat pattern is fine. But the plan should explicitly note "we follow the flat `BroadcastCatalogUpdated` precedent, not the nested `BroadcastCuratorEvent` precedent" so reviewers don't flag the inconsistency.

### S2. Step 6a's `tmux.ApplyTmuxServerDefaults` insertion location is correct but should pin the line

Verified `tmuxServer := di.tmuxServer` at `internal/daemon/daemon.go:389`. Plan should say "insert immediately after line 389" instead of "~line 389 area". The TERM env code in 6c says "place near the top of `Run()`" — pin to "after line 389 with the other initialization unpacks" or similar.

### S3. Subscriber wiring (Step 18c) location is unspecified

Step 18c says "Place this where session lifecycle is owned — likely `internal/session/manager.go` registration, or `internal/dashboard/server.go` startup wiring. Verify existing subscriber patterns first." This is hand-wavy. The actual path: when `*Server` is constructed and the session manager's `OutputCallback` is set (server.go:370 `s.session.SetOutputCallback(s.handleSessionOutputChunk)`), there is a clear hook for "tracker comes alive". The plan should pin the wiring location with a specific function name and grep target. Otherwise the executor must re-derive this.

### S4. Documentation gap for `docs/api.md`

The end-to-end checklist (line 2320) mentions docs/api.md must be updated, but no step body specifies what to add. CLAUDE.md says CI gate `scripts/check-api-docs.sh` enforces this. Add to Step 19 (or a Step 19d): document the new endpoint and WS event types in `docs/api.md` with the canonical sections matching the existing `/api/clipboard-paste` documentation at docs/api.md:823.

### S5. Step 16d race comment text

The plan says: "if `<-doneCh` times out (5s, line 182-183) because `run()` is stuck, `close(t.clipboardCh)` runs anyway and a still-live `fanOut` could panic with 'send on closed channel'." Verified at tracker.go:180-184. Good. One small thing: the existing code closes subscriber channels at lines 173-178 BEFORE the doneCh wait, so the race is identical to what already exists. Plan correctly acknowledges this. Minor improvement: cite that the comment should be added at the `close(t.clipboardCh)` site, not the doneCh wait.

### S6. Step 16c test naming

`TestTrackerCountersHasClipboardDrops` (Step 16a) is correctly flagged in the prompt as "trivially true once the field is added — not really a TDD failing test". Acceptable smoke test. Plan could rename to `TestTrackerCounters_ClipboardDropsField` to signal "structural assertion" intent rather than "behavior under test".

### S7. Step 16c comment on extractor goroutine safety

The plan says "Single goroutine here; no lock needed." Verify: `fanOut` is called from `run()` (tracker.go via the source's event channel), which is a single goroutine. Confirmed at tracker.go:160-162 — `Start()` spawns `go t.run()`, and `run()` is the only fanOut caller. Good.

### S8. CR/FM commit message overstates the fix

The Group B commit message (line 530) says "Fix CR/FM websocket handlers to forward zero-length frames instead of skipping, matching the main handler's existing precedent." This is true once C2 is fixed. But the plan body's frameForCROutput pseudocode doesn't actually fix the right thing (see C2). Once the body is corrected, the message is fine.

## Verified Claims

- `internal/remote/connection.go:706-755` post-handshake block — confirmed lines 736-746 use `c.client.SetOption(ctx, "window-size", "manual")` (no -g) and `c.client.Execute(ctx, "setenv -g DISPLAY :99")`. Plan's Step 4 correctly mirrors this without phantom methods.
- `internal/daemon/daemon.go:389` — `tmuxServer := di.tmuxServer` confirmed exactly at line 389.
- `internal/daemon/daemon.go:49` — `"github.com/sergeknystautas/schmux/internal/tmux"` import confirmed; `tmux.ApplyTmuxServerDefaults(...)` works without an additional import edit.
- `internal/daemon/daemon.go:1003-1011` restored-socket loop — confirmed; plan's insertion of `tmux.ApplyTmuxServerDefaults(d.shutdownCtx, srv, logger)` after StartServer is structurally correct.
- `internal/daemon/daemon.go:217` `_ = startupServer.StartServer(ctx)` confirmed in `Start()`.
- `internal/dashboard/server.go:122` `type Server struct` and `:130` `logger *log.Logger` confirmed; `s.logger` in scope throughout. `s.clipboardState = newClipboardState(s, s.logger)` works.
- `internal/dashboard/server.go:1722-1755` broadcast helpers confirmed; plan's new helpers mirror the precedent (flat-Type variant).
- `internal/dashboard/server.go:1810-1834` snapshot block confirmed; insertion AFTER curation snapshots is reasonable.
- `internal/dashboard/server.go:307-350` Server constructor structure confirmed; adding `s.clipboardState = ...` right after the literal works.
- `internal/dashboard/server.go:432-447` flat `Type` envelope precedent confirmed (`BroadcastCatalogUpdated`, `BroadcastConfigUpdated`).
- `internal/dashboard/handlers.go:91, 102` — `writeJSONError` and `writeJSON` exist; usable in Step 19b.
- `internal/dashboard/clipboard.go` and `clipboard_test.go` exist; plan correctly avoids name collision by using `clipboard_state.go` and `clipboard_ack.go`.
- `internal/api/contracts/` directory contents match plan's claim (no `dashboard.go`); creating `clipboard.go` is consistent with file-per-feature convention.
- `internal/session/tracker.go:48-55` — `TrackerCounters` struct confirmed; `ClipboardDrops atomic.Int64` is a clean addition.
- `internal/session/tracker.go:222-246` fanOut chokepoint — confirmed; plan's modification is structurally correct (extract bytes, run extractor, emit reqs, append stripped to outputLog, emit to subs).
- `internal/session/tracker.go:160-185` Stop() — confirmed; close-subscribers-before-doneCh-wait pattern matches plan's race-acknowledgement comment.
- `internal/session/manager.go:609, 697, 2005` `NewSessionRuntime(...)` direct calls — confirmed; no auto-subscriber wiring inside the constructor.
- `internal/remote/controlmode/client.go:570-574` SetOption — confirmed verbatim; plan's `SetServerOption` addition is consistent.
- `internal/dashboard/server.go:67-104` `wsConn` wrapper — confirmed; only `WriteMessage`, `ReadMessage`, `Close`, `IsClosed` (no `WriteJSON`).
- `r.Route("/api", func(r chi.Router) { ... })` at `internal/dashboard/server.go:661` — confirmed; routes inside use bare paths.
- Existing session POST routes use `{sessionID}` not `{id}` — confirmed at server.go:860-864.
- Curation snapshot pattern at server.go:1822-1834 — confirmed; uses `conn.WriteMessage(websocket.TextMessage, curPayload)`, no WriteJSON.
- Plan's `/commit` discipline references — correct per CLAUDE.md.
- Plan's `./test.sh` vs `./test.sh --quick` distinction — correct per CLAUDE.md; plan does not substitute `--quick` for the pre-commit gate (verified at lines 2316, 2330).
- `cmd/gen-types/main.go:26-82` — confirmed root types are explicitly enumerated; this surfaces the C4 gap.
