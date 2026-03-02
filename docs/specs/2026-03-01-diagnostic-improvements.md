# Diagnostic System Improvements — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Make the terminal diagnostic capture system useful for an AI agent to diagnose streaming issues, especially after the sequencing/gap detection changes.

**Architecture:** Six targeted improvements to the diagnostic capture pipeline. Each is independent — backend-only, frontend-only, or both. The screen diff fix is the most critical (currently produces misleading output). Gap detection data and cursor state are the highest-value additions for agent diagnosis.

**Tech Stack:** Go (backend diagnostic capture), TypeScript (frontend xterm.js capture + diagnostics), Vitest (frontend tests), Go `testing` (backend tests)

---

### Task 1: Fix screen diff structural mismatch

The diff currently compares ANSI-styled tmux output against plain-text xterm.js output, producing false positives on every styled row. Fix by stripping ANSI from the tmux screen before comparing.

**Files:**

- Modify: `assets/dashboard/src/lib/screenDiff.ts`
- Modify: `assets/dashboard/src/lib/screenDiff.test.ts` (if exists, otherwise create)

**Step 1: Write the failing test**

In `assets/dashboard/src/lib/screenDiff.test.ts`, add:

```typescript
it('strips ANSI from tmux screen before comparing', () => {
  // tmux capture-pane -e includes ANSI escapes; xterm translateToString() does not
  const tmuxScreen = '\x1b[32mhello\x1b[0m world\nline 2';
  const xtermScreen = 'hello world\nline 2';

  const result = computeScreenDiff(tmuxScreen, xtermScreen);

  // Should match — the text content is identical, only styling differs
  expect(result.differingRows.length).toBe(0);
  expect(result.summary).toBe('0 rows differ');
});
```

**Step 2: Run test to verify it fails**

```bash
cd assets/dashboard && npx vitest run src/lib/screenDiff.test.ts
```

Expected: FAIL — row 0 differs because `"\x1b[32mhello\x1b[0m world" !== "hello world"`

**Step 3: Write minimal implementation**

In `assets/dashboard/src/lib/screenDiff.ts`, import `stripAnsi` and strip the tmux lines before comparing:

```typescript
import { stripAnsi } from './ansiStrip';

export function computeScreenDiff(tmuxScreen: string, xtermScreen: string): ScreenDiff {
  const tmuxLines = tmuxScreen.split('\n');
  const xtermLines = xtermScreen.split('\n');
  const maxRows = Math.max(tmuxLines.length, xtermLines.length);
  const differingRows: ScreenDiff['differingRows'] = [];

  for (let i = 0; i < maxRows; i++) {
    const tmuxLine = stripAnsi(tmuxLines[i] ?? '').trimEnd();
    const xtermLine = (xtermLines[i] ?? '').trimEnd();
    if (tmuxLine !== xtermLine) {
      // Include raw (unsstripped) tmux line in output for agent analysis
      differingRows.push({ row: i, tmux: tmuxLines[i] ?? '', xterm: xtermLine });
    }
  }
  // ... rest unchanged
}
```

Note: the `differingRows[].tmux` field should keep the RAW tmux line (with ANSI) so the agent can see the styling. Only the comparison logic strips.

**Step 4: Run test to verify it passes**

```bash
cd assets/dashboard && npx vitest run src/lib/screenDiff.test.ts
```

**Step 5: Commit**

```
fix(diagnostic): strip ANSI from tmux screen before diff comparison
```

---

### Task 2: Add gap detection telemetry to diagnostic capture

The gap detection system (OutputLog, gap requests, replays) is the primary consistency mechanism but the diagnostic capture records none of it.

**Files:**

- Modify: `internal/dashboard/websocket.go` (diagnostic handler, ~line 696)
- Modify: `internal/dashboard/diagnostic.go` (DiagnosticCapture struct + meta)
- Modify: `internal/dashboard/diagnostic_test.go`
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (track gap stats)
- Modify: `assets/dashboard/src/lib/streamDiagnostics.ts` (add gap counters)
- Modify: `assets/dashboard/src/lib/terminalStream.test.ts`

**Step 1: Add gap counters to frontend StreamDiagnostics**

In `assets/dashboard/src/lib/streamDiagnostics.ts`, add fields:

```typescript
export class StreamDiagnostics {
  // ... existing fields ...
  gapsDetected = 0;
  gapRequestsSent = 0;
  gapFramesReceived = 0; // replay frames that arrived from gap recovery
  lastReceivedSeq: bigint = -1n; // mirror from TerminalStream for snapshot
```

Add `reset()` updates and a `gapSnapshot()` method:

```typescript
gapSnapshot(): { gapsDetected: number; gapRequestsSent: number; gapFramesReceived: number; lastReceivedSeq: string } {
  return {
    gapsDetected: this.gapsDetected,
    gapRequestsSent: this.gapRequestsSent,
    gapFramesReceived: this.gapFramesReceived,
    lastReceivedSeq: this.lastReceivedSeq.toString(),
  };
}
```

**Step 2: Instrument gap events in TerminalStream**

In `terminalStream.ts`, increment the diagnostics counters:

- In `sendGapRequest()`: `this.diagnostics?.gapRequestsSent++`
- In the gap detection block (where `seq > expectedSeq`): `if (this.diagnostics) this.diagnostics.gapsDetected++`
- After bootstrap dedup: when a frame with `seq <= lastReceivedSeq` is skipped and `bootstrapComplete` is true: `if (this.diagnostics) this.diagnostics.gapFramesReceived++` (these are replay frames)
- Before each `lastReceivedSeq = seq`: `if (this.diagnostics) this.diagnostics.lastReceivedSeq = seq`

**Step 3: Add OutputLog stats to backend diagnostic counters**

In `internal/session/tracker.go`, extend `DiagnosticCounters()` to include:

```go
result["currentSeq"] = int64(t.outputLog.CurrentSeq())
result["logOldestSeq"] = int64(t.outputLog.OldestSeq())
result["logTotalBytes"] = t.outputLog.TotalBytes()
```

**Step 4: Include gap snapshot in frontend diagnostic append**

In `terminalStream.ts` `enableDiagnostics`, when posting to `/api/dev/diagnostic-append`, include gap data:

```typescript
body: JSON.stringify({
  diagDir,
  xtermScreen,
  screenDiff: diff.diffText,
  ringBufferFrontend: frontendRingBuffer,
  gapStats: this.diagnostics?.gapSnapshot() ?? null,
}),
```

**Step 5: Accept and write gap stats on backend**

In `handlers_diagnostic.go`, extend the request struct:

```go
GapStats json.RawMessage `json:"gapStats"`
```

Write to `gap-stats.json` in the diagnostic directory.

**Step 6: Commit**

```
feat(diagnostic): add gap detection telemetry to diagnostic capture
```

---

### Task 3: Update automated findings tree

The findings logic only checks for drops. Extend it to check gap detection data, sequence breaks, and control mode reconnects.

**Files:**

- Modify: `internal/dashboard/websocket.go` (lines 710-727, the findings builder)
- Modify: `internal/dashboard/websocket_test.go`

**Step 1: Write failing test**

Add test cases for the new findings categories. Create a helper function `buildFindings(counters map[string]int64) ([]string, string)` extracted from the inline code, then test it:

```go
func TestBuildFindings_GapDetection(t *testing.T) {
    counters := map[string]int64{
        "eventsDropped": 0, "clientFanOutDrops": 0, "fanOutDrops": 0,
        "controlModeReconnects": 2,
        "currentSeq": 500, "logOldestSeq": 100,
    }
    findings, verdict := buildDiagnosticFindings(counters)
    // Should mention reconnects
    found := false
    for _, f := range findings {
        if strings.Contains(f, "reconnect") {
            found = true
        }
    }
    if !found {
        t.Error("expected reconnect finding")
    }
}
```

**Step 2: Extract `buildDiagnosticFindings` from inline code**

Move the findings logic (lines 710-727) into a standalone function:

```go
func buildDiagnosticFindings(counters map[string]int64) (findings []string, verdict string) {
    totalDrops := counters["eventsDropped"] + counters["clientFanOutDrops"] + counters["fanOutDrops"]
    if totalDrops > 0 {
        // ... existing drop findings ...
    }
    if counters["controlModeReconnects"] > 0 {
        findings = append(findings, fmt.Sprintf("control mode reconnected %d times (possible output gaps)", counters["controlModeReconnects"]))
    }
    logEvictions := counters["currentSeq"] - counters["logOldestSeq"]
    if logEvictions > 40000 { // approaching 50K capacity
        findings = append(findings, fmt.Sprintf("output log near capacity (%d entries used of 50000)", logEvictions))
    }
    if len(findings) == 0 {
        findings = append(findings, "No drops or anomalies detected in backend pipeline")
    }
    if verdict == "" {
        verdict = "No obvious backend cause found. Check frontend gap stats and screen diff."
    }
    return
}
```

**Step 3: Run tests, verify pass**

**Step 4: Commit**

```
refactor(diagnostic): extract and extend automated findings logic
```

---

### Task 4: Annotate sync counters as disabled

**Files:**

- Modify: `internal/dashboard/websocket.go` (stats message construction, ~line 593)

**Step 1: Implementation** (no test needed — cosmetic annotation)

In the stats message and diagnostic counters, add a `syncDisabled` boolean field:

In the `WSStatsMessage` struct:

```go
SyncDisabled bool `json:"syncDisabled"`
```

Set it to `true` when constructing the stats message (line ~593):

```go
SyncDisabled: true, // Sync goroutine disabled while investigating color artifacts
```

In the diagnostic handler, add to counters:

```go
counters["syncDisabled"] = 1
```

**Step 2: Commit**

```
fix(diagnostic): annotate sync counters as disabled to prevent agent confusion
```

---

### Task 5: Add cursor state to diagnostic capture

**Files:**

- Modify: `internal/dashboard/websocket.go` (diagnostic handler)
- Modify: `internal/dashboard/diagnostic.go` (DiagnosticCapture struct)
- Modify: `internal/dashboard/handlers_diagnostic.go` (accept frontend cursor)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (capture xterm cursor)
- Modify: `internal/dashboard/diagnostic_test.go`

**Step 1: Add tmux cursor to backend diagnostic capture**

In the diagnostic handler (websocket.go ~line 700), after capturing the tmux screen, also capture cursor state:

```go
curCtx, curCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
cursorState, curErr := tracker.GetCursorState(curCtx)
curCancel()
```

Add `CursorTmux` field to `DiagnosticCapture`:

```go
type DiagnosticCapture struct {
    // ... existing fields ...
    CursorTmux *controlmode.CursorState
}
```

Write it into `meta.json`:

```go
type diagnosticMeta struct {
    // ... existing fields ...
    CursorTmux *struct {
        X       int  `json:"x"`
        Y       int  `json:"y"`
        Visible bool `json:"visible"`
    } `json:"cursorTmux,omitempty"`
}
```

**Step 2: Add xterm cursor to frontend diagnostic capture**

In `terminalStream.ts` `enableDiagnostics`, extract xterm.js cursor state:

```typescript
const cursorXterm = {
  x: this.terminal.buffer.active.cursorX,
  y: this.terminal.buffer.active.cursorY,
  // xterm.js doesn't expose cursor visibility directly via buffer API
};
```

Include in the diagnostic-append POST body:

```typescript
cursorXterm: JSON.stringify(cursorXterm),
```

**Step 3: Accept and write cursor on backend**

In `handlers_diagnostic.go`, extend request struct and write `cursor-state.json`:

```go
CursorXterm string `json:"cursorXterm"`
```

Write as a file:

```go
os.WriteFile(filepath.Join(req.DiagDir, "cursor-state.json"), []byte(req.CursorXterm), 0o644)
```

Actually better: combine tmux and xterm cursor into a single `cursor-state.json` written by `WriteToDir`, then append the xterm side via the diagnostic-append endpoint.

**Step 4: Update test**

Add cursor data to `TestWriteDiagnosticDir` and verify it appears in `meta.json`.

**Step 5: Commit**

```
feat(diagnostic): capture tmux and xterm cursor state in diagnostic snapshot
```

---

### Task 6: Add timestamped entries to ring buffers

**Files:**

- Modify: `internal/dashboard/ringbuffer.go`
- Modify: `internal/dashboard/ringbuffer_test.go`
- Modify: `assets/dashboard/src/lib/streamDiagnostics.ts`
- Modify: `assets/dashboard/src/lib/streamDiagnostics.test.ts`

**Step 1: Backend — prepend timestamp header to each ring buffer write**

Instead of changing the `RingBuffer` itself (which is a generic byte buffer), prepend a timestamp in the WebSocket handler at the call site (websocket.go ~line 573):

```go
if ringBuf != nil {
    // Prepend microsecond timestamp for temporal correlation in diagnostics
    ts := []byte(fmt.Sprintf("\n--- %s ---\n", time.Now().Format("15:04:05.000000")))
    ringBuf.Write(ts)
    ringBuf.Write(send)
}
```

This keeps `RingBuffer` simple and generic. The timestamp markers are human-readable and grep-able by the agent.

**Step 2: Frontend — same pattern**

In `streamDiagnostics.ts`, add timestamp to `recordFrame`:

```typescript
recordFrame(data: Uint8Array): void {
  this.framesReceived++;
  this.bytesReceived += data.length;
  this.frameSizes.push(data.length);
  if (this.frameSizes.length > MAX_FRAME_SIZES) {
    this.frameSizes = this.frameSizes.slice(-MAX_FRAME_SIZES);
  }
  // Prepend timestamp marker for temporal correlation
  const ts = new TextEncoder().encode(`\n--- ${new Date().toISOString().substring(11, 23)} ---\n`);
  this.writeToRingBuffer(ts);
  this.writeToRingBuffer(data);
  this.checkSequenceBreak(data);
}
```

**Step 3: Update tests**

Backend: verify `Snapshot()` output contains timestamp markers.
Frontend: verify `ringBufferSnapshot()` output contains timestamp markers.

**Step 4: Commit**

```
feat(diagnostic): add timestamps to ring buffer entries for temporal correlation
```

---

## Execution Notes

- Tasks 1-6 are independent and can be parallelized
- Task 1 is the most critical (fixes actively misleading output)
- Task 2 is the highest-value addition (makes gap detection visible to agents)
- Task 4 is trivial (one-line annotation)
- All tasks should run `./test.sh --quick` before committing to catch regressions
- After all tasks, squash into one commit for the branch
