---
name: terminal-desync-investigation
description: Use when investigating terminal rendering desync between tmux and xterm.js in schmux's web dashboard — viewport showing wrong content, scrollback jumping, frozen display, content not matching tmux, screen-diff showing row mismatches, or users reporting the terminal "jumping around" or "scrolling back"
---

# Terminal Desync Investigation

## Overview

Diagnose rendering divergence between tmux (ground truth) and xterm.js (web dashboard). The terminal pipeline has 4 layers where bytes can be lost, transformed, or desynchronized. This skill gives you a systematic method to isolate which layer is responsible.

**Core principle:** tmux is always right. The investigation traces backward from "xterm shows wrong content" to find where and why the byte stream diverged.

## Architecture Quick Reference

```
Agent process  →  tmux session  →  control mode  →  SessionRuntime
  stdout/PTY       history:10K     %output events    fan-out + OutputLog
                                    chan(1000) each    50K entries, ~5MB
                                    drop on full

SessionRuntime  →  WebSocket handler  →  browser  →  xterm.js
  subscriber ch     8-byte seq header     writeLiveFrame()   scrollback: 5000
  chan(1000)         escbuf.SplitClean     batches per rAF    convertEol: true
  drop on full      binary frames         writeTerminal()
```

### Key source files

| Layer        | File                                               | Purpose                                         |
| ------------ | -------------------------------------------------- | ----------------------------------------------- |
| tmux wrapper | `internal/tmux/tmux.go`                            | CreateSession, CapturePane, resize              |
| Control mode | `internal/session/tracker.go`                      | SessionRuntime, fan-out, OutputLog              |
| WebSocket    | `internal/dashboard/websocket.go`                  | Bootstrap, sequenced frames, sync, gap handling |
| Frontend     | `assets/dashboard/src/lib/terminalStream.ts`       | handleOutput, writeLiveFrame, sanitize, scroll  |
| Sync compare | `assets/dashboard/src/lib/syncCompare.ts`          | Line-by-line text comparison                    |
| Surgical fix | `assets/dashboard/src/lib/surgicalCorrection.ts`   | Row-level viewport correction                   |
| Diagnostics  | `assets/dashboard/src/lib/streamDiagnostics.ts`    | Ring buffers, counters, capture                 |
| Write-race   | `assets/dashboard/src/lib/writeRaceDiagnostics.ts` | xterm write/render perf, stall detection        |
| Tmux health  | `internal/session/tmux_health.go`                  | Control mode RTT probe, ring buffer, stats      |

### Key docs

- `docs/terminal-pipeline.md` — **Read this first.** Canonical pipeline reference.
- `docs/architecture.md` — Package structure and data flow.

## Investigation Method

### Step 1: Read the pipeline doc

Read `docs/terminal-pipeline.md` before doing anything else. It documents the full data flow, known desync root causes, the bootstrap protocol, the sync/correction mechanism, and configuration values. Do not skip this.

### Step 2: Analyze the diagnostic capture

Diagnostic captures live in `~/.schmux/diagnostics/<timestamp>-<sessionId>/`. Trigger one from the dashboard dev-mode diagnostic button. Find the latest capture with `ls -t ~/.schmux/diagnostics/`.

**meta.json** — Start here. Key fields:

```
counters.eventsDropped      → non-zero = pipeline dropped events
counters.fanOutDrops        → non-zero = tracker fan-out dropped
counters.clientFanOutDrops  → non-zero = control mode client dropped
counters.wsWriteErrors      → non-zero = WebSocket write failures
counters.controlModeReconnects → non-zero = possible output gaps
counters.wsConnections      → >1 = multiple bootstraps occurred
automatedFindings           → pre-computed diagnosis hints
```

**screen-diff.txt** — Row-by-row comparison of tmux vs xterm. Count differing rows to gauge severity. Look at WHAT differs — is it content, colors, or position?

**screen-tmux.txt / screen-xterm.txt** — Raw captures from each side. Determine whether xterm shows old content, garbled content, or just wrong styling.

**gap-stats.json** — `gapsDetected` non-zero means events were lost in transit. Compare `lastReceivedSeq` with `meta.json`'s `counters.currentSeq`.

**scroll-stats.json** — `followLostCount` non-zero means followTail was disabled unexpectedly (viewport stopped tracking the bottom).

**write-race-stats.json** — xterm.js write/render performance telemetry. Collected silently by monkey-patching xterm internals. Key sections:

```
aggregates:
  totalWrites              → total terminal.write() calls
  avgWriteMs / avgParseMs  → per-write performance (parse = InputHandler.parse time)
  longestWriteMs           → worst case; >16ms = dropped frame
  longestParseMs           → worst parse; compare with longestWriteMs to see wait time
  avgScrollsPerWrite       → scroll events per write (linefeeds during parse)
  avgViewportSyncsPerWrite → Viewport._sync calls per write (should match scrolls)
  totalHandleScrollDuringSync → should be 0; non-zero = suppress bug still firing
  totalRefreshCalls        → RenderService.refreshRows calls during writes
  totalFullRefreshes       → refreshRows(0, rows-1) calls; ratio to total = wasted work
  fullRefreshRatio         → % of refreshes that are full-screen
  redundantScrollToBottom  → should be 0; non-zero = scrollToBottom fix not working

render:
  totalRenders             → actual renderer.renderRows() calls
  avgRenderMs              → time per render; >10ms = renderer bottleneck
  longestRenderMs          → worst render; >16ms = dropped frame from rendering
  longestRenderRows        → row count for worst render

stalls:                    → main thread gaps >100ms (detected by setInterval watchdog)
  ts                       → when the stall ended
  gapMs                    → duration (100-200ms = minor, >500ms = severe)
  inWrite                  → true = blocked by xterm parse; false = React/GC/browser

bufferSwitches:            → normal ↔ alternate buffer transitions
  ts, buffer               → "normal" or "alternate"; frequent switches = TUI app redraws

recentWrites:              → last 50 write events with per-write detail
  totalMs, parseMs, waitMs → timing breakdown
  scrollsDuringWrite       → linefeeds in this write
  handleScrollDuringSyncCount → suppress bug instances in this write
  baseYDelta, viewportYDelta  → buffer position change (negative = buffer reset/collapse)
```

Interpretation guide:

- `longestParseMs > 16` → InputHandler.parse blocked the main thread past one frame
- `totalHandleScrollDuringSync > 0` → The Viewport.\_suppressOnScrollHandler bug is firing (should be rare with Fix 2)
- `stalls` with `inWrite=false` → Main thread blocked by something outside xterm (React re-render, GC, browser layout)
- `stalls` with `inWrite=true` → Main thread blocked by xterm parse (very large data chunk)
- `baseYDelta` large negative → Terminal was reset (bootstrap reconnect); correlate with `bufferSwitches`
- `avgRenderMs > 5` → Renderer is slow (WebGL context issues, texture atlas rebuilds)
- `fullRefreshRatio > 60%` → Claude Code's TUI is redrawing all rows on each update (expected for TUI apps)

**slow-react-renders.json** — React renders of SessionDetailPage that exceeded 50ms. Each entry has `ts`, `phase` (mount/update), `durationMs`. If stalls in write-race-stats correlate with entries here, React re-rendering is the root cause. Common triggers: session/workspace WebSocket broadcasts causing context re-renders, diagnostic stats interval updates.

**tmux-health.json** — Time series of tmux control mode round-trip time (RTT) probes. Collected every 5 seconds by sending `display-message -p ok` through the control mode connection and measuring response time. Each entry:

```
ts       — ISO timestamp of the probe
rtt_ms   — round-trip time in milliseconds
err      — true if the probe timed out or failed
```

Interpretation:

- `rtt_ms` baseline should be 0.5-3ms. Values above 10ms indicate tmux is under load.
- **Gradual increase over time** (trending upward) → tmux performance degradation, likely from scrollback memory pressure or control mode queue growth.
- **Periodic spikes** → correlate with TUI redraws; each spike is tmux busy processing escape sequences.
- **Sustained high RTT** (>50ms for >1 minute) → tmux is severely congested, likely the root cause of perceived input latency.
- **Errors** → control mode connection instability, may trigger reconnection/bootstrap.
- Compare RTT trends with scrollback.drop events in lifecycle-events.json — if RTT spikes precede scrollback drops, tmux congestion may be triggering Claude Code's TUI recovery redraws.

**lifecycle-events.json** — Timeline of all frontend events. This is the most detailed source. Parse with:

```python
import json
events = json.load(open('lifecycle-events.json'))
for e in events:
    print(f"{e['ts']}  {e['event']}  {e.get('detail', '')}")
```

Key events: `bootstrap.reset`, `bootstrapComplete`, `sanitize.stripped`, `sync.correction`, `scroll.suppressed`, `render.fullScreen` (check `scrollback`, `baseY`, `viewportY`).

**ringbuffer-backend.txt / ringbuffer-frontend.txt** — Last 256KB of raw terminal bytes from each side of the pipeline. Backend = as sent from control mode. Frontend = as received by browser (includes 8-byte seq headers). Use Python for analysis (see Step 3).

### Step 3: Analyze ring buffers

**Never use grep/rg for escape sequences.** Binary content breaks text tools. Always use Python with `rb` mode.

**Catalog all escape sequences** to understand the application's drawing model:

```python
import re
from collections import Counter, defaultdict

data = open('ringbuffer-backend.txt', 'rb').read()
# Strip timestamp markers (ring buffer metadata, not terminal data)
clean = re.sub(rb'\n--- \d\d:\d\d:\d\d\.\d+ ---\n', b'', data)

# Find all CSI sequences (ESC [ params final_byte)
csi_seqs = re.findall(rb'\x1b\[([^@A-Z\[\\\]^_`a-z{|}~]*[@A-Z\[\\\]^_`a-z{|}~])', clean)
csi_by_final = defaultdict(list)
for seq in csi_seqs:
    csi_by_final[chr(seq[-1])].append(seq[:-1].decode('ascii', errors='replace'))

for final in sorted(csi_by_final):
    params = Counter(csi_by_final[final])
    print(f"CSI ...{final}: {len(csi_by_final[final])}x  top params: {params.most_common(5)}")

# Also check: ESC sequences, OSC sequences, raw control characters
esc_seqs = Counter(re.findall(rb'\x1b([^[\x1b])', clean))
osc_seqs = re.findall(rb'\x1b\]([^\x07\x1b]*?)(?:\x07|\x1b\\)', clean)
print(f"\nESC sequences: {dict(esc_seqs)}")
print(f"OSC sequences: {len(osc_seqs)}")
for byte_val in [7, 8, 10, 13]:
    print(f"  0x{byte_val:02x}: {clean.count(bytes([byte_val]))}")
```

**CSI final byte reference:**

| Final     | Name            | What it does                                             |
| --------- | --------------- | -------------------------------------------------------- |
| `A/B/C/D` | CUU/CUD/CUF/CUB | Cursor Up/Down/Forward/Back                              |
| `G`       | CHA             | Cursor Horizontal Absolute                               |
| `H`       | CUP             | Cursor Position (no params = home)                       |
| `J`       | ED              | Erase in Display (0=below, 1=above, 2=all, 3=scrollback) |
| `K`       | EL              | Erase in Line (0=right, 1=left, 2=entire)                |
| `h/l`     | SM/RM           | Set/Reset Mode (DEC private modes use `?` prefix)        |
| `m`       | SGR             | Colors and text attributes                               |
| `r`       | DECSTBM         | Set scroll region (top;bottom)                           |

**Compare backend vs frontend** to detect in-transit modifications:

```python
for name, path in [('backend', 'ringbuffer-backend.txt'), ('frontend', 'ringbuffer-frontend.txt')]:
    d = open(path, 'rb').read()
    print(f"{name}: ED2J={d.count(b'\\x1b[2J')} ED3J={d.count(b'\\x1b[3J')} "
          f"RIS={d.count(b'\\x1bc')} HOME={d.count(b'\\x1b[H')}")
```

If counts differ between backend and frontend, something in the pipeline is modifying the stream (check `escbuf.SplitClean` or input filtering in the WebSocket handler).

**Identify drawing patterns** — group escape sequences into logical operations:

```python
# Find paired markers (e.g., \x1b[?2026h ... \x1b[?2026l for synchronized updates)
# and measure block sizes to understand the application's drawing granularity
blocks = []
for m in re.finditer(rb'\x1b\[\?2026h', clean):
    end_m = re.search(rb'\x1b\[\?2026l', clean[m.end():])
    if end_m:
        blocks.append(m.end() + end_m.end() - m.start())
if blocks:
    print(f"Sync update blocks: {len(blocks)} total")
    print(f"  <200B: {sum(1 for b in blocks if b<200)} (animation)")
    print(f"  200B-5KB: {sum(1 for b in blocks if 200<=b<5000)} (partial updates)")
    print(f"  >5KB: {sum(1 for b in blocks if b>=5000)} (full redraws)")
```

**Find what's outside paired markers** — content sent without wrappers may behave differently:

```python
# Build a mask of "inside paired markers" and examine what's outside
# This reveals whether the application sends any naked drawing operations
```

### Step 4: Verify assumptions against xterm.js source

**Critical rule: never assume xterm.js supports a terminal feature. Check the actual source.**

The installed xterm.js lives at `assets/dashboard/node_modules/@xterm/xterm/`. Key file for escape sequence handling: `src/common/InputHandler.ts`.

To check whether a DEC private mode is implemented, search the `setModePrivate` and `resetModePrivate` switch statements:

```bash
grep -A2 "case <MODE_NUMBER>" \
  assets/dashboard/node_modules/@xterm/xterm/src/common/InputHandler.ts
```

If the mode number is not in the switch, it's silently ignored — the sequence is a no-op in xterm.js regardless of what it does in other terminals or what the spec says.

Also verify: xterm.js version (`package.json`), addon list (check `node_modules/@xterm/`), and any terminal options set in the schmux frontend code (search `new Terminal({` in `terminalStream.ts`).

### Step 5: Understand the transforms

The pipeline intentionally modifies the byte stream in several places. Read each transform and understand what it strips, why, and what side effects it has:

1. **`escbuf.SplitClean()`** in `internal/escbuf/escbuf.go` — holds back incomplete ANSI sequences at WebSocket frame boundaries to prevent splits. Can delay delivery of escape sequences by one frame.

2. **Input filtering** in `websocket.go` — terminal query responses (DA1, DA2, OSC 10/11) from xterm.js are dropped and not forwarded to tmux. Check if the filter has false positives.

3. **`writeLiveFrame()` batching** in `terminalStream.ts` — coalesces multiple WebSocket frames into a single `terminal.write()` per animation frame. This changes the atomicity of operations — sequences that the application sent as separate events become one concatenated write.

4. **`writeTerminal()` scroll guard** — Read the `writingToTerminal` / `scrollRAFPending` / `writeRAFPending` flag interaction carefully.

5. **Viewport sync patches** in `_patchViewportSync()` — Runtime monkey-patches on xterm's Viewport to fix scroll cascading and defer synchronous DOM work. Read the method comments for details.

6. **Bootstrap flash prevention** — Container visibility is toggled around `terminal.reset()` + bootstrap write to prevent blank-screen flash during reconnect.

### Step 6: Form and test hypotheses

With the data from steps 2-5, form a hypothesis about which layer and which transform causes the observed desync. Then verify:

- **If drops suspected:** Check all drop counters in meta.json. If zero, drops are ruled out.
- **If sanitize suspected:** Compare ring buffers. Count stripped sequences. Check whether the sanitized data produces different buffer state than the original would.
- **If batching suspected:** Check lifecycle events for timing between `sanitize.stripped` events and subsequent render events. Look at scrollback growth patterns.
- **If sync correction suspected:** Check for `sync.correction` events. If present, the sync is actively rewriting the viewport — it may be fighting with live writes.
- **If bootstrap suspected:** Check `bootstrap.reset` event's `dataLen` and subsequent render events. Verify the bootstrap uses `capture-pane` (rendered snapshot) not OutputLog replay — see `websocket.go` bootstrap section.

## Common Mistakes

1. **Assuming terminal features exist.** Always verify against the actual xterm.js source in `node_modules/`. The spec says one thing; the implementation may not support it.

2. **Using grep for escape sequences.** Binary content and escape bytes break text tools. Always use Python with `rb` mode.

3. **Confusing ring buffer timestamps with terminal data.** The `--- HH:MM:SS.microseconds ---` lines are diagnostic metadata injected by the capture. Strip them before analysis.

4. **Assuming bootstrap replays the OutputLog.** Read the bootstrap code in `websocket.go` to verify the actual mechanism — it may use `capture-pane` instead.

5. **Assuming the sanitize strip is the only transform.** The `writeLiveFrame()` rAF batching, `writeTerminal()` scroll coalescing, and `handleUserScroll()` suppression all interact. Read the full write chain.

6. **Ignoring the sync correction.** The defense-in-depth sync fires on a timer and on output pauses. It rewrites viewport rows independently of the live data path. Check lifecycle events for `sync.correction` to see if corrections and live writes are stepping on each other.

7. **Building theories on escape sequence semantics without checking xterm.js behavior.** What `\x1b[2J]` does depends on the terminal implementation, scrollback settings, and buffer mode. Test in xterm.js directly or read the xterm.js `InputHandler.ts` source for the specific sequence.
