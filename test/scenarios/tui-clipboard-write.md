# TUI clipboard write (OSC 52)

A user is working in a TUI inside a schmux session. The TUI emits an OSC 52
"set clipboard" escape sequence (e.g. nvim yank with `clipboard=unnamedplus`,
or `tmux copy-mode` Enter). The schmux daemon strips the OSC 52 from the
byte stream, debounces and broadcasts a `clipboardRequest` event over
`/ws/dashboard`. The session detail page renders an approve/reject banner
showing a sanitized preview of the proposed clipboard contents. When the
user clicks **Approve**, the frontend writes the text to the browser's
system clipboard via `navigator.clipboard.writeText` and POSTs an ack to
`/api/sessions/{id}/clipboard`, which clears the pending entry and
broadcasts `clipboardCleared`.

## Preconditions

- The daemon is running with `set-clipboard external` and
  `terminal-features '*:clipboard'` applied at server scope (set by
  `tmux.ApplyTmuxServerDefaults` early in `daemon.Run()`).
- A local session is spawned with a shell-like agent capable of running
  `printf` to emit raw OSC 52 bytes.
- The Chromium test context grants `clipboard-read` and `clipboard-write`
  origin permissions for the daemon's URL.

## Verifications

### Approve path

- Spawn a session, navigate to its detail page, wait for the dashboard
  WebSocket to be live.
- `tmux send-keys` a `printf '\\033]52;c;%s\\007' "$(printf
hello-clipboard-test | base64)"` into the pane — this prints raw OSC 52
  bytes to the pane's stdout.
- Within 5 seconds the page renders an element with `role="alert"`
  containing the literal text `hello-clipboard-test`.
- Click the **Approve** button.
- `navigator.clipboard.readText()` returns exactly `hello-clipboard-test`.
- The banner disappears (the `clipboardCleared` broadcast removes it).

### Reject path

- Repeat the OSC 52 send with a fresh payload.
- Wait for the banner.
- Click the **Reject** button.
- The banner disappears.
- `navigator.clipboard.readText()` is unchanged from before the reject
  click (the previous approved value).

### tmux paste-buffer fallback (load-buffer / set-buffer)

Some TUIs detect tmux control mode and bypass OSC 52 entirely — they
call `tmux load-buffer -` (or `set-buffer`) so the clipboard text never
enters the byte stream. The daemon listens for `%paste-buffer-changed`
on the control-mode pipe (and the legacy `%paste-changed` from
tmux 3.3a/earlier), fetches the buffer content with `show-buffer`,
defangs it with the same byte-level rules as the OSC 52 path, and
surfaces the same approve/reject banner.

- Spawn a session, navigate to its detail page, wait for the dashboard
  WebSocket to be live.
- From the test, drive the daemon's tmux socket directly:
  `tmux -L <socket> set-buffer -b clip-test 'hello-from-load-buffer'`.
  This writes to tmux's internal paste buffer without typing anything in
  the pane, so the OSC 52 byte path is never exercised — only the
  `%paste-buffer-changed` notification fires.
- Within 5 seconds the page renders an element with `role="alert"`
  containing the literal text `hello-from-load-buffer`.
- Click the **Approve** button.
- `navigator.clipboard.readText()` returns exactly
  `hello-from-load-buffer`.
- The banner disappears.

### Input-echo suppression (positive)

Premise: when a process inside the pane emits OSC 52 with content that
matches bytes the dashboard recently sent into the pane via
`SessionRuntime.SendInput`, the daemon suppresses the banner. This
catches the Claude-Code argv-prompt round-trip — the user types
`claude "MARKER"` in the dashboard, schmux send-keys those bytes into
the pane, Claude reads its own argv and emits OSC 52 with the same
payload. The OSC 52 is internal plumbing, not a real copy.

Suppression rules (see `internal/session/inputecho.go`):

- 5 s window from the time the bytes were sent.
- 8-byte minimum payload (short payloads might match by accident).
- Substring match against any single `SendInput` chunk.

Verification:

- Spawn a shell session, navigate to the session detail page.
- Open a `/ws/terminal/{id}` WebSocket and send a JSON `{type:"input",
data:"printf '\\033]52;c;%s\\007' \"$(printf 'MARKER-...' | base64)\"\n"}`.
  Critical: this MUST go through the WebSocket (not `tmux send-keys`),
  because only WS input is recorded in the daemon's per-session echo
  buffer.
- The literal `MARKER-...` string is in the WS-sent input bytes AND the
  shell will base64-decode it back as the OSC 52 payload. Substring
  match → daemon suppresses → no clipboard event broadcast.
- Within 1.5 s (well past the 200 ms debounce window), assert that no
  `role="alert"` element with the marker text exists. Belt-and-suspenders:
  assert no `role="alert"` element exists at all (catches a regression
  that would mangle the payload but still fire the banner).

### Input-echo suppression (negative control)

Prove suppression is targeted, not a blanket "swallow every OSC 52 from
inside the pane". The OSC 52 payload string must NEVER appear in any
WS-typed input bytes — otherwise the substring match would fire and
this would degenerate into the positive case.

Strategy:

- Stage a shell script on disk (via Node `fs.writeFileSync`, NOT through
  the pane). The script body contains `printf '\\033]52;c;<base64>\\007'`
  with `<base64>` being the encoded payload.
- Open a `/ws/terminal/{id}` WebSocket and send `bash <script-path>\n`.
  The recorded echo bytes are only the `bash` invocation — the payload
  string is read from disk by the shell, never typed.
- Banner SHOULD appear with the payload (the OSC 52 content is novel,
  no echo match). If it doesn't, suppression is over-firing.
- Click **Reject** to clean up the pending state for subsequent tests.

### Input-echo time-window expiry (covered by unit test)

Content older than the 5 s `inputEchoWindow` is NOT suppressed, even if
it would otherwise match. This is verified by
`TestFanOut_DoesNotSuppressWhenInputIsTooOld` in
`internal/session/tracker_test.go`. We don't duplicate it at the
scenario level: the only honest way to exercise the real 5 s timeout
is to sleep 6 s, which costs CI time without adding coverage the unit
test doesn't already provide. Overriding the window via an env var
would prove the override works, not the production timeout.

### Spawn-prompt suppression (covered by unit tests)

A stronger, structurally-grounded version of the input-echo check,
scoped at the workspace (daemon) level rather than per-session.
The dashboard's spawn handler
(`internal/dashboard/handlers_spawn.go`) calls
`clipboardState.RegisterSpawnPrompt(req.Prompt)` once per spawn
request, recording the prompt verbatim in a workspace-scoped
registry. Before broadcasting a `clipboardRequest`,
`clipboardState.onRequest` exact-matches the candidate text against
the registry; on match the request is dropped silently and a
separate prompt-suppression counter ticks. This catches cases the
ring-buffer heuristic misses (short prompts under the 8-byte minLen
floor, prompts assembled from per-keystroke chunks) AND fires for
every session that receives tmux's server-scoped
`%paste-buffer-changed` notification — not just the source session.
The latter matters because cross-session content+timestamp dedup
would otherwise collapse the duplicate broadcasts onto an arbitrary
session that did nothing.

Verified at the unit level by
`TestClipboardState_RegisterSpawnPrompt_SuppressesMatching`,
`TestClipboardState_RegisterSpawnPrompt_SuppressesAcrossSessions`,
`TestClipboardState_RegisterSpawnPrompt_ExpiresAfterTTL`,
`TestClipboardState_RegisterSpawnPrompt_DoesNotSuppressNonMatching`,
`TestClipboardState_RegisterSpawnPrompt_EmptyIsNoop`, and
`TestClipboardState_RegisterSpawnPrompt_IdempotentRefreshesTTL` in
`internal/dashboard/clipboard_state_test.go`. We don't duplicate at
the scenario level because the existing scenario fixture
(`shell-agent`) is a non-promptable run target, and the spawn API
explicitly rejects a prompt on such targets (`"prompt is not allowed
for command targets"`, `internal/dashboard/handlers_spawn.go`).
Exercising the real spawn flow would require either standing up a
promptable model (which needs a real agent binary on the test host)
or adding a test-only backdoor endpoint to inject the prompt —
neither of which adds coverage the unit tests don't already provide.
