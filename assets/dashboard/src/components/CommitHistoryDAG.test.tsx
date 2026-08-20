import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import CommitHistoryDAG from './CommitHistoryDAG';
import type { CommitGraphResponse, WorkspaceResponse } from '../lib/types';

// API mocks — individual fns so tests can inspect call counts.
const getCommitGraph = vi.fn();
const getDiff = vi.fn();
const getConfig = vi.fn();
const commitUncommit = vi.fn();
const getBranchDivergence = vi.fn();
const handlePushToBranch = vi.fn();
const alertMock = vi.fn();

vi.mock('../lib/api', () => ({
  getCommitGraph: (...args: unknown[]) => getCommitGraph(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
  getConfig: (...args: unknown[]) => getConfig(...args),
  getBranchDivergence: (...args: unknown[]) => getBranchDivergence(...args),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
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
    handlePushToBranch: (...args: unknown[]) => handlePushToBranch(...args),
    handlePushCommits: vi.fn(),
  }),
}));
vi.mock('./ModalProvider', () => ({
  useModal: () => ({ alert: alertMock, confirm: vi.fn() }),
}));
vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
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

// Three commits on `feat` ahead of `main`; `base000` is the merge base already
// on origin/main. The feature-branch commits (head111, c200000, c100000) should
// each get a Push button; base000 should not (already on origin/main).
function makePushableGraph(): CommitGraphResponse {
  const commit = (hash: string, message: string, parents: string[], branches: string[]) => ({
    hash,
    short_hash: hash.slice(0, 7),
    message,
    author: 'tester',
    timestamp: '2026-01-01T00:00:00Z',
    parents,
    branches,
    is_head: [] as string[],
    workspace_ids: [] as string[],
  });
  const head = { ...commit('head111', 'head commit', ['c200000'], ['feat']), is_head: ['feat'] };
  const c2 = commit('c200000', 'second commit', ['c100000'], ['feat']);
  const c1 = commit('c100000', 'first commit', ['base000'], ['feat']);
  const base = commit('base000', 'base commit', [], ['feat', 'main']);
  return {
    repo: 'test-repo',
    nodes: [head, c2, c1, base],
    branches: {
      main: { head: 'base000', is_main: true, workspace_ids: [] },
      feat: { head: 'head111', is_main: false, workspace_ids: [] },
    },
    main_ahead_count: 3,
  };
}

// Workspace ON the default branch, two commits ahead of origin/main. Mirrors
// the backend's response shape for this case: the branches map collapses to a
// single 'main' entry holding the LOCAL head (map-key collision), and
// remote_branch_head carries the origin/main position.
function makeOnMainGraph(): CommitGraphResponse {
  const commit = (hash: string, message: string, parents: string[]) => ({
    hash,
    short_hash: hash.slice(0, 7),
    message,
    author: 'tester',
    timestamp: '2026-01-01T00:00:00Z',
    parents,
    branches: ['main'],
    is_head: [] as string[],
    workspace_ids: [] as string[],
  });
  const local2 = { ...commit('local22', 'local two', ['local11']), is_head: ['main'] };
  const local1 = commit('local11', 'local one', ['base000']);
  const base = commit('base000', 'origin base', []);
  return {
    repo: 'test-repo',
    nodes: [local2, local1, base],
    branches: {
      main: { head: 'local22', is_main: true, workspace_ids: ['ws-1'] },
    },
    remote_branch_head: 'base000',
    main_ahead_count: 0,
  };
}

// Feature branch 1 commit ahead while origin/main has moved ahead: the backend
// omits the main-ahead commits from nodes (they collapse into the "Pull from
// main" row), so branches.main.head names a hash that is NOT among the loaded
// nodes and fork_point is the only "on origin/main" boundary.
function makeBehindMainGraph(): CommitGraphResponse {
  const commit = (hash: string, message: string, parents: string[], branches: string[]) => ({
    hash,
    short_hash: hash.slice(0, 7),
    message,
    author: 'tester',
    timestamp: '2026-01-01T00:00:00Z',
    parents,
    branches,
    is_head: [] as string[],
    workspace_ids: [] as string[],
  });
  const local = {
    ...commit('local11', 'my only commit', ['fork000'], ['feat']),
    is_head: ['feat'],
  };
  const fork = commit('fork000', 'fork point', ['ctx1000'], ['feat', 'main']);
  const ctx1 = commit('ctx1000', 'older main history', ['ctx2000'], ['feat', 'main']);
  const ctx2 = commit('ctx2000', 'even older history', [], ['feat', 'main']);
  return {
    repo: 'test-repo',
    nodes: [local, fork, ctx1, ctx2],
    branches: {
      main: { head: 'maintip', is_main: true, workspace_ids: [] }, // NOT in nodes
      feat: { head: 'local11', is_main: false, workspace_ids: [] },
    },
    fork_point: 'fork000',
    main_ahead_count: 1,
  };
}

describe('push button eligibility when behind main', () => {
  it('falls back to fork_point when origin/main head is not in the loaded nodes', async () => {
    getCommitGraph.mockResolvedValue(makeBehindMainGraph());
    renderDAG();

    await screen.findByText('my only commit');
    // Only the single local commit is unpushed — not the fork point or the
    // context commits below it.
    expect(screen.getAllByTestId('push-commit-btn')).toHaveLength(1);
  });

  it('counts 1 commit in the modal, not the whole loaded graph', async () => {
    getCommitGraph.mockResolvedValue(makeBehindMainGraph());
    renderDAG();

    const message = await screen.findByText('my only commit');
    const row = message.closest<HTMLElement>('.commit-dag__row')!;
    fireEvent.click(within(row).getByTestId('push-commit-btn'));

    expect(await screen.findByTestId('push-modal')).toBeInTheDocument();
    // One commit: the submit button says so, and the mode choice (a false
    // choice at n=1) is omitted entirely. The workspace is behind main, so the
    // modal opens on the branch target where the count is an estimate ("up to").
    expect(screen.getByTestId('push-modal-submit').textContent).toMatch(
      /Push up to 1 commit \(1 push\)/
    );
    expect(screen.queryByTestId('push-modal-mode-percommit-label')).not.toBeInTheDocument();
  });
});

describe('you-are-here push buttons', () => {
  it('hides "Push to branch" on the default branch, keeping "Push to main"', async () => {
    mockWorkspaces = [
      makeWorkspace({ branch: 'main', default_branch: 'main', ahead: 2, behind: 0 }),
    ];
    getCommitGraph.mockResolvedValue(makeOnMainGraph());
    renderDAG();

    await screen.findByText('local two');
    expect(screen.getByText('Push to main')).toBeInTheDocument();
    expect(screen.queryByText('Push to branch')).not.toBeInTheDocument();
  });

  it('shows both buttons on a feature branch', async () => {
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    await screen.findByText('head commit');
    expect(screen.getByText('Push to main')).toBeInTheDocument();
    expect(screen.getByText('Push to branch')).toBeInTheDocument();
  });

  it('hides "Push to branch" for local: workspaces', async () => {
    mockWorkspaces = [makeWorkspace({ repo: 'local:talkback' })];
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    await screen.findByText('head commit');
    expect(screen.queryByText('Push to branch')).not.toBeInTheDocument();
  });
});

describe('push button eligibility', () => {
  it('shows Push on unpushed commits when the workspace is on the default branch', async () => {
    mockWorkspaces = [makeWorkspace({ branch: 'main', default_branch: 'main' })];
    getCommitGraph.mockResolvedValue(makeOnMainGraph());
    renderDAG();

    await screen.findByText('local two');
    // The two local-only commits get Push buttons; the origin/main base does not.
    expect(screen.getAllByTestId('push-commit-btn')).toHaveLength(2);
  });

  it('shows Push on unpushed feature-branch commits but not on commits already on main', async () => {
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    const pushButtons = await screen.findAllByTestId('push-commit-btn');
    expect(pushButtons).toHaveLength(3);
  });

  // Push renders on every unpushed row but Uncommit only on the head row, so
  // Push must be the LAST (rightmost) button — otherwise it shifts position
  // between rows as the mouse moves up and down the graph.
  it('renders Push after Uncommit on the head row', async () => {
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    const headMessage = await screen.findByText('head commit');
    const row = headMessage.closest<HTMLElement>('.commit-dag__row')!;
    const uncommit = within(row).getByRole('button', { name: 'Uncommit' });
    const push = within(row).getByTestId('push-commit-btn');
    expect(uncommit.compareDocumentPosition(push) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('hides Push for sapling workspaces', async () => {
    mockWorkspaces = [makeWorkspace({ vcs: 'sapling' })];
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    // Wait for a commit row to render so the assertion is not vacuously true.
    await screen.findByText('head commit');
    expect(screen.queryAllByTestId('push-commit-btn')).toHaveLength(0);
  });

  it('hides per-commit push for local: workspaces', async () => {
    mockWorkspaces = [makeWorkspace({ repo: 'local:talkback' })];
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    await screen.findByText('head commit');
    expect(screen.queryAllByTestId('push-commit-btn')).toHaveLength(0);
  });

  it('opens the push modal with counts when Push… is clicked', async () => {
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();

    // Target the tip commit's row so the count assertion is deterministic
    // regardless of render order. head111 has 3 unpushed ancestors-or-self.
    const headMessage = await screen.findByText('head commit');
    const row = headMessage.closest<HTMLElement>('.commit-dag__row')!;
    fireEvent.click(within(row).getByTestId('push-commit-btn'));

    expect(await screen.findByTestId('push-modal')).toBeInTheDocument();
    // All 3 feature-branch commits are unpushed → per-commit mode advertises 3 pushes.
    expect(screen.getByTestId('push-modal-mode-percommit-label').textContent).toMatch(/3 pushes/);

    // Cancel closes it.
    fireEvent.click(screen.getByTestId('push-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('push-modal')).not.toBeInTheDocument());
  });
});

describe('shift+click force push', () => {
  // Renders the DAG with a pushable feature branch and returns the button.
  async function renderPushableDAG() {
    getCommitGraph.mockResolvedValue(makePushableGraph());
    renderDAG();
    await screen.findByText('head commit');
    return screen.getByText('Push to branch');
  }

  beforeEach(() => {
    getBranchDivergence.mockReset();
    handlePushToBranch.mockReset();
    alertMock.mockClear();
  });

  it('opens ForcePushModal when diverged', async () => {
    getBranchDivergence.mockResolvedValue({
      branch: 'feature/foo',
      local_head: 'a'.repeat(40),
      remote_head: 'b'.repeat(40),
      local_commits: [
        {
          hash: 'a'.repeat(40),
          short_hash: 'aaaaaaa',
          author: 'Alice',
          timestamp: '2026-08-19T12:00:00Z',
          subject: 'local work',
        },
      ],
      remote_commits: [
        {
          hash: 'b'.repeat(40),
          short_hash: 'bbbbbbb',
          author: 'Bob',
          timestamp: '2026-08-19T11:00:00Z',
          subject: 'remote work',
        },
      ],
      local_total: 1,
      remote_total: 1,
    });
    const button = await renderPushableDAG();
    fireEvent.click(button, { shiftKey: true });
    await screen.findByTestId('force-push-modal');
    expect(handlePushToBranch).not.toHaveBeenCalled();
  });

  it('shows a pending state on the button while the divergence check runs', async () => {
    let resolveDivergence: (value: unknown) => void = () => {};
    getBranchDivergence.mockReturnValue(
      new Promise((resolve) => {
        resolveDivergence = resolve;
      })
    );
    const button = await renderPushableDAG();
    fireEvent.click(button, { shiftKey: true });

    // Button reacts immediately — the fetch runs origin, so a silent button
    // reads as a dead click.
    await waitFor(() => expect(screen.getByText('Pushing')).toBeInTheDocument());
    expect(screen.getByText('Pushing').closest('button')).toBeDisabled();

    resolveDivergence({
      branch: 'feature/foo',
      local_head: 'a'.repeat(40),
      remote_head: 'b'.repeat(40),
      local_commits: [
        {
          hash: 'a'.repeat(40),
          short_hash: 'aaaaaaa',
          author: 'Alice',
          timestamp: '2026-08-19T12:00:00Z',
          subject: 'local work',
        },
      ],
      remote_commits: [
        {
          hash: 'b'.repeat(40),
          short_hash: 'bbbbbbb',
          author: 'Bob',
          timestamp: '2026-08-19T11:00:00Z',
          subject: 'remote work',
        },
      ],
      local_total: 1,
      remote_total: 1,
    });

    // The flow is not over when the modal opens — the button stays busy until
    // the user decides, like the plain push-to-branch button does.
    await screen.findByTestId('force-push-modal');
    expect(screen.getByText('Pushing')).toBeInTheDocument();
    expect(screen.getByText('Pushing').closest('button')).toBeDisabled();

    // Closing the modal releases it.
    fireEvent.click(screen.getByTestId('force-push-modal-cancel'));
    await waitFor(() => expect(screen.getByText('Push to branch')).toBeInTheDocument());
  });

  it('falls through to a normal push when the remote side is empty', async () => {
    getBranchDivergence.mockResolvedValue({
      branch: 'feature/foo',
      local_head: 'a'.repeat(40),
      remote_head: '',
      local_commits: [
        {
          hash: 'a'.repeat(40),
          short_hash: 'aaaaaaa',
          author: 'Alice',
          timestamp: '2026-08-19T12:00:00Z',
          subject: 'local work',
        },
      ],
      remote_commits: [],
      local_total: 1,
      remote_total: 0,
    });
    const button = await renderPushableDAG();
    fireEvent.click(button, { shiftKey: true });
    await waitFor(() => expect(handlePushToBranch).toHaveBeenCalled());
    expect(screen.queryByTestId('force-push-modal')).not.toBeInTheDocument();
  });

  it('alerts without a modal when strictly behind', async () => {
    getBranchDivergence.mockResolvedValue({
      branch: 'feature/foo',
      local_head: 'a'.repeat(40),
      remote_head: 'b'.repeat(40),
      local_commits: [],
      remote_commits: [
        {
          hash: 'b'.repeat(40),
          short_hash: 'bbbbbbb',
          author: 'Bob',
          timestamp: '2026-08-19T11:00:00Z',
          subject: 'remote work',
        },
      ],
      local_total: 0,
      remote_total: 1,
    });
    const button = await renderPushableDAG();
    fireEvent.click(button, { shiftKey: true });
    await waitFor(() =>
      expect(alertMock).toHaveBeenCalledWith(
        'Push rejected',
        expect.stringContaining('Pull or merge')
      )
    );
    expect(screen.queryByTestId('force-push-modal')).not.toBeInTheDocument();
    expect(handlePushToBranch).not.toHaveBeenCalled();
  });

  it('alerts when the divergence fetch fails', async () => {
    getBranchDivergence.mockRejectedValue(new Error('boom'));
    const button = await renderPushableDAG();
    fireEvent.click(button, { shiftKey: true });
    await waitFor(() => expect(alertMock).toHaveBeenCalled());
    expect(screen.queryByTestId('force-push-modal')).not.toBeInTheDocument();
  });
});
