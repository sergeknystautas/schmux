import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import React from 'react';
import { MemoryRouter } from 'react-router-dom';
import { SessionsProvider } from './SessionsContext';
import { useClipboard } from './ClipboardContext';

// Mocks parallel SessionsContext.test.tsx — single dispatch site for the
// useSessionsWebSocket hook is mocked so we can drive the context's
// pendingClipboard state directly.
let mockReturnOverrides: Record<string, unknown> = {};

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => vi.fn() };
});

vi.mock('../hooks/useSessionsWebSocket', () => ({
  default: () => ({
    workspaces: [],
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
    pendingClipboard: {},
    clearPendingClipboard: vi.fn(),
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
  mockReturnOverrides = {};
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ClipboardContext (sub-context of SessionsProvider)', () => {
  it('exposes pendingClipboard from the WS hook', () => {
    mockReturnOverrides = {
      pendingClipboard: {
        'sess-1': {
          requestId: 'req-1',
          text: 'hello',
          byteCount: 5,
          strippedControlChars: 0,
        },
      },
    };

    const { result } = renderHook(() => useClipboard(), { wrapper: makeWrapper() });

    expect(result.current.pendingClipboard['sess-1']).toEqual({
      requestId: 'req-1',
      text: 'hello',
      byteCount: 5,
      strippedControlChars: 0,
    });
  });

  it('exposes empty pendingClipboard when nothing pending', () => {
    const { result } = renderHook(() => useClipboard(), { wrapper: makeWrapper() });
    expect(result.current.pendingClipboard).toEqual({});
  });

  it('exposes clearPendingClipboard from the WS hook', () => {
    const clearMock = vi.fn();
    mockReturnOverrides = {
      pendingClipboard: {
        'sess-1': { requestId: 'r1', text: 't', byteCount: 1, strippedControlChars: 0 },
      },
      clearPendingClipboard: clearMock,
    };

    const { result } = renderHook(() => useClipboard(), { wrapper: makeWrapper() });

    act(() => {
      result.current.clearPendingClipboard('sess-1');
    });

    expect(clearMock).toHaveBeenCalledWith('sess-1');
  });

  it('throws when used outside SessionsProvider', () => {
    // Suppress React's expected error log
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => renderHook(() => useClipboard())).toThrow(
      /useClipboard must be used within a SessionsProvider/
    );
    consoleError.mockRestore();
  });
});
