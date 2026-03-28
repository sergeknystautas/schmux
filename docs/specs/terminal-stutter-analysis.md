# Terminal Visual Stutter During TUI Redraws

**Date:** 2026-03-27
**Diagnostic capture:** `~/.schmux/diagnostics/2026-03-27T22-51-03-schmux-001-856be9bb/`
**Session:** `schmux-001-856be9bb` (Claude Code, Opus 4.6, 189x61 terminal)

---

## Reported Symptom

The xterm.js terminal in the web dashboard periodically "scrolls back in time" — the viewport jumps to show older conversation content, the display freezes for roughly a second, then catches up to the current state. The effect repeats in a stutter pattern across several seconds. It breaks the native terminal feel and resembles a rendering lock or resource contention issue.

---

## Pipeline Health Summary

All pipeline drop and error counters are zero. The data transport layer is healthy.

| Metric                | Value          | Interpretation                                |
| --------------------- | -------------- | --------------------------------------------- |
| eventsDropped         | 0              | No drops at parser level                      |
| fanOutDrops           | 0              | Tracker fan-out clean                         |
| clientFanOutDrops     | 0              | Control mode client clean                     |
| wsWriteErrors         | 0              | All WebSocket writes succeeded                |
| gapsDetected          | 0              | Sequence numbers contiguous                   |
| controlModeReconnects | 0              | Stable control mode connection                |
| wsConnections         | 3              | 3 WebSocket connections over session lifetime |
| sync corrections      | 0 of 39 checks | Defense-in-depth never found a mismatch       |
| eventsDelivered       | 19,870         | ~6.3 MB total                                 |

The `screen-diff.txt` at capture time says "Screens match." — tmux and xterm.js are in agreement. The desync is transient, not persistent.

---

## What the Escape Sequence Analysis Shows

Ring buffer analysis (last ~250 KB of both backend and frontend streams) reveals Claude Code's drawing model:

| Sequence            | Backend count | Frontend count | Purpose                 |
| ------------------- | ------------- | -------------- | ----------------------- |
| `\x1b[2J` (ED2J)    | 3             | 3              | Clear visible screen    |
| `\x1b[3J` (ED3J)    | 3             | 3              | Clear scrollback buffer |
| `\x1b[H` (CUP home) | 3             | 3              | Cursor to top-left      |
| `\x1b[?2026h`       | 279           | 279            | Synchronized output ON  |
| `\x1b[?2026l`       | 280           | 280            | Synchronized output OFF |
| `\x1b[...m` (SGR)   | 8,232         | 8,141          | Colors/attributes       |
| `\x1b[...C` (CUF)   | 5,278         | 5,273          | Cursor forward          |

Backend and frontend counts match within ring buffer boundary noise. No sequences are being eaten or injected by the pipeline. The `escbuf.SplitClean` holdback is working correctly — no split-escape artifacts in the delivered data.

### Synchronized output block distribution

All ED2J+ED3J sequences are inside `?2026h...?2026l` synchronized output pairs — Claude Code wraps its screen clears atomically. Block size distribution:

| Size category      | Count | Description                          |
| ------------------ | ----- | ------------------------------------ |
| < 200 bytes        | 271   | Spinner/status bar animations        |
| 200 bytes - 5 KB   | 5     | Partial content updates              |
| > 5 KB (max 61 KB) | 3     | Full TUI redraws (contain ED2J+ED3J) |

No erase operations occur outside synchronized output blocks.

### xterm.js compatibility

xterm.js 6.0.0 fully implements DEC private mode 2026 (synchronized output). The `RenderService` has two-level suppression: `refreshRows()` buffers row ranges when `synchronizedOutput` is true, and `_renderRows()` (the actual pixel draw) rechecks the flag before rendering. A safety timeout of 1,000 ms force-disables sync mode if `?2026l` is never received.

The `onRender` event fires exclusively from `_renderRows()`, after `renderer.renderRows()` draws pixels. If `onRender` fires, the user saw those pixels.

---

## The Render Event Timeline

The lifecycle event buffer (500 entries, ring) captured render events with buffer state metadata. Filtering for state transitions reveals the exact visual sequence during TUI redraws.

### Collapse sequence 1 (ts ~1774674708217)

```
ts                sb    baseY  vY     writing  wRAF   note
1774674704166     354   293    293    True     False  stable TUI state
                                                      [4051ms gap]
1774674708217     100    39     39    False    False  COLLAPSE: -254 lines
                                                      [903ms gap — screen frozen]
1774674709120     104    43     43    False    True   +4 lines appear
1774674709126     108    47     47    True     False  +4 more lines
                                                      [1274ms gap — screen frozen]
1774674710400     354   293    293    True     False  TUI fully redrawn
```

The user sees: complete TUI (354 lines) -> instant collapse to 100 lines -> **903 ms frozen** -> tiny 8-line update -> **1,274 ms frozen** -> complete TUI appears. Total stutter duration: ~2.2 seconds across two visible "steps."

### Collapse sequence 2 (ts ~1774674719216)

```
ts                sb    baseY  vY     writing  wRAF   note
1774674710400     354   293    293    True     False  stable TUI state
                                                      [8816ms gap]
1774674719216      99    38     38    False    False  COLLAPSE: -255 lines
                                                      [1134ms gap — screen frozen]
1774674720350     103    42     42    False    False  +4 lines appear
1774674720367     105    44     44    True     False  +2 more
1774674720392     107    46     46    True     False  +2 more
                                                      [2117ms gap — screen frozen]
1774674722509     111    50     50    True     True   +4 lines
1774674722518     363   302    302    True     False  TUI fully redrawn
```

Stutter: collapse -> **1,134 ms frozen** -> 6 lines trickle -> **2,117 ms frozen** -> completion. Total: ~3.3 seconds.

### Collapse sequence 3 (ts ~1774677040965, most detailed)

This collapse was captured with higher resolution in the lifecycle buffer:

```
ts                sb    baseY  vY     writing  wRAF   note
1774677040965     103    42     42    False    False  COLLAPSE: -260 lines
                                                      [407ms gap]
1774677041372     105    44     44    True     False  growth begins
1774677041389     108    47     47    True     False  +3 lines   [+17ms = 1 rAF]
1774677041405     137    76     76    True     False  +29 lines  [+16ms]
1774677041422     147    86     86    True     True   +10 lines  [+17ms]
1774677041430     158    97     97    True     False  +11 lines  [+8ms]
1774677041447     172   111    111    True     False  +14 lines  [+17ms]
1774677041464     186   125    125    True     False  +14 lines  [+17ms]
1774677041480     209   148    148    True     True   +23 lines  [+16ms]
1774677041488     213   152    152    True     False  +4 lines   [+8ms]
1774677041505     216   155    155    True     False  +3 lines   [+17ms]
1774677041522     252   191    191    True     True   +36 lines  [+17ms]
1774677041530     270   209    209    True     False  +18 lines  [+8ms]
1774677041547     275   214    214    True     False  +5 lines   [+17ms]
1774677041563     348   287    287    True     False  +73 lines  [+16ms]
1774677041580     352   291    291    False    True   +4 lines   [+17ms]
1774677041588     355   294    294    True     False  TUI complete
```

This sequence is different from the others: the buffer grows across 15 render events over 216 ms, with renders at rAF cadence (~16-17 ms each). Each render event fires from `_renderRows()`, meaning each intermediate state is drawn to the screen as pixels.

The `?2026h...?2026l` synchronized output wraps the ED2J+ED3J clear but not the full TUI redraw content. Once `?2026l` fires (within the initial clear block), the renderer is unblocked and draws each subsequent chunk of content as it arrives.

### Collapse sequence 4 (ts ~1774677053976)

```
ts                sb    baseY  vY     writing  wRAF
1774677053976      82    21     21    False    False  COLLAPSE: -273 lines
                                                      [1018ms gap]
1774677054994      90    29     29    False    False  +8 lines
                                                      [891ms gap]
1774677055885     348   287    287    True     False  TUI complete
```

Stutter: collapse -> **1,018 ms frozen** -> 8 lines -> **891 ms frozen** -> completion. Total: ~1.9 seconds.

### Collapse sequence 5 (ts ~1774677060167)

```
ts                sb    baseY  vY     writing  wRAF
1774677060167      80    19     19    False    False  COLLAPSE: -268 lines
                                                      [717ms gap]
1774677060884      86    25     25    True     False  +6 lines
                                                      [1109ms gap]
1774677061993      95    34     34    True     False  +9 lines
1774677062009     120    59     59    True     False  +25 lines  [+16ms]
1774677062025     226   165    165    True     False  +106 lines [+16ms]
1774677062051     249   188    188    True     True   +23 lines  [+26ms]
...
1774677062209     345   284    284    False    False  TUI complete (200ms)
```

Stutter: collapse -> **717 ms frozen** -> 6 lines -> **1,109 ms frozen** -> 200 ms progressive rebuild -> completion. Total: ~2.0 seconds.

---

## The Collapse Mechanism

Each collapse follows the same pattern. Claude Code sends a full TUI redraw that the pipeline delivers as:

**Phase 1: Atomic clear** (inside `?2026h...?2026l`)

- `\x1b[?2026h` — synchronized output ON
- `\x1b[2J` — erase visible screen
- `\x1b[3J` — erase scrollback
- `\x1b[H` — cursor home
- Initial content (a few dozen lines of TUI from the top)
- `\x1b[?2026l` — synchronized output OFF -> render fires

This renders atomically. The user sees: the old TUI disappears, replaced by the top ~40 lines of the new TUI. The scrollback is destroyed — `baseY` drops from ~290 to ~40. The viewport jumps to follow.

**Phase 2: Inter-burst gap** (700 ms - 2,100 ms)
No output events arrive. The terminal shows ~100 lines (40 of content + 60 viewport rows), with the rest of the screen empty or showing the top of the old TUI. The screen is frozen.

The write-race diagnostics confirm no rendering work occurs during these gaps — the writes themselves take <1 ms. The gaps are in the **data arrival**:

```
recentWrites around collapse 5:

ts               dataLen   totalMs  parseMs  baseY delta
1774677059158     13,210    0.5      0.3      -268          <- collapse write
                                               [1,717 ms gap — no data]
1774677060875      3,072    0.2      0.2       +6           <- trickle
                                               [1,109 ms gap — no data]
1774677061984      3,072    0.4      0.3       +9           <- trickle
1774677062000      3,047    0.5      0.1      +25           <- burst begins
1774677062018     20,383    0.8      0.4     +106           <- main content
1774677062042      5,120    0.5      0.2      +23
...
```

Each write completes in under 1 ms. The pipeline isn't blocking. The 1,717 ms and 1,109 ms gaps are intervals where Claude Code produces no terminal output.

**Phase 3: Content burst** (50 ms - 216 ms)
The remaining TUI content arrives rapidly — often 87 WebSocket frames coalesced into one write burst (43-46 KB). The rAF batching in `writeLiveFrame()` coalesces these into a small number of `terminal.write()` calls. The buffer grows from ~100 to ~350 lines across a few render frames.

---

## Why Synchronized Output Doesn't Prevent the Visual Stutter

xterm.js's synchronized output (`?2026h...?2026l`) works correctly — verified by reading `RenderService.ts` line-by-line. When enabled, `refreshRows()` buffers row ranges and `_renderRows()` returns early. No pixels are drawn until `?2026l` fires.

The problem is **scope**: Claude Code wraps the screen clear + initial content in one sync block, but the subsequent content (Phase 2 trickles and Phase 3 bursts) arrives as separate events outside that block. Once `?2026l` fires at the end of Phase 1, the renderer is unblocked. Every subsequent event renders immediately.

This is visible in the Phase 1 collapse render event: `writing=False, writeRAF=False` — the write guard has expired (8 ms `armWriteGuardClear` timer), and the renderer draws the collapsed state to the screen.

---

## What the User Sees (Reconstructed)

Combining the render timeline with the screen captures:

1. **Frame 0:** Complete Claude Code TUI — 350+ lines, viewport at the bottom showing the current prompt, status bar, and recent conversation.

2. **Frame 1 (atomic, ~0 ms):** Screen clears. Viewport jumps from `baseY=293` to `baseY=39`. The visible area now shows the **top** of the TUI — which contains older conversation content (tool invocations, earlier responses). The bottom 2/3 of the screen is empty. The scrollback above the viewport is destroyed.

3. **Frames 2-3 (frozen, 700-2,100 ms):** Nothing changes. The partially-filled TUI sits motionless. A small trickle of 4-8 lines may appear partway through, but the update is too small to be meaningful.

4. **Frame 4 (burst, 50-216 ms):** The remaining 250+ lines of TUI content arrive rapidly. The buffer grows from ~100 to ~350 lines across several rAF frames. The viewport scrolls down through the conversation as content fills in. The current prompt and status bar reappear at the bottom.

The perceptual experience: the terminal "rewinds" to show old content (step 2), freezes (step 3), then fast-forwards through the conversation history back to the present (step 4). Total duration: 1.9-3.3 seconds per occurrence, with 5 occurrences in this capture session (~57 minutes).

---

## Why the Gaps Exist: The `pause-after` Race Condition

The initial hypothesis attributed the 700-2,100 ms gaps entirely to Claude Code's output timing. Frontend evidence supports this — writes complete in <1 ms, no stalls detected during active rendering, zero drops at all pipeline layers. But the backend tells a different story.

### The `pause-after=1` mechanism

The tracker enables tmux's `pause-after` flow control at session startup (`tracker.go:479`):

```go
client.EnablePauseAfter(pauseCtx, 1) // pause-after=1 second
```

This means: **after 1 second of no `%output` events, tmux pauses the pane.** While paused:

- tmux stops sending `%output` events for the pane
- Output accumulates in tmux's internal buffer
- A `%pause` notification is sent to the control mode client
- The pane stays paused until `continue-pane` is received

With Claude Code's TUI redraws producing natural 1-2 second pauses between content bursts, **every inter-burst gap triggers a pause-after event.** The 5 scrollback collapses in this capture, each with 2-3 inter-burst gaps, means 10-15 pause-after triggers during this session.

### The race condition

When the tracker receives a `%pause` notification (`tracker.go:529-544`), it does two things:

```go
case pausedPane := <-client.Pauses():
    // Step A: Signal sync goroutine (non-blocking send)
    select {
    case t.syncTrigger <- struct{}{}:
    default:
    }
    // Step B: Resume output delivery (BLOCKING — calls Execute())
    client.ContinuePane(contCtx, pausedPane)
```

Step A fires before Step B, so the sync goroutine (`websocket.go:493-559`) wakes up **concurrently**:

```go
case <-tracker.SyncTrigger():
    doSync("pause")  // calls CapturePane() then GetCursorState()
```

Both goroutines call `Execute()`, which serializes on `stdinMu`:

```
Tracker goroutine                    Sync goroutine
─────────────────                    ──────────────
receives %pause
  │
  ├─ syncTrigger <- ────────────→    receives syncTrigger
  │                                    │
  ├─ ContinuePane() {                 doSync() {
  │    Execute() {                       CapturePane() {
  │      stdinMu.Lock() ←─ RACE ──→       stdinMu.Lock()
  │      write cmd                         write cmd
  │      wait response                     wait response
  │    }                                 }
  │  }                                   GetCursorState() {
  │                                        stdinMu.Lock()
  │                                        write cmd
  │                                        wait response
  │                                      }
  │                                    }
```

**If the sync goroutine wins the `stdinMu` race**, the command order to tmux becomes:

1. `capture-pane -e -p` — tmux executes this **while the pane is still paused**
2. `display-message` (cursor state) — pane **still paused**
3. `refresh-client -A %0:continue` — pane **finally resumes**

tmux is single-threaded (libevent). While executing `capture-pane` for a 189x61 terminal (generating ~61 lines of ANSI-escaped text, 20-50 KB response), tmux is not reading from the PTY and not processing any other events. The pane remains paused for the entire duration of both sync commands.

**If the tracker goroutine wins the race**, the order is correct — `continue-pane` executes first, the pane resumes, and `capture-pane` runs against a live pane. But this outcome is non-deterministic.

### The feedback loop hypothesis

The `pause-after` mechanism may not just amplify the stutter — it may **cause** the TUI redraw cycle:

1. Claude Code writes output to its PTY
2. tmux reads the PTY, sends `%output` events via control mode
3. A natural pause of ≥1 second occurs → `pause-after` fires → pane paused
4. If sync goroutine wins the `stdinMu` race: pane stays paused during `capture-pane` + `cursor-state` (10-50 ms of command processing where tmux is **not reading the PTY**)
5. Claude Code continues writing to the PTY → kernel PTY buffer fills (typically 4-16 KB)
6. If the PTY buffer fills: Claude Code's `stdout.write()` blocks → Node.js event loop stalls → Ink (TUI framework) experiences backpressure
7. When the pane resumes and tmux drains the PTY buffer, Ink may detect the backpressure-induced state inconsistency and trigger a full screen clear (`ED2J` + `ED3J`) as a recovery mechanism

This would mean the sync mechanism is not just adding delay during the gaps — it is **triggering the scrollback collapses themselves**. The evidence is circumstantial (the diagnostic capture doesn't include server-side timing for the sync commands or PTY buffer state), but the mechanism is plausible: `pause-after=1` fires on every natural pause, the `stdinMu` race can keep the pane paused during command processing, and PTY backpressure from the extended pause could trigger Ink's recovery redraw.

### What the frontend diagnostics confirm

The frontend evidence is consistent with — but cannot distinguish between — "Claude Code pauses naturally" and "the pause-after mechanism extends the pause":

1. **Write-race diagnostics:** Every `terminal.write()` call completes in <1 ms. This confirms the browser rendering pipeline is not the bottleneck, but says nothing about whether data arrived late due to pipeline contention or application pauses.

2. **Stall detector:** No stalls during active rendering. The stalls recorded (48 events) are all during the tab-hidden period — browser timer throttling, not relevant.

3. **Pipeline counters:** Zero drops. The non-blocking channels (cap 1000) didn't overflow. But drops measure **capacity**, not **latency**. Data can be delayed without being dropped.

4. **Render event cadence:** During Phase 3 bursts, render events fire at ~16 ms intervals. This confirms the browser processes data efficiently once it arrives.

5. **Backend gap logging:** The tracker (`tracker.go:503`) and WebSocket handler (`websocket.go:619`) both log output gaps >500 ms. The parser also logs when command responses block it for >100 ms (`parser.go:205`). These server-side logs — not captured in the frontend diagnostic — would conclusively show whether the gaps originate in Claude Code or in the `pause-after` → sync → `stdinMu` pipeline.

---

## The Amplification Effect

Whether the gaps originate from Claude Code's natural output timing or from the `pause-after` race condition (or both), the visual severity is disproportionate to the underlying delay. A 1-2 second pause in terminal output is normally imperceptible — the user sees a stable display and waits for more content. The pause becomes a jarring visual stutter because of three compounding factors:

### Factor 1: Scrollback destruction (`\x1b[3J`)

ED3J (`Erase in Display` mode 3) destroys the entire scrollback buffer. In xterm.js, this triggers `buffer._trimScrollback()`, which removes lines from the top of the buffer. The effect:

- `baseY` drops from ~290 to ~40 (a shift of 250+ rows)
- `viewportY` follows `baseY` (auto-scroll is active, `followTail=true`)
- The viewport now shows content from the **top** of the new buffer

Without ED3J, a 1-second output pause would show the previous complete TUI state (the last fully-rendered frame). With ED3J, the pause shows a half-built new TUI — the incomplete state is what the user stares at for 1-2 seconds.

### Factor 2: Visible intermediate states

The synchronized output block (`?2026h...?2026l`) wraps only the clear operation, not the full TUI redraw. The content that rebuilds the TUI arrives as separate events and renders immediately (each rAF frame draws whatever content exists). The `onRender` events during collapse sequence 3 confirm this — 15 consecutive full-screen renders fire at 16-17 ms intervals, each showing a different buffer size (103, 105, 108, 137, 147, ..., 355).

### Factor 3: Asymmetric burst pattern

The content arrives in a pattern that maximizes visual disruption:

| Phase   | Duration     | Data     | Visual change                 |
| ------- | ------------ | -------- | ----------------------------- |
| Clear   | <1 ms        | 13 KB    | Viewport jumps to old content |
| Gap 1   | 700-1,717 ms | 0        | Frozen partial TUI            |
| Trickle | <1 ms        | 3 KB     | +4-8 lines (barely visible)   |
| Gap 2   | 891-2,117 ms | 0        | Frozen partial TUI            |
| Burst   | 50-216 ms    | 20-46 KB | Remaining 250+ lines appear   |

The small trickle between gaps is psychologically damaging — it confirms the connection is alive (not disconnected) but the update is too small to be useful, resetting the user's patience timer.

---

## Open Questions

### Does the sync goroutine consistently win the `stdinMu` race?

Go's goroutine scheduler is non-deterministic. When the tracker sends to `syncTrigger` and then calls `ContinuePane()`, the sync goroutine may or may not be scheduled before the tracker's `Execute()` call acquires `stdinMu`. If the sync goroutine wins more than 50% of the time, the pane stays paused during capture-pane on most pause events. Instrumenting the command order (logging which command is sent first after each `%pause`) would quantify this.

### Does PTY backpressure actually trigger Claude Code's full redraws?

The feedback loop hypothesis (pause → PTY buffer fills → Ink backpressure → ED3J recovery redraw) is plausible but unverified. Testing requires either:

- Disabling `pause-after` entirely and observing whether TUI redraws still occur at the same frequency
- Monitoring the kernel PTY buffer fill level during pause events
- Checking if Ink/Claude Code has a recovery codepath that triggers full redraws after stdout backpressure clears

### How long do the sync commands actually take?

The server logs `sync commands completed` with `capture_ms`, `cursor_ms`, and `total_ms` (`websocket.go:525-533`). The parser logs `command response blocked parser` when a response takes >100 ms or exceeds 100 lines (`parser.go:205-211`). These logs would show the actual duration that the pane stays paused when the sync goroutine wins the race. For a 189x61 terminal, the capture-pane response is ~61 lines — just below the 100-line logging threshold.

### `totalHandleScrollDuringSync: 22`

The write-race diagnostics recorded 22 instances of `_handleScroll` firing during a `Viewport._sync` call. The Fix 2 patch (`_patchViewportSync`) suppresses these — without suppression, they could corrupt the viewport position. All 22 occurred before the `recentWrites` window (all recent writes show `handleScrollDuringSyncCount: 0`). These events indicate the viewport position was under stress during earlier TUI redraws but was protected by the patch.

### Missing `slow-react-renders.json`

The diagnostic capture does not include this file. If React re-renders of the `SessionDetailPage` component are interleaving with xterm.js chunk processing via `setTimeout`, they would add main-thread contention between render frames. The dashboard WebSocket sends stats every 3 seconds (`statsTickerC`), which triggers React state updates. Without profiling data, this interaction cannot be ruled out as a contributing factor to perceived stutter severity.

---

## Data Appendix

### Write burst correlation with scrollback drops

```
ts              event                frames  bytes   sb from->to  delta
1774674703692   write.burst           10       618
1774674707208   write.burst           14    16,263
1774674708217   SCROLLBACK DROP                       354 -> 100   -254
1774674710391   write.burst           87    43,646
1774674715799   write.burst           12       748
1774674718208   BUFFER COLLAPSED                      354 ->  61
1774674719216   SCROLLBACK DROP                       354 ->  99   -255
1774674722509   write.burst           57    42,367
                                                      [~38 minutes gap]
1774677039954   write.burst           17    19,342
1774677040965   SCROLLBACK DROP                       363 -> 103   -260
1774677041513   write.burst            8     9,199
1774677041654   write.burst            8       454
1774677042204   write.burst            7       339
1774677052967   write.burst           12    13,203
1774677053976   SCROLLBACK DROP                       355 ->  82   -273
1774677055875   write.burst           87    46,373
1774677059157   write.burst           10    13,210
1774677060167   SCROLLBACK DROP                       348 ->  80   -268
1774677062018   write.burst           18    20,383
1774677062200   write.burst            6       560
```

### Tab visibility and stall detector

Tab hidden at ts 1774675737855, visible at 1774675849705 (112 seconds). During this period: 48 stalls detected, all ~1,000 ms gaps with `inWrite=false`, consistent with browser timer throttling for background tabs. Buffer state unchanged throughout (`lines=363, baseY=302, viewportY=302`). No data was lost.

### Sync check history

39 sync checks received over the session (~57 minutes). The sync goroutine fires on two triggers: a 60-second timer and `SyncTrigger()` (which fires on every `pause-after` event). With `pause-after=1`, every 1-second output gap triggers an additional sync. The 39 checks over 57 minutes (~1 per 87 seconds) suggests most syncs are timer-driven, but pause-triggered syncs are likely undercounted in the frontend lifecycle events (the ring buffer wraps at 500 entries, dominated by 407 render events).

Zero corrections were triggered — the defense-in-depth comparison never found a mismatch. The sync is doing work (capture-pane + cursor-state commands through control mode) but never finding anything to fix. Meanwhile, each sync triggered by `pause-after` contends with `ContinuePane()` for the `stdinMu` mutex, potentially extending the pane's paused state.

### `pause-after` configuration

Set at session startup (`tracker.go:479`): `refresh-client -f pause-after=1`. The value of 1 second was chosen for flow control (preventing tmux from silently dropping output when the control mode client falls behind). In practice, the client never falls behind (zero drops at all layers), so the mechanism triggers exclusively on natural application-level output pauses — exactly when the TUI redraw stutter is visible.
