import { describe, it, expect, beforeEach } from 'vitest';
import { StreamDiagnostics } from './streamDiagnostics';

describe('StreamDiagnostics', () => {
  let diag: StreamDiagnostics;

  beforeEach(() => {
    diag = new StreamDiagnostics();
  });

  it('tracks frame count and byte count', () => {
    diag.recordFrame(new Uint8Array([1, 2, 3]));
    diag.recordFrame(new Uint8Array([4, 5]));
    expect(diag.framesReceived).toBe(2);
    expect(diag.bytesReceived).toBe(5);
  });

  it('tracks bootstrap count', () => {
    diag.recordBootstrap();
    diag.recordBootstrap();
    expect(diag.bootstrapCount).toBe(2);
  });

  it('ring buffer stores recent data', () => {
    diag.recordFrame(new TextEncoder().encode('hello'));
    diag.recordFrame(new TextEncoder().encode(' world'));
    const snapshot = new TextDecoder().decode(diag.ringBufferSnapshot());
    expect(snapshot).toContain('hello');
    expect(snapshot).toContain(' world');
  });

  it('ring buffer wraps around', () => {
    // Use a buffer large enough to hold timestamps but small enough to wrap
    const smallDiag = new StreamDiagnostics(64);
    // Write enough data to cause wrapping (each recordFrame adds ~24B timestamp + data)
    smallDiag.recordFrame(new TextEncoder().encode('abcdefghijklmnop')); // ~40B total
    smallDiag.recordFrame(new TextEncoder().encode('qrstuvwxyz')); // ~34B total, exceeds 64
    const snapshot = new TextDecoder().decode(smallDiag.ringBufferSnapshot());
    // After wrapping, the most recent data should be present
    expect(snapshot).toContain('qrstuvwxyz');
  });

  it('ring buffer snapshot contains timestamp markers', () => {
    diag.recordFrame(new TextEncoder().encode('hello'));
    const snapshot = new TextDecoder().decode(diag.ringBufferSnapshot());
    expect(snapshot).toContain('---');
    expect(snapshot).toContain('hello');
    // Timestamp format: HH:MM:SS.mmm
    expect(snapshot).toMatch(/---\s+\d{2}:\d{2}:\d{2}\.\d{3}\s+---/);
  });

  it('detects incomplete escape sequences at frame boundaries', () => {
    // Frame ending with partial CSI sequence
    diag.recordFrame(new TextEncoder().encode('hello\x1b['));
    expect(diag.sequenceBreaks).toBe(1);
    expect(diag.recentBreaks).toHaveLength(1);
    expect(diag.recentBreaks[0].frameIndex).toBe(1);
    expect(diag.recentBreaks[0].byteOffset).toBe(7);
    expect(diag.recentBreaks[0].tail).toBe('1b 5b');

    // Frame ending with complete sequence — no break
    diag.recordFrame(new TextEncoder().encode('hello\x1b[0m'));
    expect(diag.sequenceBreaks).toBe(1); // unchanged
    expect(diag.recentBreaks).toHaveLength(1); // unchanged
  });

  it('records break for bare ESC at end of frame', () => {
    diag.recordFrame(new TextEncoder().encode('data\x1b'));
    expect(diag.sequenceBreaks).toBe(1);
    expect(diag.recentBreaks).toHaveLength(1);
    expect(diag.recentBreaks[0].tail).toBe('1b');
  });

  it('records break for CSI with unterminated parameters', () => {
    // ESC [ 3 2 ; 1  — parameters present but no final letter
    diag.recordFrame(new TextEncoder().encode('text\x1b[32;1'));
    expect(diag.sequenceBreaks).toBe(1);
    expect(diag.recentBreaks[0].tail).toBe('1b 5b 33 32 3b 31');
  });

  it('does not record break for complete CSI followed by text', () => {
    // \x1b[7m followed by a space — sequence is complete, space is normal text
    diag.recordFrame(new TextEncoder().encode('data\x1b[7m '));
    expect(diag.sequenceBreaks).toBe(0);

    // \x1b[27m followed by space + CR CR LF
    diag.recordFrame(new TextEncoder().encode('data\x1b[27m \r\r\n'));
    expect(diag.sequenceBreaks).toBe(0);

    // \x1b[22m followed by CR CR LF
    diag.recordFrame(new TextEncoder().encode('data\x1b[22m\r\r\n'));
    expect(diag.sequenceBreaks).toBe(0);

    // \x1b[1C (cursor forward) followed by text
    diag.recordFrame(new TextEncoder().encode("data\x1b[1CReact'"));
    expect(diag.sequenceBreaks).toBe(0);
  });

  it('does not record break for frames without trailing ESC', () => {
    diag.recordFrame(new TextEncoder().encode('plain text'));
    expect(diag.sequenceBreaks).toBe(0);
    expect(diag.recentBreaks).toHaveLength(0);
  });

  it('does not record break for empty frames', () => {
    diag.recordFrame(new Uint8Array(0));
    expect(diag.sequenceBreaks).toBe(0);
    expect(diag.recentBreaks).toHaveLength(0);
  });

  it('accumulates byteOffset correctly across interleaved clean and broken frames', () => {
    // Frame 1: 10 bytes, clean
    diag.recordFrame(new Uint8Array(10));
    // Frame 2: 5 bytes, broken
    diag.recordFrame(new TextEncoder().encode('ab\x1b['));
    // Frame 3: 20 bytes, clean
    diag.recordFrame(new Uint8Array(20));
    // Frame 4: 3 bytes, broken
    diag.recordFrame(new TextEncoder().encode('z\x1b'));

    expect(diag.sequenceBreaks).toBe(2);
    expect(diag.recentBreaks).toHaveLength(2);

    // First break: after frame 1 (10B) + frame 2 (4B) = 14B total
    expect(diag.recentBreaks[0].frameIndex).toBe(2);
    expect(diag.recentBreaks[0].byteOffset).toBe(14);

    // Second break: 14B + frame 3 (20B) + frame 4 (2B) = 36B total
    expect(diag.recentBreaks[1].frameIndex).toBe(4);
    expect(diag.recentBreaks[1].byteOffset).toBe(36);
  });

  it('records fresh frameIndex and byteOffset after reset', () => {
    diag.recordFrame(new TextEncoder().encode('abc\x1b'));
    expect(diag.recentBreaks).toHaveLength(1);

    diag.reset();

    diag.recordFrame(new TextEncoder().encode('x\x1b['));
    expect(diag.recentBreaks).toHaveLength(1);
    // After reset, frameIndex restarts from 1 and byteOffset from frame size
    expect(diag.recentBreaks[0].frameIndex).toBe(1);
    expect(diag.recentBreaks[0].byteOffset).toBe(3);
  });

  it('reset clears all counters', () => {
    diag.recordFrame(new Uint8Array([1, 2, 3]));
    diag.recordBootstrap();
    diag.recordFrame(new TextEncoder().encode('test\x1b'));
    expect(diag.recentBreaks).toHaveLength(1);
    diag.reset();
    expect(diag.framesReceived).toBe(0);
    expect(diag.bytesReceived).toBe(0);
    expect(diag.bootstrapCount).toBe(0);
    expect(diag.sequenceBreaks).toBe(0);
    expect(diag.recentBreaks).toHaveLength(0);
    expect(diag.frameSizes).toHaveLength(0);
    expect(diag.gapsDetected).toBe(0);
    expect(diag.gapRequestsSent).toBe(0);
    expect(diag.gapFramesDeduped).toBe(0);
    expect(diag.gapReplayWritten).toBe(0);
    expect(diag.emptySeqFrames).toBe(0);
    expect(diag.lastReceivedSeq).toBe(-1n);
  });

  describe('gapSnapshot', () => {
    it('returns initial gap telemetry values', () => {
      const snapshot = diag.gapSnapshot();
      expect(snapshot).toEqual({
        bootstrapCount: 0,
        gapsDetected: 0,
        gapRequestsSent: 0,
        gapFramesDeduped: 0,
        gapReplayWritten: 0,
        emptySeqFrames: 0,
        lastReceivedSeq: '-1',
      });
    });

    it('reflects updated gap counters', () => {
      diag.gapsDetected = 3;
      diag.gapRequestsSent = 2;
      diag.gapFramesDeduped = 5;
      diag.gapReplayWritten = 1;
      diag.emptySeqFrames = 4;
      diag.lastReceivedSeq = 42n;
      const snapshot = diag.gapSnapshot();
      expect(snapshot).toEqual({
        bootstrapCount: 0,
        gapsDetected: 3,
        gapRequestsSent: 2,
        gapFramesDeduped: 5,
        gapReplayWritten: 1,
        emptySeqFrames: 4,
        lastReceivedSeq: '42',
      });
    });

    it('serializes lastReceivedSeq as string for large bigint values', () => {
      diag.lastReceivedSeq = 9007199254740993n; // Beyond Number.MAX_SAFE_INTEGER
      const snapshot = diag.gapSnapshot();
      expect(snapshot.lastReceivedSeq).toBe('9007199254740993');
    });

    it('resets gap counters correctly', () => {
      diag.gapsDetected = 5;
      diag.gapRequestsSent = 3;
      diag.gapFramesDeduped = 10;
      diag.gapReplayWritten = 2;
      diag.emptySeqFrames = 7;
      diag.lastReceivedSeq = 100n;
      diag.reset();
      const snapshot = diag.gapSnapshot();
      expect(snapshot).toEqual({
        bootstrapCount: 0,
        gapsDetected: 0,
        gapRequestsSent: 0,
        gapFramesDeduped: 0,
        gapReplayWritten: 0,
        emptySeqFrames: 0,
        lastReceivedSeq: '-1',
      });
    });
  });

  it('caps recentBreaks at 20 entries', () => {
    for (let i = 0; i < 25; i++) {
      diag.recordFrame(new TextEncoder().encode('x\x1b'));
    }
    expect(diag.sequenceBreaks).toBe(25);
    expect(diag.recentBreaks).toHaveLength(20);
    // First entry should be from frame 6 (frames 1-5 were shifted out)
    expect(diag.recentBreaks[0].frameIndex).toBe(6);
    expect(diag.recentBreaks[19].frameIndex).toBe(25);
  });

  describe('frame size tracking', () => {
    it('getFrameSizeStats returns correct P50 and P90', () => {
      // Record 10 frames of increasing size: 100, 200, ..., 1000
      for (let i = 1; i <= 10; i++) {
        diag.recordFrame(new Uint8Array(i * 100));
      }
      const stats = diag.getFrameSizeStats();
      expect(stats).not.toBeNull();
      expect(stats!.count).toBe(10);
      // Sorted: [100, 200, 300, 400, 500, 600, 700, 800, 900, 1000]
      // P50 = index floor(10/2) = index 5 = 600
      expect(stats!.median).toBe(600);
      // P90 = index floor(10*0.9) = index 9 = 1000
      expect(stats!.p90).toBe(1000);
      expect(stats!.max).toBe(1000);
    });

    it('getFrameSizeStats returns null when no frames recorded', () => {
      expect(diag.getFrameSizeStats()).toBeNull();
    });

    it('frame sizes cap at 5000 samples', () => {
      for (let i = 0; i < 5100; i++) {
        diag.recordFrame(new Uint8Array(i + 1));
      }
      expect(diag.frameSizes).toHaveLength(5000);
      // Oldest samples should have been dropped — first entry should be 101 (not 1)
      expect(diag.frameSizes[0]).toBe(101);
      expect(diag.frameSizes[4999]).toBe(5100);
    });

    it('reset clears frame sizes', () => {
      diag.recordFrame(new Uint8Array(50));
      diag.recordFrame(new Uint8Array(100));
      expect(diag.frameSizes).toHaveLength(2);
      diag.reset();
      expect(diag.frameSizes).toHaveLength(0);
      expect(diag.getFrameSizeStats()).toBeNull();
      expect(diag.getFrameSizeDistribution()).toBeNull();
    });

    it('getFrameSizeDistribution returns bucket data', () => {
      // Record some frames of different sizes
      for (let i = 0; i < 20; i++) {
        diag.recordFrame(new Uint8Array(50)); // 50 bytes
      }
      for (let i = 0; i < 5; i++) {
        diag.recordFrame(new Uint8Array(200)); // 200 bytes
      }
      const dist = diag.getFrameSizeDistribution();
      expect(dist).not.toBeNull();
      expect(dist!.buckets.length).toBeGreaterThan(0);
      expect(dist!.maxCount).toBeGreaterThan(0);
      expect(dist!.maxBytes).toBeGreaterThanOrEqual(64);
    });

    it('getFrameSizeDistribution returns null when no frames recorded', () => {
      expect(diag.getFrameSizeDistribution()).toBeNull();
    });
  });
});

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
