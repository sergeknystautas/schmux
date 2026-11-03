import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightDisposeButton from './FlightDisposeButton';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';
import type { WorkspaceResponse } from '../../lib/types';

const mockAlert = vi.fn();
const mockConfirm = vi.fn();
const mockToast = vi.fn();
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: mockConfirm }),
}));
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ success: mockToast, error: vi.fn() }),
}));

// Each test sets the workspaces the dispose modal reads its git state from.
const sessions = vi.hoisted(() => ({ workspaces: [] as WorkspaceResponse[] }));
vi.mock('../../contexts/SessionsContext', () => ({
  useSessions: () => sessions,
}));

const api = {
  disposeFlight: vi.fn(),
};
vi.mock('../../lib/api', () => ({
  disposeFlight: (...a: unknown[]) => api.disposeFlight(...a),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const T = '2026-01-01T00:00:00Z';

function flight(overrides: Partial<FlightSummaryResponse> = {}): FlightSummaryResponse {
  return {
    id: 'fl-1',
    repo: 'r',
    short_name: 'Login fix',
    planner_model: 'm',
    executor_model: 'm',
    workflow_state: 'executing',
    cleanup_state: 'idle',
    order: 0,
    sessions: [
      {
        id: 'sa1',
        role: 'planner',
        target: 'm',
        session_id: 'ws-7-aaaa1111',
        prior_session_ids: ['ws-7-00000000'],
        outcome: 'succeeded',
        available: true,
        created_at: T,
        updated_at: T,
      },
    ],
    owned_workspace_id: 'ws-7',
    workspace: {
      id: 'wa',
      requested_branch: 'schmux/flight/fl-1',
      workspace_id: 'ws-7',
      path: '/w/ws-7',
      outcome: 'succeeded',
      available: true,
      created_at: T,
      updated_at: T,
    },
    created_at: T,
    updated_at: T,
    ...overrides,
  };
}

function transition(f: FlightSummaryResponse, removed = false): FlightTransitionResponse {
  return { flight: { ...f, prompt: 'p' }, removed };
}

function workspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-7',
    repo: 'r',
    branch: 'schmux/flight/fl-1',
    path: '/w/ws-7',
    default_branch: 'main',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    commits_synced_with_remote: false,
    remote_branch_exists: false,
    local_unique_commits: 0,
    remote_unique_commits: 0,
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  sessions.workspaces = [];
});

describe('FlightDisposeButton', () => {
  it('names the workspace and its sessions when the flight owns one', async () => {
    const f = flight({ workflow_state: 'completing' });
    api.disposeFlight.mockResolvedValue(transition(f, true));
    const onDisposed = vi.fn();
    render(<FlightDisposeButton flight={f} size="sm" onDisposed={onDisposed} />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));
    expect(screen.getByTestId('flight-dispose-modal')).toHaveTextContent('ws-7-aaaa1111');
    expect(screen.getByTestId('flight-dispose-modal')).toHaveTextContent('ws-7-00000000');
    expect(screen.getByTestId('flight-dispose-modal')).toHaveTextContent('destroy workspace ws-7');
    expect(mockConfirm).not.toHaveBeenCalled();

    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));

    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', { confirm: true });
    expect(onDisposed).toHaveBeenCalledWith(expect.objectContaining({ removed: true }));
    expect(mockToast).toHaveBeenCalledWith('Login fix disposed');
  });

  it('opens the same dialog and promises no workspace when none carries the flight id', async () => {
    api.disposeFlight.mockResolvedValue(transition(flight(), true));
    const f = flight({ owned_workspace_id: undefined, sessions: undefined });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(mockConfirm).not.toHaveBeenCalled();
    const modal = screen.getByTestId('flight-dispose-modal');
    expect(modal).toHaveTextContent('close no sessions (none were started)');
    expect(modal).toHaveTextContent("touch no workspace (none carries this flight's id)");
    expect(modal).not.toHaveTextContent('ws-7');

    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));
    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', { confirm: true });
  });

  it('sends nothing when the consequence dialog is dismissed', async () => {
    const f = flight({ workflow_state: 'completing' });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));
    await userEvent.click(screen.getByRole('button', { name: 'Keep flight' }));

    expect(api.disposeFlight).not.toHaveBeenCalled();
    expect(screen.queryByTestId('flight-dispose-modal')).not.toBeInTheDocument();
  });

  it('reads Retry dispose while cleanup is in progress and reports a failure', async () => {
    api.disposeFlight.mockRejectedValue(new Error('workspace busy'));
    const f = flight({ cleanup_state: 'disposing' });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Retry dispose' }));
    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));

    expect(mockAlert).toHaveBeenCalledWith('Dispose failed', 'workspace busy');
    expect(screen.getByRole('button', { name: 'Retry dispose' })).toBeEnabled();
  });

  it('shows the diff size and the remote-branch position', async () => {
    sessions.workspaces = [
      workspace({
        files_changed: 2,
        lines_added: 10,
        lines_removed: 3,
        remote_branch_exists: true,
        local_unique_commits: 1,
        remote_unique_commits: 4,
        commits_synced_with_remote: false,
        ahead: 0,
      }),
    ];
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.getByTestId('flight-dispose-diff')).toHaveTextContent(
      'discard uncommitted changes in 2 file(s): +10 −3 lines'
    );
    expect(screen.getByTestId('flight-dispose-position')).toHaveTextContent(
      '1 commit(s) ahead of and 4 behind the remote branch'
    );
    expect(screen.queryByTestId('flight-dispose-blocked')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-dispose-confirm')).toBeEnabled();
  });

  it('shows the no-remote position and blocks when commits are not on main', async () => {
    sessions.workspaces = [
      workspace({ ahead: 2, commits_synced_with_remote: false, remote_branch_exists: false }),
    ];
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.getByTestId('flight-dispose-diff')).toHaveTextContent(
      'no uncommitted file changes'
    );
    expect(screen.getByTestId('flight-dispose-position')).toHaveTextContent(
      '2 commit(s) ahead of main; the branch is on no remote'
    );
    expect(screen.getByTestId('flight-dispose-blocked')).toHaveTextContent(
      'Dispose is blocked: 2 commit(s) not in main, and the branch is on no remote. Push or merge first.'
    );
    const confirm = screen.getByTestId('flight-dispose-confirm');
    expect(confirm).toBeDisabled();
    await userEvent.click(confirm);
    expect(api.disposeFlight).not.toHaveBeenCalled();
  });

  it('blocks with the remote wording when the local branch does not match its remote', async () => {
    sessions.workspaces = [
      workspace({
        ahead: 2,
        remote_branch_exists: true,
        local_unique_commits: 2,
        commits_synced_with_remote: false,
      }),
    ];
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.getByTestId('flight-dispose-blocked')).toHaveTextContent(
      'Dispose is blocked: 2 commit(s) not in main, and the local branch does not match its remote. Push or merge first.'
    );
  });

  it('enables confirm when the branch is only behind its remote', async () => {
    sessions.workspaces = [
      workspace({
        ahead: 0,
        remote_branch_exists: true,
        remote_unique_commits: 3,
        commits_synced_with_remote: false,
      }),
    ];
    api.disposeFlight.mockResolvedValue(transition(flight({ workflow_state: 'completing' }), true));
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.queryByTestId('flight-dispose-blocked')).not.toBeInTheDocument();
    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));
    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', { confirm: true });
  });

  it('falls back to main when default_branch is blank', async () => {
    sessions.workspaces = [
      workspace({ default_branch: '', ahead: 1, commits_synced_with_remote: false }),
    ];
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.getByTestId('flight-dispose-position')).toHaveTextContent('ahead of main');
  });

  it('shows no git numbers for a non-git workspace', async () => {
    sessions.workspaces = [workspace({ vcs: 'sapling', lines_added: 5, ahead: 1 })];
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.queryByTestId('flight-dispose-diff')).not.toBeInTheDocument();
    expect(screen.queryByTestId('flight-dispose-position')).not.toBeInTheDocument();
    expect(screen.queryByTestId('flight-dispose-blocked')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-dispose-confirm')).toBeEnabled();
  });

  it('omits the numbers and still disposes when the workspace is not in the list', async () => {
    sessions.workspaces = [];
    api.disposeFlight.mockResolvedValue(transition(flight({ workflow_state: 'completing' }), true));
    render(<FlightDisposeButton flight={flight({ workflow_state: 'completing' })} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.queryByTestId('flight-dispose-diff')).not.toBeInTheDocument();
    expect(screen.queryByTestId('flight-dispose-position')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-dispose-modal')).toHaveTextContent('destroy workspace ws-7');

    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));
    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', { confirm: true });
  });
});
