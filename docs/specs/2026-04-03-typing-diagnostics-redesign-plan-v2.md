# Plan: Typing Diagnostics Redesign (v2)

**Goal**: Replace single-tuple breakdown picking with group-median cohorts (typical/outlier), rename misleading segments, and fix data quality issues — so the typing performance widget shows honest, actionable latency breakdowns.

**Architecture**: Changes are confined to two frontend files (`inputLatency.ts`, `TypingPerformance.tsx`) and one doc file. No backend changes. The `LatencyBreakdown` type gains a `segmentSum` field. The `getBreakdown()` method changes from single-tuple picking to cohort-median computation. The MessageChannel probe moves from `markReceived()` to `recordServerSegments()` to pair jsQueue per-tuple. Display labels change via `SEGMENT_LABELS` map only — no internal field renames.

**Tech Stack**: TypeScript, React, Vitest

**Design spec**: `docs/specs/2026-04-03-typing-diagnostics-redesign-design-final.md`

---

## Changes from previous version

**C1 (Steps not parallelizable):** Steps 1-5 are now fully sequential. The dependency table no longer claims Steps 1-3 can run in parallel. All five steps modify `inputLatency.ts` and have logical dependencies on each other.

**C2 (Test mocking fixed):** All test code now uses the correct mocking pattern from the existing test file:

- `markSent()` calls `performance.now()` twice (lastInputTime + sentTime) and the MessageChannel handler calls it once more — so MockMessageChannel is required and 3 mock values must be provided per `markSent()` call.
- `markReceived()` calls `performance.now()` once (for rtt) plus once more for the receive lag probe start — so 2 mock values plus 1 for the handler when using MockMessageChannel, or just 2 when the probe will be removed.
- The `addSamples` helper in Step 4 uses MockMessageChannel and provides the correct number of mock values per iteration.

**C3 (`jf submit` replaced):** All commit steps now say "Commit using `/commit`" instead of `jf submit`. The `/commit` skill enforces the project's definition of done (runs `./test.sh`, checks API docs, requires self-assessment).

**C4 (Internal inconsistency fixed):** Step 2 now correctly says "Keep `receiveLagSamples` for now; it will be removed in Step 5." This matches the actual plan structure where Step 4 rewrites `getBreakdown` (stops reading `receiveLagSamples`) and Step 5 removes the field.

**C5 (`receiveLag ?? 0` per design):** Step 4 now uses `receiveLag ?? 0` instead of `receiveLag ?? fallbackLag`. The design explicitly states: "getBreakdown() treats tuples with receiveLag === undefined as having receiveLag = 0." The `lagSamples` send-time probe measures a fundamentally different thing (event loop lag at send time vs receive time), so using it as a fallback mixes signals. Using 0 is honest: it says "we don't know."

**S1 (Outlier test uses 80 samples):** The "returns null for outlier cohort" test now uses 80 samples instead of 20, so P95+ gives 4 tuples (below the 5 minimum) for a non-degenerate reason, rather than 0 tuples from a degenerate edge case.

**S3 (Old test updates merged into Step 4):** When Step 4 changes the API from `'p50'`/`'p99'` to `'typical'`/`'outlier'`, it also updates all existing tests in the same step. This ensures tests are never broken between steps and `./test.sh --quick` passes at every commit boundary.

**S4 (getWireContext implementation shown):** Step 5 now includes the full implementation code for updating `getWireContext()` to compute receiveLag stats from `serverSegmentSamples[].receiveLag`.

**S6 (Step 4 size noted):** Step 4 is acknowledged as the largest step (~15-20 min) but kept as one step since splitting a `getBreakdown` rewrite mid-function does not make sense.

---

## Step 1: Add staleness timeout to `markReceived()`

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 1a. Write failing test

In `assets/dashboard/src/lib/inputLatency.test.ts`, add after the existing tests:

```typescript
it('markReceived discards samples when lastInputTime is stale (>2s)', () => {
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
  // markSent: lastInputTime, sentTime, lagHandler
  mockNow.mockReturnValueOnce(100); // lastInputTime
  mockNow.mockReturnValueOnce(100); // sentTime in lag probe
  mockNow.mockReturnValueOnce(102); // lag handler fires
  inputLatency.markSent();
  // 2500ms later — exceeds 2s staleness threshold
  // markReceived: rtt, recvTime, recvLagHandler
  mockNow.mockReturnValueOnce(2600); // performance.now() for rtt
  mockNow.mockReturnValueOnce(2600); // recvTime in receive lag probe
  mockNow.mockReturnValueOnce(2602); // receive lag handler fires
  inputLatency.markReceived();

  mockNow.mockRestore();
  globalThis.MessageChannel = originalMC;

  // Sample should have been discarded
  expect(inputLatency.getStats()).toBeNull();
  expect(inputLatency.samples.length).toBe(0);
});

it('markReceived keeps samples within staleness threshold', () => {
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
  // markSent: lastInputTime, sentTime, lagHandler
  mockNow.mockReturnValueOnce(100); // lastInputTime
  mockNow.mockReturnValueOnce(100); // sentTime in lag probe
  mockNow.mockReturnValueOnce(102); // lag handler fires
  inputLatency.markSent();
  // 1500ms later — within 2s threshold
  // markReceived: rtt, recvTime, recvLagHandler
  mockNow.mockReturnValueOnce(1600); // performance.now() for rtt
  mockNow.mockReturnValueOnce(1600); // recvTime in receive lag probe
  mockNow.mockReturnValueOnce(1602); // receive lag handler fires
  inputLatency.markReceived();

  mockNow.mockRestore();
  globalThis.MessageChannel = originalMC;

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

Commit using `/commit` with message: "fix(diagnostics): add 2s staleness timeout to discard bogus latency samples"

---

## Step 2: Move MessageChannel probe from `markReceived()` to `recordServerSegments()` and make jsQueue per-tuple

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 2a. Write failing test

Add new tests (do NOT remove the existing `receive-time lag probe fires and records in receiveLagSamples` test yet — it will be removed in Step 5):

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
  // recordServerSegments fires a MessageChannel probe: probeStart, handler
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
  // markSent: lastInputTime, sentTime, lagHandler
  mockNow.mockReturnValueOnce(100);
  mockNow.mockReturnValueOnce(100);
  mockNow.mockReturnValueOnce(102);
  inputLatency.markSent();
  // markReceived: now only calls performance.now() once (for rtt) — no receive probe
  mockNow.mockReturnValueOnce(110);
  inputLatency.markReceived();

  mockNow.mockRestore();
  globalThis.MessageChannel = originalMC;

  // receiveLagSamples should still be empty (no probe fired from markReceived)
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

3. Remove the receive-time MessageChannel probe from `markReceived()` (lines 203-215 — the `recvTime` / `channel` / `receiveLagSamples.push` block). Keep `receiveLagSamples` array for now; it will be removed in Step 5.

### 2d. Run test to verify it passes

```bash
./test.sh --quick
```

Note: The existing `receive-time lag probe fires and records in receiveLagSamples` test will now fail because the probe was removed from `markReceived()`. Update that test to verify the probe no longer fires from `markReceived()` (it has been moved to `recordServerSegments`, which is tested by the new test above). Specifically, update the assertion at the end of that test from `expect(inputLatency.receiveLagSamples.length).toBe(1)` to `expect(inputLatency.receiveLagSamples.length).toBe(0)` and adjust the mock count (markReceived no longer needs `recvTime` and `recvLagHandler` mocks).

### 2e. Commit

Commit using `/commit` with message: "refactor(diagnostics): move MessageChannel probe into recordServerSegments for per-tuple jsQueue"

---

## Step 3: Add `segmentSum` to `LatencyBreakdown` type

**File**: `assets/dashboard/src/lib/inputLatency.ts`

### 3a. Write failing test

Use the current `'p50'` API (it will be changed to `'typical'` in Step 4 along with all other tests):

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

Note: this test calls `getBreakdown('p50')` which still works at this point. Step 4 will change the API and update this test along with all others. The test uses 5 samples (not 3) to satisfy the new minimum that will be introduced in Step 4. At this step the minimum is still 3, so the test passes regardless.

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

Commit using `/commit` with message: "feat(diagnostics): add segmentSum field to LatencyBreakdown type"

---

## Step 4: Rewrite `getBreakdown()` with cohort-median computation + update all existing tests

**File**: `assets/dashboard/src/lib/inputLatency.ts`, `assets/dashboard/src/lib/inputLatency.test.ts`

This is the core statistical change and the largest step (~15-20 min). The method signature changes from `getBreakdown(level: 'p50' | 'p99')` to `getBreakdown(level: 'typical' | 'outlier')`. Existing tests are updated in the same step so that tests are never broken between commits.

### 4a. Write new tests and update existing tests

First, define the MockMessageChannel and addSamples helper at the top of the new `describe` block:

```typescript
describe('getBreakdown cohort-median', () => {
  // Shared MockMessageChannel — markSent fires a MessageChannel probe,
  // and recordServerSegments fires one too (after Step 2). Both must
  // resolve synchronously in tests.
  const originalMC = globalThis.MessageChannel;
  class MockMC {
    port1 = { onmessage: null as ((ev: any) => void) | null };
    port2 = {
      postMessage: () => {
        if (this.port1.onmessage) {
          this.port1.onmessage({} as any);
        }
      },
    };
  }

  beforeEach(() => {
    globalThis.MessageChannel = MockMC as any;
  });
  afterEach(() => {
    globalThis.MessageChannel = originalMC;
  });

  // Helper: adds `count` paired samples. Each iteration mocks:
  //   markSent:                3 calls (lastInputTime, sentTime, lagHandler)
  //   markReceived:            1 call  (rtt — no receive probe after Step 2)
  //   recordServerSegments:    2 calls (probeStart, probeHandler)
  // Total: 6 performance.now() calls per iteration.
  function addSamples(count: number, rttBase: number, rttJitter: number) {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < count; i++) {
      const rtt = rttBase + (i % 2 === 0 ? rttJitter : -rttJitter);
      // markSent: lastInputTime, sentTime, lagHandler
      mockNow.mockReturnValueOnce(100); // lastInputTime
      mockNow.mockReturnValueOnce(100); // sentTime
      mockNow.mockReturnValueOnce(101); // lagHandler
      // markReceived: rtt (no receive probe after Step 2)
      mockNow.mockReturnValueOnce(100 + rtt); // rtt
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      const serverTotal = rtt * 0.3;
      // recordServerSegments: probeStart, probeHandler
      mockNow.mockReturnValueOnce(200); // probeStart
      mockNow.mockReturnValueOnce(201); // probeHandler (1ms lag)
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
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(130);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      // recordServerSegments: 2 calls
      mockNow.mockReturnValueOnce(200);
      mockNow.mockReturnValueOnce(201);
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
    // 120 samples: mostly 30ms, 10 outliers at 100ms
    // 120 total => P95 index = floor(120*0.95) = 114
    // So P95+ cohort has 6 tuples (indices 114-119), above minimum of 5
    addSamples(110, 30, 3);
    addSamples(10, 100, 5);
    const breakdown = inputLatency.getBreakdown('outlier');
    expect(breakdown).not.toBeNull();
    // Outlier total should reflect the high-RTT samples
    expect(breakdown!.total).toBeGreaterThan(50);
  });

  it('returns null for outlier cohort when fewer than 5 P95+ tuples', () => {
    // 80 samples — P95 index = floor(80*0.95) = 76
    // P95+ is tuples with RTT > sortedRTTs[76], which is 4 tuples (indices 77-79)
    // 4 < 5 minimum, so outlier returns null
    addSamples(80, 30, 3);
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
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(105);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(0);
      // recordServerSegments: 2 calls
      mockNow.mockReturnValueOnce(200);
      mockNow.mockReturnValueOnce(201);
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
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(130);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      // recordServerSegments: 2 calls (probe will set receiveLag,
      // but we override it below with a known value)
      mockNow.mockReturnValueOnce(200);
      mockNow.mockReturnValueOnce(205); // probeHandler: 5ms lag
      inputLatency.recordServerSegments({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
      });
    }
    mockNow.mockRestore();

    const breakdown = inputLatency.getBreakdown('typical')!;
    // jsQueue should reflect the per-tuple receiveLag values (5ms from probe)
    expect(breakdown.jsQueue).toBe(5);
  });

  it('uses receiveLag 0 when per-tuple receiveLag is undefined', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 20; i++) {
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(130);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
    }
    mockNow.mockRestore();

    // Record server segments WITHOUT MockMessageChannel so receiveLag stays undefined
    // (the real MessageChannel handler fires asynchronously and won't run in this tick)
    globalThis.MessageChannel = originalMC;
    for (let i = 0; i < 20; i++) {
      inputLatency.recordServerSegments({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
      });
      // Manually clear receiveLag to simulate it never firing
      inputLatency.serverSegmentSamples[inputLatency.serverSegmentSamples.length - 1].receiveLag =
        undefined;
    }
    globalThis.MessageChannel = MockMC as any;

    const breakdown = inputLatency.getBreakdown('typical')!;
    // jsQueue should be 0 (not a fallback from lagSamples)
    expect(breakdown.jsQueue).toBe(0);
  });
});
```

Second, update the existing tests that use the old `'p50'`/`'p99'` API. The following tests must be updated or removed:

**Update** — change minimum from 3 to 5 and API from `'p50'` to `'typical'`:

- `getBreakdown returns null without enough paired segment samples` — change assertion to use `'typical'`
- `getBreakdown returns segments from the same keystroke (paired)` — change to `'typical'`, use 5 samples instead of 3
- `getBreakdown with lag samples subtracts jsQueue from network` — rewrite: per-tuple receiveLag replaces global lag computation
- `getBreakdown clamps network to zero when render exceeds infra budget` — change to `'typical'`, use 5 samples
- `getBreakdown discards mispaired samples where server > RTT` — change to `'typical'`, use 5 samples
- `getBreakdown returns segmentSum field` (from Step 3) — change to `'typical'`

**Remove** — these test behaviors that no longer exist:

- `getBreakdown picks the keystroke at the P50 rank` — single-tuple picking is gone
- `getBreakdown uses receiveLagSamples for jsQueue when available` — receiveLagSamples no longer used by getBreakdown
- `getBreakdown falls back to lagSamples when receiveLagSamples is empty` — fallback removed

### 4b. Run test to verify the new tests fail

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

    // Per design: receiveLag === undefined means 0 ("we don't know")
    const jsQueue = seg.receiveLag ?? 0;
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
const typical = inputLatency.getBreakdown('typical');
const outlier = inputLatency.getBreakdown('outlier');
```

### 4d. Run test to verify they pass

```bash
./test.sh --quick
```

### 4e. Commit

Commit using `/commit` with message: "feat(diagnostics): replace single-tuple picking with cohort-median breakdowns"

---

## Step 5: Clean up `receiveLagSamples`, update `getWireContext`, and update `TrackerSnapshot`

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

Update the `getWireContext` test to compute receiveLag from serverSegmentSamples:

```typescript
it('getWireContext returns correct P50/P99 values', () => {
  // No data → null
  expect(inputLatency.getWireContext()).toBeNull();

  // Add some samples
  inputLatency.framesBetweenSamples = [0, 2, 4, 6, 8, 10, 12, 14, 16, 18];
  inputLatency.handleOutputTimeSamples = [1, 1, 1, 1, 1, 2, 2, 2, 2, 10];
  // receiveLag now comes from serverSegmentSamples
  const lags = [0.5, 0.5, 0.5, 0.5, 0.5, 1, 1, 1, 1, 50];
  for (let i = 0; i < lags.length; i++) {
    inputLatency.serverSegmentSamples.push({
      dispatch: 1,
      sendKeys: 1,
      echo: 1,
      frameSend: 0.5,
      total: 3.5,
      receiveLag: lags[i],
    });
  }

  const ctx = inputLatency.getWireContext();
  expect(ctx).not.toBeNull();
  // With 10 samples: P50 = index 5, P99 = index 9
  expect(ctx!.framesBetweenP50).toBe(10);
  expect(ctx!.framesBetweenP99).toBe(18);
  expect(ctx!.handleOutputMsP50).toBe(2);
  expect(ctx!.handleOutputMsP99).toBe(10);
  // receiveLag sorted: [0.5,0.5,0.5,0.5,0.5,1,1,1,1,50] → P50=1, P99=50
  expect(ctx!.receiveLagP50).toBe(1);
  expect(ctx!.receiveLagP99).toBe(50);
});
```

### 5b. Run test to verify it fails

```bash
./test.sh --quick
```

### 5c. Write implementation

1. Remove `receiveLagSamples` field from the class declaration, from `TrackerSnapshot`, from `switchMachine()` (both save and restore branches), and from `reset()`.

2. The MessageChannel probe in `markReceived()` was already removed in Step 2.

3. Update `getWireContext()` to compute receiveLag stats from `serverSegmentSamples[].receiveLag`:

```typescript
getWireContext(): WireContext | null {
  const receiveLags = this.serverSegmentSamples
    .map(s => s.receiveLag)
    .filter((v): v is number => v !== undefined);
  if (
    this.framesBetweenSamples.length === 0 &&
    this.handleOutputTimeSamples.length === 0 &&
    receiveLags.length === 0
  ) {
    return null;
  }
  const fbStats = this.computeStats(this.framesBetweenSamples);
  const hoStats = this.computeStats(this.handleOutputTimeSamples);
  const rlStats = this.computeStats(receiveLags);
  return {
    framesBetweenP50: fbStats?.median ?? 0,
    framesBetweenP99: fbStats?.p99 ?? 0,
    handleOutputMsP50: hoStats?.median ?? 0,
    handleOutputMsP99: hoStats?.p99 ?? 0,
    receiveLagP50: rlStats?.median ?? 0,
    receiveLagP99: rlStats?.p99 ?? 0,
  };
}
```

4. Remove tests that reference `receiveLagSamples` directly:
   - `receive-time lag probe fires and records in receiveLagSamples` — remove entirely
   - `markReceived no longer fires a receive-time MessageChannel probe` (from Step 2) — this test checked `receiveLagSamples`; update to verify `markReceived` only calls `performance.now()` once (for rtt) by checking that no extra mocks are consumed
   - Any remaining references to `receiveLagSamples` in test assertions

### 5d. Run test to verify it passes

```bash
./test.sh --quick
```

### 5e. Commit

Commit using `/commit` with message: "refactor(diagnostics): remove receiveLagSamples array, jsQueue now per-tuple only"

---

## Step 6: Update segment display labels, colors, and ordering

**File**: `assets/dashboard/src/components/TypingPerformance.tsx`

Steps 6 and 7 can be done in parallel since they modify different parts of `TypingPerformance.tsx` (Step 6: the three constants; Step 7: the `BreakdownRow` and `LatencyBreakdownBars` functions).

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

Commit using `/commit` with message: "feat(diagnostics): rename segments and reorder to causal flow"

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
      {showTooltip && (
        <div className="typing-perf__tooltip" data-testid="breakdown-tooltip">
          {SEGMENTS.map((seg) => {
            const value = breakdown[seg as keyof LatencyBreakdown] as number;
            if (value == null || value < 0.05) return null;
            return (
              <div key={seg} className="typing-perf__tooltip-row">
                <span
                  className="typing-perf__tooltip-swatch"
                  style={{ backgroundColor: SEGMENT_COLORS[seg] }}
                />
                <span className="typing-perf__tooltip-name">{SEGMENT_LABELS[seg]}</span>
                <span className="typing-perf__tooltip-value">{value.toFixed(1)}ms</span>
              </div>
            );
          })}
        </div>
      )}
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

Commit using `/commit` with message: "feat(diagnostics): use segmentSum for bar widths, relabel to Typical/Outlier"

---

## Step 8: Update documentation

**File**: `docs/typing-performance.md`

### 8a. Update the following sections:

1. **Display Names table**: Update old display names to new ones (network->unmeasured, tmux cmd->transport, pane output->tmux + agent)
2. **Known Issues**: Mark #1 (single-tuple picking) as resolved. Mark #5 (stale lastInputTime) as resolved. Mark #6 (negative residual) as resolved.
3. **Timestamps and Segments**: Update to reflect that breakdowns now use cohort medians, not single-tuple picking

### 8b. Commit

Commit using `/commit` with message: "docs(diagnostics): update typing-performance.md for cohort-median redesign"

---

## Step 9: End-to-end verification

### 9a. Run full test suite

```bash
./test.sh
```

### 9b. Build dashboard and verify visually

```bash
go run ./cmd/build-dashboard
```

Start the daemon and navigate to a terminal session. Type in the terminal, collect ~50+ samples, and verify:

1. The breakdown shows "Typical" and "Outlier" labels instead of "P50" and "P99"
2. Segment labels read: handler, transport, tmux + agent, ws write, js queue, xterm, unmeasured
3. Colors are grouped: greens (schmux), grays (host), blues (browser)
4. Segments are in causal order (handler -> transport -> tmux + agent -> ws write -> js queue -> xterm -> unmeasured)
5. "Outlier" bar is wider than "Typical" bar
6. If fewer than ~100 samples, "Outlier: insufficient data" appears (P95+ cohort too small)
7. The "unmeasured" segment is small relative to other segments

---

## Task Dependencies

All steps are sequential except Steps 6 and 7, which modify different parts of `TypingPerformance.tsx` and can be parallelized.

| Step                                         | Depends on | Can parallelize with | Notes                                                                                                                      |
| -------------------------------------------- | ---------- | -------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Step 1 (staleness)                           | —          | —                    | Modifies `markReceived()` in inputLatency.ts                                                                               |
| Step 2 (move probe)                          | Step 1     | —                    | Modifies `markReceived()` and `recordServerSegments()` in inputLatency.ts                                                  |
| Step 3 (segmentSum type)                     | Step 2     | —                    | Modifies `LatencyBreakdown` type and `getBreakdown()` return in inputLatency.ts                                            |
| Step 4 (rewrite getBreakdown + update tests) | Step 3     | —                    | Largest step (~15-20 min). Rewrites `getBreakdown()`, updates all existing tests, updates TypingPerformance.tsx call sites |
| Step 5 (remove receiveLagSamples)            | Step 4     | —                    | Cleanup: removes field, updates `getWireContext()`, removes obsolete tests                                                 |
| Step 6 (labels/colors/ordering)              | Step 5     | Step 7               | Display-only changes to constants in TypingPerformance.tsx                                                                 |
| Step 7 (segmentSum bars + Typical/Outlier)   | Step 5     | Step 6               | Function changes in TypingPerformance.tsx                                                                                  |
| Step 8 (docs)                                | Steps 6, 7 | —                    | Documentation updates                                                                                                      |
| Step 9 (e2e verification)                    | Step 8     | —                    | Full test suite + visual verification                                                                                      |
