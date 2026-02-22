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
});
