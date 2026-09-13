# Typed characters echo back through the terminal

A user types into an agent's terminal and the characters must come back: each
keystroke travels the full echo pipeline — browser xterm → WebSocket → server
→ tmux → the agent process (`cat`) → tmux → server → WebSocket → xterm — and
the typed characters render in the terminal.

Two conditions are verified: **idle**, where the agent echoes input with no
other output, and **stressed**, where the agent simultaneously floods stdout
while still echoing keystrokes.

The user navigates to a running session's terminal, waits until the agent's
`READY` banner has rendered (the terminal socket is open and the agent is
running), types one run-unique warm-up string and waits once for its echo,
then types a run-unique letters-only marker string. Every marker character
must render in the terminal, in order, after the warm-up. Under the flood
condition the echoed characters interleave with flood output, so order — not
contiguity — is the assertion. No input is retried: if the warm-up echo does
not render by its deadline the scenario fails, naming the warm-up and
reporting the last observed terminal buffer.

## Preconditions

- The daemon is running
- For the idle condition: a promptable agent running `cat` (echoes stdin back)
- For the stressed condition: a promptable agent running `cat` with a
  background process flooding stdout (`while true; do seq 1 20; sleep 0.05; done`)
- The echo pipeline is operational before the marker is typed: the agent's
  `READY` banner and one unique warm-up string's echo have rendered

## Verifications

- The session detail page shows the terminal viewport
- The agent's `READY` banner renders in the terminal (socket open, agent up)
- One unique warm-up string's echo renders in the terminal (pipeline
  operational), awaited once with a deadline
- After typing a unique letters-only marker, every marker character renders
  in the terminal buffer, in order after the warm-up, in both idle and
  stressed conditions

## Performance objective

Typing should feel responsive: median keystroke round-trip latency under
**500 ms** in both conditions. This is a product objective, not a CI
assertion — this scenario asserts echo content only, and shared-runner timing
never passes or fails a PR (docs/testing.md rule 8). Native PTY/WebSocket
latency percentiles are available from the manual `./test.sh --bench` run.
