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
    const snapshot = diag.ringBufferSnapshot();
    expect(new TextDecoder().decode(snapshot)).toBe('hello world');
  });

  it('ring buffer wraps around', () => {
    const smallDiag = new StreamDiagnostics(8); // 8-byte ring buffer
    smallDiag.recordFrame(new TextEncoder().encode('abcdefgh'));
    smallDiag.recordFrame(new TextEncoder().encode('ij'));
    const snapshot = smallDiag.ringBufferSnapshot();
    expect(new TextDecoder().decode(snapshot)).toBe('cdefghij');
  });

  it('detects incomplete escape sequences at frame boundaries', () => {
    // Frame ending with partial CSI sequence
    diag.recordFrame(new TextEncoder().encode('hello\x1b['));
    expect(diag.sequenceBreaks).toBe(1);

    // Frame ending with complete sequence — no break
    diag.recordFrame(new TextEncoder().encode('hello\x1b[0m'));
    expect(diag.sequenceBreaks).toBe(1); // unchanged
  });

  it('reset clears all counters', () => {
    diag.recordFrame(new Uint8Array([1, 2, 3]));
    diag.recordBootstrap();
    diag.reset();
    expect(diag.framesReceived).toBe(0);
    expect(diag.bytesReceived).toBe(0);
    expect(diag.bootstrapCount).toBe(0);
    expect(diag.sequenceBreaks).toBe(0);
  });
});
