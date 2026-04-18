# Bug: Pasting a line beginning with `-` truncates the paste

**Branch:** `bug/dash-paste`
**Scope:** tmux control-mode literal `send-keys` codepath
**Reviewer requested:** someone with tmux control-mode expertise, please sanity-check the diagnosis and the `--` fix before merge.

## Summary

When a user pastes multi-line text into a schmux terminal and any line begins with `-` (ASCII hyphen-minus), everything from that line onward is silently dropped. The earlier lines arrive; the `-…` line and everything after it do not.

Triggered consistently by pasting text like:

```
hello
-world
goodbye
```

Only `hello\n` reaches the pane. `-world` and `goodbye` are lost.

## Where it lives

`internal/remote/controlmode/client.go:489`

```go
} else if run.Literal {
    cmd = fmt.Sprintf("send-keys -t %s -l %s", paneID, shellutil.Quote(run.Text))
}
```

The command `SendKeys` sends to tmux via control mode, for a literal run whose text starts with `-`, is:

```
send-keys -t %N -l '-world'
```

tmux's control-mode command parser rejects that with:

```
parse error: command send-keys: unknown flag -w
```

`SendKeys` returns on the first error (client.go:494-497), so every subsequent run — including the rest of the paste — is dropped.

## Diagnosis walk-through

### 1. How paste reaches tmux

Browser paste → `TerminalStream.sendInput` (`assets/dashboard/src/lib/terminalStream.ts`) → WebSocket → dashboard input loop → `SessionRuntime.SendInput` (`internal/session/tracker.go:249`) → `ControlSource.SendKeys` → `controlmode.Client.SendKeys`.

`Client.SendKeys` (`internal/remote/controlmode/client.go:480`) classifies the input into runs using `ClassifyKeyRuns` (`internal/remote/controlmode/keyclassify.go:43`). Newlines are classified as the tmux key name `Enter` (keyclassify.go:90), which splits a multi-line paste into alternating runs:

For a paste of `hello\n-world\ngoodbye` the runs are:

1. `{Text:"hello",   Literal:true}`
2. `{Text:"Enter",   Literal:false}`
3. `{Text:"-world",  Literal:true}`
4. `{Text:"Enter",   Literal:false}`
5. `{Text:"goodbye", Literal:true}`

Each run is dispatched as a separate `send-keys` command.

### 2. What tmux does with `-l '-world'`

Empirical test against tmux 3.6a control mode (exactly the command shape the client builds):

```
send-keys -t %0 -l '-bar hello'
```

Response from tmux `-C`:

```
%begin 1776534753 284 1
parse error: command send-keys: unknown flag -b
%error 1776534753 284 1
```

Even though the text is single-quoted, tmux's command parser strips the quotes, then passes `-bar hello` to the `send-keys` argv parser, which reads `-bar` as a combined short-flag cluster (`-b`, `-a`, `-r`). `-b` is not a `send-keys` flag, so parsing fails and the command is never executed.

### 3. Why the whole paste ends, not just that line

`Client.SendKeys` stops the loop on the first error (client.go:494-497):

```go
_, mutexWait, err := c.Execute(ctx, cmd)
if err != nil {
    return timings, err
}
```

Once `send-keys -l '-world'` returns an error, the two following runs (`Enter` and `goodbye`) are never sent. From the user's perspective the paste simply stops at the first line starting with `-`.

### 4. Why `--` fixes it

tmux honors POSIX `--` as an end-of-options marker in its command parser. With it, subsequent tokens are always treated as positional args regardless of their leading character.

Empirical test — same tmux 3.6a control mode, command changed to:

```
send-keys -t %0 -l -- '-bar hello'
```

Response:

```
%begin ... %end ...
%output %0 -bar hello\015\012
```

The literal text `-bar hello` is typed into the pane as intended.

## Simple repro (no browser needed)

Runs against plain tmux; reproduces the control-mode behavior that schmux triggers.

```bash
# Clean slate, isolated tmux socket.
tmux -L schmux-dashrepro kill-server 2>/dev/null

# Start a session whose shell just captures what gets typed at it.
tmux -L schmux-dashrepro new-session -d -s test -x 80 -y 24 \
  'cat > /tmp/schmux-dashrepro.txt; sleep 10'
sleep 0.3
PANE=$(tmux -L schmux-dashrepro list-panes -t test -F '#{pane_id}')

# Issue the exact shape of command that controlmode.Client.SendKeys builds
# today for a paste "hello\n-world\ngoodbye".
cat > /tmp/schmux-dashrepro-cmds.txt <<EOF
send-keys -t $PANE -l 'hello'
send-keys -t $PANE Enter
send-keys -t $PANE -l '-world'
send-keys -t $PANE Enter
send-keys -t $PANE -l 'goodbye'
send-keys -t $PANE Enter
EOF

( cat /tmp/schmux-dashrepro-cmds.txt; sleep 0.5 ) \
  | tmux -L schmux-dashrepro -C attach -t test

sleep 0.3
tmux -L schmux-dashrepro send-keys -t test 'C-d'
sleep 0.3

echo '=== what the pane actually received ==='
cat /tmp/schmux-dashrepro.txt
echo '=== end ==='

tmux -L schmux-dashrepro kill-server 2>/dev/null
```

**Observed (bug):**

```
parse error: command send-keys: unknown flag -w
=== what the pane actually received ===
hello
=== end ===
```

`-world` and `goodbye` never make it.

**After the fix** (change each `-l '…'` to `-l -- '…'`) the same script produces:

```
=== what the pane actually received ===
hello
-world
goodbye
=== end ===
```

## The fix

`internal/remote/controlmode/client.go:489`

```diff
-        cmd = fmt.Sprintf("send-keys -t %s -l %s", paneID, shellutil.Quote(run.Text))
+        cmd = fmt.Sprintf("send-keys -t %s -l -- %s", paneID, shellutil.Quote(run.Text))
```

That is the entire behavior change. `--` tells tmux's command parser to stop looking for option flags, so `shellutil.Quote(run.Text)` is always treated as the positional key argument regardless of whether it starts with `-`.

### Why this placement (Design Placement Rule)

- **Layer:** The bug is in how we construct the tmux command string, not in classification, not in the browser. `Client.SendKeys` owns the `send-keys` command shape; the fix belongs there.
- **Pattern:** `--` is the standard POSIX end-of-options terminator. tmux supports it. This is not a new pattern; it is applying the standard pattern we currently omit.
- **5× test:** Every literal run we emit benefits — pasting diffs, markdown bullet lists, CLI examples with flags, etc. No one-off special-casing.
- **Symptom vs cause:** The symptom is "paste truncates at `-`." The cause is "our literal `send-keys` command is ambiguous to tmux's arg parser." We're fixing the cause.

### What the fix does _not_ touch

- `client.go:487` (`-H` hex run) — run.Text is space-separated hex pairs (`toHexBytes`, keyclassify.go:190). They never begin with `-`.
- `client.go:491` (non-literal run) — run.Text is a tmux key name (`Enter`, `Up`, `M-Enter`, `C-a`, …). None begin with `-`.
- `client.go:789` (`RunCommand`'s `send-keys -l`) — this sends a constructed shell command string. It is a plausible secondary site for the same class of bug, but it is not on the paste path and is not what the reported bug is about. Flagged for a follow-up review; out of scope for this change.

## Verification

New test: `TestClientSendKeys_LiteralLeadingDashUsesDashDash` in `internal/remote/controlmode/client_test.go`.

- Calls `client.SendKeys(ctx, "%1", "-bar")`.
- Asserts the bytes written to the control-mode stdin contain `send-keys -t %1 -l -- '-bar'`.
- Current behavior: absent (test fails).
- After fix: present (test passes).

Plus `./test.sh` for the full suite.

## Questions for the tmux-control-mode reviewer

1. Is `--` the right terminator for tmux's command-parser-level argv, not just for getopt inside individual commands? Empirically it works in 3.6a; I'd like confirmation it's portable back through the tmux versions schmux officially supports.
2. Are there any `send-keys` flag combinations where inserting `--` between `-l` and the key argument changes semantics (e.g., interaction with `-H`, `-K`, `-R`, `-X`)? My read of the man page says no — `-l` is a bool — but a second pair of eyes would be welcome.
3. Is there a reason we wouldn't also want to apply `--` to the non-literal branch at client.go:491? Key names like `Enter` can't start with `-`, so it's defensive rather than required; I left it out to keep the diff minimal.
4. Same question for `RunCommand` at client.go:789 — `fullCmd` is constructed from user/agent input and could plausibly start with `-`. Worth a separate ticket?
