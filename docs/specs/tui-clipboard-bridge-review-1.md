VERDICT: NEEDS_REVISION

## Summary Assessment

The overall two-part architecture (tmux passthrough + xterm.js OSC 52 handler) is sound and matches industry practice, but the spec has several concrete defects that will cause it to silently not work or to introduce real bugs as written: (a) `set-clipboard` and `terminal-features` are tmux **server**-scope options, not session options, so calling `s.SetOption(ctx, name, ...)` per session is the wrong scope; (b) for remote sessions, "sessions" are actually tmux **windows** inside a single shared session — the design hand-waves this; (c) the gap-replay path will re-fire historical OSC 52 sequences and silently overwrite the user's clipboard; (d) the security narrative is wrong — pastejacking via TUI output is a real, well-known attack class that the spec dismisses; (e) the spec references a "fake-cmd pattern" in `internal/tmux/tmux_test.go` that does not exist.

## Critical Issues (must fix)

### 1. Wrong tmux option scope — `set-clipboard` is a server option, not a session option

`internal/tmux/tmux.go:196` defines `SetOption` as `set-option -t <session> <opt> <val>`. The spec at lines 27-28 calls this for `set-clipboard` and `terminal-features`. Both are **server**-scoped options. Verified locally:

```
$ tmux show-options -s | grep clipboard
set-clipboard external
$ tmux show-options -s | grep terminal-features
terminal-features[0] xterm*:clipboard:ccolour:cstyle:focus:title
```

Calling `set-option -t <session> set-clipboard on` will either be rejected ("set-clipboard is not a session option" on modern tmux) or silently no-op. The spec's "graceful degradation" hand-wave will turn into "this never worked and we didn't notice." The fix is to add a server-scope variant — call `set-option -s set-clipboard on` and `set-option -s terminal-features '*:clipboard'` once at server startup (in `internal/daemon/daemon.go:217` next to `StartServer`), not per session in `CreateSession`. There is precedent: `internal/remote/connection.go:736` already calls `SetOption(ctx, "window-size", "manual")` once after connect for a global option.

The spec also needs a new `SetServerOption` (or `SetGlobalOption`) helper on `TmuxServer`, since `SetOption` validates a session name and routes to `-t`.

### 2. Remote sessions are tmux windows, not sessions — spec is hand-wavy

The spec at line 34 says "the same options must be set on the remote-side tmux ... wherever remote sessions are created (likely `internal/remote/connection.go` or `internal/session/manager.go` — verify during implementation)". Verified: remote "sessions" are tmux windows inside a single shared `schmux` session created at SSH connect time (see `internal/remote/README.md:14` and `internal/remote/connection.go:975` which delegates to `client.CreateWindow`). There is exactly one tmux server per remote host and exactly one session ("schmux") per server. The right place to set server-scope options is once, after the control-mode handshake completes — adjacent to `internal/remote/connection.go:736` (which already sets `window-size manual` and `setenv -g DISPLAY :99`). Setting them per `CreateWindow` is wasteful and confused about scope.

The spec should specify: "set `set-clipboard on` and `terminal-features '*:clipboard'` once via `controlmode.Client.SetOption` immediately after `setenv -g DISPLAY :99` at `internal/remote/connection.go:746`". The existing `controlmode.Client.SetOption` (`internal/remote/controlmode/client.go:571`) executes `set-option <opt> <val>` with no scope flag — that defaults to the _session_ scope and will fail for server options for the same reason as Critical Issue 1. A `SetServerOption` variant (or just a literal `set-option -s …` `Execute` call) is required there too.

### 3. Scrollback / gap-replay will silently overwrite the user's clipboard

The bootstrap path at `internal/dashboard/websocket.go:314` uses `tmux capture-pane -e -p -S -<n>` which captures rendered cell contents only, not OSC sequences — so initial reconnect bootstrap is safe. But the **gap-recovery** path at `internal/dashboard/websocket.go:98` (`buildGapReplayFrames`) replays raw bytes from the in-memory output log, including any OSC 52 a TUI emitted during the WS gap. When that replay reaches xterm.js, the new handler will fire and clobber the user's current clipboard with stale data. This is a real, user-observable bug: WiFi blip → clipboard silently overwritten with whatever was yanked 30s ago.

The spec at line 96 ("No clipboard ring / history. Just last-write-wins, matching native terminal behavior") doesn't address this: in a real terminal, the OSC 52 handler in the _outer_ terminal only sees writes once. In schmux with replay, it sees them twice. Mitigation options to specify:

- Strip OSC 52 sequences from the replay path before they reach the WS (server-side filter).
- Tag each OSC 52 with a recency timestamp and have the JS handler ignore writes older than ~2s past the WS reconnect event.
- Pick one and document it. The current "no opinion" stance ships a regression.

### 4. Pastejacking is a real threat — spec dismisses it incorrectly

The spec at lines 102-103 claims: "The user already trusts the TUI process they spawned, so propagating its clipboard writes is no privilege escalation." This is wrong. The threat is not privilege escalation; it's data integrity / phishing. Concrete attack scenarios that the design enables:

- `git log -p` of an attacker-controlled commit message containing `\e]52;c;cm0gLXJmIH4=\a` (base64 of `rm -rf ~`). Browser clipboard now silently contains the attacker payload. User pastes into another terminal expecting their last yank.
- `cat ./README.md` from a malicious cloned repo.
- `npm install` log output, `curl` of a hostile URL, `tail -f` on a log a network attacker can write to, viewing CI output, viewing a `man` page from a third-party package.
- Any AI agent (the entire point of schmux!) that reads attacker-controlled web content into its context — Claude Code, Codex, etc. — could be coaxed into emitting OSC 52 with attacker-chosen text via prompt injection.

This is the well-known "OSC 52 pastejacking" threat that landed in iTerm2 (CVE-2018-19038 lineage), and is why every modern terminal that supports OSC 52 either gates it behind a config opt-in or raises a per-write confirmation. The spec's "no toast" decision (line 92) plus "no per-session opt-in" (line 93) is the worst combination: invisible, ungated. At minimum the design must:

1. Make OSC 52 forwarding opt-in via config, default off, OR
2. Raise a one-line, dismissible toast on each write ("TUI copied 42 bytes to clipboard — Allow always / Block / X"), OR
3. Cap individual writes to a small size (e.g., ≤512 bytes, matching xterm's default), AND
4. Ignore writes that contain control characters / shell metacharacters silently — at least defang the obvious pastejacking payloads.

This is the single biggest issue and a hard blocker on shipping the feature as currently written.

### 5. The "fake-cmd pattern" the spec asks tests to use does not exist

Spec at line 108: "Add a test in `internal/tmux/tmux_test.go` asserting that `CreateSession` issues `set-option set-clipboard on` and `set-option terminal-features '*:clipboard'` (using the existing fake-cmd pattern)."

There is no fake-cmd pattern in `internal/tmux/tmux_test.go`. Existing tests (e.g., `TestTmuxServerCreateSessionArgs` at `internal/tmux/tmux_test.go:186`) only build an `*exec.Cmd` via the unexported `srv.cmd(...)` helper and inspect its `Args`. They do **not** intercept actual `exec.Command` invocations. With the current `CreateSession` (which runs a real `cmd.CombinedOutput()` at `internal/tmux/tmux.go:135`), there is no way to assert the _sequence_ of subcommands without one of: refactoring to inject a `commandRunner` interface, using `exec.LookPath`-style faking via the `os/exec` test helper pattern (re-exec `os.Args[0]` with an env flag), or moving to E2E coverage.

Fix the spec to either (a) prescribe the refactor and which dependency to inject, or (b) fall back to an E2E assertion in `internal/e2e/`.

### 6. `decodeURIComponent(escape(text))` uses a deprecated, removed-in-spec API

`escape()` is officially deprecated (MDN, Annex B of the JS spec — non-normative, browsers may remove). Replace with the modern equivalent that is also faster and safer:

```ts
const bin = atob(payload);
const bytes = new Uint8Array(bin.length);
for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
const text = new TextDecoder('utf-8', { fatal: false }).decode(bytes);
```

I verified locally that both paths produce identical output for valid UTF-8, but the deprecated `escape` trick throws `URIError: URI malformed` on invalid UTF-8 (e.g., a lone `0x80`), which the spec's `try/catch` at lines 60-62 would silently leave as a binary string — producing mojibake in the user's clipboard. `TextDecoder({fatal: false})` substitutes U+FFFD instead, which is the right behavior. Also: `escape` is an extra synchronous string scan; `TextDecoder` is one allocation + a native call.

## Suggestions (nice to have)

### 7. Unfocused-tab failure is more confusing than the spec admits

Spec line 94: "We accept this — the user is overwhelmingly looking at the dashboard at the moment they hit Yank." This is empirically wrong for the schmux use case: the user often hits "y" in nvim/lazygit _because_ they're about to switch to another window/app to paste. The dashboard tab loses focus immediately on the cmd-tab, and `navigator.clipboard.writeText` rejects with `NotAllowedError` exactly when the user wants it to succeed.

Better options to consider:

- Buffer the most recent OSC 52 payload and flush on the next `visibilitychange` to "visible" event (with a 5s TTL so we don't paste stale clipboard data on tab return hours later).
- Show a one-line, auto-dismissing toast in the dashboard ("TUI copied to clipboard. Click to allow.") and call `writeText` from the click handler — this satisfies the user-gesture requirement most browsers enforce.
- At minimum, log to console so users can diagnose "why isn't yank working".

### 8. Multi-tab / multi-window race

Spec line 101: "whichever tab is focused wins". If two tabs view the same session, both have terminals attached, both register OSC handlers, both try to call `writeText`. Only the focused tab will succeed — the others reject — but the design should explicitly say "background tabs reject silently and that's the correct behavior" rather than leave it implicit. Verify that the rejection from the non-focused tab does not produce an unhandled-promise warning in the console (the spec's `.catch(() => {})` should cover this, but the test list doesn't enumerate the multi-tab case).

### 9. tmux 3.2 `terminal-features` failure mode is more drastic than spec says

Spec line 99: "If a user has an older tmux, the `set-option terminal-features` call will fail; the warning is logged and clipboard forwarding silently doesn't work — graceful degradation." Verify that on tmux <3.2, `set-option set-clipboard on` _also_ needs the outer terminal to advertise `Ms` in terminfo; if the daemon's PTY inherits a TERM that lacks `Ms` (very likely, since the daemon runs in the background with whatever TERM the launcher had — possibly empty), then even with `set-clipboard on`, OSC 52 won't be forwarded. The fix is the `terminal-features '*:clipboard'` override (which is a tmux 3.2+ feature). The spec correctly notes 3.2 as the cutoff, but doesn't note that the daemon's TERM is undefined / inherited from `./schmux start`'s shell — if the user starts the daemon from a `screen` session or under launchd with no TERM, this matters. Recommend setting `TERM=xterm-256color` explicitly when forking the daemon at `internal/daemon/daemon.go:235`, and documenting the requirement.

### 10. OSC 52 selection char (`Pc`) is parsed too loosely

Spec lines 47-50:

```ts
const semi = data.indexOf(';');
if (semi < 0) return false;
const payload = data.slice(semi + 1);
```

This blindly trusts that the first `;` separates `Pc` from `Pd`. But `Pc` can be `c`, `p`, `s`, `q`, `b`, `0..7`, or any combination — and the spec says "empty means c+s". The handler doesn't validate `Pc` at all; if a malformed payload contains a `;` inside what's supposed to be base64 (some implementations), it'll happily pass garbage to `atob`. The fix: validate `data.slice(0, semi)` matches `/^[cpsqb0-7]*$/` and reject otherwise. Also: the spec ignores selection ('s' for primary X11 selection vs 'c' for clipboard) — for a browser, both should map to the same destination, but the design should say that explicitly.

Also consider: a malicious sender could send OSC 52 with `Pc='q'` (query) in some interpretations; treat unrecognized Pc values as a no-op rather than letting them through.

### 11. Spec doesn't say where to dispose the OSC handler

Spec line 70: "store it on the instance and call `.dispose()` in the existing teardown path next to where other listeners are released." There are at least three teardown paths in `terminalStream.ts` (the `dispose` flow, the WebSocket reconnect-recreate flow at `recreationCount`, and the visibility-change cleanup). The spec should be explicit, since registering twice on a re-created Terminal would produce two `writeText` calls per OSC 52.

### 12. No mention of large payload denial-of-service

Spec line 102 says "OSC 52 in the wild is usually <8 KB. xterm.js's parser handles arbitrarily long OSC payloads via its existing chunking. We do not add a size cap." The xterm.js typings at `assets/dashboard/node_modules/@xterm/xterm/typings/xterm.d.ts:1856-1857` document a 10 MB cap on OSC payloads. A hostile TUI can yank 10 MB to the user's clipboard, which is annoying and on some OSes can hang Finder/explorer. Pair this with Issue 4 — cap individual writes at e.g. 64 KB and reject larger payloads with a console warning.

### 13. Replace `navigator.clipboard?.writeText` optional chain with explicit feature detection

If `navigator.clipboard` is unavailable (e.g., insecure context, very old browser), `?.` returns undefined and the OSC payload is silently dropped with no diagnostic. Add a one-time `console.warn` so users can diagnose why yank doesn't work in their browser.

## Verified Claims

These claims in the spec checked out against the codebase:

- `terminal.parser.registerOscHandler(52, ...)` exists in the vendored `@xterm/xterm@6.0.0` (`assets/dashboard/node_modules/@xterm/xterm/typings/xterm.d.ts:1864`).
- `allowProposedApi: true` is already set on the `Terminal` constructor (`assets/dashboard/src/lib/terminalStream.ts:339`), satisfying the requirement for the experimental parser API.
- The `SetOption` method exists on both `*tmux.TmuxServer` (`internal/tmux/tmux.go:196`) and `*controlmode.Client` (`internal/remote/controlmode/client.go:571`), so the wiring points the spec gestures at do exist — but with the scope problem in Critical Issue 1/2.
- The `history-limit` warning-on-fail pattern at `internal/tmux/tmux.go:140-144` is the right precedent for non-fatal `set-option` calls; `CreateSession` does not bail when it fails.
- Browser→TUI image paste is fully implemented in `internal/dashboard/clipboard.go` and `assets/dashboard/src/lib/terminalStream.ts:449-478`, confirming the spec's "out of scope for the reverse direction" framing.
- The xterm.js OSC handler runs on every `terminal.write()` call, so it will fire for both live and replayed bytes — confirming that the gap-replay concern in Critical Issue 3 is real, not theoretical.
- `bootstrap` uses `capture-pane -e -p -S -<n>` (`internal/dashboard/websocket.go:314` + `internal/tmux/tmux.go:296-302`) which renders cell contents only — confirming that initial WS reconnect does NOT re-fire OSC 52 (only the gap-replay path does).
- The `onTitleChange` (`assets/dashboard/src/lib/terminalStream.ts:388`) is the right neighbor for the new handler registration.
