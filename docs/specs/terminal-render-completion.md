# Terminal Render Completion and Single-Shot Assertions

**Status:** implemented
**Parent:** Task 4A of `docs/plans/2026-09-10-test-reliability-practices.md` (the test-reliability plan currently held in the schmux-003 worktree).
**Rubric:** `docs/testing.md` example 6 — "Await 'render settled' (a named marker parsed and no pending write/render work), then capture tmux and xterm once, compare once."

## Problem

The terminal scenario helpers synchronize by retrying correctness assertions until they pass or a retry budget runs out. Four loops do this:

- `waitForSentinel` (`test/scenarios/generated/helpers-terminal.ts:340-426`) polls the xterm.js buffer every 100 ms until the sentinel appears.
- `assertTerminalMatchesTmux` (`helpers-terminal.ts:155-325`) captures tmux and xterm, compares, and retries up to 50 times × 200 ms on mismatch.
- `assertCursorMatchesTmux` (`helpers-terminal.ts:638-718`) and `assertCursorVisibilityMatchesTmux` (`helpers-terminal.ts:757-783`) retry cursor equality the same way.

A transiently wrong terminal therefore **passes as soon as it converges**. The retry cannot detect a render race that settles late within 10 seconds — exactly the class of bug the terminal-fidelity suite exists to catch. A genuinely stable mismatch and a still-converging screen are indistinguishable until the budget expires, which also makes failures slow and their diagnostics (convergence logs, first-vs-last captures) a description of the masking rather than of the race.

The sentinel itself is also not a completion boundary. `sendTmuxCommandWithSentinel` (`helpers-terminal.ts:56-61`) types `echo '__FIDELITY_N__'` into an interactive PTY. The line discipline echoes characters on receipt, so the echoed command line — which contains the sentinel — can render while the _preceding_ command is still producing output, and the echo's output line in any case precedes the next prompt redraw. Both races are currently absorbed by the same retry loops. Removing the retries without fixing the boundary would make every assertion compare against a moving pane.

`openTerminal` compounds this:

- It mutates `TerminalStream` private fields directly (`helpers-terminal.ts:479-488`) — clearing `writeBuffer`, `writeRAFPending`, `pendingWriteCb` without cancelling the armed rAF callback, which still fires and calls `writeTerminal('')` against the freshly reset terminal.
- It polls for content, for emptiness after reset, and for prompt redraw (`helpers-terminal.ts:456-470`, `491-505`, `512-527`).
- It stabilizes terminal size with a 2-consecutive-match loop plus a 400 ms post-debounce sleep (`helpers-terminal.ts:539-591`), guessing past the frontend's 300 ms resize debounce instead of observing it.

These are the shapes the test rubric prohibits (rules 1, 2, 7) and the shapes the parent plan's gap 6 names for this file.

## Spike result

The riskiest unknown was whether xterm.js exposes an exact "data parsed" signal, or whether settle detection would have to lean on timeout heuristics. Reading the installed `@xterm/xterm` 6.0.0 source (extracted from the shipped source map, `src/common/input/WriteBuffer.ts`):

1. **`write(data, cb)` callbacks are an ordered parse barrier.** `_innerWrite` calls the parser for a chunk (`_action(data)`) and then invokes that chunk's callback, FIFO, before moving on. A callback therefore fires only after that write's data — and everything queued before it — has been parsed and is reflected in `buffer`. The public typings state the same contract ("This callback must be provided and awaited in order for {@link buffer} to reflect the change in the write").
2. **`onWriteParsed` fires at the end of every `_innerWrite` round**, including intermediate rounds while large writes are still chunked across `setTimeout`s. It is a re-evaluation point, not a settle signal.
3. The comment at `terminalStream.ts:926-933` ("our write callback fires after the first chunk, but subsequent chunks continue to fire scroll events") conflates parsing with viewport sync: the scroll events that outlive the callback come from the deferred `_sync` in `_patchViewportSync` (Fix 3), not from un-parsed data.
4. **Nothing in `WriteBuffer` clears the queued-chunk state on `reset()`** — data already submitted via `terminal.write()` but not yet parsed survives a terminal reset and lands in the fresh buffer. (Drain-before-reset makes this moot regardless of xterm internals; the ordered barrier from point 1 is the drain primitive.)

Consequence: an exact, event-driven completion signal exists. The 8 ms `writeGuardTimer` debounce is not needed as a correctness crutch — it remains part of the predicate only because it is the stream's own write-quiescence notion ("re-arming keeps the guard up until the last chunk finishes").

## Goals

- Terminal scenario helpers synchronize on a semantic completion event with one deadline, never on retry loops or guessed elapsed time.
- The completion marker is emitted at a true quiescent point — the post-command prompt — and cannot match echoed input, so single-shot comparison is sound.
- Every behavioral assertion compares exactly once, after a caller-supplied semantic boundary that the helper enforces structurally.
- `openTerminal` uses narrow test-owned operations instead of private-field mutation, and observes the resize debounce instead of sleeping past it.
- Failure telemetry improves: boundary timestamps, both captures, stream state, sequence numbers, and the awaited marker, preserved in `test/scenarios/artifacts/` via the existing `entrypoint.sh` copy.
- No assertion is weakened. The fidelity, cursor, scrollback, and ordering claims the suite makes today keep their full strength.

## Non-goals

- **Backend changes.** No new control messages (e.g., a resize ack). The backend→tmux resize boundary stays a bounded probe; if a seam is ever wanted, that is Task 4B's territory.
- **Task 5's scope.** `typing-latency.spec.ts` and performance thresholds are handled there; this spec's completion event must not become a latency gate.
- **Task 4B's scope.** `helpers.ts` session readiness, `waitForDashboardLive`, rate-limiter clocks, and the clipboard/remote-host `waitForTimeout` calls are not touched here.
- **Mechanical sleeps elsewhere.** Nothing outside the files listed below changes.

## Marker generation (boundary semantics)

`sendTmuxCommandWithSentinel(session, command)` becomes a true boundary generator:

1. Send `command`.
2. Send a `PS1` assignment whose value contains a fresh unique marker (e.g. `__FIDELITY_N__` plus trailing space), replacing the previous PS1 wholesale so prompts stay short and deterministic for comparison. The assignment is sent with an inline quote-split (or equivalent encoding) so the **echoed command line never contains the contiguous marker string** — only the rendered prompt does.
3. Return the marker.

The marker's appearance in a drawn prompt proves: the preceding command finished, the PS1 assignment executed, and the shell redrew the prompt — a point at which the shell emits nothing further until the next input. That is the quiescent state single-shot comparison requires. Prompt and marker appear identically in tmux's capture and xterm's buffer, so the compared claim is unaffected. Tests own their session's shell state (rubric rule 9); the mechanism is verified against the scenario container's shell in the first implementation batch.

## API design

Methods on `TerminalStream`, inert unless the existing exposure gate (`import.meta.env.DEV || import.meta.env.VITE_EXPOSE_TERMINAL`, `terminalStream.ts:590-597`) is active. In ungated builds the watcher infrastructure is never initialized and the methods resolve `{ ok: false, reason: 'not-exposed' }` immediately. Hook-point checks are guarded by "any waiter registered?" so the production hot path pays nothing.

```ts
type RenderSettleCondition =
  | { marker: string }            // marker string parsed into the terminal buffer
  | { bootstrapComplete: true }   // the bootstrapComplete control message was seen
  | { resizeApplied: true };      // resize debounce drained, resize applied + sent

type RenderSettleResult =
  | {
      ok: true;
      markerSeenAt?: number;      // performance.now() when the marker was found
      settledAt: number;          // performance.now() when the predicate held
      lastSeq: string;            // stream's lastReceivedSeq at settle (decimal string)
    }
  | {
      ok: false;
      timedOut?: boolean;
      reason?: string;            // 'not-exposed' | 'disposed' | 'reset' | ...
      diagnostics: RenderSettleDiagnostics;
    };

waitForRenderSettled(
  condition: RenderSettleCondition,
  opts?: { timeoutMs?: number }   // default 15_000; failure backstop only
): Promise<RenderSettleResult>;

resetAndSettle(): Promise<RenderSettleResult>;
```

Contract rules:

- **Never rejects.** The result object serializes cleanly through `page.evaluate`; the Node-side helpers own failure semantics and throw. This keeps the deadline a failure backstop (rubric rule 2) at the layer that reports it.
- **No idle variant, by design.** "Stream currently quiet" proves nothing about future output; every condition names a semantic event. `bootstrapComplete` conditions on the existing control-message state (`terminalStream.ts:1596-1599`, reset per connection in `connect()`).
- **Immediate evaluation on registration** so an already-satisfied condition resolves without waiting for a new event. This makes re-awaits of the same marker idempotent and free.
- **`resetAndSettle()` drains first, then resets.** Step 1: queue a barrier write (`terminal.write('', cb)`) and wait for its callback — by the ordered-parse-barrier property, all previously submitted data has then been parsed, so nothing survives in xterm's queue. Step 2: cancel the app's pending write-flush rAF with `cancelAnimationFrame` (fixing the armed-rAF leak), clear `writeBuffer`/`pendingWriteCb`, call `terminal.reset()`, void outstanding waiters with `{ ok: false, reason: 'reset' }`. Step 3: resolve once settled. Same default deadline (15 s) as a failure backstop, so a wedged reset surfaces as `{ ok: false, timedOut: true }` with diagnostics rather than a hang.
- **Disposal safety.** `disconnect()` voids all waiters (`reason: 'disposed'`) so no in-page promise leaks across navigations.
- `Window.__schmuxStream` typing in `vite-env.d.ts` picks the methods up automatically if it types the stream as `TerminalStream`; otherwise extend it there.

## Settle predicate and event wiring

**Predicate** — resolve when the condition holds AND the pipeline is clean:

```
writeBuffer === '' && !writeRAFPending && pendingWriteCb === null
&& !writingToTerminal && writeGuardTimer === null && !scrollRAFPending
&& !gapRequestPending          // a replay is still outstanding — output incomplete
&& resizeDebounceTimer === null // a resize reflow is still pending
&& !viewportSyncRAFPending      // the deferred _patchViewportSync rAF (Fix 3)
```

plus the condition check: the marker scanned in the parsed buffer (`baseY−50 … baseY+rows`, the window `waitForSentinel` scans today), the `bootstrapComplete` field, or the resize bookkeeping (debounce drained after having been armed, last applied dims recorded).

The viewport-sync rAF does not affect buffer or cursor state — the things assertions compare — but the rubric's contract is "no pending write/render work," and the flag is observable, so the predicate honors the contract as written rather than a narrower private reading.

**Evaluation points** — only real dirty→clean transitions:

1. The `terminal.write` callback inside `writeTerminal` — the exact parse barrier from the spike (also the drain primitive in `resetAndSettle`).
2. `writeGuardTimer` expiry in `armWriteGuardClear`.
3. The write-flush rAF completion and the scroll/viewport-sync rAF completions (including the `_patchViewportSync` deferred rAF).
4. The resize debounce callback in `handleResize` (where `fitTerminal` applies and sends the new dims).
5. `onWriteParsed` — marker check point.
6. Frame arrival in `handleOutput` (where `gapRequestPending` clears); downstream write events re-evaluate after it.

There is no polling loop, no interval, no timer-driven re-sampling. The deadline timer exists solely to fail the wait.

## Helper rework

All wait/assert logic in `test/scenarios/generated/helpers-terminal.ts`; the pure comparison and diagnostic-artifact assembly extracted into a new `test/scenarios/generated/terminalCompare.ts` with no Playwright imports so it is unit-testable in the lowest gate.

| Today                                                                    | Becomes                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `sendTmuxCommandWithSentinel` echo-based sentinel                        | Prompt-embedded marker per the Marker generation section. Signature unchanged.                                                                                                                                                                                                                                                                                                                                                                                         |
| `waitForSentinel` 100 ms poll loop                                       | One `page.evaluate` → `waitForRenderSettled({ marker })`. Signature collapses to `(sessionId, sentinel, page, timeoutMs?)` with `page` required — the `pageOrTimeout` union overload existed only to serve the legacy no-page fallback (separate-WebSocket `waitForTerminalOutput`), which is deleted along with its five stale call sites (`gap-detection.spec.ts:77`, `:164`, `:213`; `bootstrap-scroll-position.spec.ts:61`; `resize-scroll-stability.spec.ts:59`). |
| `assertTerminalMatchesTmux` 50×200 ms retry-compare                      | Takes a required `sentinel` option: await marker + settle → `capturePane` once → `readXtermBuffer` once → compare once. Mismatch throws immediately with the diagnostic artifact. The required parameter makes the semantic boundary structural — a bare assert-without-boundary call no longer compiles.                                                                                                                                                              |
| `assertCursorMatchesTmux`, `assertCursorVisibilityMatchesTmux` 50×200 ms | Same shape: required sentinel → settle → capture both once → compare once. Re-awaits of an already-satisfied marker resolve immediately (registration-time evaluation), so paired content/cursor asserts on one boundary cost one wait total.                                                                                                                                                                                                                          |
| `openTerminal` private-field mutation + armed rAF                        | `resetAndSettle()` — drain, then reset.                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `openTerminal` content / empty / prompt polls                            | `waitForRenderSettled({ bootstrapComplete: true })` for connection readiness (the real event, not a content proxy); the emptiness poll is subsumed (`terminal.reset()` clears synchronously and `resetAndSettle()` waits for settle); the prompt-redraw wait after the ED0 clear becomes a prompt-marker wait (the clear is sent marker-terminated).                                                                                                                   |
| `openTerminal` size-stability loop + 400 ms sleep                        | `waitForRenderSettled({ resizeApplied: true })`, then one bounded probe against tmux pane dimensions (deadline, interval, last observation, diagnostics). The probe is a rubric rule 6 exception: backend→tmux resize propagation is an opaque external process boundary with no event channel to the browser.                                                                                                                                                         |

## Failure telemetry

Diagnostics keep writing to `/tmp/terminal-diagnostics` (copied to `test/scenarios/artifacts/terminal-diagnostics/` by `entrypoint.sh:63-67`). On any failure — mismatch or timeout — the artifact gains:

- Boundary timestamps: wait start, marker seen (if applicable), settle or deadline expiry.
- The completion-event trace (which evaluation points fired, with timestamps).
- Stream snapshot extended with `lastReceivedSeq`, `gapRequestPending`, `bootstrapped`, `bootstrapComplete` alongside the existing flags.
- The awaited marker, both captures, mismatch detail, and tmux pane dims — as today.

Timeout paths fetch a diagnostics snapshot from the page before writing the artifact, so a deadline failure is diagnosable from its first occurrence (rubric rule 12).

## Testing

**Unit — `assets/dashboard/src/lib/terminalStream.test.ts` (runs in `./test.sh --quick`, the lowest capable gate):** the existing MockTerminal harness captures write callbacks, rAFs, and timers, which tests fire manually:

- Delayed parsing does not resolve early: a queued write whose callback has not fired keeps `waitForRenderSettled` unresolved even after all other state clears (the plan's acceptance test).
- Marker split across two write flushes is found after the second parses.
- Timeout resolves `{ ok: false, timedOut: true }` with populated diagnostics.
- `resetAndSettle` drains before resetting: with an outstanding write callback spanning the reset, the reset does not happen until the barrier callback fires; the pending app rAF is cancelled (spy on `cancelAnimationFrame`); outstanding waiters are voided.
- Marker cannot match echoed input: the sentinel-assignment encoding produces a PS1 whose rendered prompt contains the marker while the raw assignment text does not (pure string-level check of the generator).
- Resize condition waits out the 300 ms debounce under fake timers and records applied dims.
- Ungated builds resolve `{ ok: false, reason: 'not-exposed' }`.
- Registration-time evaluation resolves already-satisfied conditions without a new event.

**Pure-comparison self-test — `tools/test-runner/src/self-tests/terminal-compare.test.ts` (node:test via tsx, runs in `./test.sh` before any suite):** fabricated divergent captures fed to the extracted compare module prove a stable mismatch throws on exactly one comparison and writes the artifact. Placement follows the existing self-test pattern so the pure logic runs in the lowest gate without new vitest infrastructure.

**Scenario acceptance — the parent plan's four criteria, verified as follows:**

1. "A test that deliberately delays parsing does not compare early" — the unit tests above, plus one scenario exercising a large-output command where chunked parsing is real.
2. "A test that produces a stable mismatch fails on the first post-completion comparison and saves diagnostics" — the self-test above (durable, automated), plus the helpers exposing a comparison counter that an existing fidelity scenario asserts increments by exactly one per assertion call.
3. "No terminal correctness helper contains `maxRetries`, retry-delay sleeps, or repeated capture-and-compare assertions" — `grep` over `helpers-terminal.ts`.
4. "Focused terminal scenarios pass repeatedly in both ordinary and CPU-constrained runs" — `./test.sh --scenarios --run 'terminal-fidelity|bootstrap-scroll-position|resize-scroll-stability|gap-detection|escbuf-gap-replay' --repeat 10 --no-cache` isolated (ordinary), and the same suite inside a CPU-capped scenario container (`docker run --cpus` on the suite container; a manual evidence invocation, no runner change in this task) for the CPU-constrained run.

Full evidence sequence before completion: `./test.sh --quick`, the focused scenario repeats above, `./test.sh`, `./badcode.sh`, and a `test-rules-review` pass over the changed tests (a violations verdict blocks, per the commit gate).

## Documentation

`docs/testing.md` drops its two "until it lands" caveats — example 6 and the terminal-fidelity patterns section now teach the landed pattern (prompt-marker completion event → single comparison) and stop describing the sentinel wait as the interim exception. This file extends the parent plan's file list for the docs-current definition of done; no other doc changes.

## Files

- `assets/dashboard/src/lib/terminalStream.ts` — completion API, hook wiring, drain-first reset.
- `assets/dashboard/src/lib/terminalStream.test.ts` — unit tests above.
- `assets/dashboard/src/vite-env.d.ts` — Window typing for the new methods, if needed.
- `test/scenarios/generated/helpers-terminal.ts` — marker generation, wait/assert rework, diagnostics, tmux dims probe.
- `test/scenarios/generated/terminalCompare.ts` — new: extracted pure comparison + diagnostic-artifact assembly (no Playwright imports).
- `tools/test-runner/src/self-tests/terminal-compare.test.ts` — new: durable mismatch/single-comparison/artifact self-test.
- `test/scenarios/generated/terminal-fidelity.spec.ts`, `escbuf-gap-replay.spec.ts`, `gap-detection.spec.ts`, `bootstrap-scroll-position.spec.ts`, `resize-scroll-stability.spec.ts` — legacy `waitForSentinel` call sites gain `page`; assert calls gain the required `sentinel`; comments describing retry behavior (e.g. `terminal-fidelity.spec.ts:622`) updated to describe the completion wait.
- `docs/testing.md` — caveat removal described above.

## Risks

- **Marker scrolled beyond the 50-line scan window** before the waiter evaluates — prompt markers land at the end of output, well inside the window; it is a named constant, raisable in one place.
- **Prompt-marker depends on the scenario shell rendering PS1 after each command** — true for the interactive shells the suite drives; verified against the container's shell in the first implementation batch. Tests own the session's shell state (rule 9), so there is no ambient-state hazard.
- **Assertions read the buffer, not the canvas** — deliberate: `readXtermBuffer` and `capturePane` both read pane/buffer state, so canvas paint (WebGL) is outside the compared claim.
- **xterm major-version bumps** could change `WriteBuffer` callback semantics; the spike evidence is pinned to 6.0.0. The delayed-parse and drain-before-reset unit tests plus scenario coverage would catch a regression; a comment at the barrier's use site cites the verified version.
- **Overhead in production** — none: hook-point checks are guarded by waiter presence, and the watcher state is only constructed behind the exposure gate.
