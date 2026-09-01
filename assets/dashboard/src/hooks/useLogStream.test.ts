import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import useLogStream from './useLogStream';

// --- MockWebSocket ---

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  send = vi.fn();
  constructor(public url: string) {
    MockWebSocket.instances.push(this);
  }
}

beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
});

afterEach(() => {
  vi.restoreAllMocks();
});

function lastWS(): MockWebSocket {
  return MockWebSocket.instances[MockWebSocket.instances.length - 1];
}

function openWS(ws: MockWebSocket) {
  ws.onopen?.();
}

function sendMsg(ws: MockWebSocket, data: unknown) {
  ws.onmessage?.({ data: JSON.stringify(data) });
}

function closeWS(ws: MockWebSocket, code = 1000) {
  ws.onclose?.({ code });
}

describe('useLogStream', () => {
  it('appends history, prepends live records, and assigns stable ids', () => {
    const { result } = renderHook(() => useLogStream('/ws/logs/spawn', Number));
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history', line: '30' });
      sendMsg(ws, { type: 'history', line: '20' });
      sendMsg(ws, { type: 'history_end', has_more: true });
    });
    const historyIds = result.current.items.map((item) => item.id);
    expect(result.current.items.map((item) => item.value)).toEqual([30, 20]);

    act(() => sendMsg(ws, { type: 'append', line: '40' }));
    expect(result.current.items.map((item) => item.value)).toEqual([40, 30, 20]);
    // Append kept the same ids for the existing history rows.
    expect(result.current.items.slice(1).map((item) => item.id)).toEqual(historyIds);
  });

  it('sends one load_older per page request and serializes two calls into one send', () => {
    const { result } = renderHook(() => useLogStream('/ws/logs/spawn', Number));
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history', line: '5' });
      sendMsg(ws, { type: 'history_end', has_more: true });
    });

    // Two rapid loadOlder calls must collapse into a single network send.
    act(() => {
      result.current.loadOlder();
      result.current.loadOlder();
    });
    expect(ws.send).toHaveBeenCalledTimes(1);
    expect(ws.send).toHaveBeenCalledWith(JSON.stringify({ type: 'load_older' }));

    act(() => sendMsg(ws, { type: 'history_end', has_more: true }));

    // After the page completes, a subsequent loadOlder fires once more.
    act(() => result.current.loadOlder());
    expect(ws.send).toHaveBeenCalledTimes(2);
  });

  it('preserves items and historyError on history_error, then clears on Retry', () => {
    const { result } = renderHook(() => useLogStream('/ws/logs/spawn', Number));
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history', line: '1' });
      sendMsg(ws, { type: 'history', line: '2' });
      sendMsg(ws, { type: 'history_end', has_more: true });
    });
    const idsBefore = result.current.items.map((item) => item.id);

    act(() => result.current.loadOlder());
    expect(result.current.loadingOlder).toBe(true);
    expect(ws.send).toHaveBeenCalledTimes(1);

    act(() => sendMsg(ws, { type: 'history_error', message: 'boom' }));
    expect(result.current.items.map((item) => item.id)).toEqual(idsBefore);
    expect(result.current.items.map((item) => item.value)).toEqual([1, 2]);
    expect(result.current.loadingOlder).toBe(false);
    expect(result.current.historyError).toBe('boom');

    // Retry should clear the error and send load_older again.
    act(() => result.current.loadOlder());
    expect(result.current.historyError).toBeNull();
    expect(ws.send).toHaveBeenCalledTimes(2);
  });

  it('loadOlder is a no-op after terminal history_end', () => {
    const { result } = renderHook(() => useLogStream('/ws/logs/spawn', Number));
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history_end', has_more: false });
    });
    expect(result.current.hasMore).toBe(false);

    act(() => result.current.loadOlder());
    expect(ws.send).not.toHaveBeenCalled();
  });

  it('resets items, ids, and paging state when the path changes', () => {
    const { result, rerender } = renderHook(({ path }) => useLogStream(path, Number), {
      initialProps: { path: '/ws/logs/spawn' as string },
    });
    const ws1 = lastWS();
    act(() => openWS(ws1));
    act(() => {
      sendMsg(ws1, { type: 'history', line: '1' });
      sendMsg(ws1, { type: 'history_end', has_more: true });
    });
    expect(result.current.items[0].id).toBe(1);
    expect(result.current.hasMore).toBe(true);

    // Switching path closes the old socket, opens a new one, and resets state.
    rerender({ path: '/ws/logs/oneshot' });
    expect(ws1.close).toHaveBeenCalled();
    const ws2 = lastWS();
    expect(ws2).not.toBe(ws1);
    expect(ws2.url).toMatch(/oneshot/);
    expect(result.current.loadingOlder).toBe(true);
    expect(result.current.items).toEqual([]);

    act(() => openWS(ws2));
    act(() => {
      sendMsg(ws2, { type: 'history', line: '7' });
      sendMsg(ws2, { type: 'history_end', has_more: false });
    });
    expect(result.current.items.map((item) => item.value)).toEqual([7]);
    expect(result.current.items[0].id).toBe(1);
    expect(result.current.hasMore).toBe(false);
  });

  it('logs and skips a parser failure, then keeps a subsequent valid line', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    const { result } = renderHook(() =>
      useLogStream('/ws/logs/spawn', (line) => {
        if (line === 'bad') throw new Error('parse fail');
        return Number(line);
      })
    );
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history', line: '5' });
      sendMsg(ws, { type: 'history', line: 'bad' });
      sendMsg(ws, { type: 'history', line: '6' });
      sendMsg(ws, { type: 'history_end', has_more: false });
    });
    expect(result.current.items.map((item) => item.value)).toEqual([5, 6]);
    expect(consoleError).toHaveBeenCalled();
    consoleError.mockRestore();
  });

  it('closes with connected=false and loadingOlder=false but keeps items', () => {
    const { result } = renderHook(() => useLogStream('/ws/logs/spawn', Number));
    const ws = lastWS();
    act(() => openWS(ws));

    act(() => {
      sendMsg(ws, { type: 'history', line: '1' });
      sendMsg(ws, { type: 'history_end', has_more: true });
    });
    act(() => result.current.loadOlder());
    expect(result.current.loadingOlder).toBe(true);

    act(() => closeWS(ws));

    expect(result.current.connected).toBe(false);
    expect(result.current.loadingOlder).toBe(false);
    expect(result.current.items.length).toBe(1);
  });
});
