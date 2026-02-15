import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import LinearSyncResolveConflictProgress from './LinearSyncResolveConflictProgress';
import type { LinearSyncResolveConflictStatePayload, WorkspaceResponse } from '../lib/types';

// --- Mocks ---

const navigate = vi.fn();
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

const mockDismissLinearSyncResolveConflictState = vi.fn().mockResolvedValue(undefined);
vi.mock('../lib/api', () => ({
  dismissLinearSyncResolveConflictState: (...args: unknown[]) =>
    mockDismissLinearSyncResolveConflictState(...args),
}));

const clearLinearSyncResolveConflictState = vi.fn();
let mockLinearSyncResolveConflictStates: Record<string, LinearSyncResolveConflictStatePayload> = {};
let mockWorkspaces: WorkspaceResponse[] = [];

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    linearSyncResolveConflictStates: mockLinearSyncResolveConflictStates,
    workspaces: mockWorkspaces,
    clearLinearSyncResolveConflictState,
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
    git_ahead: 0,
    git_behind: 0,
    git_lines_added: 0,
    git_lines_removed: 0,
    git_files_changed: 0,
    ...overrides,
  };
}

function makeState(
  overrides: Partial<LinearSyncResolveConflictStatePayload> = {}
): LinearSyncResolveConflictStatePayload {
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
        message: 'Cherry-picking commit',
        at: new Date().toISOString(),
      },
    ],
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockLinearSyncResolveConflictStates = {};
  mockWorkspaces = [];
});

// --- Tests ---

describe('LinearSyncResolveConflictProgress', () => {
  it('returns null when no state exists for workspace', () => {
    mockWorkspaces = [makeWorkspace()];

    const { container } = render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(container.innerHTML).toBe('');
  });

  it('renders in-progress state with spinner', () => {
    const state = makeState({ status: 'in_progress' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace()];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(screen.getByText('Resolving conflicts...')).toBeInTheDocument();
  });

  it('auto-dismisses when status is done and no more commits behind', () => {
    const state = makeState({ status: 'done' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 0 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(clearLinearSyncResolveConflictState).toHaveBeenCalledWith('ws-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/session-1');
    expect(mockDismissLinearSyncResolveConflictState).toHaveBeenCalledWith('ws-1');
  });

  it('auto-dismisses to / when done with no sessions', () => {
    const state = makeState({ status: 'done' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 0, sessions: [], session_count: 0 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(clearLinearSyncResolveConflictState).toHaveBeenCalledWith('ws-1');
    expect(navigate).toHaveBeenCalledWith('/');
  });

  it('does NOT auto-dismiss when done but has more commits behind', () => {
    const state = makeState({ status: 'done' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 3 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(clearLinearSyncResolveConflictState).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText('Continue syncing')).toBeInTheDocument();
  });

  it('does NOT auto-dismiss when status is in_progress', () => {
    const state = makeState({ status: 'in_progress' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 0 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(clearLinearSyncResolveConflictState).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('does NOT auto-dismiss when status is failed', () => {
    const state = makeState({ status: 'failed' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 0 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(clearLinearSyncResolveConflictState).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
    expect(screen.getByText('Conflict resolution failed')).toBeInTheDocument();
  });

  it('shows dismiss button for failed state', () => {
    const state = makeState({ status: 'failed' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace({ git_behind: 0 })];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    expect(screen.getByText('dismiss')).toBeInTheDocument();
  });

  it('clicking dismiss navigates to first session', async () => {
    const state = makeState({ status: 'failed' });
    mockLinearSyncResolveConflictStates = { 'ws-1': state };
    mockWorkspaces = [makeWorkspace()];

    render(<LinearSyncResolveConflictProgress workspaceId="ws-1" />);

    await act(async () => {
      screen.getByText('dismiss').click();
    });

    expect(clearLinearSyncResolveConflictState).toHaveBeenCalledWith('ws-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/session-1');
  });
});
