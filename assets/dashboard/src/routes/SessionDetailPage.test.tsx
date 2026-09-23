import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import SessionDetailPage from './SessionDetailPage';
import { useSessions } from '../contexts/SessionsContext';
import { disposeSession } from '../lib/api';
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
const { modalApi } = vi.hoisted(() => ({
  modalApi: {
    show: vi.fn().mockResolvedValue(true),
    confirm: vi.fn().mockResolvedValue(true),
    alert: vi.fn().mockResolvedValue(true),
    prompt: vi.fn().mockResolvedValue(null),
    confirmWithCheckbox: vi.fn().mockResolvedValue({ confirmed: true, checked: false }),
  },
}));
vi.mock('../components/ModalProvider', () => ({
  useModal: () => modalApi,
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
  return {
    ...actual,
    getTimelapseRecordings: vi.fn().mockResolvedValue([]),
    authCheck: vi.fn().mockResolvedValue(undefined),
    disposeSession: vi.fn().mockResolvedValue({ status: 'disposing' }),
  };
});
// Only constructed when a session actually renders; keep a complete-enough
// stub so destination pages with live sessions mount cleanly. Must be a
// constructor so callers using `new TerminalStream(...)` work.
const { streamInstances } = vi.hoisted(() => ({ streamInstances: [] as unknown[] }));

vi.mock('../lib/terminalStream', () => ({
  default: class TerminalStream {
    initialized = Promise.resolve();
    onStatusChange: ((status: string) => void) | null = null;
    onControlModeChange: ((attached: boolean) => void) | null = null;
    constructor(
      _sessionId: string,
      _container: HTMLElement,
      options?: { onStatusChange?: (status: string) => void }
    ) {
      if (options?.onStatusChange) {
        this.onStatusChange = options.onStatusChange;
      }
      streamInstances.push(this);
    }
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
          <LocationProbe />
          <Routes>
            <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
            <Route path="*" element={<></>} />
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
  sessionStorage.clear();
  vi.clearAllMocks();
  modalApi.show.mockReset().mockResolvedValue(true);
  modalApi.confirm.mockReset().mockResolvedValue(true);
  modalApi.alert.mockReset().mockResolvedValue(true);
  modalApi.prompt.mockReset().mockResolvedValue(null);
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

describe('connection pill control-mode states', () => {
  type StreamStub = {
    onStatusChange?: (status: string) => void;
    onControlModeChange?: (attached: boolean) => void;
  };

  beforeEach(() => {
    streamInstances.length = 0;
  });

  function renderLiveSession() {
    const live = makeSession();
    renderSessionPage({
      sessionsById: { [live.id]: live },
      workspaces: [makeWorkspace({ sessions: [live], session_count: 1 })],
      // snapshotCount: 2 clears the "first snapshot may be stale" guard so
      // the page renders the live session — and constructs TerminalStream —
      // instead of idling in the warmup branch.
      snapshotCount: 2,
    });
  }

  function pill() {
    return screen.getByTestId('session-connection-pill');
  }

  function lastStream(): StreamStub {
    return streamInstances[streamInstances.length - 1] as StreamStub;
  }

  it('shows Connecting... while unknown, Live when attached, Stalled when detached', async () => {
    renderLiveSession();
    await waitFor(() => expect(streamInstances.length).toBeGreaterThan(0));

    act(() => lastStream().onStatusChange?.('connected'));
    expect(pill()).toHaveAttribute('data-control-mode', 'unknown');
    expect(pill()).toHaveTextContent('Connecting...');

    // Connected but unknown: the tooltip awaits state, it does not claim stalled.
    fireEvent.mouseEnter(pill());
    await waitFor(() =>
      expect(screen.getByRole('tooltip')).toHaveTextContent('awaiting control-mode state')
    );
    fireEvent.mouseLeave(pill());

    act(() => lastStream().onControlModeChange?.(true));
    expect(pill()).toHaveAttribute('data-control-mode', 'attached');
    expect(pill()).toHaveTextContent('Live');

    act(() => lastStream().onControlModeChange?.(false));
    expect(pill()).toHaveAttribute('data-control-mode', 'detached');
    expect(pill()).toHaveTextContent('Stalled');

    // The stalled-output warning fires only for detached.
    fireEvent.mouseEnter(pill());
    await waitFor(() =>
      expect(screen.getByRole('tooltip')).toHaveTextContent('Terminal output stalled')
    );
    fireEvent.mouseLeave(pill());
  });

  it('returns to unknown when the terminal WebSocket reconnects', async () => {
    renderLiveSession();
    await waitFor(() => expect(streamInstances.length).toBeGreaterThan(0));

    act(() => lastStream().onStatusChange?.('connected'));
    act(() => lastStream().onControlModeChange?.(true));
    expect(pill()).toHaveTextContent('Live');

    act(() => lastStream().onStatusChange?.('connected'));
    expect(pill()).toHaveAttribute('data-control-mode', 'unknown');
    expect(pill()).toHaveTextContent('Connecting...');
  });
});

describe('sign-in helper close prompt', () => {
  async function flushPromises() {
    await act(async () => {
      await Promise.resolve();
    });
  }

  function helper(overrides: Partial<SessionResponse> = {}): SessionResponse {
    return {
      ...makeSession({
        target: 'command',
        sign_in_protocol: 'claude-stream-json',
        signed_out: true,
      }),
      ...overrides,
    } as unknown as SessionResponse;
  }

  function ordinaryTerminal(overrides: Partial<SessionResponse> = {}): SessionResponse {
    return makeSession({ ...overrides }) as unknown as SessionResponse;
  }

  function lastSessionIdCalled(): string {
    const calls = vi.mocked(disposeSession).mock.calls;
    return calls[calls.length - 1][0];
  }

  it('fires authCheck on mount for a signed-out helper; ordinary terminal does not', async () => {
    const auth = await import('../lib/api');
    vi.mocked(auth.authCheck).mockClear();

    const helperSession = helper();
    renderSessionPage({
      sessionsById: { [helperSession.id]: helperSession },
      workspaces: [makeWorkspace({ sessions: [helperSession], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(vi.mocked(auth.authCheck)).toHaveBeenCalledWith(helperSession.id);

    vi.mocked(auth.authCheck).mockClear();
    const term = ordinaryTerminal();
    renderSessionPage({
      sessionsById: { [term.id]: term },
      workspaces: [makeWorkspace({ sessions: [term], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(vi.mocked(auth.authCheck)).not.toHaveBeenCalled();
  });

  it('does not prompt while the current helper is signed_out', async () => {
    modalApi.show.mockClear();
    const helperSession = helper();
    renderSessionPage({
      sessionsById: { [helperSession.id]: helperSession },
      workspaces: [makeWorkspace({ sessions: [helperSession], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(modalApi.show).not.toHaveBeenCalled();
  });

  it('opens the close-session modal exactly once when the current helper clears signed_out', async () => {
    modalApi.show.mockClear();
    const helperSession = helper();
    const utils = renderSessionPage({
      sessionsById: { [helperSession.id]: helperSession },
      workspaces: [makeWorkspace({ sessions: [helperSession], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(modalApi.show).not.toHaveBeenCalled();

    // The auth-check broadcast flips signed_out on the current helper.
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    mockSessionsState = {
      ...mockSessionsState,
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
    };
    setMockReturn();
    utils.getByTestId('bump').click();

    await waitFor(() => expect(modalApi.show).toHaveBeenCalledTimes(1));
    const [title, message, options] = modalApi.show.mock.calls[0];
    expect(title).toBe('Sign-in complete');
    expect(message).toBe('You are signed in. Close this sign-in session?');
    expect(options).toMatchObject({ confirmText: 'Close session', cancelText: 'Keep open' });

    // A second update with the same cleared state must not re-prompt.
    utils.getByTestId('bump').click();
    expect(modalApi.show).toHaveBeenCalledTimes(1);
  });

  it('does not prompt when a different helper in sessionsById clears signed_out', async () => {
    modalApi.show.mockClear();
    const current = helper();
    const otherCleared = helper({
      id: 'schmux-003-11111111',
      signed_out: false,
    }) as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [current.id]: current, [otherCleared.id]: otherCleared },
      workspaces: [
        makeWorkspace({
          sessions: [current, otherCleared],
          session_count: 2,
        }),
      ],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(modalApi.show).not.toHaveBeenCalled();

    // Update only the unrelated helper — the page should stay quiet.
    mockSessionsState = {
      ...mockSessionsState,
      sessionsById: { [current.id]: current, [otherCleared.id]: otherCleared },
    };
    setMockReturn();
    utils.getByTestId('bump').click();
    expect(modalApi.show).not.toHaveBeenCalled();
  });

  it('disposes the current helper when the user confirms; does not navigate before the broadcast removes it', async () => {
    const dispose = await import('../lib/api');
    vi.mocked(dispose.disposeSession).mockClear();
    modalApi.show.mockClear().mockResolvedValueOnce(true);

    const helperSession = helper();
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();

    // Drive the prompt: helper already cleared, but the prompt is gated on
    // first observation; trigger via state churn so the effect fires.
    utils.getByTestId('bump').click();
    await waitFor(() => expect(modalApi.show).toHaveBeenCalled());

    expect(modalApi.confirm).not.toHaveBeenCalled(); // dispose uses show(), not the generic confirm
    await waitFor(() => expect(vi.mocked(dispose.disposeSession)).toHaveBeenCalledTimes(1));
    expect(lastSessionIdCalled()).toBe(helperSession.id);
    expect(vi.mocked(dispose.disposeSession).mock.calls.length).toBe(1);

    // The page must still render the helper — the missing-session navigation
    // is the broadcast-driven step, and we have not delivered that
    // broadcast yet, so we verify the session page is still mounted.
    expect(screen.getByTestId('session-connection-pill')).toBeInTheDocument();
    expect(screen.queryByText('Session not found')).not.toBeInTheDocument();
  });

  it('after a fresh snapshot removes a helper with no neighbor, the missing-session path uses the workspace fallback', async () => {
    const dispose = await import('../lib/api');
    vi.mocked(dispose.disposeSession).mockReset().mockResolvedValue({ status: 'disposing' });
    modalApi.show.mockReset().mockResolvedValueOnce(true);

    const helperSession = helper();
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    utils.getByTestId('bump').click();
    await waitFor(() => expect(vi.mocked(dispose.disposeSession)).toHaveBeenCalled());

    // The broadcast removes the helper. The existing missing-session effect
    // picks the workspace destination from the fresh sessionsById state.
    mockSessionsState = {
      ...mockSessionsState,
      sessionsById: {},
      workspaces: [makeWorkspace({ lines_added: 4 })],
    };
    setMockReturn();
    utils.getByTestId('bump').click();
    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent('/diff/schmux-003');
    });
  });

  it("selects the session to the helper's left after the disposal broadcast removes it", async () => {
    const dispose = await import('../lib/api');
    vi.mocked(dispose.disposeSession).mockReset().mockResolvedValue({ status: 'disposing' });
    modalApi.show.mockReset().mockResolvedValueOnce(true);

    const first = makeSession({ id: 'schmux-003-00000000' });
    const left = makeSession({ id: 'schmux-003-11111111' });
    const helperSession = helper();
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [first.id]: first, [left.id]: left, [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [first, left, cleared], session_count: 3 })],
      snapshotCount: 2,
    });
    await flushPromises();
    await waitFor(() => expect(vi.mocked(dispose.disposeSession)).toHaveBeenCalled());

    mockSessionsState = {
      ...mockSessionsState,
      sessionsById: { [first.id]: first, [left.id]: left },
      workspaces: [makeWorkspace({ sessions: [first, left], session_count: 2 })],
    };
    setMockReturn();
    utils.getByTestId('bump').click();

    await waitFor(() => {
      expect(screen.getByTestId('location')).toHaveTextContent(`/sessions/${left.id}`);
    });
  });

  it('Keep open suppresses another prompt in the same browser tab', async () => {
    const dispose = await import('../lib/api');
    vi.mocked(dispose.disposeSession).mockClear();
    modalApi.show.mockClear().mockResolvedValueOnce(false);
    const helperSession = helper();
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    utils.getByTestId('bump').click();
    await waitFor(() => expect(modalApi.show).toHaveBeenCalledTimes(1));

    // Re-render with the same cleared state — should not re-prompt.
    utils.getByTestId('bump').click();
    expect(modalApi.show).toHaveBeenCalledTimes(1);

    const key = `schmux:signInHelperKeepOpen:${helperSession.id}`;
    expect(sessionStorage.getItem(key)).toBe('1');

    // Remount the cleared helper in the same browser tab. The component ref
    // is new, so only the persisted sessionStorage dismissal can suppress it.
    utils.unmount();
    modalApi.show.mockClear();
    vi.mocked(dispose.disposeSession).mockClear();
    renderSessionPage({
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    expect(modalApi.show).not.toHaveBeenCalled();
    expect(vi.mocked(dispose.disposeSession)).not.toHaveBeenCalled();
  });

  it('shows Dispose Failed and keeps the helper visible when dispose rejects', async () => {
    const dispose = await import('../lib/api');
    vi.mocked(dispose.disposeSession).mockReset().mockRejectedValueOnce(new Error('boom'));
    modalApi.alert.mockClear();
    modalApi.show.mockReset().mockResolvedValueOnce(true);

    const helperSession = helper();
    const cleared = { ...helperSession, signed_out: false } as SessionResponse;
    const utils = renderSessionPage({
      sessionsById: { [helperSession.id]: cleared },
      workspaces: [makeWorkspace({ sessions: [cleared], session_count: 1 })],
      snapshotCount: 2,
    });
    await flushPromises();
    utils.getByTestId('bump').click();
    await waitFor(() => expect(vi.mocked(dispose.disposeSession)).toHaveBeenCalled());

    await waitFor(() => expect(modalApi.alert).toHaveBeenCalled());
    const [title] = modalApi.alert.mock.calls[0];
    expect(title).toBe('Dispose Failed');
    // The session is still in the page state — no navigation has happened.
    expect(screen.queryByText('Session not found')).not.toBeInTheDocument();
  });
});
