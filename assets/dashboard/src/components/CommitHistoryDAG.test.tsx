import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import CommitHistoryDAG from './CommitHistoryDAG';
import type { WorkspaceResponse } from '../lib/types';

// API mocks — individual fns so tests can inspect call counts.
const getCommitGraph = vi.fn();
const getDiff = vi.fn();
const getConfig = vi.fn();

vi.mock('../lib/api', () => ({
  getCommitGraph: (...args: unknown[]) => getCommitGraph(...args),
  getDiff: (...args: unknown[]) => getDiff(...args),
  getConfig: (...args: unknown[]) => getConfig(...args),
  commitStage: vi.fn(),
  commitAmend: vi.fn(),
  commitDiscard: vi.fn(),
  commitUncommit: vi.fn(),
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

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', MockResizeObserver);
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
});
