# Xterm Scroll Diagnostics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add scroll state telemetry to the existing StreamDiagnostics system so diagnostic captures can identify why the Resume button appears without user scrolling.

**Architecture:** Extend `StreamDiagnostics` with scroll event ring buffer and counters. Instrument `setFollow`, `handleUserScroll`, `writeTerminal`, `fitTerminal`, and `jumpToBottom` in `TerminalStream`. Include scroll data in the existing diagnostic capture POST pipeline. Display counters in `StreamMetricsPanel`.

**Tech Stack:** TypeScript (Vitest tests), React, Go (handler), existing StreamDiagnostics/TerminalStream classes

**Spec:** `docs/specs/2026-03-19-xterm-scroll-diagnostics-design.md`

---

## File Structure

| File                                                          | Responsibility                                                                                                    |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/streamDiagnostics.ts`               | `ScrollDiagnosticEvent` type, scroll ring buffer, scroll counters, `scrollSnapshot()`                             |
| `assets/dashboard/src/lib/terminalStream.ts`                  | Emit diagnostic events at instrumentation points, include scroll data in capture POST, `recreationCount` property |
| `assets/dashboard/src/lib/streamDiagnostics.test.ts`          | Direct unit tests for scroll ring buffer, counters, snapshot, and reset                                           |
| `assets/dashboard/src/lib/terminalStream.test.ts`             | Integration tests for scroll diagnostic recording through TerminalStream                                          |
| `assets/dashboard/src/components/StreamMetricsPanel.tsx`      | Display new counters in pill bar and dropdown                                                                     |
| `assets/dashboard/src/components/StreamMetricsPanel.test.tsx` | UI tests for new counter rendering                                                                                |
| `assets/dashboard/src/routes/SessionDetailPage.tsx`           | `terminalRecreationCount` state, poll new counters, set `recreationCount` on stream                               |
| `internal/dashboard/handlers_diagnostic.go`                   | Accept and write `scrollEvents`, `scrollStats` fields                                                             |
| `docs/api.md`                                                 | Document `POST /api/dev/diagnostic-append` endpoint including new fields                                          |

---

### Task 1: Add ScrollDiagnosticEvent type and scroll fields to StreamDiagnostics

**Files:**

- Modify: `assets/dashboard/src/lib/streamDiagnostics.ts`

- [ ] **Step 1: Add the type and constant**

After line 3 (`const MAX_FRAME_SIZES = 5000;`), add:

```typescript
const MAX_SCROLL_EVENTS = 100;
```

After line 22 (after `FrameSizeDistribution` type), add:

```typescript
export type ScrollDiagnosticEvent = {
  ts: number;
  trigger: 'userScroll' | 'jumpToBottom' | undefined;
  followBefore: boolean;
  followAfter: boolean;
  writingToTerminal: boolean;
  scrollRAFPending: boolean;
  viewportY: number;
  baseY: number;
  lastReceivedSeq: string;
};
```

- [ ] **Step 2: Add scroll fields to the class**

After line 38 (`lastReceivedSeq: bigint = -1n;`), add:

```typescript
  // Scroll diagnostic telemetry
  scrollEvents: ScrollDiagnosticEvent[] = [];
  scrollCoalesceHits = 0;
  followLostCount = 0;
  scrollSuppressedCount = 0;
  resizeCount = 0;
  lastResizeTs = 0;
```

- [ ] **Step 3: Add recordScrollEvent method**

After the `recordBootstrap()` method (after line 65), add:

```typescript
  recordScrollEvent(event: ScrollDiagnosticEvent): void {
    this.scrollEvents.push(event);
    if (this.scrollEvents.length > MAX_SCROLL_EVENTS) {
      this.scrollEvents = this.scrollEvents.slice(-MAX_SCROLL_EVENTS);
    }
    if (event.followBefore && !event.followAfter) {
      this.followLostCount++;
    }
  }
```

- [ ] **Step 4: Add scrollSnapshot method**

After `recordScrollEvent`, add:

```typescript
  scrollSnapshot(): {
    events: ScrollDiagnosticEvent[];
    counters: {
      followLostCount: number;
      scrollSuppressedCount: number;
      scrollCoalesceHits: number;
      resizeCount: number;
      lastResizeTs: number;
    };
  } {
    return {
      events: [...this.scrollEvents],
      counters: {
        followLostCount: this.followLostCount,
        scrollSuppressedCount: this.scrollSuppressedCount,
        scrollCoalesceHits: this.scrollCoalesceHits,
        resizeCount: this.resizeCount,
        lastResizeTs: this.lastResizeTs,
      },
    };
  }
```

- [ ] **Step 5: Update reset() to clear scroll fields**

In `reset()` (currently lines 96-111), add before the closing brace:

```typescript
this.scrollEvents = [];
this.scrollCoalesceHits = 0;
this.followLostCount = 0;
this.scrollSuppressedCount = 0;
this.resizeCount = 0;
this.lastResizeTs = 0;
```

- [ ] **Step 6: Verify build**

Run: `./test.sh --quick`
Expected: PASS (no consumers yet, just new exports)

---

### Task 2: Instrument TerminalStream — setFollow, handleUserScroll, writeTerminal, jumpToBottom, fitTerminal

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts:889-938` (setFollow, handleUserScroll, writeTerminal, jumpToBottom)
- Modify: `assets/dashboard/src/lib/terminalStream.ts:421-445` (fitTerminal)
- Modify: `assets/dashboard/src/lib/terminalStream.ts:136-165` (add recreationCount property)

- [ ] **Step 1: Add recreationCount property**

After the `imagePasteHandler` field (line 114), add:

```typescript
// Terminal recreation count — set by SessionDetailPage, read during diagnostic capture
recreationCount = 0;
```

- [ ] **Step 2: Update setFollow signature and add diagnostic recording**

Replace `setFollow` (lines 889-893) with:

```typescript
  setFollow(follow: boolean, trigger?: 'userScroll' | 'jumpToBottom') {
    const before = this.followTail;
    this.followTail = follow;
    if (this.followCheckbox) this.followCheckbox.checked = follow;
    this.onResume(!follow);
    if (this.diagnostics && before !== follow) {
      this.diagnostics.recordScrollEvent({
        ts: Date.now(),
        trigger,
        followBefore: before,
        followAfter: follow,
        writingToTerminal: this.writingToTerminal,
        scrollRAFPending: this.scrollRAFPending,
        viewportY: this.terminal?.buffer.active.viewportY ?? -1,
        baseY: this.terminal?.buffer.active.baseY ?? -1,
        lastReceivedSeq: this.lastReceivedSeq.toString(),
      });
    }
  }
```

- [ ] **Step 3: Update handleUserScroll with suppression counter and trigger context**

Replace `handleUserScroll` (lines 928-931) with:

```typescript
  handleUserScroll() {
    if (!this.terminal) return;
    if (this.writingToTerminal) {
      if (this.diagnostics) this.diagnostics.scrollSuppressedCount++;
      return;
    }
    this.setFollow(this.isAtBottom(1), 'userScroll');
  }
```

- [ ] **Step 4: Update writeTerminal with coalescing counter**

Replace `writeTerminal` (lines 911-926) with:

```typescript
  private writeTerminal(data: string, cb?: () => void) {
    this.writingToTerminal = true;
    this.terminal!.write(data, () => {
      cb?.();
      if (!this.scrollRAFPending) {
        this.scrollRAFPending = true;
        requestAnimationFrame(() => {
          if (this.followTail) {
            this.terminal!.scrollToBottom();
          }
          this.scrollRAFPending = false;
          this.writingToTerminal = false;
        });
      } else {
        if (this.diagnostics) this.diagnostics.scrollCoalesceHits++;
      }
    });
  }
```

- [ ] **Step 5: Update jumpToBottom with trigger context**

Replace `jumpToBottom` (lines 933-938) with:

```typescript
  jumpToBottom() {
    if (this.terminal) {
      this.terminal.scrollToBottom();
      this.setFollow(true, 'jumpToBottom');
    }
  }
```

- [ ] **Step 6: Add resize counter to fitTerminal**

In `fitTerminal()` (line 421), add after `if (!measured) return;` (line 423):

```typescript
if (this.diagnostics) {
  this.diagnostics.resizeCount++;
  this.diagnostics.lastResizeTs = Date.now();
}
```

- [ ] **Step 7: Verify build**

Run: `./test.sh --quick`
Expected: PASS (existing tests should still pass — behavior unchanged)

---

### Task 3: Add scroll data to diagnostic capture POST

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts:598-613` (enableDiagnostics POST body)

- [ ] **Step 1: Add scroll fields to the POST body**

In `enableDiagnostics()`, in the `body: JSON.stringify({...})` call (lines 603-612), add after the `cursorXterm` field (line 612):

```typescript
            scrollEvents: this.diagnostics
              ? JSON.stringify(this.diagnostics.scrollSnapshot().events)
              : null,
            scrollStats: this.diagnostics
              ? JSON.stringify({
                  ...this.diagnostics.scrollSnapshot().counters,
                  recreationCount: this.recreationCount,
                })
              : null,
```

- [ ] **Step 2: Verify build**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 4: Update backend diagnostic-append handler

**Files:**

- Modify: `internal/dashboard/handlers_diagnostic.go:13-19` (request struct)
- Modify: `internal/dashboard/handlers_diagnostic.go:33-38` (file writes)

- [ ] **Step 1: Add fields to request struct**

In the `req` struct (lines 13-19), add after `CursorXterm`:

```go
		ScrollEvents string `json:"scrollEvents"`
		ScrollStats  string `json:"scrollStats"`
```

- [ ] **Step 2: Add file writes**

After the `cursorXterm` write block (line 38), add:

```go
	if req.ScrollEvents != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "scroll-events.json"), []byte(req.ScrollEvents), 0o644)
	}
	if req.ScrollStats != "" {
		os.WriteFile(filepath.Join(req.DiagDir, "scroll-stats.json"), []byte(req.ScrollStats), 0o644)
	}
```

- [ ] **Step 3: Verify Go builds**

Run: `go build ./cmd/schmux`
Expected: compiles without errors

---

### Task 5: Update SessionDetailPage — recreation counter and diagnostics polling

**Files:**

- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx:67-75` (frontendStats state type)
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx:157-234` (terminal effect)

- [ ] **Step 1: Add terminalRecreationCount state**

After line 77 (`const [ioWorkspaceStats, ...`), add:

```typescript
const [terminalRecreationCount, setTerminalRecreationCount] = useState(0);
```

- [ ] **Step 2: Extend frontendStats state type**

In the `frontendStats` state type (lines 67-75), add after the `frameSizeDist` field (line 74):

```typescript
    followLostCount?: number;
    scrollSuppressedCount?: number;
    scrollCoalesceHits?: number;
    resizeCount?: number;
```

- [ ] **Step 3: Increment recreation counter and set it on stream**

In the terminal effect (after line 180, `terminalStreamRef.current = terminalStream;`), add:

```typescript
setTerminalRecreationCount((prev) => {
  const next = prev + 1;
  terminalStream.recreationCount = next;
  return next;
});
```

- [ ] **Step 4: Add new counters to the diagnostics polling interval**

In the `setFrontendStats` call (lines 198-206), add after `frameSizeDist` (line 205):

```typescript
            followLostCount: diag.followLostCount,
            scrollSuppressedCount: diag.scrollSuppressedCount,
            scrollCoalesceHits: diag.scrollCoalesceHits,
            resizeCount: diag.resizeCount,
```

- [ ] **Step 5: Verify build**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 6: Update StreamMetricsPanel — display new counters

**Files:**

- Modify: `assets/dashboard/src/components/StreamMetricsPanel.tsx:18-26` (FrontendStats interface)
- Modify: `assets/dashboard/src/components/StreamMetricsPanel.tsx:46-84` (pill bar)
- Modify: `assets/dashboard/src/components/StreamMetricsPanel.tsx:315-322` (dropdown rows, before frame size histogram)

- [ ] **Step 1: Extend FrontendStats interface**

In the `FrontendStats` interface (lines 18-26), add after `frameSizeDist`:

```typescript
  followLostCount?: number;
  scrollSuppressedCount?: number;
  scrollCoalesceHits?: number;
  resizeCount?: number;
```

- [ ] **Step 2: Add followLost variable and pill bar indicator**

After line 56 (`const seqBreaks = ...`), add:

```typescript
const followLost = frontendStats?.followLostCount ?? 0;
```

In the `connection-pill` div, after the seq breaks span (line 84), add:

```tsx
{
  followLost > 0 && (
    <span className="warning" data-testid="follow-lost-pill">
      {followLost} follow lost
    </span>
  );
}
```

- [ ] **Step 3: Add counter rows in the expanded dropdown**

After the "Control mode reconnects" row (after line 322), add:

```tsx
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Follow lost (true→false)
                </td>
                <td
                  className={followLost > 0 ? 'warning' : ''}
                  style={{ padding: '2px 0', textAlign: 'right' }}
                >
                  {followLost}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Scroll suppressed
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {frontendStats?.scrollSuppressedCount ?? 0}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Write coalesce hits
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {frontendStats?.scrollCoalesceHits ?? 0}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Resizes
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {frontendStats?.resizeCount ?? 0}
                </td>
              </tr>
```

- [ ] **Step 4: Verify build**

Run: `./test.sh --quick`
Expected: PASS

---

### Task 7: Write streamDiagnostics direct unit tests

**Files:**

- Modify: `assets/dashboard/src/lib/streamDiagnostics.test.ts` (append new describe block)

- [ ] **Step 1: Add scroll diagnostics unit tests**

Append a new `describe` block at the end of the existing test file:

```typescript
describe('StreamDiagnostics scroll telemetry', () => {
  let diag: StreamDiagnostics;

  beforeEach(() => {
    diag = new StreamDiagnostics();
  });

  it('recordScrollEvent stores event and increments followLostCount on true→false', () => {
    diag.recordScrollEvent({
      ts: 1000,
      trigger: 'userScroll',
      followBefore: true,
      followAfter: false,
      writingToTerminal: false,
      scrollRAFPending: false,
      viewportY: 0,
      baseY: 10,
      lastReceivedSeq: '5',
    });

    expect(diag.scrollEvents).toHaveLength(1);
    expect(diag.followLostCount).toBe(1);
  });

  it('recordScrollEvent does not increment followLostCount on false→true', () => {
    diag.recordScrollEvent({
      ts: 1000,
      trigger: 'jumpToBottom',
      followBefore: false,
      followAfter: true,
      writingToTerminal: false,
      scrollRAFPending: false,
      viewportY: 10,
      baseY: 10,
      lastReceivedSeq: '5',
    });

    expect(diag.scrollEvents).toHaveLength(1);
    expect(diag.followLostCount).toBe(0);
  });

  it('caps scroll events at MAX_SCROLL_EVENTS (100)', () => {
    for (let i = 0; i < 110; i++) {
      diag.recordScrollEvent({
        ts: i,
        trigger: 'userScroll',
        followBefore: true,
        followAfter: false,
        writingToTerminal: false,
        scrollRAFPending: false,
        viewportY: 0,
        baseY: 10,
        lastReceivedSeq: String(i),
      });
    }

    expect(diag.scrollEvents).toHaveLength(100);
    // Oldest events trimmed — first event should be ts=10
    expect(diag.scrollEvents[0].ts).toBe(10);
  });

  it('scrollSnapshot returns a copy of events and all counters', () => {
    diag.recordScrollEvent({
      ts: 1000,
      trigger: 'userScroll',
      followBefore: true,
      followAfter: false,
      writingToTerminal: false,
      scrollRAFPending: false,
      viewportY: 0,
      baseY: 10,
      lastReceivedSeq: '5',
    });
    diag.scrollSuppressedCount = 3;
    diag.scrollCoalesceHits = 7;
    diag.resizeCount = 2;
    diag.lastResizeTs = 999;

    const snapshot = diag.scrollSnapshot();

    expect(snapshot.events).toHaveLength(1);
    expect(snapshot.counters).toEqual({
      followLostCount: 1,
      scrollSuppressedCount: 3,
      scrollCoalesceHits: 7,
      resizeCount: 2,
      lastResizeTs: 999,
    });

    // Verify snapshot is a copy (mutating original doesn't affect snapshot)
    diag.scrollEvents.push(diag.scrollEvents[0]);
    expect(snapshot.events).toHaveLength(1);
  });

  it('reset clears all scroll fields', () => {
    diag.recordScrollEvent({
      ts: 1000,
      trigger: 'userScroll',
      followBefore: true,
      followAfter: false,
      writingToTerminal: false,
      scrollRAFPending: false,
      viewportY: 0,
      baseY: 10,
      lastReceivedSeq: '5',
    });
    diag.scrollSuppressedCount = 10;
    diag.scrollCoalesceHits = 20;
    diag.resizeCount = 5;
    diag.lastResizeTs = 999;

    diag.reset();

    expect(diag.scrollEvents).toHaveLength(0);
    expect(diag.followLostCount).toBe(0);
    expect(diag.scrollSuppressedCount).toBe(0);
    expect(diag.scrollCoalesceHits).toBe(0);
    expect(diag.resizeCount).toBe(0);
    expect(diag.lastResizeTs).toBe(0);
  });
});
```

- [ ] **Step 2: Run tests**

Run: `./test.sh --quick`
Expected: All new tests PASS, all existing streamDiagnostics tests PASS

---

### Task 8: Write terminalStream scroll diagnostic tests

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.test.ts` (append new describe block)

- [ ] **Step 1: Add the scroll diagnostics test suite**

Append at end of file (before final closing, if any):

```typescript
describe('TerminalStream scroll diagnostics', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('records scroll event when handleUserScroll changes followTail', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    expect((stream as any).followTail).toBe(true);

    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    stream.handleUserScroll();

    expect((stream as any).followTail).toBe(false);
    expect(stream.diagnostics!.scrollEvents).toHaveLength(1);
    expect(stream.diagnostics!.scrollEvents[0]).toMatchObject({
      trigger: 'userScroll',
      followBefore: true,
      followAfter: false,
      writingToTerminal: false,
      viewportY: 0,
      baseY: 10,
    });
    expect(stream.diagnostics!.followLostCount).toBe(1);
  });

  it('increments scrollSuppressedCount when writingToTerminal is true', async () => {
    await stream.initialized;
    stream.enableDiagnostics();

    (stream as any).writingToTerminal = true;

    stream.handleUserScroll();

    expect((stream as any).followTail).toBe(true);
    expect(stream.diagnostics!.scrollSuppressedCount).toBe(1);
    expect(stream.diagnostics!.scrollEvents).toHaveLength(0);
  });

  it('increments scrollCoalesceHits when writeTerminal coalesces', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) cb();
    });

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // Bootstrap first
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    // First live frame: schedules rAF, scrollRAFPending = true
    stream.handleOutput(buildSeqFrame(1n, 'frame1'));
    // Second live frame: callback finds scrollRAFPending=true, increments counter
    stream.handleOutput(buildSeqFrame(2n, 'frame2'));

    expect(stream.diagnostics!.scrollCoalesceHits).toBeGreaterThanOrEqual(1);
  });

  it('caps scroll events ring buffer at 100', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    for (let i = 0; i < 110; i++) {
      (stream as any).followTail = i % 2 === 0;
      (terminal.buffer.active as any).viewportY = i % 2 === 0 ? 0 : 10;
      (terminal.buffer.active as any).baseY = 10;
      stream.handleUserScroll();
    }

    expect(stream.diagnostics!.scrollEvents.length).toBeLessThanOrEqual(100);
  });

  it('follow state changes work correctly when diagnostics are disabled', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    expect(stream.diagnostics).toBeNull();

    // Verify follow state still transitions without diagnostics
    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    expect(() => stream.handleUserScroll()).not.toThrow();
    expect((stream as any).followTail).toBe(false);

    // Verify jumpToBottom recovery also works without diagnostics
    stream.jumpToBottom();
    expect((stream as any).followTail).toBe(true);
  });

  it('records jumpToBottom recovery event', async () => {
    await stream.initialized;
    stream.enableDiagnostics();

    (stream as any).followTail = false;

    stream.jumpToBottom();

    expect((stream as any).followTail).toBe(true);
    expect(stream.diagnostics!.scrollEvents).toHaveLength(1);
    expect(stream.diagnostics!.scrollEvents[0]).toMatchObject({
      trigger: 'jumpToBottom',
      followBefore: false,
      followAfter: true,
    });
  });

  it('scrollSnapshot returns events and counters', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;
    stream.handleUserScroll();

    (stream as any).writingToTerminal = true;
    (stream as any).followTail = true;
    stream.handleUserScroll();
    (stream as any).writingToTerminal = false;

    const snapshot = stream.diagnostics!.scrollSnapshot();
    expect(snapshot.events).toHaveLength(1);
    expect(snapshot.counters.followLostCount).toBe(1);
    expect(snapshot.counters.scrollSuppressedCount).toBe(1);
    expect(snapshot.counters).toHaveProperty('resizeCount');
    expect(snapshot.counters).toHaveProperty('lastResizeTs');
  });

  it('reset clears scroll events and counters', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;
    stream.handleUserScroll();

    expect(stream.diagnostics!.scrollEvents).toHaveLength(1);
    expect(stream.diagnostics!.followLostCount).toBe(1);

    stream.diagnostics!.reset();

    expect(stream.diagnostics!.scrollEvents).toHaveLength(0);
    expect(stream.diagnostics!.followLostCount).toBe(0);
    expect(stream.diagnostics!.scrollSuppressedCount).toBe(0);
    expect(stream.diagnostics!.scrollCoalesceHits).toBe(0);
    expect(stream.diagnostics!.resizeCount).toBe(0);
    expect(stream.diagnostics!.lastResizeTs).toBe(0);
  });
});
```

- [ ] **Step 2: Run tests**

Run: `./test.sh --quick`
Expected: All new tests PASS, all existing tests PASS

---

### Task 9: Write StreamMetricsPanel UI tests

**Files:**

- Modify: `assets/dashboard/src/components/StreamMetricsPanel.test.tsx` (append new tests)

- [ ] **Step 1: Add follow-lost pill bar tests**

Append to the existing `describe('StreamMetricsPanel', ...)` block:

```typescript
  it('shows follow-lost warning in pill when followLostCount > 0', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 50,
          bytesReceived: 25000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
          followLostCount: 3,
        }}
      />
    );
    expect(screen.getByTestId('follow-lost-pill')).toBeTruthy();
    expect(screen.getByText(/3 follow lost/)).toBeTruthy();
  });

  it('does not show follow-lost pill when followLostCount is 0', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 50,
          bytesReceived: 25000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
          followLostCount: 0,
        }}
      />
    );
    expect(screen.queryByTestId('follow-lost-pill')).toBeNull();
  });

  it('renders scroll diagnostic counters in expanded dropdown', () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 0,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 50,
          bytesReceived: 25000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
          followLostCount: 2,
          scrollSuppressedCount: 150,
          scrollCoalesceHits: 42,
          resizeCount: 5,
        }}
      />
    );

    // Expand the dropdown
    fireEvent.click(screen.getByText(/50 frames/));

    expect(screen.getByText(/Follow lost/)).toBeTruthy();
    expect(screen.getByText(/Scroll suppressed/)).toBeTruthy();
    expect(screen.getByText(/Write coalesce hits/)).toBeTruthy();
    expect(screen.getByText(/Resizes/)).toBeTruthy();
  });
```

- [ ] **Step 2: Run tests**

Run: `./test.sh --quick`
Expected: All tests PASS

---

### Task 10: Document diagnostic-append endpoint in api.md

**Files:**

- Modify: `docs/api.md`

- [ ] **Step 1: Add endpoint documentation**

Find the dev endpoints section (near `POST /api/dev/rebuild` around line 3148). Add after the rebuild endpoint documentation:

````markdown
### POST /api/dev/diagnostic-append

Receives frontend diagnostic artifacts and writes them to an existing diagnostic directory created by the WebSocket diagnostic handler. Dev mode only.

**Request body:**

```json
{
  "diagDir": "/path/to/.schmux/diagnostics/2026-03-19T...",
  "xtermScreen": "...",
  "screenDiff": "...",
  "ringBufferFrontend": "...",
  "gapStats": "{...}",
  "cursorXterm": "{...}",
  "scrollEvents": "[{...}, ...]",
  "scrollStats": "{...}"
}
```
````

All fields are optional strings except `diagDir` (required). Files are written best-effort (write errors are ignored).

**Files written to `diagDir`:**

- `screen-xterm.txt` — xterm.js visible viewport text
- `screen-diff.txt` — diff between tmux and xterm screens
- `ringbuffer-frontend.txt` — raw frame ring buffer
- `gap-stats.json` — gap detection counters
- `cursor-xterm.json` — xterm cursor position
- `scroll-events.json` — scroll state transition ring buffer (last 100 events)
- `scroll-stats.json` — scroll diagnostic counters (followLostCount, scrollSuppressedCount, scrollCoalesceHits, resizeCount, lastResizeTs, recreationCount)

**Response:** `200 OK` (empty body)

```

- [ ] **Step 2: Verify CI gate would pass**

Run: `git diff --name-only HEAD | head -20`
Expected: `docs/api.md` is in the changed files alongside `internal/dashboard/handlers_diagnostic.go`

---

### Task 11: Full verification

- [ ] **Step 1: Format code**

Run: `./format.sh`
Expected: exit code 0 or 2 (both are success)

- [ ] **Step 2: Run full test suite**

Run: `./test.sh`
Expected: ALL tests pass

- [ ] **Step 3: Build dashboard**

Run: `go run ./cmd/build-dashboard`
Expected: builds successfully

- [ ] **Step 4: Build Go binary**

Run: `go build ./cmd/schmux`
Expected: compiles without errors

- [ ] **Step 5: Peer review**

Dispatch a code review agent with the full diff. The review should verify:
- Instrumentation only — no behavioral changes to scroll suppression
- All diagnostics gated on `if (this.diagnostics)`
- Data flows correctly: StreamDiagnostics → capture POST → backend files
- Test coverage for all new counters and events
- `docs/api.md` updated
```
