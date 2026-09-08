# Clipboard Bridge

## What it does

When a TUI inside a schmux tmux session writes to the system clipboard (via OSC 52 or tmux's paste-buffer-changed notification), the daemon intercepts the write, broadcasts a structured event to the dashboard, and renders a confirmation banner with a sanitized preview. The user clicks Approve to commit the write to their browser system clipboard, or Reject to discard it. Every write is gated on an explicit user click — pastejacking is the threat model.

## Key files

| File                                                  | Purpose                                                                                           |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `internal/session/osc52.go`                           | `osc52Extractor`, `ClipboardRequest`, byte-level defang helper                                    |
| `internal/session/tracker.go`                         | Extractor wired into `SessionRuntime.fanOut`; `clipboardCh` (cap 1, drop-on-overflow)             |
| `internal/session/localsource.go`                     | Local tmux control-mode source feeding the same `fanOut`                                          |
| `internal/session/remotesource.go`                    | Remote SSH-tunneled source feeding the same `fanOut`                                              |
| `internal/session/controlsource.go`                   | `SourcePasteBuffer` event for tmux `%paste-buffer-changed` notifications                          |
| `internal/session/inputecho.go`                       | Echo of local `tmux load-buffer -` writes (for paste-buffer path coverage)                        |
| `internal/tmux/tmux.go`                               | `SetServerOption` on `*TmuxServer` (server-scope `set-option -s`)                                 |
| `internal/tmux/defaults.go`                           | `ApplyTmuxServerDefaults`: `set-clipboard` + `terminal-features '*:clipboard'`                    |
| `internal/remote/controlmode/client.go`               | `SetServerOption` on `*Client` (no scope flag = wrong scope, hence the new method)                |
| `internal/remote/defaults.go`                         | `applyRemoteTmuxDefaults`: server options + `window-size manual` + `DISPLAY :99`                  |
| `internal/remote/connection.go`                       | `ClipboardExternal` per-connection, applied via `applyRemoteTmuxDefaults` in `waitForControlMode` |
| `internal/daemon/daemon.go`                           | Calls `ApplyTmuxServerDefaults` for default socket and every restored-socket                      |
| `internal/dashboard/clipboard_state.go`               | `clipboardState` map, debounce/TTL/dedup timers, spawn-prompt suppression, `RegisterSpawnPrompt`  |
| `internal/dashboard/clipboard_ack.go`                 | `makeClipboardAckHandler` for `POST /api/sessions/{id}/clipboard`                                 |
| `internal/dashboard/server.go`                        | WS reconnect rehydrate: snapshot `pendingClipboard` to new dashboard clients                      |
| `internal/dashboard/handlers_spawn.go`                | Calls `RegisterSpawnPrompt(req.Prompt)` after a spawn                                             |
| `internal/api/contracts/clipboard.go`                 | `ClipboardRequestEvent`, `ClipboardClearedEvent`, `ClipboardAckRequest`, `ClipboardAckResponse`   |
| `assets/dashboard/src/hooks/useSessionsWebSocket.ts`  | `pendingClipboard` state, WS message dispatch, snapshot-as-source-of-truth on reconnect           |
| `assets/dashboard/src/contexts/ClipboardContext.tsx`  | Reads `pendingClipboard` from the sessions context                                                |
| `assets/dashboard/src/components/ClipboardBanner.tsx` | Banner UI with Approve/Reject, in-flight Approve lock, truncated preview                          |
| `internal/config/config.go`                           | `GetClipboardSyncEnabled()` settings toggle (turns OSC 52 forward on/off)                         |
| `test/scenarios/tui-clipboard-write.md` + `.spec.ts`  | Playwright scenario: pastejacking, defang, multi-tab, browser clipboard assertion                 |

## Architecture decisions

- **Server-side extraction at `SessionRuntime.fanOut`, not in xterm.js.** OSC 52 is stripped from the byte stream _before_ it reaches `outputLog.Append`. Gap-replay is clean by construction — no separate stripper, no seq rewriting, no in-band OSC 52 handler in xterm.js. The extractor runs in the source goroutine (lock-free) and emits on a per-session channel `clipboardCh` (cap 1, drop-on-overflow).

- **Two write paths converge on the same dashboard state.** The OSC 52 byte extractor (`internal/session/osc52.go`) and tmux's `%paste-buffer-changed` notifications (raised by `controlmode` and emitted as `SourcePasteBuffer` events) both flow into the same `clipboardState.onRequest` with the shared `defangClipboardBytes` helper. Security parity is automatic — anything one path strips, the other strips too.

- **The dashboard server owns canonical pending state.** `pendingClipboard: map[sessionID]*pendingEntry`, mutex-protected. Every emit mints a UUID `requestID`; the HTTP ack endpoint posts `{action, requestId}` and returns `ok` vs `stale` based on whether the ID still matches. All tabs receive `clipboardCleared` on every ack, so multi-tab semantics work by construction — no `BroadcastChannel` plumbing needed in the browser.

- **Debounce lives in the dashboard server, not the extractor.** 200 ms debounce on the broadcast layer collapses selection-driven OSC 52 flicker (nvim with selection-tracking, mouse-drag in tmux copy-mode) into one banner. Debounce + TTL + dedup all use `time.AfterFunc` — no per-entry goroutines, just Go's runtime timer wheel. `pendingEntry.gen` defends against the stale-callback race (`time.AfterFunc.Stop()` doesn't abort queued callbacks behind the lock).

- **Cross-session content+timestamp dedup (200 ms).** tmux's `%paste-buffer-changed` is server-scoped — every control-mode client on a shared daemon socket sees the same notification. Without dedup, N sessions produce N banners for one copy. Same window also collapses copy-mode's OSC-52 + set-buffer pair into one banner.

- **Spawn-prompt suppression.** Claude Code reads its own argv and OSC-52s the same string back through the system clipboard, which would surface a banner for a string the user just typed. `RegisterSpawnPrompt(text)` records the prompt in a workspace-scoped registry with a 60 s TTL; matching incoming requests are silently dropped.

- **Byte-level defang, shared helper.** Strip C0 controls except `\n` (0x0a) and `\t` (0x09), plus DEL (0x7f). UTF-8 lead/continuation bytes are ≥ 0x80 and unaffected, so multibyte characters round-trip cleanly. C1 controls (0x80–0x9F) and Unicode bidi/zero-width characters are NOT stripped — the byte count and `strippedControlChars` give the user one signal to notice anomalies. Revisit if real abuse appears.

- **tmux passthrough via server-scope options.** `set-clipboard external` + `terminal-features '*:clipboard'` are tmux **server** options, not session options. `SetServerOption` is a new method on `*TmuxServer` and `*controlmode.Client` precisely because the existing `SetOption` defaults to session scope, which is the wrong scope for these. `external` (not `on`) avoids tmux retaining every yanked secret in its paste buffer.

- **Two entry points for tmux option application.** Local: `ApplyTmuxServerDefaults` runs in `Run()` early (for the default socket, with an explicit `StartServer` for the `daemon-run` path) and per restored-socket in `daemon.go:1149`. Remote: `applyRemoteTmuxDefaults` runs from `waitForControlMode`, which is called from both `connect()` and `Reconnect()`, so reconnect-after-remote-server-restart re-applies by construction. Errors are logged as warnings — never fatal.

- **Frontend in-flight lock during Approve.** Clicking Approve ignores inbound `clipboardRequest` events for the same session between click and `writeText` settlement. Prevents the race where a second OSC 52 arrives mid-click, replaces `pendingClipboard` server-side, and the user thinks they approved the new text but actually approved the old.

- **WS reconnect rehydrates snapshot.** `handleDashboardWebSocket` reads `pendingClipboard` on every connect and emits one `clipboardRequest` per active entry to the new client. The frontend clears local banners not present in the snapshot (snapshot-as-source-of-truth) — handles WS drops, reloads, and daemon restarts (where `pendingClipboard` is empty post-restart and any pre-restart banner is dropped).

- **In-memory only, no persistence.** Pending state lives in `clipboardState` and dies with the daemon. Crash/restart between emit and ack loses the request; the user re-yanks. Persisting an ephemeral confirmation prompt to `state.json` would be over-engineering.

- **Empty `output` still produces a seq.** A single source event consisting entirely of OSC 52 produces `output = []byte{}`. We still call `outputLog.Append([]byte{})` to consume a seq, and the WebSocket handlers forward a zero-length frame — without this, the frontend's gap detector sees a phantom gap. The main handler already did this; the CR handler at `:920` and FM handler at `:1013` needed the parallel fix.

## Gotchas

- **`SessionRuntime.clipboardCh` capacity 1 with drop-on-overflow.** Mirrors `LocalSource.emit`. A momentarily missed request is benign: the dashboard's debounce/dedup means it surfaces as a stale yank the user can reproduce.

- **Carry buffer scope is narrow.** Only `\x1b`, `\x1b]`, `\x1b]5`, `\x1b]52`, `\x1b]52;` are held across event boundaries. Title OSC (`\x1b]0;…`), CSI, DCS, and lone trailing `\x1b` flush through immediately. Stale `\x1b` alone is NOT held — it matches no prefix. The 64 KiB carry cap is a failsafe: if a TUI never closes the sequence, the carried bytes flush to output as-is.

- **Defang happens before string conversion.** `defangClipboardBytes` works on the decoded byte slice. UTF-8 replacement chars (U+FFFD) are produced later by Go's `string([]byte)` conversion for invalid UTF-8 — pinning that behavior in tests is enough.

- **`time.AfterFunc.Stop()` does not abort queued callbacks.** `pendingEntry.gen` is bumped on every `onRequest`; debounce/TTL closures capture the gen at arm time and abort if it no longer matches. Without this, a re-armed timer produces a duplicate broadcast.

- **`ApplyTmuxServerDefaults` is silent on failure.** Options are belt-and-braces; failure must not block server startup. If tmux < 3.2 doesn't support `terminal-features`, `set-clipboard external` alone is the fallback. If both fail, OSC 52 silently never arrives — fail-safe.

- **Pre-existing tmux servers schmux did not start** still receive the option via `SetServerOption` for as long as they live; they may drop it if killed and restarted outside schmux's control. Acceptable for v1.

- **ClipboardExternal is per remote connection, not global.** Toggled independently on `Connection`. Default `true`.

- **`clearAll()` exists for the settings toggle.** When clipboard sync is turned off via `GetClipboardSyncEnabled() == false`, `clearAll()` drops pending entries and broadcasts `clipboardCleared` so already-visible banners disappear immediately rather than lingering for the 5 min TTL.

- **`extractRequest` silently drops malformed sequences.** Invalid `Pc`, read queries (`Pd == "?"`), base64 decode failure, decoded payload > 64 KiB — all return `(zero, false)`. Bytes were originally OSC 52, so dropping is correct (don't re-emit them as text).

- **`text` in the banner is post-defang.** Approve passes the defanged text to `navigator.clipboard.writeText`. The banner preview shows the defanged text; the byte count and stripped-control count reflect pre-defang values so the user has one signal that something was removed.

- **Banner truncates preview >4 KiB visually** but the full defanged text still goes to `writeText`. The truncation is purely UI.

- **The dashboard server's `clipboardPromptSuppressionTTL` is 60 s**, not 5 min. The argv-prompt round-trip happens within seconds of startup; longer registration would over-suppress legitimate yanks of identical content.

## Common modification patterns

- **To add a new clipboard write trigger.** Emit on `clipboardState.onRequest` with a `session.ClipboardRequest`. The dashboard's debounce/dedup/broadcast is path-agnostic.

- **To change debounce/TTL/dedup windows.** Package-level vars in `internal/dashboard/clipboard_state.go` (`clipboardDebounceWindow`, `clipboardTTL`, `clipboardDedupWindow`, `clipboardPromptSuppressionTTL`) are overridden in tests. Production values live next to the var definitions with rationale comments.

- **To add a new OSC 52 selector (Pc).** Update `pcValidationRe` regex in `osc52.go`. Out-of-spec selectors are silently rejected at extraction time. The reference is the OSC 52 spec's selection-char table.

- **To add a new tmux server default.** Add a `[2]string` entry to `ApplyTmuxServerDefaults` (local) and `applyRemoteTmuxDefaults` (remote). Tests in `internal/tmux/defaults_test.go` and `internal/remote/defaults_test.go` assert the call shape — add a row there.

- **To change the banner UX.** All banner logic lives in `ClipboardBanner.tsx`. Approve/Reject handlers in that file own the in-flight lock; `clearPendingClipboard` (from the sessions context) handles the local state clear, and the daemon's broadcast handles cross-tab.

- **To add a new spawn-time suppression reason.** Add a new field/method on `clipboardState` and call it from the relevant `handlers_*.go` site. Suppression entries are workspace-scoped, so a workspace ID is the natural key.
