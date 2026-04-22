# TUI → Browser Clipboard Bridge (OSC 52, server-extracted, user-in-loop)

**Status:** designed, not implemented
**Scope:** when a TUI inside a schmux-managed tmux session writes to the clipboard via OSC 52, the daemon extracts the request from the byte stream, broadcasts a structured event to all dashboard clients, and the dashboard surfaces a sanitized confirmation prompt; the user clicks Approve to commit the write to their browser system clipboard.
**Out of scope:** OSC 52 read (`?` query), images (already covered by the existing image-paste path), automatic / silent forwarding.

## Problem

When a TUI inside a schmux session writes to the clipboard via `ESC ] 52 ; <Pc> ; <base64> BEL`, the write is silently dropped:

1. tmux's default `set-clipboard` mode swallows OSC 52.
2. Even if forwarded, the schmux daemon pipes raw bytes to xterm.js, and xterm.js by default does not act on OSC 52.

So `y` in nvim/helix/lazygit / `Enter` in tmux copy-mode appears to do nothing as far as the user's system clipboard is concerned.

The naive fix — call `navigator.clipboard.writeText` whenever an OSC 52 sequence arrives at xterm.js — has two problems:

1. **Pastejacking.** OSC 52 is a known attack vector: any program whose stdout flows through the terminal can write to the user's clipboard, including programs whose output is attacker-influenced (`git log` of an open-source repo, `cat README.md` from a hostile clone, an AI agent reading attacker-controlled web content via prompt injection, `npm install` build-script output, `tail -f` on logs of a network-facing service). A silent always-on bridge means a single byte stream you read can swap your clipboard with `rm -rf ~` (base64) and you'd have no idea until you paste it into another terminal. iTerm2, xterm, kitty, WezTerm, and VS Code all gate OSC 52 behind opt-in or per-write confirmation for this reason.
2. **Replay re-fires.** schmux's `OutputLog` stores raw bytes per session. When a WebSocket reconnects, `buildGapReplayFrames` (`internal/dashboard/websocket.go:98`) replays the historical bytes; an in-band OSC 52 handler in xterm.js would then re-fire for stale sequences. Stripping OSC 52 from gap-replay frames in-place is hard to do correctly because OSC 52 routinely spans multiple `OutputLog` entries (each entry is one source event), and re-stitching across entries breaks the seq-number contract the frontend uses for dedup.

## Design

Server-side extraction at the single chokepoint where all session bytes flow.

Three pieces:

- **(1) tmux config** — server-scope options on every tmux server schmux owns, local and remote, so tmux forwards inner-pane OSC 52 instead of swallowing it.
- **(2) Daemon-side OSC 52 extractor** — a per-session scanner inserted before `outputLog.Append`. Strips OSC 52 from the byte stream that goes into the log; emits structured `ClipboardRequest` events on a separate channel. Because the strip happens upstream of the log, gap-replay is clean by construction — no separate stripper, no seq juggling.
- **(3) Dashboard UI** — broadcast `ClipboardRequest` over `/ws/dashboard`, surface a confirm-with-sanitized-preview banner in the session detail page, send Approve/Reject back via a new HTTP endpoint, daemon broadcasts cleared state to all tabs.

### 1. tmux: enable OSC 52 passthrough at server scope

`set-clipboard` and `terminal-features` are tmux **server**-scope options, not session-scope. They must be applied per server with `-s` (server). Schmux uses one tmux server per repository (one socket per repo), so the helper must be invoked for every server we own — local and remote, on first start and on restart after host reboot.

**New helpers:**

- `*tmux.TmuxServer.SetServerOption(ctx, opt, value string) error` — runs `set-option -s <opt> <value>`. No name validation (no session arg).
- `*controlmode.Client.SetServerOption(ctx, opt, value string) error` — runs `set-option -s <opt> <value>` (no scope flag in current `SetOption` defaults to session scope, which is wrong for these options — same v1 bug).
- A small Go helper `applyTmuxServerDefaults(ctx, srv)` (in `internal/tmux/`) that calls both `set-clipboard external` and `terminal-features '*:clipboard'`, logs warnings on failure but never fails.

  Note: `external` (not `on`) — modern tmux's default is `external`, which forwards OSC 52 to the outer terminal _without_ also keeping a copy in tmux's own paste buffer. Avoids tmux retaining every yanked password indefinitely. Both modes forward the sequence; `external` is the more conservative choice.

**Where to invoke:**

The daemon process can be reached via two entry points (`cmd/schmux/main.go:93-130`):

- `schmux start` → `daemon.Start()` (parent shim, line ~213) → forks → child runs `daemon.Run()`.
- `schmux daemon-run` → directly runs `daemon.Run()` (used by `./dev.sh` and as the forked child).

Therefore the option application MUST live inside `Run()`, not inside `Start()`. Spec v2 placed it at `Start()`'s `StartServer` call (line 217) — that's wrong because `daemon-run` skips `Start()` entirely.

Concrete invocation sites in `Run()`:

- **Default socket** (`tmuxServer` constructed in the `daemonInit` returned by `initConfigAndState` at `daemon.go:578`): no `StartServer` is called for the default socket inside `Run()` today — it's only called from `Start()` (the parent shim, line 217). Under `daemon-run` (dev mode), the default socket's tmux server is auto-started lazily by tmux on the first command. To make the option-application deterministic, add an explicit `tmuxServer.StartServer(ctx)` (idempotent — no-op if already running) followed by `applyTmuxServerDefaults(ctx, tmuxServer)` early in `Run()`, immediately after `tmuxServer` is available. This is a one-call addition with no behavior change for the `start`-launched path (which already starts the server in the parent shim) and a benign explicit-start for the `daemon-run` path.
- **Restored-session sockets**: the loop at `internal/daemon/daemon.go:1003-1011` already calls `srv.StartServer` per non-default socket; immediately after that call, add `applyTmuxServerDefaults(ctx, srv)`.
- **Any future `tmux.NewTmuxServer` + `StartServer` invocation** — `applyTmuxServerDefaults` should be called immediately after every `StartServer` to keep the contract simple. If lazy server creation is ever added in `internal/session/manager.go.CreateSession`, pair it there too.

**Pre-existing tmux server caveat.** If a user has a sidecar `tmux` server running on the same socket _before_ schmux starts, `SetServerOption` will succeed against it, but the option lives on that pre-existing server's lifecycle — it disappears if the user kills it. Acceptable for v1; we do not own that server. Document in code comment.

**Remote (`internal/remote/`):**

A remote "session" is a tmux **window** inside a single shared `schmux` session per host (`internal/remote/README.md:14`, `internal/remote/connection.go:975`). One tmux server per host. The existing post-handshake setup block (`window-size manual` and `setenv -g DISPLAY :99`) lives inside `waitForControlMode` (`internal/remote/connection.go:643`, with the option calls at lines 736-746). `waitForControlMode` is invoked from BOTH `connect()` (line 373) AND `Reconnect()` (line 495), so any options set there already survive a reconnect-after-server-restart by construction.

Factor the inline option calls into a single helper `applyRemoteTmuxDefaults(ctx, client)` and replace lines 736-746 with one call to it. The helper runs:

```
set-option -s set-clipboard external
set-option -s terminal-features '*:clipboard'
set-option -g window-size manual
setenv -g DISPLAY :99
```

This is purely organizational — same call sites, same behavior, just one place where "every remote server gets these defaults" is asserted. All errors logged as warnings.

**TERM environment.** When `schmux start` forks the daemon (`internal/daemon/daemon.go:226-236`), it inherits `os.Environ()`. If the parent has no TERM (launchd, cron, foreign shell), tmux's outer-terminal `Ms` capability check on tmux <3.2 will silently refuse to forward OSC 52 even with `set-clipboard on`. Set `TERM=xterm-256color` explicitly in the daemon's `cmd.Env` (and inside the daemon process itself before spawning tmux children) regardless of tmux version. Cost is zero, covers a real fail-silent case.

### 2. Daemon-side OSC 52 extractor

**Where it sits:**

`SessionRuntime.fanOut` (`internal/session/tracker.go:222`) is the single chokepoint where every byte of session output flows. Today it does `outputLog.Append([]byte(event.Data))` then fans out to subscribers. We insert the extractor here.

**Per-session state:**

Add an `osc52Extractor` field to `SessionRuntime`. The extractor owns:

- A small carry-buffer (≤64 KiB) holding only trailing bytes that match an OSC 52 prefix table — see "Carry buffer scope" below.
- A reference to a session-scoped channel `clipboardCh chan ClipboardRequest` (capacity 1, non-blocking send, drop-on-overflow). Mirrors the existing `LocalSource.emit` pattern at `internal/session/localsource.go:424-430`. Drop-on-overflow is benign: the dashboard server's coalescing layer (see §3) makes a momentarily missed request equivalent to a stale yank that the user can reproduce.

The single-goroutine guarantee for `fanOut` (it runs in the source loop, verified at `internal/session/tracker.go:222-246`) is preserved: the extractor mutates carry/state only from the source goroutine, and the channel send is non-blocking. **No debounce inside the extractor** — debounce lives in the dashboard server's broadcast layer (§3) where a mutex is already required for the multi-tab broadcast map. Keeps the extractor lock-free.

**Carry buffer scope (narrow):**

The carry buffer holds bytes ONLY when the trailing portion of the input matches one of the partial OSC 52 prefixes:

```
\x1b
\x1b]
\x1b]5
\x1b]52
\x1b]52;<...up to terminator>
```

Anything else — including other OSC sequences (`\x1b]0;…` for title), CSI (`\x1b[…`), DCS, etc. — flushes through to the output immediately. This means a TUI emitting a window-title escape and going idle still has its title update delivered (no stuck-on-idle behavior). The carry only delays bytes that are genuinely candidates for OSC 52.

**Algorithm (sketch — tighten in implementation):**

```
input  := carry || event.Data
output := []byte{}
i      := 0
for i < len(input):
    if input[i:] starts with "\x1b]52;":
        end := find first BEL or ESC\\ in input[i+5:]
        if end found:
            payload := input[i+5 : end]
            extractRequest(payload)
            i = end + terminator_len
            continue
        else:
            // unterminated: hold from i to end-of-input as carry
            carry = input[i:]
            input = input[:i]
            break
    else if input[i:] matches one of the OSC 52 partial prefixes
              ("\x1b", "\x1b]", "\x1b]5", "\x1b]52", "\x1b]52;..."):
        // hold trailing bytes as carry — only OSC 52 candidates,
        // never other escape sequences
        carry = input[i:]
        input = input[:i]
        break
    else:
        output = append(output, input[i])
        i++

// Bound carry to 64 KiB; if exceeded, flush carry into output as-is and reset.
// (Pastejacker can't force a leak by sending a forever-unterminated OSC 52.)
```

`extractRequest` validates `Pc` (`^[cpsqb0-7]*$`, empty allowed), validates and decodes base64, runs the defang at the **byte level** (strip C0 controls except `\n` and `\t`, plus `\x7f`; UTF-8 lead/continuation bytes are ≥ 0x80 and unaffected), checks the 64 KiB decoded-size cap, then converts to a Go string (which substitutes invalid UTF-8 bytes as U+FFFD) and emits a `ClipboardRequest{SessionID, Text, ByteCount, StrippedControlChars, RequestID, Timestamp}` on `clipboardCh`. If any validation fails, the bytes are dropped — they were originally OSC 52, so dropping is correct (do not re-emit them as text).

**What `outputLog.Append` receives:**

Only `output` — the bytes with OSC 52 fully removed. Replay is clean by construction. The seq number returned by `Append` corresponds to this stripped chunk; xterm.js never sees the OSC 52 bytes either live or replayed. Subscribers receive the stripped chunk in their `SequencedOutput`.

**Cross-event correctness:**

The carry buffer is the key: if event N ends with `\x1b]52;c;<half-base64>` and event N+1 begins with `<rest-base64>\x07`, the extractor stitches them and emits one request. Event N's `outputLog.Append` receives the bytes UP TO `\x1b]52;` (stored as seq N); the OSC 52 itself is not stored. Event N+1's `Append` receives only the bytes AFTER the BEL. Seq numbers stay monotonic; no rewriting historical seq.

**Zero-length-output handling:**

When a single source event consists ENTIRELY of OSC 52 (the common case for a clean `nvim` yank that emits one `%output` line containing only the escape), the extractor produces `output = []byte{}`. We still call `outputLog.Append([]byte{})` to consume a seq number — this matches the existing precedent at `internal/dashboard/websocket.go:91` (`bootstrapFrameSeq` calls `Append(nil)` to reserve a seq) and keeps the seq stream contiguous with subscriber expectations.

The downstream WebSocket terminal handlers must then forward zero-length frames so the frontend's gap detector sees a contiguous seq stream. The main handler at `internal/dashboard/websocket.go:558-563` already does this (its comment explicitly warns about phantom-gap detection if zero-length frames are skipped). The CR handler at `:920` and FM handler at `:1013` currently skip empty events with `if len(event.Data) == 0 { continue }` — they need a parallel fix to send a zero-length frame instead. Without this fix, an OSC-52-only event triggers a phantom gap → unnecessary gap-replay round-trip (the replay returns the same empty entry, gets dedup'd, no user-visible breakage but wasted bandwidth). Fix the two handlers as part of the implementation; covered by the new test cases in §Tests.

**Dispose ordering:**

`SessionRuntime.Stop()` (`internal/session/tracker.go:165-185`) closes subscriber channels and waits on `doneCh` for `run()` to exit. The extractor lives inside `fanOut`, which runs only on the source goroutine, so `clipboardCh` is only ever sent-to from inside `run()`. Close `clipboardCh` AFTER `run()` exits (i.e., after `<-doneCh` in Stop), so there is no "send on closed channel" race. The dashboard server's subscriber goroutine unblocks on the close, removes the session's entry from `pendingClipboard`, and broadcasts `clipboardCleared`.

### 3. Dashboard UI

**Daemon-side broadcast state:**

The dashboard server maintains `pendingClipboard map[sessionID]*pendingEntry` (mutex-protected). Each `pendingEntry` holds the current `ClipboardRequest`, a `requestId` (UUID per emit), a TTL timer, and a 200ms debounce timer. A subscriber goroutine per session drains `clipboardCh`. On receiving a request:

- Acquire the map lock.
- If a debounce timer is already armed for this session: cancel it, replace `pendingEntry.req` and `requestId` with the new one, re-arm the 200ms timer.
- Otherwise: store the request, generate a new `requestId`, arm a 200ms debounce timer.
- When the debounce timer fires (on a separate goroutine): re-acquire the lock, re-arm the 5-minute TTL timer, broadcast on `/ws/dashboard` an event `{type: "clipboardRequest", sessionId, text, byteCount, strippedControlChars, requestId}`.

Debounce lives here (not in the extractor) so all timer callbacks share the same mutex with the broadcast map — no extra lock-ownership reasoning. Some TUIs emit OSC 52 on every selection-change tick (nvim with selection-tracking plugins, mouse-drag in tmux copy-mode); the 200ms window collapses these into a single user-facing prompt.

`requestId` disambiguates races: the Approve API call carries the requestId; if it doesn't match the current pending entry, daemon returns `{status: "stale"}`.

On session dispose / removal (subscriber goroutine sees `clipboardCh` closed, OR sessions update broadcasts removal): cancel both timers, delete `pendingClipboard[sessionID]`, broadcast `{type: "clipboardCleared", sessionId, requestId: ""}`.

TTL: 5 minutes of no new request and no ack auto-clears the entry via the same path. Mitigates the "yanked password lingers in React state" privacy concern. `time.AfterFunc` is used (no per-entry goroutine — Go's runtime timer wheel handles it).

**WS reconnect rehydration:**

When a `/ws/dashboard` connection opens (initial or reconnect), `handleDashboardWebSocket` (`internal/dashboard/server.go:1810-1834`) already sends initial state for `linearSyncResolveConflictStates` and `curationTracker.Active()`. Add an iteration over `pendingClipboard` and emit one `clipboardRequest` event per active entry to the new connection. Without this, a reload or WS drop would leave the user with no banner while the daemon still holds the pending state, until the user re-yanks.

This also handles the daemon-restart case: after a daemon restart, `pendingClipboard` is empty (in-memory only, see below). The WS reconnect snapshot delivers nothing. Any banner the frontend was holding pre-restart will not be re-delivered. The frontend should treat "WS reconnect with no `clipboardRequest` for a session whose banner was open" as "clear the banner" — the daemon snapshot is the source of truth on every reconnect.

**Daemon-restart durability:**

`pendingClipboard` is in-memory only. If the daemon dies (crash, OS update, manual restart) between OSC 52 emit and user ack, the request is lost. The user can re-yank in the TUI to re-trigger. Acceptable for v1; persisting an ephemeral confirmation prompt to `state.json` would be over-engineering. The frontend's "clear banner on WS reconnect with no matching snapshot entry" rule (above) prevents stuck banners across daemon restarts.

**HTTP endpoint:**

`POST /api/sessions/{id}/clipboard` with body `{action: "approve" | "reject", requestId: string}`. The daemon:

- Looks up `pendingClipboard[sessionID]`. If absent or `requestId` mismatch, returns 200 with `{status: "stale"}` — the request was already cleared (by another tab, by TTL, by session dispose).
- Otherwise clears `pendingClipboard[sessionID]` and broadcasts `clipboardCleared`.
- Returns 200 with `{status: "ok"}`.

The daemon does NOT distinguish approve vs reject in its state — the bookkeeping is identical. It records the action in logs for auditability.

**Frontend:**

- `SessionsContext` (or a new `ClipboardContext` if `SessionsContext` is too crowded) holds `pendingClipboard: Record<sessionId, ClipboardRequest | undefined>`. Updated by `clipboardRequest` / `clipboardCleared` events on the existing `/ws/dashboard` connection.
- `SessionDetailPage` renders a banner when `pendingClipboard[currentSessionId]` is set:

  ```
  ┌─────────────────────────────────────────────────────────┐
  │ TUI wants to copy to your clipboard (412 bytes)         │
  │ ┌─────────────────────────────────────────────────────┐ │
  │ │ const greet = (name) => `hello, ${name}`;            │ │
  │ │ greet("world");                                      │ │
  │ └─────────────────────────────────────────────────────┘ │
  │ 3 control characters were stripped.                     │
  │                                  [ Reject ] [ Approve ] │
  └─────────────────────────────────────────────────────────┘
  ```

  - Preview rendered in a `<pre>` with `white-space: pre-wrap`. Newlines preserved (decision: render naturally; defang already removed dangerous controls server-side).
  - Visual truncation at 4 KiB with "(N more bytes)" indicator; full text still goes to the clipboard on Approve.
  - **Approve** click handler:
    1. Mark the banner as "in-flight" — disable the buttons and **ignore inbound `clipboardRequest` events for this session** for the duration of the click. Prevents the slow-`writeText` race where a second OSC 52 arrives mid-click, replaces `pendingClipboard` server-side, and the user thinks they approved the new text but actually approved the old.
    2. Call `navigator.clipboard.writeText(text)` (with the text snapshotted at click time, not from current state). If it rejects, show inline error ("Browser blocked clipboard write — try clicking Approve again."), un-mark in-flight, do NOT call the API. Banner stays with whatever current state is.
    3. On `writeText` success, POST `/api/sessions/{id}/clipboard` with `{action: "approve", requestId}`. Response is informational; daemon broadcast clears the banner everywhere.
    4. If the API returns `{status: "stale"}`, the banner was replaced server-side mid-click — the clipboard already has the just-written text (correct), and the new pending request will appear after un-marking in-flight.
  - **Reject** click handler:
    1. POST `/api/sessions/{id}/clipboard` with `{action: "reject", requestId}`.
    2. Clear local state eagerly; daemon broadcast confirms cross-tab.
  - The user-click satisfies the browser's user-gesture requirement, so `writeText` works regardless of whether the tab was unfocused before the click.

- A small dot/badge on the session row in the sidebar when `pendingClipboard[sessionId]` is set, so the user can find a pending request from any route. **Note:** the "sidebar" is inline in `assets/dashboard/src/components/AppShell.tsx` (1054 lines, no extracted `SessionRow`). The badge wiring should be a separate small step in the implementation order — extract `SessionRow` first if needed, or thread the data inline if a one-line addition suffices.

**Multi-tab semantics (now correct by construction):**

Because the daemon owns the canonical pending state and broadcasts `clipboardCleared` on any approve/reject, all tabs see the same banner appear and the same banner disappear. There is no "stale banner in background tab" problem; no double-write; no `BroadcastChannel` plumbing needed in the browser.

If two tabs both click Approve simultaneously, the first POST clears the state; the second POST returns `{status: "stale"}` and does nothing (its `writeText` already ran, which is fine — the user clicked, the clipboard is set, no harm).

## Data flow

```
TUI (nvim) ──OSC 52──▶ inner tmux ──forwards (set-clipboard=external)──▶ outer tmux/PTY
                                                                              │
                       daemon SessionRuntime.fanOut receives event ◀──────────┘
                                                                              │
                       osc52Extractor (single goroutine, lock-free):          │
                         carry only OSC 52 prefixes,                          │
                         strip OSC 52 from bytes,                             │
                         emit ClipboardRequest on clipboardCh (cap 1, drop) ──┤
                                                                              │
                       outputLog.Append(stripped bytes; possibly empty) ◀─────┘
                                                                              │
                       (gap-replay never sees OSC 52 — clean by ctor)         │
                                                                              │
                       dashboard server (subscriber goroutine):               │
                         hold map lock, 200ms debounce-coalesce,              │
                         arm 5min TTL, broadcast clipboardRequest ────────────┐
                       on /ws/dashboard                                       │
                                                                              │
                       SessionsContext receives event, updates state ◀────────┘
                                                                              │
                       SessionDetailPage / sidebar render banner + badge ─────┘
                                                                              │
                       User clicks Approve → in-flight lock →                 │
                       writeText → POST /api/sessions/{id}/clipboard ─────────┐
                                                                              │
                       Daemon validates requestId, clears entry, ─────────────┘
                       broadcasts clipboardCleared → all tabs drop banner
```

**WS reconnect:** `handleDashboardWebSocket` snapshots `pendingClipboard` to the new client; banner reappears (or clears if not in snapshot).

## What we are deliberately not doing

- **No OSC 52 read.** `Pd === '?'` queries are ignored.
- **No images.** OSC 52 is text-only; image paste already exists.
- **No silent / always-allow / per-session trust mode.** Every write requires explicit user approval. Revisit if heavy-yank workflows make this painful.
- **No xterm.js OSC 52 handler.** Extraction is server-side; xterm.js never sees OSC 52. (If tmux fails to forward OSC 52 for any reason, the bytes also never reach the daemon, so this is consistent.)
- **No clipboard ring / history panel.** Pending = the most recent un-acknowledged write per session.
- **No buffering for unfocused tabs.** The user click satisfies the user-gesture requirement.
- **No fallback for browsers without `navigator.clipboard`.** If `writeText` rejects, banner stays with an inline error — user can retry or reject.
- **No cross-tab `BroadcastChannel`.** Daemon broadcast of `clipboardCleared` makes this unnecessary.

## Compatibility & risks

- **tmux version:** `terminal-features` requires tmux 3.2 (April 2021). On older tmux, the option fails (logged warning) and TERM=xterm-256color + `set-clipboard external` is the fallback. If both fail, OSC 52 silently never arrives — fail-safe.
- **Pastejacking:** addressed by user-in-loop confirm + sanitized preview + control-char strip (so an injected `\e]52;…` _inside the previewed text_ can't re-trigger OSC 52 if pasted into another OSC-52-honoring terminal) + 64 KiB cap.
- **Bidi / zero-width characters in preview:** the defang strips C0 + DEL but NOT C1 controls (0x80-0x9F) or non-printable Unicode (RTL override U+202E, zero-width spaces, invisible separators). A determined pastejacker could emit `eval` rendered as `evаl` (Cyrillic а) or hide content with U+200B. Out of scope for v1; the byte count and stripped-control note give the user one signal to notice anomalies. Revisit if real abuse appears.
- **Privacy:** pending text lives in the daemon's `pendingClipboard` map and in browser memory. 5-minute TTL clears the daemon side; the broadcast clears the browser side. If the user closes the dashboard tab without acknowledging, browser-side state goes with the tab. Daemon restart drops all pending entries (in-memory only).
- **Performance:** the OSC 52 extractor is a byte scan over each event in `fanOut`. Single goroutine per session, O(n) with a small constant factor. The 64 KiB carry cap bounds memory at one buffer per active session. `clipboardCh` capacity 1 with drop-on-overflow prevents back-pressure stalling the source loop. TTL/debounce timers use `time.AfterFunc` (runtime timer wheel — no per-entry goroutines).
- **Pre-existing tmux server:** documented in code; option lives on a server we don't own, gone if user kills it.
- **Multiple dashboard tabs:** consistent by construction (daemon-broadcast clear + WS-reconnect snapshot).
- **Selection-driven OSC 52 flicker:** addressed by 200ms debounce in the dashboard server's broadcast layer.
- **Slow `writeText` race:** addressed by the frontend's in-flight lock during Approve clicks (ignores inbound `clipboardRequest` events for the same session until `writeText` settles).
- **Daemon restart:** in-memory pending state is lost; user re-yanks. Frontend clears stale banners on WS reconnect via the snapshot rule.
- **Tests with OSC 52 in fixtures:** existing tests like `TestTrackerOutputLog_FanOutRecordsSequences` (`internal/session/tracker_test.go:177-210`) verify 1:1 event-to-seq mapping for plain bytes. Tests that intentionally feed OSC 52 through `fanOut` will see stripped output; flag this in the test docstring so it's clear, not surprising.

## Tests

**Go unit (tmux helpers):**

- `internal/tmux/tmux_test.go`: assert `SetServerOption` builds `set-option -s <opt> <val>` (inspect `srv.cmd(...).Args`, the existing pattern at line 222).
- `internal/tmux/tmux_test.go`: assert `applyTmuxServerDefaults` issues both `set-clipboard external` and `terminal-features *:clipboard`. Use a small `tmuxServerOptionSetter` interface (one method) accepted by `applyTmuxServerDefaults`; pass a fake recorder in the test. (Daemon package has no existing interface seams today; sibling packages do — this matches sibling idiom.)
- `internal/remote/controlmode/client_test.go`: assert `SetServerOption` issues `set-option -s …` (no scope flag default = wrong, hence the new method).

**Go unit (daemon hookpoint):**

Either:

- (a) Refactor the per-socket loop at `daemon.go:1003-1011` to call `applyTmuxServerDefaults` and add a small test that constructs a daemon with a fake `tmuxServerOptionSetter` and asserts the call. Same test should cover the new explicit default-socket `StartServer` + `applyTmuxServerDefaults` early in `Run()`.
- (b) Cover via E2E in `internal/e2e/`: spawn a session under `daemon-run`, run `tmux -L <socket> show-options -s set-clipboard` and assert `external`. Required if (a) is rejected as scope creep.

**Go unit (CR/FM zero-length frame fix):**

- Add cases in `internal/dashboard/websocket_test.go` (or wherever the CR / FM handler logic is exercised) asserting that a zero-length `event.Data` produces an outbound frame (matching the existing main-handler precedent at lines 558-563), not a skip.

**Go unit (osc52Extractor):**

`internal/session/osc52_test.go` (or `internal/escbuf/`-adjacent) table-driven:

- Plain bytes pass through unchanged; no `ClipboardRequest`.
- `\x1b]52;c;aGVsbG8=\x07` → `ClipboardRequest{Text:"hello", ByteCount:5, StrippedControlChars:0}`; output bytes empty.
- `\x1b]52;c;aGVsbG8=\x1b\\` (ST terminator) → same.
- `before\x1b]52;c;aGVsbG8=\x07after` → `Text:"hello"`, output is `beforeafter`.
- Two adjacent OSC 52 in one input → two requests, both stripped.
- `\x1b]0;title\x07` (other OSC, NOT 52) → unchanged, no request, **no carry held back across events** (since OSC 0 doesn't match the OSC-52-prefix table).
- `\x1b[31m` (CSI) → unchanged, no carry.
- Lone trailing `\x1b` at end of event with NO `]` following → flushed through to output (not held in carry — it's not part of the OSC-52-prefix table on its own; or held briefly for one event then released — pin behavior in the test).
- Empty `Pc` (`\x1b]52;;aGVsbG8=\x07`) → accepted, request emitted.
- Invalid `Pc` (`\x1b]52;xyz;aGVsbG8=\x07`) → bytes stripped, NO request (validation failure).
- Read query `\x1b]52;c;?\x07` → bytes stripped, no request.
- Decoded payload >64 KiB → bytes stripped, no request.
- Cross-event: feed `\x1b]52;c;aGVsb` then `G8=\x07` → one request `Text:"hello"`, both events' outputs are empty.
- Cross-event with non-OSC bytes around: `before\x1b]52;c;aGVsb` then `G8=\x07after` → one request, outputs `before` and `after`.
- Carry overflow: feed `\x1b]52;c;` followed by 64+ KiB of base64 with no terminator → carry overflows, all carried bytes flushed to output (failsafe), no request.
- Defang (byte-level): payload base64 of bytes `0x61 0x0a 0x62 0x1b 0x63 0x07 0x64 0x00 0x65` (`a\nb\x1bc\x07d\x00e`) → `Text:"a\nbcde"` with `\x1b`, `\x07`, `\x00` stripped, `\n` preserved; `StrippedControlChars:3`.
- Lone byte `0x80`: payload base64 of `[0x80]` → `Text` contains U+FFFD (Go's `string([]byte{0x80})` keeps the byte; the conversion to a JS string over the wire substitutes — pin chosen behavior in the test).
- Empty-output Append: `\x1b]52;c;aGVsbG8=\x07` produces `Append([]byte{})`, which returns a seq and propagates as a zero-length SequencedOutput.

**Go unit (dashboard server pendingClipboard / debounce / TTL):**

- Two requests on `clipboardCh` within 200ms → one broadcast emitted (with the second request's contents and a fresh requestId).
- Two requests separated by 300ms → two broadcasts.
- Single request, no ack → TTL fires at 5min, `clipboardCleared` broadcast emitted.
- Session dispose during pending → `clipboardCh` closes, subscriber cleans entry, `clipboardCleared` broadcast emitted, both timers cancelled (no leak).
- POST with valid sessionId + matching requestId + action=approve → 200 ok, entry cleared, broadcast emitted.
- POST with stale requestId → 200 stale, no broadcast, entry untouched.
- POST with unknown sessionId → 404.
- WS reconnect snapshot: connect a new `/ws/dashboard` while `pendingClipboard[sid]` is non-empty → new client receives a `clipboardRequest` for `sid` as part of initial state.
- WS reconnect with no pending: connect → no `clipboardRequest` initial event for `sid` (frontend uses absence to clear stale banners).

**TS unit (`SessionsContext` / `ClipboardContext`):**

- Receive `clipboardRequest` event → context populated.
- Receive `clipboardCleared` event → context cleared.
- New `clipboardRequest` for same session → replaces previous (no merging, no stack).
- WS reconnect with snapshot → context rehydrated from initial events.
- WS reconnect with empty snapshot but a previously-known session → that session's banner clears (snapshot-is-source-of-truth rule).

**TS unit (`SessionDetailPage` banner):**

- Pending request in context → banner renders with text, byte count, stripped count.
- Click Reject → POST issued with `{action: reject, requestId}`; `writeText` not called; banner cleared eagerly.
- Click Approve, `writeText` resolves → POST issued with matching requestId; banner cleared.
- Click Approve, `writeText` rejects → POST NOT issued; inline error shown; banner stays; in-flight lock released.
- During in-flight Approve, an inbound `clipboardRequest` for the same session → ignored until `writeText` settles, then applied.
- POST returns `{status: stale}` after Approve → no error shown; banner state reflects whatever the broadcast set.
- Banner truncates preview >4 KiB visually but full text still passed to `writeText`.

**Scenario (Playwright, `test/scenarios/`):**

- `tui-clipboard-write.md`: spawn a session, `printf '\e]52;c;%s\a' "$(printf hello | base64)"`, assert banner appears with "hello", click Approve, `navigator.clipboard.readText()` returns "hello".
- Pastejacking case: payload containing `rm -rf ~`, assert preview shows literal text and clipboard contains literal text after Approve (no shell expansion, just bytes).
- Defang case: payload base64 of text containing `\e[31m` ANSI escape, assert preview shows it stripped and stripped-count is correct.
- Multi-tab: open two tabs on the same dashboard, trigger one OSC 52, both show banner, approve in tab A, tab B's banner disappears via the `clipboardCleared` broadcast.

## Implementation order

Each step is independently committable and reviewable:

1. **`SetServerOption` helpers** on `*tmux.TmuxServer` and `*controlmode.Client` (Go + unit tests) — pure refactor, no behavior change yet.
2. **`applyTmuxServerDefaults` (local) and `applyRemoteTmuxDefaults` (remote) helpers** + all invocation sites:
   - Default-socket: explicit `StartServer` + `applyTmuxServerDefaults` early in `Run()`.
   - Restored-session sockets: pair with the existing per-socket loop at `daemon.go:1003-1011`.
   - Remote: replace inline option calls in `waitForControlMode` (`connection.go:736-746`) with one `applyRemoteTmuxDefaults` call.
   - Set `TERM=xterm-256color` in daemon `cmd.Env` and inside `Run()` before any tmux child is forked.
   - Local sessions now have OSC 52 forwarded by tmux but the daemon still has no extractor — bytes flow into xterm.js as raw OSC 52 (silently ignored by xterm.js, harmless interim state).
3. **CR/FM zero-length-frame fix** in `internal/dashboard/websocket.go` (handlers at `:920` and `:1013`): forward zero-length frames instead of skipping. Independently useful — already a latent phantom-gap risk for any other zero-byte event source.
4. **`osc52Extractor` package** (`internal/session/osc52.go` or new package) with all the unit tests including cross-event, narrow-prefix carry, and overflow cases.
5. **Wire extractor into `fanOut`** in `internal/session/tracker.go`. Add `clipboardCh` (cap 1, drop-on-overflow) to `SessionRuntime`. Close ordering: after `run()` exits in `Stop()`. OSC 52 now extracted server-side; OutputLog is clean.
6. **Dashboard server: subscriber goroutines per session, `pendingClipboard` map, debounce + TTL timers, broadcast events, HTTP endpoint, WS-reconnect snapshot in `handleDashboardWebSocket`.** Generate TS types via `cmd/gen-types`.
7. **Frontend context + banner + sidebar badge** with in-flight Approve lock and snapshot-as-source-of-truth on WS reconnect. Step the sidebar badge separately if `AppShell.tsx` extraction is needed.
8. **Manual verification** with `nvim` (`:set clipboard=unnamedplus`, then `yy`), `tmux copy-mode`, `lazygit` if installed.
9. **Scenario tests** including multi-tab and reconnect cases.
