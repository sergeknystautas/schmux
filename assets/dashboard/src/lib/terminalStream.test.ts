import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock xterm and addons before importing TerminalStream
vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    loadAddon = vi.fn();
    open = vi.fn();
    onData = vi.fn();
    onRender = vi.fn();
    onScroll = vi.fn();
    writeln = vi.fn();
    write = vi.fn();
    clear = vi.fn();
    reset = vi.fn();
    resize = vi.fn();
    focus = vi.fn();
    scrollToBottom = vi.fn();
    dispose = vi.fn();
    element = null;
    buffer = { active: { viewportY: 0, baseY: 0, cursorY: 0, length: 0 } };
    rows = 24;
    cols = 80;
    markers = [];
    unicode = { activeVersion: '6' };
    parser = {
      registerCsiHandler: vi.fn(() => ({ dispose: vi.fn() })),
      registerEscHandler: vi.fn(() => ({ dispose: vi.fn() })),
    };
    onWriteParsed = vi.fn();
  }
  return { Terminal: MockTerminal };
});
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: vi.fn() }));
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn() }));
vi.mock('@xterm/addon-webgl', () => ({
  WebglAddon: vi.fn().mockImplementation(() => ({
    onContextLoss: vi.fn(),
    dispose: vi.fn(),
  })),
}));

import TerminalStream from './terminalStream';
import { inputLatency } from './inputLatency';

/** Build a sequenced binary frame with 8-byte big-endian header. */
function buildSeqFrame(seq: bigint, text: string): ArrayBuffer {
  const encoded = new TextEncoder().encode(text);
  const buf = new ArrayBuffer(8 + encoded.length);
  new DataView(buf).setBigUint64(0, seq, false);
  new Uint8Array(buf, 8).set(encoded);
  return buf;
}

describe('TerminalStream.handleOutput', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    // Create a container element stub
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('dispatches controlMode attached=true to onControlModeChange', async () => {
    await stream.initialized;
    const handler = vi.fn();
    stream.onControlModeChange = handler;

    stream.handleOutput(JSON.stringify({ type: 'controlMode', attached: true }));

    expect(handler).toHaveBeenCalledWith(true);
  });

  it('dispatches controlMode attached=false to onControlModeChange', async () => {
    await stream.initialized;
    const handler = vi.fn();
    stream.onControlModeChange = handler;

    stream.handleOutput(JSON.stringify({ type: 'controlMode', attached: false }));

    expect(handler).toHaveBeenCalledWith(false);
  });

  it('does not throw when onControlModeChange is not set', async () => {
    await stream.initialized;
    stream.onControlModeChange = null;

    expect(() => {
      stream.handleOutput(JSON.stringify({ type: 'controlMode', attached: false }));
    }).not.toThrow();
  });

  it('dispatches stats messages to onStatsUpdate', async () => {
    await stream.initialized;
    const handler = vi.fn();
    stream.onStatsUpdate = handler;

    const statsMsg = { type: 'stats', eventsDelivered: 10 };
    stream.handleOutput(JSON.stringify(statsMsg));

    expect(handler).toHaveBeenCalledWith(expect.objectContaining({ type: 'stats' }));
  });

  it('dispatches diagnostic messages to onDiagnosticResponse', async () => {
    await stream.initialized;
    const handler = vi.fn();
    stream.onDiagnosticResponse = handler;

    const diagMsg = { type: 'diagnostic', diagDir: '/tmp/diag', verdict: 'ok', findings: [] };
    stream.handleOutput(JSON.stringify(diagMsg));

    expect(handler).toHaveBeenCalledWith(expect.objectContaining({ type: 'diagnostic' }));
  });
});

describe('TerminalStream sync handling', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('discards sync messages received within 2s of binary data', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate binary data arriving (bootstrap)
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    // Immediately send sync message
    const syncMsg = {
      type: 'sync',
      screen: 'different content',
      cursor: { row: 0, col: 0, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // terminal.reset should have been called only once (bootstrap), not again for sync
    expect(terminal.reset).toHaveBeenCalledTimes(1);
  });

  it('applies surgical correction when content mismatches (no reset)', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    // Clear mock call counts so we only measure sync behavior
    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    // Manually set lastBinaryTime to be old (>500ms ago)
    (stream as any).lastBinaryTime = Date.now() - 3000;

    // Mock the buffer to return specific lines
    const mockLine = { translateToString: () => 'wrong content' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorY: 0,
        length: 1,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 1;

    // Send sync with different content
    const syncMsg = {
      type: 'sync',
      screen: '\x1b[1mcorrect content\x1b[0m',
      cursor: { row: 0, col: 0, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // CRITICAL: reset must NOT be called from sync path
    expect(terminal.reset).not.toHaveBeenCalled();

    // write should be called with surgical correction (contains DECSC save cursor)
    expect(terminal.write).toHaveBeenCalledWith(
      expect.stringContaining('\x1b7'), // DECSC
      expect.any(Function) // write callback
    );
  });

  it('does not correct when content matches', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    // Clear mock call counts so we only measure sync behavior
    vi.mocked(terminal.reset).mockClear();

    (stream as any).lastBinaryTime = Date.now() - 3000;

    // Mock buffer to return matching content
    const mockLine = { translateToString: () => 'hello world' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorY: 0,
        length: 1,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 1;

    const syncMsg = {
      type: 'sync',
      screen: 'hello world',
      cursor: { row: 0, col: 0, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // reset should NOT have been called for sync (content matches)
    expect(terminal.reset).toHaveBeenCalledTimes(0);
  });

  it('forced sync bypasses activity guard and applies correction', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate binary data arriving (bootstrap)
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    // Clear mock call counts so we only measure sync behavior
    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    // lastBinaryTime is very recent (within 500ms guard window)
    // A normal sync would be skipped, but forced should not be

    // Mock the buffer to return specific lines
    const mockLine = { translateToString: () => 'wrong content' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorY: 0,
        length: 1,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 1;

    // Send forced sync with different content
    const syncMsg = {
      type: 'sync',
      screen: '\x1b[1mcorrect content\x1b[0m',
      cursor: { row: 0, col: 0, visible: true },
      forced: true,
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // Should have applied surgical correction despite recent binary data (no reset)
    expect(terminal.reset).not.toHaveBeenCalled();
    expect(terminal.write).toHaveBeenCalledWith(
      expect.stringContaining('\x1b7'), // DECSC
      expect.any(Function)
    );
  });

  it('corrects cursor position when content matches but cursor differs', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    (stream as any).lastBinaryTime = Date.now() - 3000;

    // Mock buffer: content matches sync, but cursor is at wrong position
    const mockLine = { translateToString: () => 'hello world' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorX: 5,
        cursorY: 0,
        length: 1,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 1;

    // Send sync: content matches ('hello world') but cursor is at row 3, col 10
    const syncMsg = {
      type: 'sync',
      screen: 'hello world',
      cursor: { row: 3, col: 10, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // Should write cursor correction (CUP to row 4, col 11)
    expect(terminal.write).toHaveBeenCalledWith(
      expect.stringContaining('\x1b[4;11H'),
      expect.any(Function)
    );
    // Should NOT do a full surgical correction (no DECSC)
    expect(terminal.write).not.toHaveBeenCalledWith(
      expect.stringContaining('\x1b7'),
      expect.any(Function)
    );
  });

  it('does not write anything when content and cursor both match', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));

    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    (stream as any).lastBinaryTime = Date.now() - 3000;

    // Mock buffer: content AND cursor match sync
    const mockLine = { translateToString: () => 'hello world' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorX: 10,
        cursorY: 3,
        length: 1,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 1;

    const syncMsg = {
      type: 'sync',
      screen: 'hello world',
      cursor: { row: 3, col: 10, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // Should NOT write anything — both content and cursor match
    expect(terminal.write).not.toHaveBeenCalled();
  });
});

describe('TerminalStream sequenced frames', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('parses sequence number from binary frame header', async () => {
    await stream.initialized;

    // Build a frame: 8 bytes big-endian seq=42 + "hello"
    const buf = new ArrayBuffer(8 + 5);
    const view = new DataView(buf);
    view.setBigUint64(0, 42n, false); // big-endian
    new Uint8Array(buf, 8).set(new TextEncoder().encode('hello'));

    stream.handleOutput(buf);

    expect((stream as any).lastReceivedSeq).toBe(42n);
  });

  it('tracks lastReceivedSeq across multiple frames', async () => {
    await stream.initialized;

    // Send bootstrap (seq=5)
    const buf1 = new ArrayBuffer(8 + 1);
    new DataView(buf1).setBigUint64(0, 5n, false);
    new Uint8Array(buf1, 8).set(new TextEncoder().encode('a'));
    stream.handleOutput(buf1);

    // Send live frame (seq=6)
    const buf2 = new ArrayBuffer(8 + 1);
    new DataView(buf2).setBigUint64(0, 6n, false);
    new Uint8Array(buf2, 8).set(new TextEncoder().encode('b'));
    stream.handleOutput(buf2);

    expect((stream as any).lastReceivedSeq).toBe(6n);
  });
});

describe('TerminalStream bootstrap write chaining', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('does not call scrollToBottom after writes — viewport sync handles it', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Clear any scrollToBottom calls from initialization (fitTerminalSync)
    vi.mocked(terminal.scrollToBottom).mockClear();

    // Track write callback invocations
    const writeCallbacks: (() => void)[] = [];
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) writeCallbacks.push(cb);
    });

    // Capture rAF callbacks so we can trigger them manually
    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // Send bootstrap frame (seq=0)
    stream.handleOutput(buildSeqFrame(0n, 'chunk1'));

    // Simulate xterm.js completing the write
    writeCallbacks[0]();

    // Fire all rAFs
    rafCallbacks.forEach((cb) => cb(0));

    // scrollToBottom should NOT be called — xterm's Viewport._sync already
    // positions the viewport at buffer.ydisp during parse (Fix 1).
    expect(terminal.scrollToBottom).not.toHaveBeenCalled();
  });

  it('does not call scrollToBottom on live frames either', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Bootstrap first (to set bootstrapped=true)
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    vi.mocked(terminal.scrollToBottom).mockClear();

    // Track write callbacks
    const writeCallbacks: (() => void)[] = [];
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) writeCallbacks.push(cb);
    });

    // Capture rAF callbacks
    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // Send live frame (writeLiveFrame defers to rAF)
    stream.handleOutput(buildSeqFrame(1n, 'live-data'));

    // Fire the write-coalescing rAF — this calls writeTerminal, which calls terminal.write
    rafCallbacks.shift()!(0);

    // Simulate write completion
    writeCallbacks[0]();

    // Fire remaining rAFs
    rafCallbacks.forEach((cb) => cb(0));

    // scrollToBottom should NOT be called (Fix 1)
    expect(terminal.scrollToBottom).not.toHaveBeenCalled();
  });
});

describe('TerminalStream gap detection', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('detects gap when frame seq jumps', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();

    // Mock WebSocket
    (stream as any).ws = { readyState: 1, send: wsSendSpy }; // WebSocket.OPEN = 1

    // Send seq 0 (bootstrap)
    stream.handleOutput(buildSeqFrame(0n, 'A'));

    // Signal bootstrap complete (gap detection only enabled after this)
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Send seq 5 (gap: 1,2,3,4 missing)
    stream.handleOutput(buildSeqFrame(5n, 'B'));

    // Should have sent a gap message
    expect(wsSendSpy).toHaveBeenCalledWith(expect.stringContaining('"type":"gap"'));
  });

  it('sends correct fromSeq in gap request', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();

    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    // Send seq 0 (bootstrap)
    stream.handleOutput(buildSeqFrame(0n, 'A'));

    // Signal bootstrap complete
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Send seq 3 (gap: 1,2 missing — expected next is 1)
    stream.handleOutput(buildSeqFrame(3n, 'B'));

    // Verify the gap message contains fromSeq = "1" (lastReceivedSeq was 0, expected next = 1)
    const gapCall = wsSendSpy.mock.calls.find(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('"type":"gap"')
    );
    expect(gapCall).toBeDefined();
    const parsed = JSON.parse(gapCall![0]);
    const gapData = JSON.parse(parsed.data);
    expect(gapData.fromSeq).toBe('1');
  });

  it('does not send gap message when seq is sequential', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();

    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    // Bootstrap frame
    stream.handleOutput(buildSeqFrame(0n, 'A'));

    // Signal bootstrap complete
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Sequential frames: 1, 2
    stream.handleOutput(buildSeqFrame(1n, 'B'));
    stream.handleOutput(buildSeqFrame(2n, 'C'));

    // Should NOT have sent any gap message
    const gapCalls = wsSendSpy.mock.calls.filter(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('"type":"gap"')
    );
    expect(gapCalls.length).toBe(0);
  });

  it('resets state on reconnection and detects gaps correctly', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();

    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    // First connection: send seq 0, 1, 2
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));
    stream.handleOutput(buildSeqFrame(1n, 'B'));
    stream.handleOutput(buildSeqFrame(2n, 'C'));

    // Simulate reconnection — reset state like connect() does
    (stream as any).bootstrapped = false;
    (stream as any).bootstrapComplete = false;
    (stream as any).lastReceivedSeq = -1n;
    (stream as any).utf8Decoder = new TextDecoder();

    // New connection: bootstrap starts at seq 0 again
    stream.handleOutput(buildSeqFrame(0n, 'X'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Sequential frame after new bootstrap — should NOT trigger gap
    stream.handleOutput(buildSeqFrame(1n, 'Y'));

    // No gap messages should have been sent after reconnection
    const gapCalls = wsSendSpy.mock.calls.filter(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('"type":"gap"')
    );
    expect(gapCalls.length).toBe(0);
  });
});

describe('TerminalStream binary frame edge cases', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('handles header-only (empty data) binary frame', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Build an 8-byte frame with no data payload
    const buf = new ArrayBuffer(8);
    new DataView(buf).setBigUint64(0, 0n, false);

    // Should not throw
    expect(() => {
      stream.handleOutput(buf);
    }).not.toThrow();

    // lastReceivedSeq should be updated
    expect((stream as any).lastReceivedSeq).toBe(0n);

    // terminal.write should be called with empty string
    expect(terminal.write).toHaveBeenCalled();
  });

  it('handles UTF-8 character split across binary frames', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Capture rAF callbacks to flush write coalescing
    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // "é" is U+00E9, encoded as UTF-8: [0xC3, 0xA9]
    // Split across two frames

    // Frame 1: first byte of é
    const buf1 = new ArrayBuffer(8 + 1);
    new DataView(buf1).setBigUint64(0, 0n, false);
    new Uint8Array(buf1, 8).set([0xc3]);
    stream.handleOutput(buf1);

    // Frame 2: second byte of é (coalesced into same write batch)
    const buf2 = new ArrayBuffer(8 + 1);
    new DataView(buf2).setBigUint64(0, 1n, false);
    new Uint8Array(buf2, 8).set([0xa9]);
    stream.handleOutput(buf2);

    // Flush the write-coalescing rAF
    rafCallbacks.forEach((cb) => cb(0));

    // The streaming TextDecoder should have decoded "é" across the two frames
    const writeCalls = vi.mocked(terminal.write).mock.calls;
    const allText = writeCalls.map((c: any[]) => c[0]).join('');
    expect(allText).toContain('é');
  });
});

describe('TerminalStream sync dimension-mismatch skip', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('skips sync when dimensions mismatch (resize race)', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    // Set lastBinaryTime to old (past activity guard)
    (stream as any).lastBinaryTime = Date.now() - 3000;

    // Mock buffer with 24 rows
    const mockLine = { translateToString: () => 'line content' };
    (terminal as any).buffer = {
      active: {
        viewportY: 0,
        baseY: 0,
        cursorY: 0,
        length: 24,
        getLine: () => mockLine,
      },
    };
    (terminal as any).rows = 24;

    // Send sync with 40 rows — dimension mismatch should cause skip
    const syncLines = Array(40).fill('line content').join('\n');
    const syncMsg = {
      type: 'sync',
      screen: syncLines,
      cursor: { row: 0, col: 0, visible: true },
    };
    stream.handleOutput(JSON.stringify(syncMsg));

    // Neither reset nor write should be called (skip path)
    expect(terminal.reset).not.toHaveBeenCalled();
    // write may or may not be called for syncResult, but no surgical correction
    // The key is no DECSC sequence in any write call
    const surgicalCalls = vi
      .mocked(terminal.write)
      .mock.calls.filter((c: any[]) => typeof c[0] === 'string' && c[0].includes('\x1b7'));
    expect(surgicalCalls.length).toBe(0);
  });
});

describe('TerminalStream.setupResizeHandler', () => {
  let observedElements: Element[];

  beforeEach(() => {
    observedElements = [];
    // ResizeObserver must be a class (used with `new`)
    class MockResizeObserver {
      constructor(_cb: ResizeObserverCallback) {}
      observe(el: Element) {
        observedElements.push(el);
      }
      disconnect() {}
      unobserve() {}
    }
    vi.stubGlobal('ResizeObserver', MockResizeObserver);
  });

  it('observes .session-detail parent when present', async () => {
    // Create DOM: .session-detail > .session-detail__main > container
    const sessionDetail = document.createElement('div');
    sessionDetail.className = 'session-detail';
    const main = document.createElement('div');
    main.className = 'session-detail__main';
    const container = document.createElement('div');

    sessionDetail.appendChild(main);
    main.appendChild(container);

    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });

    const stream = new TerminalStream('test-session', container);
    await stream.initialized;

    // Should observe both the container and the .session-detail ancestor
    expect(observedElements).toContain(container);
    expect(observedElements).toContain(sessionDetail);
  });

  it('falls back to parentElement when .session-detail is absent', async () => {
    // Create DOM: parent > container (no .session-detail)
    const parent = document.createElement('div');
    parent.className = 'fm-terminal-column';
    const container = document.createElement('div');
    parent.appendChild(container);

    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });

    const stream = new TerminalStream('test-session', container);
    await stream.initialized;

    // Should observe both the container and its parent
    expect(observedElements).toContain(container);
    expect(observedElements).toContain(parent);
  });

  it('does not observe parent when container has no parent', async () => {
    // Standalone container with no parent
    const container = document.createElement('div');

    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });

    const stream = new TerminalStream('test-session', container);
    await stream.initialized;

    // Should only observe the container itself
    expect(observedElements).toEqual([container]);
  });
});

describe('TerminalStream replay dedup', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('skips replay frames with seq <= lastReceivedSeq', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Receive frames 1..5
    for (let i = 1n; i <= 5n; i++) {
      stream.handleOutput(buildSeqFrame(i, `msg${i}`));
    }

    // Clear write mock to track only replay writes
    vi.mocked(terminal.write).mockClear();

    // Simulate replay frames 3,4,5 (overlap with already-received)
    stream.handleOutput(buildSeqFrame(3n, 'msg3-replay'));
    stream.handleOutput(buildSeqFrame(4n, 'msg4-replay'));
    stream.handleOutput(buildSeqFrame(5n, 'msg5-replay'));

    // None of the replay frames should have been written (all seq <= lastReceivedSeq=5)
    expect(terminal.write).not.toHaveBeenCalled();
  });

  it('processes replay frames with seq > lastReceivedSeq', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // Bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Receive frame 1
    stream.handleOutput(buildSeqFrame(1n, 'B'));

    // Flush all pending rAFs (bootstrap scroll + live write coalescing)
    while (rafCallbacks.length) rafCallbacks.shift()!(0);

    vi.mocked(terminal.write).mockClear();

    // Frame 3 arrives (gap: 2 missing)
    stream.handleOutput(buildSeqFrame(3n, 'D'));

    // Flush write-coalescing rAF
    while (rafCallbacks.length) rafCallbacks.shift()!(0);

    expect(terminal.write).toHaveBeenCalled();
    expect((stream as any).lastReceivedSeq).toBe(3n);
  });
});

describe('TerminalStream first event after bootstrap', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('does not drop the first live event when its seq equals bootstrapSeq', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Bootstrap frame with seq=41 (backend uses CurrentSeq()-1 to avoid
    // colliding with the first live event's seq).
    stream.handleOutput(buildSeqFrame(41n, 'bootstrap content'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    vi.mocked(terminal.write).mockClear();

    // First live event has seq=42 (OutputLog.Append() assigns CurrentSeq()=42).
    // This must NOT be dropped — it's the echo of the user's first keystroke.
    stream.handleOutput(buildSeqFrame(42n, 'first keystroke echo'));

    // Flush write-coalescing rAF
    rafCallbacks.forEach((cb) => cb(0));

    expect(terminal.write).toHaveBeenCalled();
  });
});

describe('TerminalStream gap debouncing', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('sends only one gap request for consecutive gaps', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();
    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    // Bootstrap + complete
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    wsSendSpy.mockClear();

    // Frame seq=5 (gap: 1,2,3,4 missing) — triggers first gap request
    stream.handleOutput(buildSeqFrame(5n, 'B'));

    // Frame seq=10 (gap: 6,7,8,9 missing) — should NOT trigger another gap request
    stream.handleOutput(buildSeqFrame(10n, 'C'));

    const gapCalls = wsSendSpy.mock.calls.filter(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('"type":"gap"')
    );
    expect(gapCalls.length).toBe(1);
  });

  it('clears gap pending flag when sequential data arrives', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();
    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    // Bootstrap + complete
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    wsSendSpy.mockClear();

    // Frame seq=5 (gap) — triggers gap request
    stream.handleOutput(buildSeqFrame(5n, 'B'));

    // Replay fills the gap: sequential frame 6 arrives
    stream.handleOutput(buildSeqFrame(6n, 'C'));

    // Now another gap should be detectable: frame seq=10
    stream.handleOutput(buildSeqFrame(10n, 'D'));

    const gapCalls = wsSendSpy.mock.calls.filter(
      (call: any[]) => typeof call[0] === 'string' && call[0].includes('"type":"gap"')
    );
    // First gap (from 1) + second gap (from 7)
    expect(gapCalls.length).toBe(2);
  });
});

describe('TerminalStream scroll suppression during writes', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('ignores scroll events triggered during terminal.write processing', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // followTail starts true
    expect((stream as any).followTail).toBe(true);

    // Mock write to simulate xterm.js behavior: processing escape sequences
    // causes a viewport scroll (e.g., cursor positioning to an upper row).
    // The scroll handler fires DURING write processing, before the callback.
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      // Simulate: xterm.js processes the write, viewport scrolls as a side effect
      // This triggers the scroll listener which calls handleUserScroll()
      stream.handleUserScroll();

      // Then the write callback fires (on next rAF in real xterm.js)
      if (cb) cb();
    });

    // Make isAtBottom return false (simulating viewport shifted by cursor positioning)
    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    // Send a live frame — this triggers terminal.write()
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));
    stream.handleOutput(buildSeqFrame(1n, 'output with cursor positioning'));

    // followTail should STILL be true — the scroll was programmatic, not user-initiated
    expect((stream as any).followTail).toBe(true);
  });
});

describe('TerminalStream scroll suppression during resize', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    vi.useFakeTimers();
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('does not disable followTail when resize triggers scroll events', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    expect((stream as any).followTail).toBe(true);

    // Mock resize to simulate xterm.js behavior: resize adjusts the buffer,
    // which fires a DOM scroll event as rows are pushed into scrollback.
    vi.mocked(terminal.resize).mockImplementation(() => {
      stream.handleUserScroll();
    });

    // Make isAtBottom return false (simulating viewport shifted by resize)
    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    stream.fitTerminal();

    // followTail should STILL be true — the scroll was from a resize, not user action
    expect((stream as any).followTail).toBe(true);
  });

  it('scrolls to bottom after resize when followTail is true', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Bootstrap the stream so it's in a normal state
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));
    vi.mocked(terminal.scrollToBottom).mockClear();

    expect((stream as any).followTail).toBe(true);

    stream.fitTerminal();

    expect(terminal.scrollToBottom).toHaveBeenCalled();
  });

  it('does not scroll to bottom after resize when followTail is false', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Disable followTail (user scrolled up)
    (stream as any).followTail = false;
    vi.mocked(terminal.scrollToBottom).mockClear();

    stream.fitTerminal();

    expect(terminal.scrollToBottom).not.toHaveBeenCalled();
  });

  it('suppresses scroll when resize rAF cleared writingToTerminal but write rAF is still pending', async () => {
    // Regression for H2 race: fitTerminal's independent rAF fired and cleared
    // writingToTerminal, but writeTerminal's rAF is still queued (scrollRAFPending=true).
    // Observed in diagnostic: writingToTerminal=false, scrollRAFPending=true, followLost.
    await stream.initialized;
    const terminal = stream.terminal!;

    (stream as any).writingToTerminal = false;
    (stream as any).scrollRAFPending = true;

    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    stream.handleUserScroll();

    expect((stream as any).followTail).toBe(true);
  });

  it('fitTerminal sets scrollRAFPending when no write is pending', async () => {
    await stream.initialized;
    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    (stream as any).scrollRAFPending = false;

    stream.fitTerminal();

    expect((stream as any).writingToTerminal).toBe(true);
    expect((stream as any).scrollRAFPending).toBe(true);

    rafCallbacks.forEach((cb) => cb(0));
    // writingToTerminal is now cleared by armWriteGuardClear (setTimeout),
    // not directly in the rAF. Advance timers to let it fire.
    await vi.advanceTimersByTimeAsync(10);
    expect((stream as any).writingToTerminal).toBe(false);
    expect((stream as any).scrollRAFPending).toBe(false);
  });

  it('fitTerminal does not schedule extra rAF when write rAF is already pending', async () => {
    // Regression for H2: old code scheduled an independent rAF from fitTerminal
    // which could race the pending writeTerminal rAF and clear writingToTerminal early.
    await stream.initialized;
    let rafScheduled = 0;
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((_cb) => {
      rafScheduled++;
      return rafScheduled;
    });

    (stream as any).scrollRAFPending = true;

    stream.fitTerminal();

    expect(rafScheduled).toBe(0);
    expect((stream as any).writingToTerminal).toBe(true);
    expect((stream as any).scrollRAFPending).toBe(true);
  });

  it('pending writeTerminal rAF clears writingToTerminal set by fitTerminal in coalesce-skip path', async () => {
    // When fitTerminal skips scheduling its own rAF (scrollRAFPending already true),
    // the existing writeTerminal rAF must still clear writingToTerminal correctly.
    await stream.initialized;
    const terminal = stream.terminal!;

    // Make terminal.write() fire its callback synchronously so the rAF is queued
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) cb();
    });

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    // Bootstrap (writeTerminal sync → write cb sync → queues scroll rAF)
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Fire bootstrap's scroll rAF to clear that state
    while (rafCallbacks.length) rafCallbacks.shift()!(0);

    // Send a live frame: writeLiveFrame queues write-coalescing rAF
    stream.handleOutput(buildSeqFrame(1n, 'live output'));
    expect(rafCallbacks).toHaveLength(1);

    // Fire write-coalescing rAF → writeTerminal → write cb sync → queues scroll rAF
    rafCallbacks.shift()!(0);

    expect((stream as any).scrollRAFPending).toBe(true);
    expect(rafCallbacks).toHaveLength(1);

    // Now fitTerminal fires: sees scrollRAFPending=true, skips scheduling extra rAF
    stream.fitTerminal();
    expect(rafCallbacks).toHaveLength(1); // no new rAF added
    expect((stream as any).writingToTerminal).toBe(true);

    // The scroll rAF fires and clears scrollRAFPending; armWriteGuardClear
    // handles writingToTerminal via setTimeout.
    rafCallbacks.forEach((cb) => cb(0));
    await vi.advanceTimersByTimeAsync(10);
    expect((stream as any).writingToTerminal).toBe(false);
    expect((stream as any).scrollRAFPending).toBe(false);
  });

  it('writingToTerminal stays true after rAF fires until armWriteGuardClear timeout expires', async () => {
    // Core fix validation: the rAF no longer clears writingToTerminal.
    // It stays true until the debounced setTimeout (8ms) fires.
    await stream.initialized;
    const terminal = stream.terminal!;

    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) cb();
    });

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    (stream as any).scrollRAFPending = false;
    (stream as any).writingToTerminal = false;

    // Simulate writeTerminal
    (stream as any).writeTerminal('some data');

    expect((stream as any).writingToTerminal).toBe(true);

    // Fire the rAF — writingToTerminal must still be true
    rafCallbacks.forEach((cb) => cb(0));
    expect((stream as any).writingToTerminal).toBe(true);
    expect((stream as any).scrollRAFPending).toBe(false);

    // Advance 4ms — still within the 8ms guard window
    await vi.advanceTimersByTimeAsync(4);
    expect((stream as any).writingToTerminal).toBe(true);

    // Advance past the 8ms guard window
    await vi.advanceTimersByTimeAsync(5);
    expect((stream as any).writingToTerminal).toBe(false);
  });

  it('onScroll re-arms the write guard, keeping it up across simulated setTimeout chunks', async () => {
    // Simulates xterm's setTimeout chunking: after the rAF clears scrollRAFPending,
    // xterm fires more scroll events from subsequent setTimeout chunks.
    // The onScroll re-arming must keep writingToTerminal = true.
    await stream.initialized;
    const terminal = stream.terminal!;

    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) cb();
    });

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    (stream as any).scrollRAFPending = false;
    (stream as any).writingToTerminal = false;

    // Write data
    (stream as any).writeTerminal('chunk data');
    expect((stream as any).writingToTerminal).toBe(true);

    // Fire the rAF (scrollToBottom)
    rafCallbacks.forEach((cb) => cb(0));
    expect((stream as any).writingToTerminal).toBe(true); // still guarded

    // Advance 5ms — simulating xterm's setTimeout chunk firing
    await vi.advanceTimersByTimeAsync(5);
    // Simulate xterm firing a scroll event from a setTimeout chunk
    // (this is what xterm.onScroll handler does — re-arms the timer)
    (stream as any).armWriteGuardClear();
    expect((stream as any).writingToTerminal).toBe(true);

    // Advance another 5ms — simulate another chunk
    await vi.advanceTimersByTimeAsync(5);
    (stream as any).armWriteGuardClear();
    expect((stream as any).writingToTerminal).toBe(true);

    // Now stop re-arming (xterm finished all chunks) and wait for timeout
    await vi.advanceTimersByTimeAsync(10);
    expect((stream as any).writingToTerminal).toBe(false);
  });

  it('handleUserScroll is suppressed during simulated setTimeout chunks after rAF', async () => {
    // The actual user-visible bug: after the rAF clears, a scroll event from
    // xterm's next setTimeout chunk would pass through handleUserScroll and
    // falsely disable followTail. With the fix, the guard stays up.
    await stream.initialized;
    const terminal = stream.terminal!;

    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) cb();
    });

    const rafCallbacks: FrameRequestCallback[] = [];
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      rafCallbacks.push(cb);
      return rafCallbacks.length;
    });

    (stream as any).scrollRAFPending = false;
    (stream as any).writingToTerminal = false;
    (stream as any).followTail = true;

    // Make isAtBottom return false (simulating viewport not at bottom
    // during a TUI redraw — this is what triggers the oscillation)
    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 100;

    // Write data
    (stream as any).writeTerminal('tui redraw data');

    // Fire the rAF
    rafCallbacks.forEach((cb) => cb(0));

    // Simulate a scroll event arriving from xterm's setTimeout chunk.
    // Before the fix, writingToTerminal would be false here and
    // handleUserScroll would disable followTail.
    stream.handleUserScroll();

    // followTail must still be true — the scroll was from xterm, not the user
    expect((stream as any).followTail).toBe(true);
  });
});

describe('TerminalStream.sendInput binary frames', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('sends input as a binary Uint8Array, not JSON text', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();
    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    stream.sendInput('a');

    expect(wsSendSpy).toHaveBeenCalledTimes(1);
    const sent = wsSendSpy.mock.calls[0][0];
    // Should be a typed array (binary), not a string (JSON)
    expect(typeof sent).not.toBe('string');
    expect(ArrayBuffer.isView(sent)).toBe(true);
    // Decode and verify contents
    const decoded = new TextDecoder().decode(sent);
    expect(decoded).toBe('a');
  });

  it('sends escape sequences as binary', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();
    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    stream.sendInput('\x1b[A'); // Up arrow

    const sent = wsSendSpy.mock.calls[0][0];
    expect(typeof sent).not.toBe('string');
    expect(ArrayBuffer.isView(sent)).toBe(true);
    const decoded = new TextDecoder().decode(sent);
    expect(decoded).toBe('\x1b[A');
  });

  it('sends control messages (resize, gap) as JSON text, not binary', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();
    (stream as any).ws = { readyState: 1, send: wsSendSpy };

    stream.sendResize(80, 24);

    expect(wsSendSpy).toHaveBeenCalledTimes(1);
    const sent = wsSendSpy.mock.calls[0][0];
    // Resize should still be a JSON string (text frame)
    expect(typeof sent).toBe('string');
    const parsed = JSON.parse(sent);
    expect(parsed.type).toBe('resize');
  });
});

describe('TerminalStream inputEcho handling', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    inputLatency.reset();
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('dispatches inputEcho message to inputLatency.recordServerSegments', async () => {
    await stream.initialized;

    const echoMsg = {
      type: 'inputEcho',
      serverMs: 5.2,
      dispatchMs: 0.1,
      sendKeysMs: 3.0,
      echoMs: 1.5,
      frameSendMs: 0.6,
    };
    stream.handleOutput(JSON.stringify(echoMsg));

    expect(inputLatency.serverSegmentSamples).toEqual([
      { dispatch: 0.1, sendKeys: 3.0, echo: 1.5, frameSend: 0.6, total: 5.2 },
    ]);
  });

  it('handles multiple inputEcho messages', async () => {
    await stream.initialized;

    stream.handleOutput(
      JSON.stringify({
        type: 'inputEcho',
        serverMs: 3.1,
        dispatchMs: 0.1,
        sendKeysMs: 1.5,
        echoMs: 1.0,
        frameSendMs: 0.5,
      })
    );
    stream.handleOutput(
      JSON.stringify({
        type: 'inputEcho',
        serverMs: 7.4,
        dispatchMs: 0.2,
        sendKeysMs: 4.0,
        echoMs: 2.5,
        frameSendMs: 0.7,
      })
    );

    expect(inputLatency.serverSegmentSamples).toEqual([
      { dispatch: 0.1, sendKeys: 1.5, echo: 1.0, frameSend: 0.5, total: 3.1 },
      { dispatch: 0.2, sendKeys: 4.0, echo: 2.5, frameSend: 0.7, total: 7.4 },
    ]);
  });
});

describe('TerminalStream wire instrumentation', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    inputLatency.reset();
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('calls recordFrameProcessed on non-bootstrap binary frames', async () => {
    await stream.initialized;

    const spy = vi.spyOn(inputLatency, 'recordFrameProcessed');

    // Bootstrap frame (seq=0) — should NOT call recordFrameProcessed
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    expect(spy).not.toHaveBeenCalled();

    // Live frame (seq=1) — should call recordFrameProcessed
    stream.handleOutput(buildSeqFrame(1n, 'live'));
    expect(spy).toHaveBeenCalledTimes(1);

    // Another live frame (seq=2)
    stream.handleOutput(buildSeqFrame(2n, 'more'));
    expect(spy).toHaveBeenCalledTimes(2);

    spy.mockRestore();
  });

  it('calls recordHandleOutputTime on non-bootstrap binary frames', async () => {
    await stream.initialized;

    const spy = vi.spyOn(inputLatency, 'recordHandleOutputTime');

    // Bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'bootstrap'));
    expect(spy).not.toHaveBeenCalled();

    // Live frame
    stream.handleOutput(buildSeqFrame(1n, 'live'));
    expect(spy).toHaveBeenCalledTimes(1);
    // Should be called with a non-negative number
    expect(spy.mock.calls[0][0]).toBeGreaterThanOrEqual(0);

    spy.mockRestore();
  });
});

describe('MockTerminalWebSocket protocol compatibility', () => {
  // Integration test: ensures the demo's MockTerminalWebSocket produces
  // binary frames that TerminalStream.handleOutput() can parse correctly.
  // This prevents the demo from silently breaking when the WebSocket
  // protocol evolves (e.g., adding sequence headers, changing frame format).

  let stream: TerminalStream;

  beforeEach(() => {
    vi.useFakeTimers();
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('mock frames contain 8-byte sequence header parseable by TerminalStream', async () => {
    await stream.initialized;

    const { MockTerminalWebSocket } =
      await import('../../website/src/demo/transport/MockWebSocket');

    const mockWS = new MockTerminalWebSocket('ws://localhost/ws/terminal/test');
    const frames: ArrayBuffer[] = [];
    mockWS.onmessage = (ev: any) => {
      if (ev.data instanceof ArrayBuffer) {
        frames.push(ev.data);
      }
    };

    // Open the socket and start playback
    vi.advanceTimersByTime(50);
    mockWS.startPlayback({
      sessionId: 'test-session',
      frames: [
        { delay: 0, data: 'hello world' },
        { delay: 10, data: 'second frame' },
      ],
    });
    vi.advanceTimersByTime(100);

    expect(frames.length).toBe(2);

    // Each frame must have at least 8 bytes (the sequence header)
    for (const frame of frames) {
      expect(frame.byteLength).toBeGreaterThanOrEqual(8);
    }

    // Feed frames into TerminalStream — should not throw
    for (const frame of frames) {
      stream.handleOutput(frame);
    }

    // Verify sequence numbers are monotonically increasing starting from 0
    const seqs = frames.map((f) => new DataView(f).getBigUint64(0, false));
    expect(seqs[0]).toBe(0n);
    expect(seqs[1]).toBe(1n);

    // Verify terminal data is preserved (not truncated by header parsing)
    const decoder = new TextDecoder();
    const text0 = decoder.decode(new Uint8Array(frames[0], 8));
    const text1 = decoder.decode(new Uint8Array(frames[1], 8));
    expect(text0).toBe('hello world');
    expect(text1).toBe('second frame');

    mockWS.close();
  });

  it('mock sends bootstrapComplete after first frame', async () => {
    await stream.initialized;

    const { MockTerminalWebSocket } =
      await import('../../website/src/demo/transport/MockWebSocket');

    const mockWS = new MockTerminalWebSocket('ws://localhost/ws/terminal/test');
    const messages: any[] = [];
    mockWS.onmessage = (ev: any) => messages.push(ev.data);

    vi.advanceTimersByTime(50);
    mockWS.startPlayback({
      sessionId: 'test-session',
      frames: [
        { delay: 0, data: 'bootstrap content' },
        { delay: 10, data: 'live content' },
      ],
    });
    vi.advanceTimersByTime(100);

    // Should have: binary frame 0, bootstrapComplete text, binary frame 1
    const textMessages = messages.filter((m) => typeof m === 'string');
    expect(textMessages.length).toBeGreaterThanOrEqual(1);
    const parsed = JSON.parse(textMessages[0]);
    expect(parsed.type).toBe('bootstrapComplete');

    // bootstrapComplete must arrive after the first binary frame
    const firstBinaryIdx = messages.findIndex((m) => m instanceof ArrayBuffer);
    const bootstrapCompleteIdx = messages.findIndex(
      (m) => typeof m === 'string' && m.includes('bootstrapComplete')
    );
    expect(firstBinaryIdx).toBeLessThan(bootstrapCompleteIdx);

    mockWS.close();
  });

  it('TerminalStream writes correct text from mock frames (no truncation)', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;
    const writtenChunks: string[] = [];
    (terminal.write as ReturnType<typeof vi.fn>).mockImplementation(
      (data: string, cb?: () => void) => {
        writtenChunks.push(data);
        cb?.();
      }
    );

    const { MockTerminalWebSocket } =
      await import('../../website/src/demo/transport/MockWebSocket');

    const mockWS = new MockTerminalWebSocket('ws://localhost/ws/terminal/test');
    const allMessages: any[] = [];
    mockWS.onmessage = (ev: any) => allMessages.push(ev.data);

    vi.advanceTimersByTime(50);
    mockWS.startPlayback({
      sessionId: 'test-session',
      frames: [{ delay: 0, data: 'ABCDEFGHIJKLMNOP' }], // 16 chars — longer than 8-byte header
    });
    vi.advanceTimersByTime(50);

    // Feed all messages (binary + text) into TerminalStream
    for (const msg of allMessages) {
      stream.handleOutput(msg);
    }

    // The full 16-character string must appear in terminal writes
    const allWritten = writtenChunks.join('');
    expect(allWritten).toContain('ABCDEFGHIJKLMNOP');

    mockWS.close();
  });
});

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

    // Ensure writingToTerminal is false (bootstrap complete)
    (stream as any).writingToTerminal = false;

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

  it('increments scrollSuppressedCount when scrollRAFPending is true (H1/H2 race window)', async () => {
    // Regression: writingToTerminal may be false while scrollRAFPending is still true
    // (resize rAF fired first, clearing writingToTerminal, but write rAF still queued).
    await stream.initialized;
    stream.enableDiagnostics();

    (stream as any).writingToTerminal = false;
    (stream as any).scrollRAFPending = true;

    stream.handleUserScroll();

    expect((stream as any).followTail).toBe(true);
    expect(stream.diagnostics!.scrollSuppressedCount).toBe(1);
    expect(stream.diagnostics!.scrollEvents).toHaveLength(0);
    expect(stream.diagnostics!.followLostCount).toBe(0);
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
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Clear bootstrap rAFs
    while (rafCallbacks.length) rafCallbacks.shift()!(0);
    stream.diagnostics!.scrollCoalesceHits = 0;

    // Two live frames in the same rAF batch — writeLiveFrame coalesces them
    // into one writeTerminal call. The second handleOutput finds writeRAFPending
    // already true, so it coalesces into the same buffer.
    stream.handleOutput(buildSeqFrame(1n, 'frame1'));
    stream.handleOutput(buildSeqFrame(2n, 'frame2'));

    // Flush write-coalesce rAF → one writeTerminal call → scroll rAF
    rafCallbacks.shift()!(0);

    // Now scrollRAFPending is true (scroll rAF pending in queue).
    // Send another frame — its writeLiveFrame queues a write-coalesce rAF.
    stream.handleOutput(buildSeqFrame(3n, 'frame3'));
    // Fire the write-coalesce rAF (last in queue) BEFORE the scroll rAF,
    // so writeTerminal finds scrollRAFPending=true → coalesce hit.
    rafCallbacks.pop()!(0);

    expect(stream.diagnostics!.scrollCoalesceHits).toBeGreaterThanOrEqual(1);
  });

  it('caps scroll events ring buffer at 100', async () => {
    await stream.initialized;
    stream.enableDiagnostics();
    const terminal = stream.terminal!;

    // Ensure writingToTerminal is false
    (stream as any).writingToTerminal = false;

    for (let i = 0; i < 110; i++) {
      (stream as any).followTail = i % 2 === 0;
      (terminal.buffer.active as any).viewportY = i % 2 === 0 ? 0 : 10;
      (terminal.buffer.active as any).baseY = 10;
      stream.handleUserScroll();
    }

    expect(stream.diagnostics!.scrollEvents.length).toBeLessThanOrEqual(100);
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

    // Ensure writingToTerminal is false
    (stream as any).writingToTerminal = false;

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

    // Ensure writingToTerminal is false
    (stream as any).writingToTerminal = false;

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

describe('TerminalStream scroll without diagnostics', () => {
  let stream: TerminalStream;

  beforeEach(() => {
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
  });

  it('follow state changes work correctly when diagnostics are disabled', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Ensure diagnostics are disabled
    stream.disableDiagnostics();
    expect(stream.diagnostics).toBeNull();

    // Ensure writingToTerminal is false
    (stream as any).writingToTerminal = false;

    // Verify follow state still transitions without diagnostics
    (terminal.buffer.active as any).viewportY = 0;
    (terminal.buffer.active as any).baseY = 10;

    expect(() => stream.handleUserScroll()).not.toThrow();
    expect((stream as any).followTail).toBe(false);

    // Verify jumpToBottom recovery also works without diagnostics
    stream.jumpToBottom();
    expect((stream as any).followTail).toBe(true);
  });
});
