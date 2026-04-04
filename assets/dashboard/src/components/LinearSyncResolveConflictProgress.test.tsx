import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import LinearSyncResolveConflictProgress from './LinearSyncResolveConflictProgress';
import type { ResolveConflictRecordPayload, WorkspaceResponse } from '../lib/types';

// --- Mocks ---

const navigate = vi.fn();
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

vi.mock('../lib/api', () => ({
  getCommitGraph: vi.fn(),
}));

let mockWorkspaces: WorkspaceResponse[] = [];

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: mockWorkspaces,
  }),
}));

vi.mock('../hooks/useSync', () => ({
  useSync: () => ({
    handleLinearSyncFromMain: vi.fn(),
  }),
}));

// --- Helpers ---

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'test-repo',
    branch: 'feat',
    path: '/tmp/ws',
    session_count: 1,
    sessions: [
      {
        id: 'session-1',
        target: 'claude',
        branch: 'feat',
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
    ...overrides,
  };
}

function makeState(
  overrides: Partial<ResolveConflictRecordPayload> = {}
): ResolveConflictRecordPayload {
  return {
    type: 'linear_sync_resolve_conflict',
    workspace_id: 'ws-1',
    status: 'in_progress',
    hash: 'abc1234567890',
    started_at: new Date().toISOString(),
    steps: [
      {
        action: 'cherry-pick',
        status: 'in_progress',
        message: ['Cherry-picking commit'],
        at: new Date().toISOString(),
      },
    ],
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockWorkspaces = [];
});

// --- Tests ---

describe('LinearSyncResolveConflictProgress', () => {
  it('renders in-progress state with spinner', () => {
    const state = makeState({ status: 'in_progress' });
    mockWorkspaces = [makeWorkspace()];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(screen.getByText('Resolving conflicts...')).toBeInTheDocument();
  });

  it('does NOT auto-dismiss when status is done and no more commits behind', () => {
    const state = makeState({ status: 'done' });
    mockWorkspaces = [makeWorkspace({ behind: 0 })];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(navigate).not.toHaveBeenCalled();
  });

  it('does NOT auto-dismiss when done with no sessions', () => {
    const state = makeState({ status: 'done' });
    mockWorkspaces = [makeWorkspace({ behind: 0, sessions: [], session_count: 0 })];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(navigate).not.toHaveBeenCalled();
  });

  it('does NOT auto-dismiss when done but has more commits behind', () => {
    const state = makeState({ status: 'done' });
    mockWorkspaces = [makeWorkspace({ behind: 3 })];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText('Continue syncing')).toBeInTheDocument();
  });

  it('does NOT auto-dismiss when status is in_progress', () => {
    const state = makeState({ status: 'in_progress' });
    mockWorkspaces = [makeWorkspace({ behind: 0 })];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(navigate).not.toHaveBeenCalled();
  });

  it('does NOT auto-dismiss when status is failed', () => {
    const state = makeState({ status: 'failed' });
    mockWorkspaces = [makeWorkspace({ behind: 0 })];

    render(
      <LinearSyncResolveConflictProgress
        workspaceId="ws-1"
        resolveConflict={state}
        displayHash="abc1234"
      />
    );

    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText('Conflict resolution failed')).toBeInTheDocument();
  });

  describe('terminal panel', () => {
    it('does not render terminal panel when tmux_session is absent', () => {
      const state = makeState({ status: 'in_progress' });
      mockWorkspaces = [makeWorkspace()];

      render(
        <LinearSyncResolveConflictProgress
          workspaceId="ws-1"
          resolveConflict={state}
          displayHash="abc1234"
        />
      );

      expect(screen.queryByText('Agent output')).not.toBeInTheDocument();
    });

    it('does not render terminal panel when status is done', () => {
      const state = makeState({
        status: 'done',
        tmux_session: 'cr-ws-1-abc1234',
      });
      mockWorkspaces = [makeWorkspace({ behind: 3 })];

      render(
        <LinearSyncResolveConflictProgress
          workspaceId="ws-1"
          resolveConflict={state}
          displayHash="abc1234"
        />
      );

      expect(screen.queryByText('Agent output')).not.toBeInTheDocument();
    });
  });
});
