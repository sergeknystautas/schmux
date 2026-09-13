# Typed input is acknowledged back through the terminal

A user types a line into an agent's terminal and the agent's acknowledgement
of exactly that line must come back: the input travels browser xterm →
WebSocket → server → tmux → the agent's stdin, and the agent's reply travels
agent stdout → tmux → server → WebSocket → xterm, rendering in the terminal.

Two conditions are verified: **idle**, where the agent produces no output
other than acknowledgements, and **stressed**, where the agent simultaneously
floods stdout while still acknowledging input.

The agent acknowledges every line it reads as `ACK<line>KCA`, written in one
call so the frame lands contiguously on a single terminal line even under the
flood. Echoed keystrokes can never produce the frame (the user never types
`ACK<`), and a dropped or altered character changes the frame, so a contiguous
match proves the agent received exactly what was typed.

The user navigates to a running session's terminal, waits until the agent's
`READY` banner has rendered (the terminal socket is open and the agent is
running), types one run-unique warm-up nonce plus Enter and waits once for its
exact acknowledgement frame, then types a run-unique nonce plus Enter. That
nonce's exact acknowledgement frame must render contiguously on one terminal
line. No input is retried: if either acknowledgement does not render by its
deadline the scenario fails, naming the expected frame and reporting the last
observed terminal buffer.

## Preconditions

- The daemon is running
- For the idle condition: a promptable agent that prints `READY`, then reads
  stdin line by line and prints `ACK<line>KCA` for each line
- For the stressed condition: the same agent with a background process
  flooding stdout (`while true; do seq 1 20; sleep 0.05; done`)
- The round-trip pipeline is operational before the measured nonce is typed:
  the agent's `READY` banner and one unique warm-up nonce's acknowledgement
  frame have rendered

## Verifications

- The session detail page shows the terminal viewport
- The agent's `READY` banner renders in the terminal (socket open, agent up)
- One unique warm-up nonce's exact `ACK<nonce>KCA` frame renders contiguously
  on one terminal line (round trip operational), awaited once with a deadline
- After typing a unique nonce plus Enter, its exact `ACK<nonce>KCA` frame
  renders contiguously on one terminal line, in both idle and stressed
  conditions

## Performance objective

Typing should feel responsive: median keystroke round-trip latency under
**500 ms** in both conditions. This is a product objective, not a CI
assertion — this scenario asserts acknowledgement content only, and
shared-runner timing never passes or fails a PR (docs/testing.md rule 8).
Native PTY/WebSocket latency percentiles are available from the manual
`./test.sh --bench` run.
