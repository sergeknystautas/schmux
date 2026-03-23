import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import React from 'react';
import { MemoryRouter } from 'react-router-dom';
import { SessionsProvider, useSessions } from './SessionsContext';
import { useSyncState } from './SyncContext';
import { useOverlay } from './OverlayContext';
import { useRemoteAccess } from './RemoteAccessContext';
import { useMonitor } from './MonitorContext';
import type { WorkspaceResponse } from '../lib/types';

// --- Mocks ---

const mockWorkspaces: WorkspaceResponse[] = [];
let mockReturnOverrides: Record<string, unknown> = {};

vi.mock('../hooks/useSessionsWebSocket', () => ({
  default: () => ({
    workspaces: mockWorkspaces,
    loading: false,
    connected: true,
    stale: false,
    linearSyncResolveConflictStates: {},
    clearLinearSyncResolveConflictState: vi.fn(),
    workspaceLockStates: {},
    syncResultEvents: [],
    clearSyncResultEvents: vi.fn(),
    overlayEvents: [],
    clearOverlayEvents: vi.fn(),
    remoteAccessStatus: { state: 'off' },
    curatorEvents: {},
    monitorEvents: [],
    clearMonitorEvents: vi.fn(),
    ...mockReturnOverrides,
  }),
}));

vi.mock('./ConfigContext', () => ({
  useConfig: () => ({ config: { notifications: {} } }),
}));

vi.mock('../lib/notificationSound', () => ({
  soundForState: () => null,
  playAttentionSound: vi.fn(),
  playCompletionSound: vi.fn(),
  warmupAudioContext: vi.fn(),
}));

vi.mock('../lib/previewKeepAlive', () => ({
  removePreviewIframe: vi.fn(),
}));

function makeWrapper() {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <MemoryRouter>
        <SessionsProvider>{children}</SessionsProvider>
      </MemoryRouter>
    );
  };
}

beforeEach(() => {
  mockWorkspaces.length = 0;
  mockReturnOverrides = {};
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('SessionsContext', () => {
  it('provides sessionsById derived from workspaces', () => {
    mockWorkspaces.push({
      id: 'ws-1',
      repo: 'https://example.com/repo.git',
      branch: 'main',
      path: '/tmp/ws-1',
      session_count: 1,
      sessions: [
        {
          id: 'sess-1',
          target: 'claude',
          branch: 'main',
          created_at: '',
          running: true,
          attach_cmd: '',
        },
      ],
      ahead: 0,
      behind: 0,
      lines_added: 0,
      lines_removed: 0,
      files_changed: 0,
    });

    const { result } = renderHook(() => useSessions(), { wrapper: makeWrapper() });

    expect(result.current.sessionsById).toHaveProperty('sess-1');
    expect(result.current.sessionsById['sess-1'].workspace_id).toBe('ws-1');
    expect(result.current.sessionsById['sess-1'].repo).toBe('https://example.com/repo.git');
  });

  it('waitForSession resolves immediately when session exists', async () => {
    mockWorkspaces.push({
      id: 'ws-1',
      repo: 'r',
      branch: 'main',
      path: '/tmp',
      session_count: 1,
      sessions: [
        {
          id: 'sess-1',
          target: 'claude',
          branch: 'main',
          created_at: '',
          running: true,
          attach_cmd: '',
        },
      ],
      ahead: 0,
      behind: 0,
      lines_added: 0,
      lines_removed: 0,
      files_changed: 0,
    });

    const { result } = renderHook(() => useSessions(), { wrapper: makeWrapper() });

    let resolved: boolean | undefined;
    await act(async () => {
      resolved = await result.current.waitForSession('sess-1');
    });
    expect(resolved).toBe(true);
  });

  it('waitForSession resolves false on timeout for missing session', async () => {
    vi.useFakeTimers();

    const { result } = renderHook(() => useSessions(), { wrapper: makeWrapper() });

    let resolved: boolean | undefined;
    const promise = act(async () => {
      const p = result.current.waitForSession('nonexistent', { timeoutMs: 100 });
      // Advance past timeout
      vi.advanceTimersByTime(150);
      resolved = await p;
    });
    await promise;

    expect(resolved).toBe(false);
    vi.useRealTimers();
  });

  it('ackSession stores seq in localStorage', () => {
    mockWorkspaces.push({
      id: 'ws-1',
      repo: 'r',
      branch: 'main',
      path: '/tmp',
      session_count: 1,
      sessions: [
        {
          id: 'sess-1',
          target: 'claude',
          branch: 'main',
          created_at: '',
          running: true,
          attach_cmd: '',
          nudge_seq: 5,
          nudge_state: 'Completed',
        },
      ],
      ahead: 0,
      behind: 0,
      lines_added: 0,
      lines_removed: 0,
      files_changed: 0,
    });

    const { result } = renderHook(() => useSessions(), { wrapper: makeWrapper() });

    act(() => {
      result.current.ackSession('sess-1');
    });

    expect(localStorage.getItem('schmux:ack:sess-1')).toBe('5');
  });

  it('provides sub-context values via focused hooks', () => {
    const { result: sync } = renderHook(() => useSyncState(), { wrapper: makeWrapper() });
    const { result: overlay } = renderHook(() => useOverlay(), { wrapper: makeWrapper() });
    const { result: remote } = renderHook(() => useRemoteAccess(), { wrapper: makeWrapper() });
    const { result: monitor } = renderHook(() => useMonitor(), { wrapper: makeWrapper() });

    expect(sync.current).toHaveProperty('linearSyncResolveConflictStates');
    expect(sync.current).toHaveProperty('workspaceLockStates');
    expect(overlay.current).toHaveProperty('overlayEvents');
    expect(overlay.current).toHaveProperty('overlayUnreadCount');
    expect(remote.current).toHaveProperty('remoteAccessStatus');
    expect(remote.current).toHaveProperty('simulateRemote');
    expect(monitor.current).toHaveProperty('monitorEvents');
  });
});
