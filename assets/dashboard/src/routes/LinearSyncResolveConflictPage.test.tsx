import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import LinearSyncResolveConflictPage from './LinearSyncResolveConflictPage';
import type { WorkspaceResponse } from '../lib/types';

const mockWorkspaces: WorkspaceResponse[] = [];
const mockNavigate = vi.fn();
const mockProgress = vi.fn(
  ({
    displayHash,
    resolveConflict,
  }: {
    displayHash: string;
    resolveConflict: { hash: string };
  }) => (
    <div>
      <span>{displayHash}</span>
      <span>{resolveConflict.hash}</span>
    </div>
  )
);

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ workspaces: mockWorkspaces }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div>header</div>,
}));

vi.mock('../components/SessionTabs', () => ({
  default: () => <div>tabs</div>,
}));

vi.mock('../components/LinearSyncResolveConflictProgress', () => ({
  default: (props: {
    workspaceId: string;
    displayHash: string;
    resolveConflict: { hash: string };
  }) => mockProgress(props),
}));

describe('LinearSyncResolveConflictPage', () => {
  it('loads the persisted conflict record by tab meta hash', () => {
    mockWorkspaces.length = 0;
    mockProgress.mockClear();

    mockWorkspaces.push({
      id: 'ws-1',
      repo: 'https://example.com/repo.git',
      branch: 'main',
      path: '/tmp/ws-1',
      session_count: 0,
      sessions: [],
      ahead: 0,
      behind: 0,
      lines_added: 0,
      lines_removed: 0,
      files_changed: 0,
      tabs: [
        {
          id: 'sys-resolve-conflict-abcdef1',
          kind: 'resolve-conflict',
          label: 'Conflict abcdef1',
          route: '/resolve-conflict/ws-1/sys-resolve-conflict-abcdef1',
          closable: true,
          meta: { hash: 'abcdef1' },
          created_at: new Date().toISOString(),
        },
      ],
      resolve_conflicts: [
        {
          type: 'linear_sync_resolve_conflict',
          workspace_id: 'ws-1',
          status: 'done',
          hash: 'abcdef1',
          started_at: new Date().toISOString(),
          steps: [],
        },
      ],
    });

    render(
      <MemoryRouter initialEntries={['/resolve-conflict/ws-1/sys-resolve-conflict-abcdef1']}>
        <Routes>
          <Route
            path="/resolve-conflict/:workspaceId/:tabId"
            element={<LinearSyncResolveConflictPage />}
          />
        </Routes>
      </MemoryRouter>
    );

    expect(screen.getAllByText('abcdef1')).toHaveLength(2);
    expect(mockProgress).toHaveBeenCalledWith(
      expect.objectContaining({
        workspaceId: 'ws-1',
        displayHash: 'abcdef1',
        resolveConflict: expect.objectContaining({ hash: 'abcdef1' }),
      })
    );
  });
});
