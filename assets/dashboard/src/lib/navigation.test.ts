import { describe, it, expect, vi } from 'vitest';
import { navigateToWorkspace } from './navigation';
import type { WorkspaceResponse } from './types';

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
    git_ahead: 0,
    git_behind: 0,
    git_lines_added: 0,
    git_lines_removed: 0,
    git_files_changed: 0,
    ...overrides,
  };
}

describe('navigateToWorkspace', () => {
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
    const workspaces = [makeWorkspace({ id: 'ws-1', git_lines_added: 10, git_lines_removed: 5 })];

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
    const workspaces = [makeWorkspace({ id: 'ws-1', git_lines_added: 5, git_lines_removed: 0 })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/diff/ws-1');
  });

  it('navigates to diff when only lines_removed > 0', () => {
    const navigate = vi.fn();
    const workspaces = [makeWorkspace({ id: 'ws-1', git_lines_added: 0, git_lines_removed: 3 })];

    navigateToWorkspace(navigate, workspaces, 'ws-1');
    expect(navigate).toHaveBeenCalledWith('/diff/ws-1');
  });
});
