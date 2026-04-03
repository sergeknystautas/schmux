# Plan: Typing Diagnostics Redesign

**Goal**: Replace single-tuple breakdown picking with group-median cohorts (typical/outlier), rename misleading segments, and fix data quality issues — so the typing performance widget shows honest, actionable latency breakdowns.

**Architecture**: Changes are confined to two frontend files (`inputLatency.ts`, `TypingPerformance.tsx`) and one doc file. No backend changes. The `LatencyBreakdown` type gains a `segmentSum` field. The `getBreakdown()` method changes from single-tuple picking to cohort-median computation. The MessageChannel probe moves from `markReceived()` to `recordServerSegments()` to pair jsQueue per-tuple. Display labels change via `SEGMENT_LABELS` map only — no internal field renames.

**Tech Stack**: TypeScript, React, Vitest

**Design spec**: `docs/specs/2026-04-03-typing-diagnostics-redesign-design-final.md`

---

## Step 1: Add staleness timeout to `markReceived()`

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 1a. Write failing test

In `assets/dashboard/src/lib/inputLatency.test.ts`, add after the existing tests:

```typescript
it('markReceived discards samples when lastInputTime is stale (>2s)', () => {
  const mockNow = vi.spyOn(performance, 'now');
  mockNow.mockReturnValueOnce(100);
  inputLatency.markSent();
  // 2500ms later — exceeds 2s staleness threshold
  mockNow.mockReturnValueOnce(2600);
  mockNow.mockReturnValueOnce(2600); // receive lag probe timestamp
  inputLatency.markReceived();
  mockNow.mockRestore();

  // Sample should have been discarded
  expect(inputLatency.getStats()).toBeNull();
  expect(inputLatency.samples.length).toBe(0);
});

it('markReceived keeps samples within staleness threshold', () => {
  const mockNow = vi.spyOn(performance, 'now');
  mockNow.mockReturnValueOnce(100);
  inputLatency.markSent();
  // 1500ms later — within 2s threshold
  mockNow.mockReturnValueOnce(1600);
  mockNow.mockReturnValueOnce(1600); // receive lag probe timestamp
  inputLatency.markReceived();
  mockNow.mockRestore();

  expect(inputLatency.samples.length).toBe(1);
});
```

### 1b. Run test to verify it fails

```bash
./test.sh --quick
```

### 1c. Write implementation

In `inputLatency.ts`, `markReceived()` method (line 186), add staleness check:

```typescript
markReceived() {
  if (this.lastInputTime === 0) return;
  const now = performance.now();
  const rtt = now - this.lastInputTime;
  // Staleness guard: discard if >2s since keystroke (agent is thinking, not echoing)
  if (rtt > 2000) {
    this.lastInputTime = 0;
    return;
  }
  this.lastInputTime = 0;
  // ... rest unchanged
```

### 1d. Run test to verify it passes

```bash
./test.sh --quick
```

### 1e. Commit

```bash
jf submit -m "fix(diagnostics): add 2s staleness timeout to discard bogus latency samples"
```

---

## Step 2: Move MessageChannel probe from `markReceived()` to `recordServerSegments()` and make jsQueue per-tuple

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 2a. Write failing test

Replace the existing `receive-time lag probe fires and records in receiveLagSamples` test and add new ones:

```typescript
it('recordServerSegments fires MessageChannel probe and stores receiveLag on the tuple', () => {
  const originalMC = globalThis.MessageChannel;
  class MockMessageChannel {
    port1 = { onmessage: null as ((ev: any) => void) | null };
    port2 = {
      postMessage: () => {
        if (this.port1.onmessage) {
          this.port1.onmessage({} as any);
        }
      },
    };
  }
  globalThis.MessageChannel = MockMessageChannel as any;

  const mockNow = vi.spyOn(performance, 'now');
  // Two calls: probeStart and handler fires
  mockNow.mockReturnValueOnce(500); // probe start
  mockNow.mockReturnValueOnce(503); // handler fires: lag = 3ms
  const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
  inputLatency.recordServerSegments(tuple);

  mockNow.mockRestore();
  globalThis.MessageChannel = originalMC;

  // The tuple should now have receiveLag attached
  const stored = inputLatency.serverSegmentSamples[0];
  expect((stored as any).receiveLag).toBe(3);
});

it('markReceived no longer fires a receive-time MessageChannel probe', () => {
  const mockNow = vi.spyOn(performance, 'now');
  mockNow.mockReturnValueOnce(100);
  inputLatency.markSent();
  mockNow.mockReturnValueOnce(110);
  inputLatency.markReceived();
  mockNow.mockRestore();

  // receiveLagSamples should not exist or be empty
  expect(inputLatency.receiveLagSamples).toEqual([]);
});
```

### 2b. Run test to verify it fails

```bash
./test.sh --quick
```

### 2c. Write implementation

1. Add `receiveLag` to `ServerSegmentTuple`:

```typescript
export type ServerSegmentTuple = {
  dispatch: number;
  sendKeys: number;
  echo: number;
  frameSend: number;
  total: number;
  mutexWait?: number;
  executeNet?: number;
  executeCount?: number;
  sessionType?: 'local' | 'remote';
  receiveLag?: number; // event loop lag at sideband processing time
};
```

2. Move the MessageChannel probe from `markReceived()` into `recordServerSegments()`:

```typescript
recordServerSegments(tuple: ServerSegmentTuple) {
  this.serverSegmentSamples.push(tuple);
  if (this.serverSegmentSamples.length > MAX_SAMPLES) {
    this.serverSegmentSamples = this.serverSegmentSamples.slice(-MAX_SAMPLES);
  }

  // Event loop lag probe: measures JS main thread congestion at the moment
  // the sideband message is processed. The callback fires in a subsequent
  // macrotask and writes directly into the just-pushed tuple.
  const probeStart = performance.now();
  const channel = new MessageChannel();
  const target = this.serverSegmentSamples[this.serverSegmentSamples.length - 1];
  channel.port1.onmessage = () => {
    target.receiveLag = performance.now() - probeStart;
  };
  channel.port2.postMessage(null);
}
```

3. Remove the receive-time MessageChannel probe from `markReceived()` (lines 203-215). Keep `receiveLagSamples` array for now (existing `getBreakdown` still reads it). It will be removed in Step 4 when `getBreakdown` is rewritten.

### 2d. Run test to verify it passes

```bash
./test.sh --quick
```

### 2e. Commit

```bash
jf submit -m "refactor(diagnostics): move MessageChannel probe into recordServerSegments for per-tuple jsQueue"
```

---

## Step 3: Add `segmentSum` to `LatencyBreakdown` type

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 3a. Write failing test

```typescript
it('getBreakdown returns segmentSum field', () => {
  const mockNow = vi.spyOn(performance, 'now');
  for (let i = 0; i < 5; i++) {
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(130);
    inputLatency.markReceived();
  }
  for (let i = 0; i < 5; i++) inputLatency.markRenderTime(2);
  mockNow.mockRestore();

  const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
  for (let i = 0; i < 5; i++) inputLatency.recordServerSegments(tuple);

  const breakdown = inputLatency.getBreakdown('p50');
  expect(breakdown).not.toBeNull();
  expect(breakdown!.segmentSum).toBeDefined();
  expect(breakdown!.segmentSum).toBeGreaterThan(0);
});
```

### 3b. Run test to verify it fails

```bash
./test.sh --quick
```

### 3c. Write implementation

Add `segmentSum` to the `LatencyBreakdown` type:

```typescript
export type LatencyBreakdown = {
  network: number;
  jsQueue: number;
  handler: number;
  wsWrite: number;
  xterm: number;
  tmuxCmd: number;
  paneOutput: number;
  total: number;
  segmentSum: number; // sum of all segment medians (for bar width computation)
};
```

Add `segmentSum` to the return value of the existing `getBreakdown()`:

```typescript
// At the end of getBreakdown(), before the return statement:
const segmentSum =
  picked.network +
  picked.jsQueue +
  picked.handler +
  picked.wsWrite +
  picked.xterm +
  picked.tmuxCmd +
  picked.paneOutput;

return {
  // ... existing fields ...
  segmentSum,
};
```

### 3d. Run test to verify it passes

```bash
./test.sh --quick
```

### 3e. Commit

```bash
jf submit -m "feat(diagnostics): add segmentSum field to LatencyBreakdown type"
```

---

## Step 4: Rewrite `getBreakdown()` with cohort-median computation

**File**: `assets/dashboard/src/lib/inputLatency.ts`

This is the core statistical change. The method signature changes from `getBreakdown(level: 'p50' | 'p99')` to `getBreakdown(level: 'typical' | 'outlier')`.

### 4a. Write failing tests

Replace the existing `getBreakdown` tests with new ones for the cohort approach:

```typescript
describe('getBreakdown cohort-median', () => {
  function addSamples(count: number, rttBase: number, rttJitter: number) {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < count; i++) {
      const rtt = rttBase + (i % 2 === 0 ? rttJitter : -rttJitter);
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(100 + rtt);
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      const serverTotal = rtt * 0.3;
      inputLatency.recordServerSegments({
        dispatch: serverTotal * 0.1,
        sendKeys: serverTotal * 0.4,
        echo: serverTotal * 0.4,
        frameSend: serverTotal * 0.1,
        total: serverTotal,
      });
    }
    mockNow.mockRestore();
  }

  it('returns null with fewer than 5 valid paired tuples', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 4; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      inputLatency.recordServerSegments({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
      });
    }
    mockNow.mockRestore();
    // Only 4 tuples — below minimum cohort size of 5
    expect(inputLatency.getBreakdown('typical')).toBeNull();
  });

  it('typical breakdown uses IQR cohort', () => {
    // 20 samples with RTTs around 30ms
    addSamples(20, 30, 3);
    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    // Total should be near 30ms (the median RTT of the IQR cohort)
    expect(breakdown!.total).toBeGreaterThan(20);
    expect(breakdown!.total).toBeLessThan(40);
  });

  it('outlier breakdown uses P95+ cohort', () => {
    // 50 samples: mostly 30ms, a few outliers at 100ms
    addSamples(45, 30, 3);
    addSamples(5, 100, 5);
    const breakdown = inputLatency.getBreakdown('outlier');
    expect(breakdown).not.toBeNull();
    // Outlier total should reflect the high-RTT samples
    expect(breakdown!.total).toBeGreaterThan(50);
  });

  it('returns null for outlier cohort when fewer than 5 P95+ tuples', () => {
    // 20 samples — P95+ is 1 tuple, below minimum
    addSamples(20, 30, 3);
    expect(inputLatency.getBreakdown('outlier')).toBeNull();
  });

  it('segmentSum equals sum of all segment medians', () => {
    addSamples(20, 30, 3);
    const breakdown = inputLatency.getBreakdown('typical')!;
    const sum =
      breakdown.network +
      breakdown.jsQueue +
      breakdown.handler +
      breakdown.wsWrite +
      breakdown.xterm +
      breakdown.tmuxCmd +
      breakdown.paneOutput;
    expect(breakdown.segmentSum).toBeCloseTo(sum, 5);
  });

  it('residual (network) is per-tuple clamped to zero then medianed', () => {
    addSamples(20, 30, 3);
    const breakdown = inputLatency.getBreakdown('typical')!;
    // Residual should be non-negative
    expect(breakdown.network).toBeGreaterThanOrEqual(0);
  });

  it('discards mispaired tuples where serverTotal > clientRTT', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // All 5 tuples have RTT=5ms but serverTotal=9ms
    for (let i = 0; i < 5; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(105);
      inputLatency.markReceived();
      inputLatency.markRenderTime(0);
      inputLatency.recordServerSegments({
        dispatch: 2,
        sendKeys: 3,
        echo: 3,
        frameSend: 1,
        total: 9,
      });
    }
    mockNow.mockRestore();
    expect(inputLatency.getBreakdown('typical')).toBeNull();
  });

  it('uses per-tuple receiveLag for jsQueue when available', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 20; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      const tuple = {
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
        receiveLag: 5, // per-tuple jsQueue
      };
      inputLatency.recordServerSegments(tuple);
    }
    mockNow.mockRestore();

    const breakdown = inputLatency.getBreakdown('typical')!;
    // jsQueue should reflect the per-tuple receiveLag values
    expect(breakdown.jsQueue).toBe(5);
  });
});
```

### 4b. Run test to verify they fail

```bash
./test.sh --quick
```

### 4c. Write implementation

Rewrite `getBreakdown()` in `inputLatency.ts`:

```typescript
getBreakdown(level: 'typical' | 'outlier'): LatencyBreakdown | null {
  const pairedCount = Math.min(
    this.samples.length,
    this.serverSegmentSamples.length,
    this.renderSamples.length
  );
  if (pairedCount < 5) return null;

  const sOff = this.samples.length - pairedCount;
  const segOff = this.serverSegmentSamples.length - pairedCount;
  const rOff = this.renderSamples.length - pairedCount;

  // Fallback event loop lag (global percentile) if per-tuple receiveLag is missing
  let fallbackLag = 0;
  if (this.lagSamples.length > 0) {
    const lagStats = this.computeStats(this.lagSamples);
    if (lagStats) fallbackLag = lagStats.median;
  }

  // Build per-tuple full breakdowns
  type FullTuple = {
    clientRTT: number;
    handler: number;
    tmuxCmd: number;
    paneOutput: number;
    wsWrite: number;
    xterm: number;
    jsQueue: number;
    network: number; // residual, clamped to 0
  };
  const tuples: FullTuple[] = [];
  for (let i = 0; i < pairedCount; i++) {
    const clientRTT = this.samples[sOff + i];
    const seg = this.serverSegmentSamples[segOff + i];
    const xterm = this.renderSamples[rOff + i];
    const serverTotal = seg.dispatch + seg.sendKeys + seg.echo + seg.frameSend;
    if (serverTotal > clientRTT) continue;

    const jsQueue = seg.receiveLag ?? fallbackLag;
    const measured = serverTotal + xterm + jsQueue;
    const network = Math.max(0, clientRTT - measured);
    tuples.push({
      clientRTT,
      handler: seg.dispatch,
      tmuxCmd: seg.sendKeys,
      paneOutput: seg.echo,
      wsWrite: seg.frameSend,
      xterm,
      jsQueue,
      network,
    });
  }
  if (tuples.length < 5) return null;

  // Compute percentile boundaries from valid paired tuple RTTs
  const sortedRTTs = tuples.map(t => t.clientRTT).sort((a, b) => a - b);
  const p25 = sortedRTTs[Math.floor(sortedRTTs.length * 0.25)];
  const p75 = sortedRTTs[Math.floor(sortedRTTs.length * 0.75)];
  const p95 = sortedRTTs[Math.floor(sortedRTTs.length * 0.95)];

  // Select cohort
  let cohort: FullTuple[];
  if (level === 'typical') {
    cohort = tuples.filter(t => t.clientRTT >= p25 && t.clientRTT <= p75);
  } else {
    cohort = tuples.filter(t => t.clientRTT > p95);
  }
  if (cohort.length < 5) return null;

  // Compute median of each segment within the cohort
  const median = (arr: number[]) => {
    const s = [...arr].sort((a, b) => a - b);
    return s[Math.floor(s.length / 2)];
  };

  const handler = median(cohort.map(t => t.handler));
  const tmuxCmd = median(cohort.map(t => t.tmuxCmd));
  const paneOutput = median(cohort.map(t => t.paneOutput));
  const wsWrite = median(cohort.map(t => t.wsWrite));
  const xtermMedian = median(cohort.map(t => t.xterm));
  const jsQueue = median(cohort.map(t => t.jsQueue));
  const network = median(cohort.map(t => t.network));
  const total = median(cohort.map(t => t.clientRTT));
  const segmentSum = network + jsQueue + handler + wsWrite + xtermMedian + tmuxCmd + paneOutput;

  return {
    network,
    jsQueue,
    handler,
    wsWrite,
    xterm: xtermMedian,
    tmuxCmd,
    paneOutput,
    total,
    segmentSum,
  };
}
```

Also update the call sites in `TypingPerformance.tsx` to use `'typical'` and `'outlier'` instead of `'p50'` and `'p99'`:

```typescript
// In LatencyBreakdownBars():
const p50 = inputLatency.getBreakdown('typical');
const p99 = inputLatency.getBreakdown('outlier');
```

### 4d. Run test to verify they pass

```bash
./test.sh --quick
```

### 4e. Commit

```bash
jf submit -m "feat(diagnostics): replace single-tuple picking with cohort-median breakdowns"
```

---

## Step 5: Clean up `receiveLagSamples` and update `TrackerSnapshot`

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 5a. Write failing test

Update the existing reset test to NOT expect `receiveLagSamples`:

```typescript
it('reset clears serverSegmentSamples and lagSamples', () => {
  inputLatency.recordServerSegments({
    dispatch: 1,
    sendKeys: 2,
    echo: 1,
    frameSend: 0.5,
    total: 4.5,
  });
  inputLatency.reset();
  expect(inputLatency.serverSegmentSamples).toEqual([]);
  expect(inputLatency.lagSamples).toEqual([]);
  // receiveLagSamples should no longer exist as a separate array
  expect(inputLatency.framesBetweenSamples).toEqual([]);
  expect(inputLatency.handleOutputTimeSamples).toEqual([]);
});
```

### 5b. Run test to verify it fails

```bash
./test.sh --quick
```

### 5c. Write implementation

1. Remove `receiveLagSamples` field from the class, from `TrackerSnapshot`, from `switchMachine()`, from `reset()`, and from `getWireContext()`
2. Remove the MessageChannel probe from `markReceived()` (the one that writes to `receiveLagSamples`)
3. Update `getWireContext()` to compute receiveLag stats from `serverSegmentSamples[].receiveLag` instead of the removed array
4. Remove tests that reference `receiveLagSamples` directly (`receive-time lag probe fires and records in receiveLagSamples`, `getBreakdown uses receiveLagSamples for jsQueue when available`, etc.)

### 5d. Run test to verify it passes

```bash
./test.sh --quick
```

### 5e. Commit

```bash
jf submit -m "refactor(diagnostics): remove receiveLagSamples array, jsQueue now per-tuple only"
```

---

## Step 6: Update segment display labels, colors, and ordering

**File**: `assets/dashboard/src/components/TypingPerformance.tsx`

### 6a. No separate test needed — this is a display-only change. Verify visually after implementation.

### 6b. Write implementation

Update the three constants:

```typescript
// Causal ordering: follows the keystroke's journey through the system
const SEGMENTS = [
  'handler', // schmux receives and decodes
  'tmuxCmd', // keystroke travels to tmux (transport)
  'paneOutput', // tmux + agent processes
  'wsWrite', // schmux sends output frame
  'jsQueue', // browser event loop picks it up
  'xterm', // terminal renders
  'network', // unmeasured residual
] as const;

const SEGMENT_COLORS: Record<string, string> = {
  // schmux (ours) — green family
  handler: 'rgba(80, 170, 120, 0.7)',
  wsWrite: 'rgba(100, 190, 140, 0.7)',
  // host environment (theirs) — gray family
  tmuxCmd: 'rgba(160, 160, 160, 0.7)',
  paneOutput: 'rgba(130, 130, 130, 0.7)',
  // browser — blue family
  jsQueue: 'rgba(80, 130, 200, 0.7)',
  xterm: 'rgba(100, 150, 220, 0.7)',
  // catch-all
  network: 'rgba(190, 150, 80, 0.5)',
};

const SEGMENT_LABELS: Record<string, string> = {
  handler: 'handler',
  wsWrite: 'ws write',
  tmuxCmd: 'transport',
  paneOutput: 'tmux + agent',
  jsQueue: 'js queue',
  xterm: 'xterm',
  network: 'unmeasured',
};
```

### 6c. Run tests

```bash
./test.sh --quick
```

### 6d. Commit

```bash
jf submit -m "feat(diagnostics): rename segments and reorder to causal flow"
```

---

## Step 7: Update `BreakdownRow` to use `segmentSum` and change labels to Typical/Outlier

**File**: `assets/dashboard/src/components/TypingPerformance.tsx`

### 7a. No separate test — verify with existing component tests and visual inspection.

### 7b. Write implementation

1. Update `BreakdownRow` to use `segmentSum` for per-segment width percentages:

```typescript
function BreakdownRow({
  label,
  breakdown,
  scale,
}: {
  label: string;
  breakdown: LatencyBreakdown;
  scale: number;
}) {
  const [showTooltip, setShowTooltip] = useState(false);
  const rowRef = useRef<HTMLDivElement>(null);
  const { total, segmentSum } = breakdown;
  if (total <= 0) return null;

  return (
    <div
      className="typing-perf__bar-row"
      ref={rowRef}
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      <span className="typing-perf__bar-label">{label}</span>
      <span className="typing-perf__bar-total">{Math.round(total)}ms</span>
      <div className="typing-perf__bar-track">
        <div className="typing-perf__bar-fill" style={{ width: `${scale * 100}%` }}>
          {SEGMENTS.map((seg) => {
            const value = breakdown[seg as keyof LatencyBreakdown] as number;
            if (value == null) return null;
            const pct = segmentSum > 0 ? (value / segmentSum) * 100 : 0;
            if (pct < 0.5) return null;
            return (
              <div
                key={seg}
                className="typing-perf__bar-segment"
                style={{
                  width: `${pct}%`,
                  backgroundColor: SEGMENT_COLORS[seg],
                }}
              />
            );
          })}
        </div>
      </div>
      {/* tooltip unchanged */}
    </div>
  );
}
```

2. Update `LatencyBreakdownBars` labels and add insufficient data handling:

```typescript
function LatencyBreakdownBars() {
  const typical = inputLatency.getBreakdown('typical');
  const outlier = inputLatency.getBreakdown('outlier');
  if (!typical && !outlier) return null;

  const maxTotal = Math.max(typical?.total ?? 0, outlier?.total ?? 0);

  return (
    <div className="typing-perf__breakdown" data-testid="latency-breakdown">
      {typical ? (
        <BreakdownRow label="Typical" breakdown={typical} scale={maxTotal > 0 ? typical.total / maxTotal : 1} />
      ) : (
        <div className="typing-perf__insufficient">Typical: insufficient data</div>
      )}
      {outlier ? (
        <BreakdownRow label="Outlier" breakdown={outlier} scale={maxTotal > 0 ? outlier.total / maxTotal : 1} />
      ) : (
        <div className="typing-perf__insufficient">Outlier: insufficient data</div>
      )}
    </div>
  );
}
```

### 7c. Run tests

```bash
./test.sh --quick
```

### 7d. Commit

```bash
jf submit -m "feat(diagnostics): use segmentSum for bar widths, relabel to Typical/Outlier"
```

---

## Step 8: Update existing tests for new API

**File**: `assets/dashboard/src/lib/inputLatency.test.ts`

### 8a. Update tests

Update all `getBreakdown('p50')` / `getBreakdown('p99')` calls to use `'typical'` / `'outlier'`. Remove tests that specifically test single-tuple picking behavior. Remove tests that directly reference `receiveLagSamples`. Ensure tests that check `getBreakdown` return values also check `segmentSum`.

Key test updates:

- `getBreakdown returns null without enough paired segment samples` — update minimum from 3 to 5
- `getBreakdown with lag samples subtracts jsQueue from network` — rewrite for per-tuple receiveLag
- `getBreakdown picks the keystroke at the P50 rank` — remove (single-tuple picking no longer exists)
- `getBreakdown uses receiveLagSamples for jsQueue when available` — remove
- `getBreakdown falls back to lagSamples when receiveLagSamples is empty` — remove
- All remaining tests referencing `receiveLagSamples` — remove or update

### 8b. Run tests

```bash
./test.sh --quick
```

### 8c. Commit

```bash
jf submit -m "test(diagnostics): update tests for cohort-median API and per-tuple jsQueue"
```

---

## Step 9: Update documentation

**File**: `docs/typing-performance.md`

### 9a. Update the following sections:

1. **Display Names table**: Update old display names to new ones (network→unmeasured, tmux cmd→transport, pane output→tmux + agent)
2. **Known Issues**: Mark #1 (single-tuple picking) as resolved. Mark #5 (stale lastInputTime) as resolved. Mark #6 (negative residual) as resolved.
3. **Timestamps and Segments**: Update to reflect that breakdowns now use cohort medians, not single-tuple picking

### 9b. Commit

```bash
jf submit -m "docs(diagnostics): update typing-performance.md for cohort-median redesign"
```

---

## Step 10: End-to-end verification

### 10a. Run full test suite

```bash
./test.sh
```

### 10b. Build dashboard and verify visually

```bash
go run ./cmd/build-dashboard
```

Start the daemon and navigate to a terminal session. Type in the terminal, collect ~50+ samples, and verify:

1. The breakdown shows "Typical" and "Outlier" labels instead of "P50" and "P99"
2. Segment labels read: handler, transport, tmux + agent, ws write, js queue, xterm, unmeasured
3. Colors are grouped: greens (schmux), grays (host), blues (browser)
4. Segments are in causal order (handler → transport → tmux + agent → ws write → js queue → xterm → unmeasured)
5. "Outlier" bar is wider than "Typical" bar
6. If fewer than ~100 samples, "Outlier: insufficient data" appears (P95+ cohort too small)
7. The "unmeasured" segment is small relative to other segments

---

## Task Dependencies

| Group | Steps         | Can Parallelize                                                                                |
| ----- | ------------- | ---------------------------------------------------------------------------------------------- |
| 1     | Steps 1, 2, 3 | Yes (independent changes to different parts of inputLatency.ts)                                |
| 2     | Step 4        | No (depends on Group 1: uses per-tuple receiveLag from Step 2 and segmentSum type from Step 3) |
| 3     | Step 5        | No (depends on Step 4: cleanup after getBreakdown rewrite)                                     |
| 4     | Steps 6, 7    | Yes (label/color changes and bar width changes are independent)                                |
| 5     | Step 8        | No (depends on Groups 2-4: tests must match final API)                                         |
| 6     | Step 9        | No (depends on all implementation being final)                                                 |
| 7     | Step 10       | No (end-to-end verification after everything)                                                  |
