import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import CommitHistoryDAG from './CommitHistoryDAG';
import type { CommitGraphResponse, WorkspaceResponse } from '../lib/types';

// API mocks — individual fns so tests can inspect call counts.
const getCommitGraph = vi.fn();
const getDiff = vi.fn();
const getConfig = vi.fn();
const commitUncommit = vi.fn();

vi.mock('../lib/api', () => ({
  getCommitGraph: (...args: unknown[]) => getCommitGraph(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
  getConfig: (...args: unknown[]) => getConfig(...args),
  commitStage: vi.fn(),
  commitAmend: vi.fn(),
  commitDiscard: vi.fn(),
  commitUncommit: (...args: unknown[]) => commitUncommit(...args),
  spawnCommitSession: vi.fn(),
  pushToBranch: vi.fn(),
  createTab: vi.fn(),
}));

// Context mocks — closure over module-level vars so tests can update between renders.
let mockWorkspaces: WorkspaceResponse[] = [];
type LockState = { locked: boolean; syncProgress?: { current: number; total: number } };
let mockWorkspaceLockStates: Record<string, LockState> = {};

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ workspaces: mockWorkspaces }),
}));
vi.mock('../contexts/SyncContext', () => ({
  useSyncState: () => ({ workspaceLockStates: mockWorkspaceLockStates }),
}));
vi.mock('../hooks/useSync', () => ({
  useSync: () => ({
    handleSmartSync: vi.fn(),
    handleLinearSyncToMain: vi.fn(),
    handlePushToBranch: vi.fn(),
  }),
}));
vi.mock('./ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn() }),
}));
vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({ setPendingNavigation: vi.fn() }),
}));

// Tall-enough container so maxCommits > 0 on first observation.
class MockResizeObserver {
  constructor(private cb: (entries: Array<{ contentRect: { height: number } }>) => void) {}
  observe() {
    this.cb([{ contentRect: { height: 2000 } }]);
  }
  unobserve() {}
  disconnect() {}
}

// Controllable IntersectionObserver — tests call triggerIntersect() to simulate
// the truncation row scrolling into view.
let intersectCallbacks: Array<(entries: Array<{ isIntersecting: boolean }>) => void> = [];
class MockIntersectionObserver {
  constructor(cb: (entries: Array<{ isIntersecting: boolean }>) => void) {
    intersectCallbacks.push(cb);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}
function triggerIntersect() {
  for (const cb of intersectCallbacks) cb([{ isIntersecting: true }]);
}

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'git@github.com:test/repo.git',
    repo_name: 'test-repo',
    branch: 'feat',
    path: '/tmp/ws',
    session_count: 0,
    sessions: [],
    ahead: 3,
    behind: 5,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  } as WorkspaceResponse;
}

// Two-commit graph on branch `feat`; `headHash` is the tip the Uncommit button targets.
function makeGraph(headHash: string, headMessage: string): CommitGraphResponse {
  const commit = (hash: string, message: string, parents: string[]) => ({
    hash,
    short_hash: hash.slice(0, 7),
    message,
    author: 'tester',
    timestamp: '2026-01-01T00:00:00Z',
    parents,
    branches: ['feat'],
    is_head: [] as string[],
    workspace_ids: [] as string[],
  });
  const head = { ...commit(headHash, headMessage, ['base000']), is_head: ['feat'] };
  const base = { ...commit('base000', 'base commit', []), branches: ['feat', 'main'] };
  return {
    repo: 'test-repo',
    nodes: [head, base],
    branches: {
      main: { head: 'base000', is_main: true, workspace_ids: [] },
      feat: { head: headHash, is_main: false, workspace_ids: [] },
    },
    main_ahead_count: 0,
  };
}

// Same shape as makeGraph but flagged truncated, so the truncation row renders.
function makeTruncatedGraph(headHash: string): CommitGraphResponse {
  return { ...makeGraph(headHash, 'head commit'), local_truncated: true };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

function renderDAG() {
  return render(
    <MemoryRouter>
      <CommitHistoryDAG workspaceId="ws-1" />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', MockResizeObserver);
  vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
  intersectCallbacks = [];
  mockWorkspaces = [makeWorkspace()];
  mockWorkspaceLockStates = {};
  getCommitGraph.mockReset().mockResolvedValue({
    commits: [],
    local_branch: 'feat',
    default_branch: 'main',
    local_head: 'abc',
    origin_main_head: 'def',
  });
  getDiff.mockReset().mockResolvedValue({ files: [] });
  getConfig.mockReset().mockResolvedValue({ commit_message: {} });
  commitUncommit.mockReset().mockResolvedValue({ success: true });
});

describe('CommitHistoryDAG', () => {
  it('refetches commit graph on each sync_progress tick', async () => {
    const { rerender } = render(
      <MemoryRouter>
        <CommitHistoryDAG workspaceId="ws-1" />
      </MemoryRouter>
    );

    // Wait for the initial fetch triggered by container measurement.
    await waitFor(() => {
      expect(getCommitGraph).toHaveBeenCalled();
    });
    const initialCalls = getCommitGraph.mock.calls.length;

    // Simulate a sync_progress tick arriving from the backend.
    mockWorkspaceLockStates = {
      'ws-1': { locked: true, syncProgress: { current: 1, total: 5 } },
    };
    rerender(
      <MemoryRouter>
        <CommitHistoryDAG workspaceId="ws-1" />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(getCommitGraph.mock.calls.length).toBeGreaterThan(initialCalls);
    });

    const afterTick1 = getCommitGraph.mock.calls.length;

    // Another tick — the graph should refetch again, animating commits in
    // as each rebase completes server-side.
    mockWorkspaceLockStates = {
      'ws-1': { locked: true, syncProgress: { current: 2, total: 5 } },
    };
    rerender(
      <MemoryRouter>
        <CommitHistoryDAG workspaceId="ws-1" />
      </MemoryRouter>
    );

    await waitFor(() => {
      expect(getCommitGraph.mock.calls.length).toBeGreaterThan(afterTick1);
    });
  });

  // The uncommit POST is rejected with 409 by the backend when the hash sent no
  // longer matches HEAD, so the button must not re-enable while the row still
  // shows the commit that was just uncommitted.
  it('keeps Uncommit disabled until the refetched graph has rendered', async () => {
    getCommitGraph.mockResolvedValue(makeGraph('head111', 'head commit'));
    renderDAG();

    const button = await screen.findByRole('button', { name: 'Uncommit' });

    const refetch = deferred<CommitGraphResponse>();
    getCommitGraph.mockReturnValueOnce(refetch.promise);

    fireEvent.click(button);
    await waitFor(() => expect(commitUncommit).toHaveBeenCalledWith('ws-1', 'head111'));

    // Refetch still in flight: the graph below is stale, so the button stays disabled.
    expect(screen.getByRole('button', { name: 'Uncommitting...' })).toBeDisabled();

    refetch.resolve(makeGraph('base000', 'base commit'));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Uncommit' })).toBeEnabled());
  });

  it('does not drop the post-uncommit refetch when another fetch is already in flight', async () => {
    getCommitGraph.mockResolvedValue(makeGraph('head111', 'head commit'));
    const { rerender } = renderDAG();

    const button = await screen.findByRole('button', { name: 'Uncommit' });

    // Hold the uncommit POST open so a WebSocket-driven refetch can start first —
    // this is the real ordering, since the daemon broadcasts before it responds.
    const uncommitCall = deferred<{ success: boolean }>();
    commitUncommit.mockReturnValueOnce(uncommitCall.promise);
    fireEvent.click(button);
    await waitFor(() => expect(commitUncommit).toHaveBeenCalled());

    const staleFetch = deferred<CommitGraphResponse>();
    const trailingFetch = deferred<CommitGraphResponse>();
    getCommitGraph.mockReturnValueOnce(staleFetch.promise);
    getCommitGraph.mockReturnValueOnce(trailingFetch.promise);
    mockWorkspaces = [makeWorkspace({ files_changed: 2 })];
    rerender(
      <MemoryRouter>
        <CommitHistoryDAG workspaceId="ws-1" />
      </MemoryRouter>
    );
    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(2));

    // POST completes while that fetch — started before the uncommit landed — is in flight.
    uncommitCall.resolve({ success: true });
    staleFetch.resolve(makeGraph('head111', 'head commit'));

    // The refetch must be re-issued rather than dropped, and the button must stay
    // disabled until it renders.
    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(3));
    expect(screen.getByRole('button', { name: 'Uncommitting...' })).toBeDisabled();

    trailingFetch.resolve(makeGraph('base000', 'base commit'));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Uncommit' })).toBeEnabled());
  });
});

describe('infinite scroll', () => {
  it('requests 10 more commits when the truncation row scrolls into view', async () => {
    getCommitGraph.mockReset().mockResolvedValue(makeTruncatedGraph('aaa1111'));
    getDiff.mockResolvedValue({ files: [] });
    renderDAG();

    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(1));
    const firstMaxTotal = getCommitGraph.mock.calls[0][1].maxTotal;

    triggerIntersect();

    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(2));
    expect(getCommitGraph.mock.calls[1][1].maxTotal).toBe(firstMaxTotal + 10);
  });

  it('does not stack bumps while a fetch is in flight', async () => {
    const first = deferred<CommitGraphResponse>();
    getCommitGraph.mockReset().mockReturnValue(first.promise);
    getDiff.mockResolvedValue({ files: [] });
    renderDAG();

    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(1));

    // Three intersects while the first fetch is unresolved must not produce +30.
    triggerIntersect();
    triggerIntersect();
    triggerIntersect();

    first.resolve(makeTruncatedGraph('aaa1111'));

    // The new truncation row must be scrolled into view before the observer fires —
    // matches real-world behavior where the user has to scroll to see the row again
    // after the previous bump.
    await waitFor(() => expect(intersectCallbacks.length).toBeGreaterThan(0));
    triggerIntersect();

    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(2));
    const firstMaxTotal = getCommitGraph.mock.calls[0][1].maxTotal;
    expect(getCommitGraph.mock.calls[1][1].maxTotal).toBe(firstMaxTotal + 10);
  });

  it('stops observing when the backend reports no truncation', async () => {
    getCommitGraph.mockReset().mockResolvedValue(makeGraph('aaa1111', 'head commit'));
    getDiff.mockResolvedValue({ files: [] });
    renderDAG();

    await waitFor(() => expect(getCommitGraph).toHaveBeenCalledTimes(1));

    triggerIntersect();

    // No truncation row means nothing was observed, so no second fetch.
    await new Promise((r) => setTimeout(r, 0));
    expect(getCommitGraph).toHaveBeenCalledTimes(1);
  });
});
