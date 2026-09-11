# Typed characters echo back through the terminal

A user types into an agent's terminal and the characters must come back: each
keystroke travels the full echo pipeline — browser xterm → WebSocket → server
→ tmux → the agent process (`cat`) → tmux → server → WebSocket → xterm — and
the typed characters render in the terminal.

Two conditions are verified: **idle**, where the agent echoes input with no
other output, and **stressed**, where the agent simultaneously floods stdout
while still echoing keystrokes.

The user navigates to a running session's terminal, waits until a warmup
keystroke's echo has rendered, then types a run-unique letters-only marker
string. Every marker character must render in the terminal, in order. Under
the flood condition the echoed characters interleave with flood output, so
order — not contiguity — is the assertion.

## Preconditions

- The daemon is running
- For the idle condition: a promptable agent running `cat` (echoes stdin back)
- For the stressed condition: a promptable agent running `cat` with a
  background process flooding stdout (`while true; do seq 1 20; sleep 0.05; done`)
- The echo pipeline is operational before the marker is typed: a warmup
  keystroke's echo has rendered

## Verifications

- The session detail page shows the terminal viewport
- A warmup keystroke's echo renders in the terminal (pipeline operational)
- After typing a unique letters-only marker, every marker character renders
  in the terminal buffer, in order, in both idle and stressed conditions

## Performance objective

Typing should feel responsive: median keystroke round-trip latency under
**500 ms** in both conditions. This is a product objective, not a CI
assertion. It is measured by the manual benchmark — `./test.sh --bench`,
spec `test/scenarios/generated/typing-latency.bench.spec.ts`, docker-scenario
profile — which correlates each browser keydown to a unique numbered agent
acknowledgement and the corresponding xterm render-settled event. Results and
environment metadata land in `bench-results/<date>/`.
Shared-runner timing never passes or fails a PR (docs/testing.md rule 8).
