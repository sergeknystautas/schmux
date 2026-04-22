VERDICT: NEEDS_REVISION

## Summary Assessment

V3's architectural pivot to server-side extraction at `SessionRuntime.fanOut` is sound and resolves the v1/v2 chunk-boundary and gap-replay problems by construction. However, the spec contains one factually wrong claim about the remote `Reconnect()` path that motivates an unneeded refactor, contradicts itself on zero-length seq frames (a real downstream bug given existing handler guards), gives an under-specified concurrency model for the 200 ms debounce (the timer callback breaks the single-goroutine guarantee the extractor relies on for lock-free state), and openly punts on locating where the _default_ tmux socket's `StartServer` lives in `Run()` ("find it during implementation"). Multiple smaller issues (WS-reconnect snapshot of pendingClipboard not specified, `set-clipboard on` vs `external` choice unjustified, defang on UTF-8 not pinned down, clipboardCh capacity / back-pressure unspecified) round out the list.

## Critical Issues (must fix)

### 1. The "Reconnect doesn't re-run the post-handshake setup" premise is false

Spec §1, lines 61-63 motivate factoring `applyRemoteTmuxDefaults` out of `connect()` "and call it from BOTH `connect()` (replacing the existing inline block) and `Reconnect()` (new call site)." This rests on the claim "Currently `Reconnect()` (`internal/remote/connection.go:393-518`) does NOT re-run that block".

Verified against the code: the inline block (`window-size manual` at `internal/remote/connection.go:736`, `setenv -g DISPLAY :99` at line 746) is **inside `waitForControlMode`**, not inside `connect()` directly. `waitForControlMode` is called from BOTH `connect()` (line 373) AND `Reconnect()` (line 495). So Reconnect _already_ re-runs the inline setup today; the v2 review's claim that motivated this section was wrong, and v3 inherited the error.

Consequence: the spec sells a "fix" for a bug that doesn't exist, and the proposed surgery (moving code out of `waitForControlMode` into a helper called from `connect()` and `Reconnect()`) doesn't change behavior — it just moves where the calls live, with `Reconnect` going from one indirection to two. Either:

- Drop the Reconnect-specific framing entirely and add `applyRemoteTmuxDefaults` _inside_ `waitForControlMode` (replacing the existing inline lines 736-750). This is one call site, both paths benefit, no risk of forgetting the second invocation.
- Or document accurately that the existing `waitForControlMode` already handles both paths and the refactor is purely organizational.

The remote-server-restart concern v2 raised (kill-server on the remote between connect and reconnect would clear options) is real, but it's already handled-by-construction because Reconnect calls waitForControlMode which would re-apply. Spec language overstates the urgency.

### 2. Default-socket `StartServer` location is hand-waved — code smell in the spec itself

§1 lines 51-52: "The default socket initialization just above that loop at line ~1004 (`already started above` comment): the default socket's `StartServer` is currently elsewhere — find it during implementation. Inspect `Run()` between line 389 and 1011 during implementation to locate."

Verified at `internal/daemon/daemon.go`: `StartServer` is called in only TWO places in the codebase (`grep StartServer internal/daemon/daemon.go` returns lines 217 and 1008). Line 217 is inside `Start()` — the parent shim that forks the daemon. Line 1008 is the per-non-default-socket loop in `restoreSessions`. **There is no `StartServer` call for the default socket inside `Run()`.** The default socket is implicitly assumed to be already running (started by `Start()` if forked, by the user otherwise, or auto-started on first `tmux` command via tmux's own socket auto-start).

This means the spec's contract "call `applyTmuxServerDefaults` immediately after every `StartServer`" leaves the default socket unaddressed when running under `daemon-run` (i.e., dev mode, and any user who runs `daemon-run` directly). The spec says "find it during implementation" — that's not a design, that's punting. Two acceptable resolutions:

- Add an explicit `StartServer(ctx)` + `applyTmuxServerDefaults(ctx, srv)` call for `tmuxServer` (the default-socket server constructed at `daemon.go:578`) early in `Run()` — this changes existing behavior (no `StartServer` for default socket today under `daemon-run`) but in a benign way.
- Or call `applyTmuxServerDefaults` from `CreateSession` after first session creation on a server (lazy init), which v3 already vaguely mentions in §1 line 55 but as a parenthetical. Pick one and pin it.

Either way, the spec needs to drop "find it during implementation" — that is a defect, not a design.

### 3. Zero-length-output cross-event case contradicts the spec and breaks downstream handler guards

§2 line 130: "Event N's `outputLog.Append` receives the bytes UP TO `\x1b]52;` (stored as seq N); the OSC 52 itself is not stored. Event N+1's `Append` receives only the bytes AFTER the BEL. Seq numbers stay monotonic; **no zero-length frames**; no rewriting historical seq."

But the algorithm at lines 92-120 produces `output = []byte{}` whenever a single event is consumed entirely by an OSC 52 sequence (e.g., a TUI emits one `%output` line containing exactly `\x1b]52;c;aGVsbG8=\x07`). The test list at line 265 confirms this: `\x1b]52;c;aGVsbG8=\x07` → "output bytes empty". When `outputLog.Append([]byte{})` runs, it assigns a seq and stores a zero-length entry (verified at `internal/session/outputlog.go:39-57`; existing precedent: `bootstrapFrameSeq` at `internal/dashboard/websocket.go:91` already calls `Append(nil)` to reserve a seq).

The contradiction matters because two of the three terminal handlers explicitly skip empty events:

- `internal/dashboard/websocket.go:549`: `if len(event.Data) > 0 { ... }` — the entire branch (including the call to `appendSequencedFrame`) is gated. The comment at lines 558-562 explicitly warns: skipping the frame "creates a phantom gap on the frontend, triggering a gap replay whose chunked data can duplicate already-delivered bytes and corrupt the terminal state (e.g. cursor jumps)." That comment was written for a _different_ hot path, but it warns about the exact failure the OSC 52 extractor will introduce.
- `internal/dashboard/websocket.go:920` (CR handler) and `:1013` (FM handler): `if len(event.Data) == 0 { continue }` — same pattern, no frame emitted.

Net effect: every "OSC 52 fills an entire event" case (which is the _common_ case for nvim/helix emitting clean OSC 52 with no surrounding noise) produces a seq gap on the frontend, triggering an unnecessary gap-replay round-trip. Replay returns the empty-data entry, which gets dedup'd. So no user-visible breakage, but the spec's claim "no zero-length frames" is wrong, and the spec needs to either:

- Fix the three terminal handlers to send a frame even when `len(event.Data) == 0` (matching the lines 558-563 pattern in the main handler — which in fact already does send a zero-length frame after `escbuf.SplitClean` holds back). The CR/FM handlers don't have this protection.
- Or accept the spurious gap round-trip and document it explicitly.
- Or have the extractor _not_ call `Append` when stripped output is empty (which then breaks the seq-monotonicity contract in a different way — gap detection would still fire on the receiver because seq jumps).

The current contradiction is a defect — pick a story.

### 4. The 200 ms debounce timer breaks the single-goroutine guarantee

§2 line 88: "The single-goroutine guarantee for `fanOut` (it runs in the source loop) means the extractor needs no synchronization beyond the channel send." Verified at `internal/session/tracker.go:339` — `run()` drains `t.source.Events()` in a single goroutine, and `fanOut` is only called from there.

§2 line 134 (debounce): "keep `pending *ClipboardRequest` + a 200ms timer; when the timer fires, send to `clipboardCh`."

A `time.Timer`/`time.AfterFunc` callback runs in a fresh goroutine, _not_ on the source loop. So `pending` and the timer handle are touched concurrently from two goroutines: the extractor (set/reset on every OSC 52) and the timer callback (read on fire). This contradicts the explicit no-sync claim and is a data race.

Three options:

- Use a `sync.Mutex` around `pending` + cancel/reset of the timer (acceptable, but say so explicitly).
- Replace `time.AfterFunc` with a per-extractor goroutine that selects on a "reset" channel and a `time.NewTicker` — single-extractor-goroutine model — but that's now N extra goroutines (one per session).
- Hoist the debounce into the dashboard server's `pendingClipboard` map (which already needs a mutex for multi-tab consistency) and emit each request on `clipboardCh` immediately, undebounced. This is cleaner and consolidates state in one place (the daemon-side broadcast layer).

Currently spec says "no synchronization beyond the channel send" — that's wrong as written.

### 5. WS reconnect should rehydrate pendingClipboard, but spec is silent

The spec at §3 (Daemon-side broadcast state) describes the broadcast on incoming request and the cleared event on approve/reject/TTL/dispose. It's silent on the case where a fresh `/ws/dashboard` WebSocket connection opens _after_ a `clipboardRequest` was broadcast. Verified at `internal/dashboard/server.go:1810-1834`: `handleDashboardWebSocket` already sends initial state for `linearSyncResolveConflictStates` and `curationTracker.Active()` to reconnecting clients — the established pattern is "snapshot active state on WS connect."

If the user reloads the page (or the WS drops and reconnects) while a clipboard request is pending, the new tab will not receive the request because broadcasts are fire-and-forget. The pendingClipboard map is the source of truth, but the spec doesn't say to push it on connect.

Add to §3: in `handleDashboardWebSocket`, after the existing initial state sends, iterate `pendingClipboard` and send a `clipboardRequest` per active entry. Also note this in test §Tests (TS unit `SessionsContext`).

### 6. Daemon restart durability is unaddressed

`pendingClipboard` is in-memory only. If the daemon dies (crash, OS update, manual restart) between OSC 52 emit and user ack, the request is lost — no big deal for v1 (the user can re-yank). But the spec doesn't discuss this at all, and the relevant test in §Tests "what happens if the daemon dies between OSC 52 emit and user ack" is not enumerated. Either:

- Document as "in-memory only; daemon restart drops pending requests; user re-yanks". Acceptable for v1, but say so.
- Or persist to `state.json`. Probably overkill for an ephemeral confirmation prompt.

The bigger risk: the dashboard's frontend banner doesn't auto-clear on daemon restart, so the user could click Approve against a banner whose corresponding pendingClipboard entry no longer exists in the (newly restarted) daemon. The spec's stale-id handling (`{status: "stale"}`) covers the API path correctly, but the banner UX should also clear when the WS reconnects without the entry in initial-state — see Issue 5.

## Suggestions (nice to have)

### 7. `set-clipboard on` vs `external` — spec doesn't explain choice

Verified locally that the modern tmux default is `external`, not `off` (`tmux show-options -s | grep clipboard` → `set-clipboard external`). Per tmux docs: `external` = forward to outer terminal, do not store in tmux buffer; `on` = forward AND store in tmux buffer; `off` = do not forward.

For our use case (forward OSC 52 from inner pane through tmux to the daemon's PTY where the extractor runs), both `on` and `external` work. `external` is more conservative (avoids tmux holding a copy of every yanked password in its paste-buffer); `on` is what the spec chose without justification. Add a one-line code comment explaining the choice (probably "we want the tmux buffer too so `prefix-]` paste also works inside the session"), or switch to `external`.

Important: `on` does NOT break forwarding — both modes forward. The spec is correct that `on` works; it's just over-specified.

### 8. Output ordering / frame batching changes when extractor strips mid-event bytes

When extractor receives `before<OSC52>after` in a single event, it produces output `beforeafter` as one chunk to `outputLog.Append`. This subtly changes:

- Per-event timing semantics: `before` and `after` were originally one event from tmux, so semantically they were emitted "together" — combining them is fine for terminal rendering.
- But existing tests like `TestTrackerOutputLog_FanOutRecordsSequences` (`internal/session/tracker_test.go:177-210`) verify exact 1:1 event-to-seq mapping. If a future test introduces an OSC 52 sequence into the test fixtures, the test would need updating. Spec should call out: "tests that feed event data through `fanOut` may need updates if their data contains OSC 52".
- Frame timing (animation): if a TUI emits `frame1<OSC52>frame2` in two ticks separated in time, but tmux merges them into one `%output` line, the extractor's combined output is fine. If they're in two separate events, the extractor handles them independently. No issue here, just worth documenting.

### 9. Defang on UTF-8 — spec doesn't pin down byte vs string semantics

§2 line 122: "runs the defang (strip C0 controls except `\n` and `\t`, plus `\x7f`)".

If implemented in Go via `bytes.Map` over the decoded base64 bytes, the defang operates byte-by-byte. UTF-8 lead bytes (0xC0-0xFF) and continuation bytes (0x80-0xBF) are all >= 0x80, so the C0 (0x00-0x1F) + DEL (0x7F) defang doesn't damage UTF-8 by accident. Good.

But if the defang is implemented via `strings.Map` on `string(decodedBytes)`, Go's string-to-rune iteration would split the bytes into runes, and invalid UTF-8 produces U+FFFD per rune. Whether this matters for a "raw bytes to clipboard" use case depends on whether the spec wants:

- Bytes-faithful: defang at byte level, send the original bytes through `TextDecoder('utf-8', {fatal: false})` browser-side (lossy substitution for invalid UTF-8). Best for general payloads.
- Rune-clean: defang at rune level, hand the user a UTF-8-clean string. Easier to reason about but loses fidelity for binary clipboard payloads.

§Tests line 278 has a test for "lone byte 0x80 → `\uFFFD`" but doesn't say which side does the substitution. Pin it down: byte-level defang on the daemon, then send the bytes (or a string-safe encoding) to the frontend, and let the browser's `TextDecoder` handle invalid UTF-8 with replacement characters. Then `writeText` receives a JS string, which is what the clipboard API takes anyway.

### 10. `clipboardCh` capacity / back-pressure unspecified

§2 line 86 says "session-scoped channel `clipboardCh chan ClipboardRequest`" but no capacity. With debounce, requests are at most one per 200 ms per session — low rate. But the dashboard server's consumer goroutine could be slow during heavy session activity (broadcast lock contention, slow WS clients). A capacity-0 (unbuffered) channel back-pressures the source loop on every OSC 52, stalling all output for that session for the duration. A capacity-1 with non-blocking send (drop on overflow) is the safe pattern — mirror the `s.events` `default:` drop at `internal/session/localsource.go:424-430`.

Spec should specify: capacity 1, non-blocking send, drop on overflow (logged once at debug). The dropped request is benign — debounce already coalesces, and the user can re-yank.

### 11. The v3 spec's "by construction" claim about clipboard-CH dispose ordering

§2 line 138 (Dispose): "When `SessionRuntime` shuts down, the extractor flushes its carry (discarded), cancels any pending debounce timer, and closes `clipboardCh`."

`SessionRuntime.Stop()` at `internal/session/tracker.go:165-185` does NOT currently invoke any extractor cleanup; it closes subscriber channels and waits on `doneCh`. The spec needs to specify _where_ in Stop() (or after run() exits) the extractor cleanup happens, and which goroutine closes `clipboardCh`. If `clipboardCh` is closed from Stop() while the source loop is still inside fanOut → extractor.send, that's a "send on closed channel" panic. Safer: close `clipboardCh` after `run()` exits (i.e., after `<-doneCh`), in the same Stop sequence the existing `subs` cleanup uses.

### 12. Broadcast-too-early race with slow `writeText`

§3 the broadcast pattern is:

1. POST `/api/sessions/{id}/clipboard` action=approve
2. Daemon clears `pendingClipboard[sid]`, broadcasts `clipboardCleared`
3. Returns 200

The frontend's flow:

1. Click Approve → `writeText(text)` (may take 100-1000ms for permission prompt or paste-buffer plumbing)
2. On success, POST API
3. Daemon broadcasts cleared

If `writeText` takes 500ms and during that window a _second_ OSC 52 arrives for the same session, `pendingClipboard[sid]` is overwritten with the new request. The first POST then arrives carrying the _old_ `requestId` — daemon returns `{status: "stale"}`. The user sees: Tab A shows banner with text X, clicks Approve, `writeText(X)` succeeds, clipboard now contains X. Meanwhile the banner has updated to text Y (the new request). User thinks the prompt they approved was for Y, but the clipboard contains X.

Mitigation: the banner re-render should be visually distinct on replace (e.g., a brief flash, or "(replaced)" indicator), and the spec already correctly handles the API stale case. But the user-perception bug is real and the spec should at least note it. Cleaner alternative: "while writeText is in flight, ignore incoming `clipboardRequest` events for this session in the frontend" — i.e., the frontend treats a click-pending state as a soft lock until the API response settles.

### 13. Visible carry-buffer latency is not quantified

§2 lines 109-113: when input ends with the start of a possible OSC sequence (`\x1b]5`, `\x1b]`, `\x1b`), the extractor carries those bytes. If the next event never arrives (idle session), the carried bytes are stuck until the session emits more output OR is disposed.

For a TUI that emits `\x1b]0;new title\x07` (set window title) and goes idle, the user sees the title update only when the next byte arrives. That could be hours for an idle nvim. Worse case: an `\x1b` literal at the end of an event (single-byte cursor escape that's actually `ESC` for "leave insert mode") would be held until the next event.

Two fixes:

- Add a flush-on-idle: after 100 ms of carry-buffer non-empty + no new event, flush the carry into output as-is, reset.
- OR: only carry bytes that are part of a _possible OSC 52 prefix_ (`\x1b`, `\x1b]`, `\x1b]5`, `\x1b]52`, `\x1b]52;`) — five specific prefixes, not "anything that could be the start of any OSC". This narrows the carry to OSC 52-only and lets all other escape prefixes pass through immediately.

The latter is closer to what the algorithm sketch implies, but the sketch literally says "could be the start of an OSC 52 (e.g., partial `\x1b]5`)" — vague. Pin down: carry only when the trailing bytes match the prefix table for OSC 52 specifically; everything else flushes.

### 14. TTL goroutines (`time.AfterFunc`) — fine, but say so

§3 line 152: "TTL: a per-entry timer auto-clears entries after 5 minutes". `time.AfterFunc` does _not_ spawn a goroutine until the callback fires (it uses runtime timer wheel). So the cost is one in-flight runtime timer per pending entry, not N goroutines. This is fine; the spec doesn't claim otherwise, but a one-line note in §Compatibility & risks would prevent a future reviewer from re-asking.

### 15. UI "preview shows text containing dangerous characters" - rendering risk

§3 lines 168-178 describe the banner rendering in a `<pre>` with `white-space: pre-wrap`. Good — `<pre>` doesn't interpret HTML. But a TUI could yank text containing emoji-encoded scams, RTL override characters (U+202E), zero-width spaces (U+200B), etc. The defang strips C0/DEL but not C1 controls (0x80-0x9F) or non-printable Unicode like RTL override. For full pastejacking defense, the preview should also visually distinguish bidirectional/zero-width characters (highlight them, or show a hex escape). v1 scope decision: out-of-scope, but worth one line in §Compatibility & risks.

## Verified Claims

- `SessionRuntime.fanOut` (`internal/session/tracker.go:222-246`) is the single chokepoint; it's only called from `run()` (line 353), which is itself a single goroutine started via `Start()` (line 161). No other call sites for `fanOut`.
- `outputLog.Append` is called in exactly two production locations: `tracker.go:229` (fanOut) and `websocket.go:91` (`bootstrapFrameSeq`, which appends nil to reserve a seq). The bootstrap path does NOT feed bytes into the log — the bootstrap is a `capture-pane -e` snapshot sent directly as a frame at `websocket.go:346`. So extracting OSC 52 in fanOut is sufficient to keep all live + replay paths clean.
- The xterm.js gap-replay path reads from `outputLog` via `buildGapReplayFrames` (`websocket.go:98-109`); since the log only contains stripped bytes, replay is clean by construction. v3's claim verified.
- `LocalSource.emit` at `internal/session/localsource.go:424-430` uses non-blocking send with drop-on-full — back-pressure on `clipboardCh` would not propagate into the source loop _if_ the same pattern is followed.
- `controlmode.Client.SetOption` at `internal/remote/controlmode/client.go:571` runs `set-option <opt> <val>` with no scope flag (defaults to session scope); the spec correctly identifies the need for a server-scope variant.
- `*tmux.TmuxServer` exposes `StartServer` at `internal/tmux/tmux.go:111`; it's currently called from `internal/daemon/daemon.go:217` (parent shim) and `:1008` (per-non-default-socket loop). No `StartServer` call exists for the default socket inside `Run()`.
- The CR (`websocket.go:920`) and FM (`websocket.go:1013`) terminal handlers skip `len(event.Data) == 0` events without emitting frames — confirming the seq-gap risk in Critical Issue 3.
- Modern tmux default is `set-clipboard external`, not `off` (verified `tmux show-options -s` on local install). Spec setting `on` works; `external` is the modern alternative.
- `waitForControlMode` at `internal/remote/connection.go:643` contains the inline post-handshake setup (lines 736-746) and is called from BOTH `connect()` (line 373) AND `Reconnect()` (line 495) — disproving the spec's premise in Critical Issue 1.
- `outputLog.Append([]byte{})` (or `nil`) is well-defined: it assigns a seq, stores a zero-length entry, increments `nextSeq`. The bootstrap path already relies on this. So the spec's algorithm producing empty output is technically supported by the log; the issue is downstream handler guards (Critical Issue 3).
- Existing pattern in `handleDashboardWebSocket` (`server.go:1810-1834`) snapshots active state for new connections (linearSync, curations) — pendingClipboard should follow this pattern (Critical Issue 5).
