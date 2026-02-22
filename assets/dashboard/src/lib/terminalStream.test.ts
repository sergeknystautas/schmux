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
