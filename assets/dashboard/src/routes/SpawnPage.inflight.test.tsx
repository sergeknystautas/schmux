import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useSyncExternalStore } from 'react';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { ConfigResponse, SpawnRequest, SpawnResult, WorkspaceResponse } from '../lib/types';
import { makeConfig } from '../lib/test-factories';
import { resetSpawnInflightForTests } from '../lib/spawn-inflight';

// --- Mocks (mirrors SpawnPage.fence.test.tsx) ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
const mockGetStyles = vi.fn<() => Promise<{ styles: unknown[] }>>();
const mockAlert = vi.fn();

vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
  suggestBranch: (...args: unknown[]) => mockSuggestBranch(...args),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  getStyles: (...args: unknown[]) => mockGetStyles(...(args as [])),
}));

vi.mock('../lib/spawn-api', () => ({
  getSpawnEntries: vi.fn().mockResolvedValue([]),
  getPromptHistory: vi.fn().mockResolvedValue([]),
}));

vi.mock('../lib/quicklaunch', () => ({
  getQuickLaunchItems: () => [],
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({
    alert: mockAlert,
    confirm: vi.fn().mockResolvedValue(true),
    prompt: vi.fn(),
  }),
}));

let configContextValue: ConfigResponse | null = null;
let workspacesContextValue: WorkspaceResponse[] = [];
// External store so tests can poke sessionsById and trigger a re-render.
const sessionsByIdStore = vi.hoisted(() => {
  type S = Record<string, unknown>;
  let current: S = {};
  const listeners = new Set<() => void>();
  return {
    get: (): S => current,
    set: (v: S): void => {
      current = v;
      listeners.forEach((l) => l());
    },
    subscribe: (l: () => void): (() => void) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
  };
});
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: configContextValue,
    loading: false,
    error: null,
    reloadConfig: vi.fn(),
    getRepoName: (url: string) => url,
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => {
    const sessionsById = useSyncExternalStore(
      sessionsByIdStore.subscribe,
      sessionsByIdStore.get,
      sessionsByIdStore.get
    );
    return {
      workspaces: workspacesContextValue,
      loading: false,
      error: '',
      connected: true,
      waitForSession: vi.fn().mockResolvedValue(true),
      sessionsById,
      ackSession: vi.fn(),
      pendingNavigation: null,
      setPendingNavigation: vi.fn(),
      clearPendingNavigation: vi.fn(),
      curatorEvents: {},
    };
  },
}));

vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
  }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: { value: string; onChange: (v: string) => void; disabled?: boolean }) => (
    <textarea
      data-testid="spawn-prompt"
      value={props.value}
      disabled={props.disabled}
      onChange={(e) => props.onChange(e.target.value)}
    />
  ),
}));
vi.mock('../components/Tooltip', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../components/RemoteHostSelector', () => ({
  default: (props: { disabled?: boolean }) => (
    <div data-testid="remote-host-selector" data-disabled={props.disabled ? 'true' : 'false'} />
  ),
}));

import SpawnPage from './SpawnPage';

function renderSpawnPage(initialEntry = '/spawn') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <SpawnPage />
    </MemoryRouter>
  );
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

// Fill the form enough to pass validateForm and click Engage.
// makeConfig() defaults: repo url https://github.com/user/gitrepo.git, model id
// "claude", branch_suggest targets empty — so no naming phase, the branch input
// is visible, and validateForm requires a branch value.
async function fillAndEngage() {
  await waitFor(() => expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument());
  fireEvent.change(screen.getByTestId('spawn-repo-select'), {
    target: { value: 'https://github.com/user/gitrepo.git' },
  });
  fireEvent.change(screen.getByTestId('agent-select'), { target: { value: 'claude' } });
  fireEvent.change(screen.getByPlaceholderText('e.g. feature/my-branch'), {
    target: { value: 'feature/test-branch' },
  });
  fireEvent.change(screen.getByTestId('spawn-prompt'), { target: { value: 'do the thing' } });
  fireEvent.click(screen.getByTestId('spawn-submit'));
}

describe('SpawnPage in-flight lock', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    sessionsByIdStore.set({});
    const cfg = makeConfig();
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('disables every editable control while spawning', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    expect(screen.getByTestId('spawn-prompt')).toBeDisabled();
    expect(screen.getByTestId('spawn-repo-select')).toBeDisabled();
    expect(screen.getByTestId('agent-select')).toBeDisabled();
    expect(screen.getByTestId('remote-host-selector')).toHaveAttribute('data-disabled', 'true');
    expect(screen.getByText('Spawning...')).toBeInTheDocument();

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });

  it('a remounted form is still locked while the spawn is in flight', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    expect(screen.getByTestId('spawn-prompt')).toBeDisabled();
    expect(screen.getByText('Spawning...')).toBeInTheDocument();

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });

  it('surfaces an error that happened while unmounted, once, and unlocks', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    await act(async () => {
      d.resolve([{ error: 'boom' }]);
    });

    renderSpawnPage();
    await waitFor(() =>
      expect(mockAlert).toHaveBeenCalledWith('Spawn Failed', 'Failed to spawn: boom')
    );
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());
    // Draft survived the failure.
    expect(sessionStorage.getItem('spawn-draft-fresh')).toContain('do the thing');
    expect(mockAlert).toHaveBeenCalledTimes(1);
  });

  it('a tmux error shows the tmux banner instead of the alert', async () => {
    mockSpawnSessions.mockRejectedValue(new Error('tmux is required to spawn sessions'));

    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByTestId('tmux-error')).toBeInTheDocument());
    expect(mockAlert).not.toHaveBeenCalled();
    expect(screen.getByTestId('spawn-submit')).not.toBeDisabled();
  });

  it('unlocks when a spawned session id appears in dashboard data', async () => {
    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByText('Downloading session...')).toBeInTheDocument());

    // Push the new sessionsById into the mock's external store, which notifies
    // subscribers and re-renders consumers; the inflight hook then observes it
    // and clears the entry.
    sessionsByIdStore.set({ 'sess-1': {} });
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());

    // The store cleared the draft on success; unlocking on a still-mounted form
    // must not resurrect it from stale field state.
    expect(sessionStorage.getItem('spawn-draft-fresh')).toBeNull();
  });

  it('an in-flight fresh spawn does not lock a workspace form', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    workspacesContextValue = [
      {
        id: 'ws-other',
        repo: 'https://github.com/user/gitrepo.git',
        branch: 'main',
        sessions: [],
      } as unknown as WorkspaceResponse,
    ];
    renderSpawnPage('/spawn?workspace_id=ws-other');
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });
});
