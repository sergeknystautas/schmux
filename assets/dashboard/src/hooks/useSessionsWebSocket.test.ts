import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import useSessionsWebSocket from './useSessionsWebSocket';

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
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

// Helper: get the latest MockWebSocket instance
function lastWS(): MockWebSocket {
  return MockWebSocket.instances[MockWebSocket.instances.length - 1];
}

// Helper: simulate the WS opening
function openWS(ws: MockWebSocket) {
  ws.onopen?.();
}

// Helper: send a message to the WS
function sendMsg(ws: MockWebSocket, data: unknown) {
  ws.onmessage?.({ data: JSON.stringify(data) });
}

describe('useSessionsWebSocket', () => {
  it('connects to /ws/dashboard on mount', () => {
    renderHook(() => useSessionsWebSocket());
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(lastWS().url).toMatch(/ws:\/\/localhost.*\/ws\/dashboard/);
  });

  it('dispatches sessions message to state', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    act(() => {
      sendMsg(ws, {
        type: 'sessions',
        workspaces: [{ id: 'ws-1', repo: 'r', branch: 'main', path: '/tmp', sessions: [] }],
      });
    });

    expect(result.current.workspaces).toHaveLength(1);
    expect(result.current.workspaces[0].id).toBe('ws-1');
    expect(result.current.loading).toBe(false);
    expect(result.current.connected).toBe(true);
  });

  it('deduplicates identical sessions messages', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    const msg = {
      type: 'sessions',
      workspaces: [{ id: 'ws-1', repo: 'r', branch: 'main', path: '/tmp', sessions: [] }],
    };

    act(() => sendMsg(ws, msg));
    const first = result.current.workspaces;

    act(() => sendMsg(ws, msg));
    const second = result.current.workspaces;

    // Same reference means no re-render triggered
    expect(first).toBe(second);
  });

  it('handles overlay_change message', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    act(() => {
      sendMsg(ws, {
        type: 'overlay_change',
        rel_path: 'src/main.go',
        source_workspace_id: 'ws-1',
        source_branch: 'main',
        target_workspace_ids: ['ws-2'],
        timestamp: 123,
      });
    });

    expect(result.current.overlayEvents).toHaveLength(1);
    expect(result.current.overlayEvents[0].rel_path).toBe('src/main.go');
  });

  it('handles workspace_locked message', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    act(() => {
      sendMsg(ws, {
        type: 'workspace_locked',
        workspace_id: 'ws-1',
        locked: true,
      });
    });

    expect(result.current.workspaceLockStates['ws-1']).toEqual({
      locked: true,
      syncProgress: undefined,
    });
  });

  it('reconnects with exponential backoff', () => {
    renderHook(() => useSessionsWebSocket());
    const ws1 = lastWS();
    openWS(ws1);

    expect(MockWebSocket.instances).toHaveLength(1);

    // Simulate close
    act(() => {
      ws1.onclose?.({ code: 1000 });
    });

    // First reconnect: base delay 2000ms + jitter [0.5x - 1.5x]
    // Advance past max jitter: 2000 * 1.5 = 3000ms
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(MockWebSocket.instances).toHaveLength(2);

    // Close again
    const ws2 = lastWS();
    act(() => {
      ws2.onclose?.({ code: 1000 });
    });

    // Second reconnect: delay doubles to 4000ms, max jitter 4000 * 1.5 = 6000ms
    act(() => {
      vi.advanceTimersByTime(6000);
    });
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it('handles remote_access_status message', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    act(() => {
      sendMsg(ws, {
        type: 'remote_access_status',
        data: { state: 'connected', url: 'https://example.com' },
      });
    });

    expect(result.current.remoteAccessStatus.state).toBe('connected');
  });

  it('handles ws.onerror gracefully', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    // Send initial data so we can verify state is preserved
    act(() => {
      sendMsg(ws, {
        type: 'sessions',
        workspaces: [{ id: 'ws-1', repo: 'r', branch: 'main', path: '/tmp', sessions: [] }],
      });
    });
    expect(result.current.workspaces).toHaveLength(1);

    // Trigger onerror — should not crash or corrupt state
    act(() => {
      ws.onerror?.();
    });

    // State should be preserved (onerror is a no-op; onclose handles reconnect)
    expect(result.current.workspaces).toHaveLength(1);
  });

  it('handles malformed JSON without crashing', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    // Send valid data first
    act(() => {
      sendMsg(ws, {
        type: 'sessions',
        workspaces: [{ id: 'ws-1', repo: 'r', branch: 'main', path: '/tmp', sessions: [] }],
      });
    });
    expect(result.current.workspaces).toHaveLength(1);

    // Send malformed JSON directly via onmessage (bypass sendMsg which JSON.stringifies)
    act(() => {
      ws.onmessage?.({ data: '{invalid json' });
    });

    // State should be unchanged — the try/catch in the hook swallows the parse error
    expect(result.current.workspaces).toHaveLength(1);
  });

  it('ignores unknown message types', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    // Send valid data first
    act(() => {
      sendMsg(ws, {
        type: 'sessions',
        workspaces: [{ id: 'ws-1', repo: 'r', branch: 'main', path: '/tmp', sessions: [] }],
      });
    });
    const before = result.current.workspaces;

    // Send unknown message type
    act(() => {
      sendMsg(ws, { type: 'unknown_type', data: { foo: 'bar' } });
    });

    // State reference should be identical — no re-render triggered
    expect(result.current.workspaces).toBe(before);
  });

  it('handles monitor event ring buffer', () => {
    const { result } = renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    act(() => {
      for (let i = 0; i < 201; i++) {
        sendMsg(ws, {
          type: 'event',
          session_id: 's1',
          event: { type: 'status', state: 'working', message: `msg-${i}` },
        });
      }
    });

    // Ring buffer caps at 200
    expect(result.current.monitorEvents).toHaveLength(200);
    // First event should be msg-1 (msg-0 was dropped)
    expect(result.current.monitorEvents[0].event.message).toBe('msg-1');
  });

  it('calls onConfigUpdated when config_updated message received', () => {
    const onConfigUpdated = vi.fn();
    renderHook(() => useSessionsWebSocket({ onConfigUpdated }));
    const ws = lastWS();
    openWS(ws);

    act(() => {
      sendMsg(ws, { type: 'config_updated' });
    });

    expect(onConfigUpdated).toHaveBeenCalledTimes(1);
  });

  it('does not crash when config_updated received without callback', () => {
    renderHook(() => useSessionsWebSocket());
    const ws = lastWS();
    openWS(ws);

    // Should not throw
    act(() => {
      sendMsg(ws, { type: 'config_updated' });
    });
  });
});
