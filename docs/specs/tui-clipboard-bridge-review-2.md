VERDICT: NEEDS_REVISION

## Summary Assessment

V2 fixes the conceptual issues from round 1 (server-vs-session option scope, pastejacking via user-in-loop confirm, deprecated `escape()` API, the missing fake-cmd-pattern claim), but introduces concrete new bugs in two of the three load-bearing hookpoints — the daemon-startup hook is wrong (line 217 is in the parent `Start()` shim that forks the daemon, not in the daemon process itself, so `daemon-run` and `./dev.sh` would never invoke it) — and hand-waves the gap-replay stripper algorithm in a way that ignores `OutputLog`'s per-event chunk structure (an OSC 52 sequence routinely spans multiple `LogEntry` chunks). Several smaller issues (Reconnect path skips the post-handshake hook, multi-tab UX, sidebar badge wiring, dispose-on-session-kill) also need addressing.

## Critical Issues (must fix)

### 1. The "daemon startup" hookpoint is in the wrong function

Spec §1, line 29: "In `internal/daemon/daemon.go` adjacent to the existing `StartServer` call (~line 217)".

`internal/daemon/daemon.go:213-217` is inside `func Start()`, which is the **parent process shim** that forks the daemon via `exec.Command(execPath, "daemon-run", "--background")` (line 226). This `Start()` is invoked only by `schmux start`. The actual daemon process runs `daemon.Run()` (`internal/daemon/daemon.go:376`) — and that path is also reached directly via `schmux daemon-run` (see `cmd/schmux/main.go:120-123`), which is the path `./dev.sh` and `daemon-run --background` use. The two paths are routed at `cmd/schmux/main.go:93-130`.

If the spec's hook is added at line 217, it will be skipped entirely whenever the daemon is launched via `daemon-run` (i.e., dev mode and any user who follows the `daemon-run` path documented in `CLAUDE.md`). The hook needs to live in the daemon process itself — most naturally somewhere in `Run()` after `tmuxServer` is constructed at `internal/daemon/daemon.go:578` (inside `initConfigAndState`'s returned `daemonInit`, used in `Run()` from line 389 onward).

Note also that `StartServer` (`tmux start-server`) is _not_ called inside the daemon process at all today — the daemon assumes the server is already running (started by `Start()` if forked, or by the first `CreateSession` otherwise, or by an explicit `tmux` invocation by the user). So "adjacent to the existing `StartServer` call" inside the daemon process doesn't have a counterpart to be adjacent to. The spec needs to either (a) add a `StartServer` call inside `Run()` and then call `SetServerOption` after it, or (b) call `SetServerOption` opportunistically from `Run()` and accept that on the very first run before any session exists the call could fail silently (and rely on the session's first `CreateSession` to start the server, then re-call `SetServerOption`). Option (a) is cleaner and matches the existing socket-init loop at `internal/daemon/daemon.go:1003-1011` which already calls `srv.StartServer(d.shutdownCtx)` for restored-session sockets.

There is also a race: even with the call moved to the daemon process, if a sidecar tmux server existed before the daemon ran (e.g., a user started one), `SetServerOption` will succeed but on a server whose lifecycle the daemon doesn't own — the option may be lost when that pre-existing server exits.

### 2. Gap-replay OSC 52 stripper hand-waves chunk boundaries

Spec §2 says the stripper "scans the gap-replay byte slice" for OSC 52 sequences. But `buildGapReplayFrames` at `internal/dashboard/websocket.go:98-109` returns one frame per `OutputLog` entry, and each entry is one `Append` call (`internal/session/outputlog.go:39`). A single Append corresponds to a single `controlmode.OutputEvent` in the local path (`internal/session/tracker.go:229`) or one `%output` line in the remote control-mode path (`internal/remote/controlmode/parser.go:232`). An OSC 52 sequence whose base64 payload is a few KB (well within the 64 KiB cap) absolutely will span multiple `Append` events, especially in remote control mode where each `%output` line is a separate event.

Concrete problems the spec doesn't address:

- If the stripper runs _per-frame_ (per `LogEntry`), it sees `\x1b]52;c;<half-of-base64>` in entry N and `<other-half-of-base64>\x07` in entry N+1; both are unterminated when scanned in isolation. The "strip through end-of-buffer if unterminated" rule applied per-entry would strip the first chunk's tail and leave the second chunk's body+terminator — which xterm.js would silently treat as garbage (not OSC 52, but still ESC parsing churn) or, worse, the second chunk's `\x07` could be ignored and following bytes interpreted oddly.
- If the stripper concatenates entries, rewrites, and re-emits, it must preserve per-entry seq numbers (the frontend's dedup at `frames[i].seq` depends on the seq numbers being individual, so collapsing N entries into M<N merged frames will skip seq numbers, which the frontend interprets as gaps and re-requests). The spec says nothing about how to preserve seq mapping when bytes are removed.
- A real OSC 52 from tmux may also contain ST as terminator (`ESC \`); if the `ESC` of the ST falls in entry N's last byte and `\\` lands in entry N+1's first byte, even concatenation followed by scanning is fine, but per-entry scanning is broken (entry N's ESC is never matched as a terminator, entry N+1's `\\` is just a backslash).

The spec must either (a) explicitly buffer cross-entry state across the loop in `buildGapReplayFrames`, preserving seq numbers by emitting zero-length frames where stripped bytes used to be (or coalescing seq across removed frames in a way the frontend tolerates), or (b) operate at the single-entry level and accept that cross-entry OSC 52 will leak through (which means the stripper protects nothing in practice — exactly the regression v1 flagged). Test case in §Tests for "Adjacent OSC 52 sequences both removed" covers in-buffer adjacency but not the cross-entry case.

The escbuf precedent at `internal/escbuf/escbuf.go:43-49` deliberately limits its scan window to 16 bytes for performance — and explicitly notes that long DCS/APC/PM/SOS sequences (analogous to OSC 52 here) "will NOT be held back". The spec's "easy state-machine" framing is at odds with the existing project precedent for handling exactly this category of sequence.

### 3. Reconnect path skips the post-handshake hook

Spec §1 says to call `SetServerOption` "immediately after `setenv -g DISPLAY :99` at `internal/remote/connection.go:746`" (which sits inside `connect()`, the post-handshake setup block at `internal/remote/connection.go:706-755`). But `Reconnect()` at `internal/remote/connection.go:393-518` runs `parseProvisioningOutput` → `waitForControlMode` → `rediscoverSessions` and then returns at line 517 — it does **not** re-run the post-handshake setup block. Today `window-size manual` and `setenv -g DISPLAY :99` also are not re-applied on reconnect, so the spec follows existing precedent.

That precedent is acceptable for `set-clipboard` and `terminal-features` _only because the remote tmux server typically outlives the SSH connection_ — the options persist on the running server. But:

- If the remote tmux server is restarted (e.g., the host reboots, or the user runs `tmux kill-server` over the SSH session for any reason), reconnecting via `Reconnect` will silently come back without those options set, and OSC 52 stops working. There is no clear failure surface — the feature just stops working.
- The existing precedent is a known limitation, not a documented "we tested this"; the spec inheriting it without comment compounds the problem rather than addressing it.

The spec should either move the option-setting into a helper that's invoked from both `connect()` and `Reconnect()`, or document the failure mode and decide it's acceptable. Inheriting the existing behavior silently is the wrong default here because OSC 52 is a feature whose absence is invisible to the user.

### 4. Per-tab pending state on session dispose is unspecified — leak / stuck-banner risk

§3 "React state (per-session)" puts pending requests in `SessionsContext` keyed by session ID. Sessions are removed from `sessionsById` when the daemon disposes them and broadcasts a sessions update. The spec is silent on whether the pending clipboard request entry is also cleared when the session disappears.

If not cleared, then:

- The map grows unboundedly across many spawn/dispose cycles in a long dashboard session (memory leak — small but real).
- More importantly, the user-facing UX is wrong: if a TUI emits OSC 52 and the user disposes the session before clicking Approve (or the session crashes), the banner stays in the React state until the tab is closed. If the same session ID is later reused (it can be — IDs are not strongly unique across schmux history), the stale banner reappears against an unrelated context. Worse, if the banner lives in `SessionDetailPage` and the session is gone, the page may not render the banner at all but the state still occupies memory.

Add an explicit rule: when a session is removed from `sessionsById` (or transitions to a terminal state), drop its pending clipboard entry.

## Suggestions (nice to have)

### 5. Sidebar badge implementation is hand-wavy

§3 line 119: "A small dot/badge on the session row in the sidebar indicates 'clipboard pending' so the user can find it." The "sidebar" is not a separate component — it lives inside `assets/dashboard/src/components/AppShell.tsx` (1054 lines), with session rows constructed inline around lines 918-986 (no extracted `SessionRow` component). Wiring a badge means threading a new piece of data (pending-clipboard set) through a large component or wiring a context selector inside the row markup. This is doable but is not "small" — the spec should either extract a `SessionRow` first or scope this badge to a separate cleanup task.

### 6. Multi-tab "harmless double-write" UX is not actually harmless

§3 "Concurrency": "Approving in two tabs writes the same text to the clipboard twice — harmless." The user-visible problem is the inverse: if Tab A approves and Tab B has the same banner pending, the user reads the banner in Tab B as a _new_ event, may approve it, and now believes a second TUI write happened (or rejects it, believing they're cancelling — but the clipboard already changed). Background-tab banners persist, so when the user later visits Tab B they see a "fresh-looking" prompt for an event they already handled. At minimum, document this clearly in the UI and code comments. A `BroadcastChannel('schmux-clipboard')` cross-tab dismiss-on-handle is a small change worth considering even in v1 (the spec defers it; verify the deferral with a usage assumption — e.g., "single dashboard tab is the common case").

### 7. Replace-on-new flicker for selection-driven OSC 52

§3 banner UX says "Replace-on-new semantics: when a new OSC 52 arrives for a session that already has a pending request, the new request silently overwrites the old." This is correct for `y` in nvim and tmux copy-mode `Enter`, which fire OSC 52 once on commit. But some configurations (nvim with `clipboard=unnamedplus` plus selection-tracking plugins, or tmux copy-mode with `set -g mouse on` and certain bindings) emit OSC 52 on every selection-change tick. With replace-on-new the banner could re-render many times per second during a drag-select. The spec asserts "matches how the system clipboard itself works (last-write-wins)" but a system clipboard doesn't render a confirmation modal. Worth either (a) debouncing the React state update on the inbound side (200ms is unlikely to harm UX), or (b) noting "we accept the flicker for v1; revisit if real users hit it." Currently unverified.

### 8. Pc validation regex inconsistency

§3 handler comment says "(empty Pc means c+s)", and the regex `pc !== '' && !/^[cpsqb0-7]+$/.test(pc)` accepts empty Pc as valid (because the conjunction `pc !== ''` short-circuits). The `+` quantifier on the character class means the regex requires at least one valid char — consistent with the comment. This is fine, but the spec should add a test asserting empty Pc is accepted (`;aGVsbG8=` → handled), since the test list at lines 211-217 only covers non-empty Pc cases.

A subtler issue: the spec validates against the literal characters `cpsqb0-7`, but `q` and `b` are non-standard / vendor extensions in different references. xterm itself documents `c`, `p`, `s`, plus `0-7` for cut buffers. If the goal is "anything resembling a real OSC 52", the validation set is fine; if the goal is strict-spec, drop `q` and `b`.

### 9. Privacy: pending clipboard text persists in React memory until manually rejected

The pending clipboard text sits in `SessionsContext` indefinitely. For a TUI password manager workflow (`pass`, `1password-cli`, etc.) where the user yanks a password and then walks away from the dashboard tab without clicking Approve or Reject, the password lingers in memory and in any React DevTools / DOM inspector. Not a hard blocker, but worth a TTL (e.g., auto-expire after 5 minutes of no user interaction) or a visual reminder. The spec explicitly says no buffering for unfocused tabs, which protects `writeText`, but the in-memory lifetime is a separate concern.

### 10. Spec language understates the daemon-start TERM situation

§1 says `TERM=xterm-256color` is "belt-and-braces for older installs". Verified the daemon spawn at `internal/daemon/daemon.go:226-236` propagates `os.Environ()` plus `SCHMUX_HOME` — TERM is whatever the parent shell had. If `schmux start` is run from `launchd`, `cron`, or any non-tty parent, TERM will be empty. The spec should be explicit: set `TERM=xterm-256color` in the daemon `cmd.Env` (or in the daemon process itself, before spawning tmux children) regardless of tmux version, since the cost is zero and it covers a real bug class.

### 11. No mention of how the OSC handler interacts with `terminal.dispose()` race

The spec says to dispose the OSC handler in `dispose()` (and "WS reconnect-recreate path that destroys/recreates the `Terminal` instance" — which doesn't actually exist; verified at `terminalStream.ts:285-309` there is no Terminal recreation outside `dispose()` itself, and `recreationCount` is only incremented as a metric, not a code path). Trim this from the spec to reduce confusion: there is exactly one Terminal lifetime per `TerminalStream`, and one dispose path (`dispose()`). The "visibility-change cleanup" mentioned in §3 is also non-existent — the `visibilityHandler` at `terminalStream.ts:507-517` only calls `tsLog`, no cleanup work.

### 12. `tmuxOptionSetter` interface seam — minor naming concern, otherwise reasonable

The daemon package has zero existing interfaces today (verified: `grep "type \w+ interface" internal/daemon/` returns nothing). Sibling packages do use small narrow interfaces (`internal/dashboard/websocket.go:41 ioWorkspaceTelemetryProvider`, `internal/dashboard/tab_hooks.go:52 PreviewDeleter`). So the pattern is project-idiomatic, just new to the daemon package. No pushback expected — but consider naming the interface for the operation rather than the implementer (e.g., `tmuxServerOptionSetter` since you're setting _server_-scope options, to disambiguate from per-session `SetOption`).

### 13. 64 KiB prelim cap math is fine

`64 * 1024 * 4 / 3 + 16` evaluates as `(64*1024*4)/3 + 16 = 262144/3 + 16 = 87381 + 16 = 87397` (Go-style integer division, but JS does the same since these are integers fitting in number range). Minimum base64 length of a 65,536-byte payload is `ceil(65536/3) * 4 = 87,384`. So 87397 ≥ 87384 with 13 bytes slack, which covers the 1-2 byte `=` padding plus a few bytes of jitter. Math is correct. Worth a one-line comment in code clarifying the 16-byte slack is for padding + safety margin, since the formula is opaque to a casual reader.

## Verified Claims

- `registerOscHandler(52, ...)` exists on `terminal.parser` in `@xterm/xterm@6.0.0` (round 1 verified).
- `allowProposedApi: true` is set at `assets/dashboard/src/lib/terminalStream.ts:339`.
- `buildGapReplayFrames` at `internal/dashboard/websocket.go:98` does iterate per-entry and emit one frame per `LogEntry`; gap-replay is the only path that re-feeds raw historical bytes to xterm.js (bootstrap uses `tmux capture-pane -e`, which renders cell contents and won't re-emit OSC 52).
- The bootstrap path uses `CaptureLastLines` which calls `tmux capture-pane -e -p -S -<n>` (`internal/tmux/tmux.go:296-302`).
- `internal/remote/connection.go:706-755` is indeed the "post-handshake setup block" with `setenv -g DISPLAY :99` at line 746 and `set-option window-size manual` at line 736 — adjacent to where the spec wants the new options set.
- `controlmode.Client.SetOption` at `internal/remote/controlmode/client.go:571` issues `set-option <opt> <val>` with no scope flag, which would default to session scope — confirming the v1 critique that a `SetServerOption` variant is needed.
- `*tmux.TmuxServer.SetOption` at `internal/tmux/tmux.go:196` adds `-t <session>`, also session-scope.
- The `escbuf.SplitClean` path at `internal/escbuf/escbuf.go:22` operates on the live WS write path with a 16-byte tail-scan window. It does NOT touch what `outputLog.Append` stores. This means OutputLog entries can begin or end mid-OSC-52 sequence, validating critical issue #2.
- Daemon-package interface seams are absent today; small narrow interfaces are used in sibling packages.
- `cmd/schmux/main.go:93-130` confirms `start` and `daemon-run` are both valid daemon-launch commands and `start` calls `Start()` (which performs the line-217 `StartServer`), while `daemon-run` skips `Start()` and goes straight to `Run()` — confirming critical issue #1.
- `internal/remote/connection.go:393-518` (`Reconnect`) does not re-run the post-handshake block at lines 706-755 — confirming critical issue #3.
- `internal/session/outputlog.go:39-57` confirms one `LogEntry` per `Append` call; each event from the source is one chunk; there is no coalescing — confirming the chunk-boundary concern in critical issue #2.
