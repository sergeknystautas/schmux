import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightDisposeButton from './FlightDisposeButton';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';

const mockAlert = vi.fn();
const mockConfirm = vi.fn();
const mockToast = vi.fn();
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: mockConfirm }),
}));
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ success: mockToast, error: vi.fn() }),
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

beforeEach(() => {
  vi.clearAllMocks();
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
});
