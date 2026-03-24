import { describe, it, expect, vi, beforeEach } from 'vitest';
import { navigateToWorkspace, findNextWorkspaceWithSessions } from './navigation';
import type { WorkspaceResponse } from './types';
import { TAB_ORDER_KEY_PREFIX } from './tabOrder';

// Mock react-router-dom's useNavigate
vi.mock('react-router-dom', () => ({
  useNavigate: vi.fn(),
}));

// Mock SessionsContext - not needed for navigateToWorkspace (pure function with navigate arg)
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: vi.fn(),
}));

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
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
  };
}

describe('navigateToWorkspace', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('navigates to first session in custom tab order', () => {
    const navigate = vi.fn();
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['session-2', 'session-1']));
    const workspaces = [
      makeWorkspace({
        id: 'ws-1',
        sessions: [
          {
            id: 'session-1',
            target: 'claude',
            branch: 'main',
            created_at: '',
            running: true,
            attach_cmd: '',
          },
          {
            id: 'session-2',
            target: 'claude',
            branch: 'main',
            created_at: '',
            running: true,
            attach_cmd: '',
          },
        ],
        session_count: 2,
      }),
    ];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/session-2');
  });

  it('navigates to first session when workspace has sessions', () => {
    const navigate = vi.fn();
    const workspaces = [
      makeWorkspace({
        id: 'ws-1',
        sessions: [
          {
            id: 'session-1',
            target: 'claude',
            branch: 'main',
            created_at: '',
            running: true,
            attach_cmd: '',
          },
          {
            id: 'session-2',
            target: 'claude',
            branch: 'main',
            created_at: '',
            running: true,
            attach_cmd: '',
          },
        ],
        session_count: 2,
      }),
    ];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/sessions/session-1');
  });

  it('navigates to diff page when no sessions but has git changes', () => {
    const navigate = vi.fn();
    const workspaces = [makeWorkspace({ id: 'ws-1', lines_added: 10, lines_removed: 5 })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/diff/ws-1');
  });

  it('navigates to spawn page when no sessions and no changes', () => {
    const navigate = vi.fn();
    const workspaces = [makeWorkspace({ id: 'ws-1' })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/spawn?workspace_id=ws-1');
  });

  it('navigates to spawn page when workspace not found', () => {
    const navigate = vi.fn();
    navigateToWorkspace(navigate, [], 'nonexistent');
    expect(navigate).toHaveBeenCalledWith('/spawn?workspace_id=nonexistent');
  });

  it('navigates to diff when only lines_added > 0', () => {
    const navigate = vi.fn();
    const workspaces = [makeWorkspace({ id: 'ws-1', lines_added: 5, lines_removed: 0 })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/diff/ws-1');
  });

  it('navigates to diff when only lines_removed > 0', () => {
    const navigate = vi.fn();
    const workspaces = [makeWorkspace({ id: 'ws-1', lines_added: 0, lines_removed: 3 })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/diff/ws-1');
  });
});

describe('findNextWorkspaceWithSessions', () => {
  const session = {
    id: 's-1',
    target: 'claude',
    branch: 'main',
    created_at: '',
    running: true,
    attach_cmd: '',
  };

  it('finds next workspace with sessions going down', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1', sessions: [session], session_count: 1 }),
      makeWorkspace({ id: 'ws-2' }), // no sessions
      makeWorkspace({ id: 'ws-3', sessions: [session], session_count: 1 }),
    ];
    expect(findNextWorkspaceWithSessions(workspaces, 0, 1)).toBe(2);
  });

  it('finds previous workspace with sessions going up', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1', sessions: [session], session_count: 1 }),
      makeWorkspace({ id: 'ws-2' }), // no sessions
      makeWorkspace({ id: 'ws-3', sessions: [session], session_count: 1 }),
    ];
    expect(findNextWorkspaceWithSessions(workspaces, 2, -1)).toBe(0);
  });

  it('returns -1 when no workspace with sessions in direction', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1', sessions: [session], session_count: 1 }),
      makeWorkspace({ id: 'ws-2' }),
      makeWorkspace({ id: 'ws-3' }),
    ];
    // Going down from ws-1, no more workspaces with sessions
    expect(findNextWorkspaceWithSessions(workspaces, 0, 1)).toBe(-1);
    // Going up from ws-1, nothing before it
    expect(findNextWorkspaceWithSessions(workspaces, 0, -1)).toBe(-1);
  });

  it('skips multiple consecutive sessionless workspaces', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1', sessions: [session], session_count: 1 }),
      makeWorkspace({ id: 'ws-2' }),
      makeWorkspace({ id: 'ws-3' }),
      makeWorkspace({ id: 'ws-4' }),
      makeWorkspace({ id: 'ws-5', sessions: [session], session_count: 1 }),
    ];
    expect(findNextWorkspaceWithSessions(workspaces, 0, 1)).toBe(4);
    expect(findNextWorkspaceWithSessions(workspaces, 4, -1)).toBe(0);
  });

  it('finds immediate neighbor when it has sessions', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1', sessions: [session], session_count: 1 }),
      makeWorkspace({ id: 'ws-2', sessions: [session], session_count: 1 }),
    ];
    expect(findNextWorkspaceWithSessions(workspaces, 0, 1)).toBe(1);
    expect(findNextWorkspaceWithSessions(workspaces, 1, -1)).toBe(0);
  });

  it('works from index -1 to find first workspace with sessions', () => {
    const workspaces = [
      makeWorkspace({ id: 'ws-1' }),
      makeWorkspace({ id: 'ws-2', sessions: [session], session_count: 1 }),
    ];
    expect(findNextWorkspaceWithSessions(workspaces, -1, 1)).toBe(1);
  });
});
