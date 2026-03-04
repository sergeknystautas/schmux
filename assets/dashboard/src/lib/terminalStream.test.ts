import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock xterm and addons before importing TerminalStream
vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    loadAddon = vi.fn();
    open = vi.fn();
    onData = vi.fn();
    writeln = vi.fn();
    write = vi.fn();
    clear = vi.fn();
    reset = vi.fn();
    resize = vi.fn();
    focus = vi.fn();
    scrollToBottom = vi.fn();
    element = null;
    buffer = { active: { viewportY: 0, baseY: 0, cursorY: 0, length: 0 } };
    rows = 24;
    cols = 80;
    markers = [];
  }
  return { Terminal: MockTerminal };
});
vi.mock('@xterm/addon-unicode11', () => ({ Unicode11Addon: vi.fn() }));
vi.mock('@xterm/addon-web-links', () => ({ WebLinksAddon: vi.fn() }));

import TerminalStream from './terminalStream';

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

  it('calls scrollToBottom only after bootstrap write callback fires', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Clear any scrollToBottom calls from initialization (fitTerminalSync)
    vi.mocked(terminal.scrollToBottom).mockClear();

    // Track write callback invocations
    const writeCallbacks: (() => void)[] = [];
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) writeCallbacks.push(cb);
    });

    // Send bootstrap frame (seq=0)
    stream.handleOutput(buildSeqFrame(0n, 'chunk1'));

    // scrollToBottom should NOT have been called yet (write hasn't "completed")
    expect(terminal.scrollToBottom).not.toHaveBeenCalled();

    // Simulate xterm.js completing the write
    writeCallbacks[0]();

    // Now scrollToBottom should fire (followTail defaults to true)
    expect(terminal.scrollToBottom).toHaveBeenCalledTimes(1);
  });

  it('calls scrollToBottom via callback on live frames too', async () => {
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

    // Send live frame
    stream.handleOutput(buildSeqFrame(1n, 'live-data'));

    // scrollToBottom should NOT have been called yet
    expect(terminal.scrollToBottom).not.toHaveBeenCalled();

    // Simulate write completion
    writeCallbacks[0]();

    // Now it should fire
    expect(terminal.scrollToBottom).toHaveBeenCalledTimes(1);
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

    // "é" is U+00E9, encoded as UTF-8: [0xC3, 0xA9]
    // Split across two frames

    // Frame 1: first byte of é
    const buf1 = new ArrayBuffer(8 + 1);
    new DataView(buf1).setBigUint64(0, 0n, false);
    new Uint8Array(buf1, 8).set([0xc3]);
    stream.handleOutput(buf1);

    // Frame 2: second byte of é
    const buf2 = new ArrayBuffer(8 + 1);
    new DataView(buf2).setBigUint64(0, 1n, false);
    new Uint8Array(buf2, 8).set([0xa9]);
    stream.handleOutput(buf2);

    // The streaming TextDecoder should have decoded "é" across the two frames
    // First call writes empty string (incomplete char), second writes "é"
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

    // Bootstrap
    stream.handleOutput(buildSeqFrame(0n, 'A'));
    stream.handleOutput(JSON.stringify({ type: 'bootstrapComplete' }));

    // Receive frame 1
    stream.handleOutput(buildSeqFrame(1n, 'B'));

    vi.mocked(terminal.write).mockClear();

    // Frame 3 arrives (gap: 2 missing)
    // This should still be written since seq=3 > lastReceivedSeq=1
    stream.handleOutput(buildSeqFrame(3n, 'D'));

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

    vi.mocked(terminal.write).mockClear();

    // First live event has seq=42 (OutputLog.Append() assigns CurrentSeq()=42).
    // This must NOT be dropped — it's the echo of the user's first keystroke.
    stream.handleOutput(buildSeqFrame(42n, 'first keystroke echo'));

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
    const container = document.createElement('div');
    Object.defineProperty(container, 'getBoundingClientRect', {
      value: () => ({ width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600 }),
    });
    stream = new TerminalStream('test-session', container);
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
});
