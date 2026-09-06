import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import SessionDetailPage from './SessionDetailPage';
import { useSessions } from '../contexts/SessionsContext';
import type { WorkspaceResponse, SessionResponse } from '../lib/types';

// Mutable sessions state. snapshotCount stands in for the provider-scoped
// WS snapshot counter — bumped to >= 2 to clear the "first snapshot may
// be stale" guard so the redirect effect can fire.
let mockSessionsState: {
  sessionsById: Record<string, SessionResponse>;
  workspaces: WorkspaceResponse[];
  loading: boolean;
  error: string | null;
  snapshotCount: number;
};

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: vi.fn(),
}));
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: {}, loading: false, error: null, reloadConfig: vi.fn() }),
}));
vi.mock('../hooks/useVersionInfo', () => ({
  default: () => ({ versionInfo: null, loading: false }),
}));
vi.mock('../contexts/ClipboardContext', () => ({
  useClipboard: () => ({ pendingClipboard: {}, clearPendingClipboard: vi.fn() }),
}));
vi.mock('../contexts/ViewedSessionsContext', () => ({
  useViewedSessions: () => ({ markAsViewed: vi.fn() }),
}));
vi.mock('../contexts/KeyboardContext', () => ({
  useKeyboardMode: () => ({ registerAction: vi.fn(), unregisterAction: vi.fn() }),
}));
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ prompt: vi.fn(), confirm: vi.fn(), alert: vi.fn() }),
}));
// After redirect, the live session page renders WorkspaceHeader and
// SessionTabs which depend on SyncContext (no provider in this test).
// Stub them to keep the redirect target renderable.
vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/SessionSidebar', () => ({
  default: () => <div data-testid="session-sidebar" />,
}));
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return { ...actual, getTimelapseRecordings: vi.fn().mockResolvedValue([]) };
});
// Only constructed when a session actually renders; keep a complete-enough
// stub so destination pages with live sessions mount cleanly. Must be a
// constructor so callers using `new TerminalStream(...)` work.
vi.mock('../lib/terminalStream', () => ({
  default: class TerminalStream {
    initialized = Promise.resolve();
    connect = vi.fn();
    disconnect = vi.fn();
    focus = vi.fn();
    setNativeTyping = vi.fn();
    disableDiagnostics = vi.fn();
    disableWriteRaceDiagnostics = vi.fn();
    jumpToBottom = vi.fn();
    sendInput = vi.fn();
    toggleSelectionMode = vi.fn();
    resizeTerminal = vi.fn();
    isAtBottom = vi.fn(() => true);
    onControlModeChange = vi.fn();
    onStatsUpdate = vi.fn();
    onDiagnosticComplete = vi.fn();
    onIOWorkspaceStatsUpdate = vi.fn();
    onIOWorkspaceDiagnosticComplete = vi.fn();
    sendDiagnostic = vi.fn();
    sendIOWorkspaceDiagnostic = vi.fn();
    downloadOutput = vi.fn();
    enableDiagnostics = vi.fn();
    enableWriteRaceDiagnostics = vi.fn();
    lifecycleLogging = false;
    recreationCount = 0;
    slowReactRenders: unknown[] = [];
  },
}));

const useSessionsMock = vi.mocked(useSessions);

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'schmux-003',
    repo: 'test-repo',
    branch: 'main',
    path: '/tmp/test',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  } as WorkspaceResponse;
}

function makeSession(overrides: Partial<SessionResponse> = {}): SessionResponse {
  return {
    id: 'schmux-003-f8ea6152',
    workspace_id: 'schmux-003',
    target: 'claude',
    branch: 'main',
    created_at: '2026-09-05T00:00:00Z',
    last_output_at: null,
    attach_cmd: 'tmux attach -t schmux',
    running: true,
    fence: false,
    status: 'running',
    ...overrides,
  } as unknown as SessionResponse;
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function setMockReturn() {
  useSessionsMock.mockReturnValue({
    sessionsById: mockSessionsState.sessionsById,
    workspaces: mockSessionsState.workspaces,
    loading: mockSessionsState.loading,
    error: mockSessionsState.error,
    snapshotCount: mockSessionsState.snapshotCount,
    ackSession: vi.fn(),
    waitForSession: vi.fn(),
    connected: true,
  } as unknown as ReturnType<typeof useSessions>);
}

// Render the page inside a stateful host so we can drive re-renders from
// the test by changing the host's `tick` state.
function renderSessionPage(
  initial: {
    sessionsById?: Record<string, SessionResponse>;
    workspaces?: WorkspaceResponse[];
    snapshotCount?: number;
  } = {}
) {
  mockSessionsState = {
    sessionsById: initial.sessionsById ?? {},
    workspaces: initial.workspaces ?? [],
    loading: false,
    error: null,
    snapshotCount: initial.snapshotCount ?? 1,
  };
  setMockReturn();

  function Host() {
    const [, force] = React.useState(0);
    return (
      <>
        <button data-testid="bump" onClick={() => force((n) => n + 1)} style={{ display: 'none' }}>
          bump
        </button>
        <MemoryRouter initialEntries={['/sessions/schmux-003-f8ea6152']}>
          <Routes>
            <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
            <Route path="*" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>
      </>
    );
  }

  const utils = render(<Host />);
  const bump = () => {
    act(() => {
      utils.getByTestId('bump').click();
    });
  };
  const nextSnapshot = (workspaces?: WorkspaceResponse[]) => {
    mockSessionsState = {
      ...mockSessionsState,
      snapshotCount: mockSessionsState.snapshotCount + 1,
      workspaces: workspaces ?? mockSessionsState.workspaces,
    };
    setMockReturn();
    bump();
  };
  return { ...utils, nextSnapshot };
}

beforeEach(() => {
  localStorage.clear();
});

describe('SessionDetailPage missing-session redirect', () => {
  it('redirects to the workspace first session when the dead session prefix-matches', async () => {
    const live = makeSession({ id: 'schmux-003-11111111' });
    const utils = renderSessionPage({
      sessionsById: { [live.id]: live }, // destination page resolves; no loop
      workspaces: [makeWorkspace({ sessions: [live], session_count: 1 })],
    });
    utils.nextSnapshot();
    // The first-session redirect lands on /sessions/<live-id>; this URL
    // also matches the :sessionId route, but the live session is in the
    // page's sessionsById, so the page renders the live session — the
    // absence of "Session not found" is the assertion.
    await waitFor(() => {
      expect(screen.queryByText('Session not found')).not.toBeInTheDocument();
    });
  });

  it('redirects to the diff view when the workspace has no sessions but changes', async () => {
    const utils = renderSessionPage({
      workspaces: [makeWorkspace({ lines_added: 10, lines_removed: 5 })],
    });
    utils.nextSnapshot();
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/diff/schmux-003');
    });
  });

  it('redirects to spawn when the workspace is empty and clean', async () => {
    const utils = renderSessionPage({ workspaces: [makeWorkspace()] });
    utils.nextSnapshot();
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/spawn?workspace_id=schmux-003');
    });
  });

  it('redirects to home when no workspace prefix-matches', async () => {
    const utils = renderSessionPage({ workspaces: [makeWorkspace({ id: 'schmux-004' })] });
    utils.nextSnapshot();
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/');
    });
  });

  it('redirects immediately on a fresh mount when snapshots already settled', async () => {
    // The remount case: provider has snapshotCount >= 2 from an existing
    // connection; the page mounts straight into the redirect with no
    // further snapshots and no page-local warmup.
    const utils = renderSessionPage({
      workspaces: [makeWorkspace({ lines_added: 3 })],
      snapshotCount: 2,
    });
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/diff/schmux-003');
    });
  });

  it('renders no error screen once the session is confirmed missing', async () => {
    const utils = renderSessionPage({ workspaces: [makeWorkspace()] });
    utils.nextSnapshot();
    await waitFor(() => {
      expect(screen.queryByText('Session unavailable')).not.toBeInTheDocument();
    });
  });

  it('does not navigate before two snapshots', () => {
    renderSessionPage({ workspaces: [makeWorkspace()], snapshotCount: 1 });
    expect(screen.getByText('Session not found')).toBeInTheDocument();
  });
});
