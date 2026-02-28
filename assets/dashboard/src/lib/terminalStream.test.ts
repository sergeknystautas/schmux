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

  it('discards sync messages received within 500ms of binary data', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate binary data arriving (bootstrap)
    stream.handleOutput(new TextEncoder().encode('bootstrap').buffer as ArrayBuffer);

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

  it('applies sync correction when content mismatches after activity window', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(new TextEncoder().encode('bootstrap').buffer as ArrayBuffer);

    // Clear mock call counts so we only measure sync behavior
    vi.mocked(terminal.reset).mockClear();
    vi.mocked(terminal.write).mockClear();

    // Manually set lastBinaryTime to be old (>500ms ago)
    (stream as any).lastBinaryTime = Date.now() - 1000;

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

    // Should have called reset + write for correction
    expect(terminal.reset).toHaveBeenCalledTimes(1);
    expect(terminal.write).toHaveBeenCalledWith(expect.stringContaining('correct content'));
  });

  it('does not correct when content matches', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Simulate bootstrap
    stream.handleOutput(new TextEncoder().encode('bootstrap').buffer as ArrayBuffer);

    // Clear mock call counts so we only measure sync behavior
    vi.mocked(terminal.reset).mockClear();

    (stream as any).lastBinaryTime = Date.now() - 1000;

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
    stream.handleOutput(new TextEncoder().encode('bootstrap').buffer as ArrayBuffer);

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

    // Should have applied the correction despite recent binary data
    expect(terminal.reset).toHaveBeenCalledTimes(1);
    expect(terminal.write).toHaveBeenCalledWith(expect.stringContaining('correct content'));
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
