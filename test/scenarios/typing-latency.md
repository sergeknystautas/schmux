# Measure typing latency in the terminal

A user wants to verify that typing into an agent's terminal feels responsive.
Keystrokes travel through the full echo pipeline: browser xterm → WebSocket →
server → tmux → the agent process (cat) → tmux → server → WebSocket → xterm.
The round-trip latency for each keystroke is measured and must stay below an
acceptable threshold to catch catastrophic regressions.

Two conditions are tested: **idle**, where the agent is simply echoing input
with no other output, and **stressed**, where the agent is simultaneously
flooding stdout with continuous output while still echoing keystrokes.

The user navigates to a running session's terminal, types 50 characters, and
the latency tracker built into the dashboard records each echo round-trip. The
median latency must remain under 500ms in both conditions.

## Preconditions

- The daemon is running
- For the idle test: a promptable agent running `cat` (echoes stdin back)
- For the stressed test: a promptable agent running `cat` with a background
  process flooding stdout (`while true; do seq 1 100; sleep 0.01; done`)
- The echo pipeline is warmed up (WebSocket connected, agent echoing) before
  measurement begins
- The latency tracker is reset after warmup so warmup samples do not pollute
  the benchmark

## Verifications

- The session detail page shows the terminal viewport
- The echo pipeline becomes operational (warmup keys produce recorded samples)
- After typing 50 characters in the idle condition, the latency tracker has
  recorded samples and the median round-trip is under 500ms
- After typing 50 characters in the stressed condition (agent flooding stdout),
  the latency tracker has recorded samples and the median round-trip is under
  500ms
- Benchmark results (p50, p95, p99, max, mean) are logged for each condition
