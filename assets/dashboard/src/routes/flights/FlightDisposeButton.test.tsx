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
  it('names the workspace in the consequence dialog and sends it on confirm', async () => {
    const f = flight({ workflow_state: 'completing' });
    api.disposeFlight.mockResolvedValue(transition(f, true));
    const onDisposed = vi.fn();
    render(<FlightDisposeButton flight={f} size="sm" onDisposed={onDisposed} />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));
    const dialog = screen.getByTestId('flight-dispose-modal');
    expect(dialog).toHaveTextContent('ws-7');
    expect(api.disposeFlight).not.toHaveBeenCalled();
    expect(mockConfirm).not.toHaveBeenCalled();

    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));

    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', {
      workspace_id: 'ws-7',
      confirm: true,
    });
    expect(onDisposed).toHaveBeenCalledWith(expect.objectContaining({ removed: true }));
    expect(mockToast).toHaveBeenCalledWith('Login fix disposed');
  });

  it('uses a plain Remove confirm for a flight with no workspace', async () => {
    mockConfirm.mockResolvedValue(true);
    api.disposeFlight.mockResolvedValue(transition(flight({ workflow_state: 'backlog' }), true));
    const f = flight({ workflow_state: 'backlog', workspace: undefined });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(mockConfirm).toHaveBeenCalledWith(
      'Remove Login fix? Nothing has been created for it yet.',
      { danger: true, confirmText: 'Remove' }
    );
    expect(screen.queryByTestId('flight-dispose-modal')).not.toBeInTheDocument();
    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', {
      workspace_id: '',
      confirm: true,
    });
  });

  it('uses the consequence dialog for a backlog flight that still owns a workspace', async () => {
    const f = flight({ workflow_state: 'backlog' });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(screen.getByTestId('flight-dispose-modal')).toBeInTheDocument();
    expect(mockConfirm).not.toHaveBeenCalled();
  });

  it('sends nothing when the consequence dialog is dismissed', async () => {
    const f = flight({ workflow_state: 'completing' });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));
    await userEvent.click(screen.getByRole('button', { name: 'Keep flight' }));

    expect(api.disposeFlight).not.toHaveBeenCalled();
    expect(screen.queryByTestId('flight-dispose-modal')).not.toBeInTheDocument();
  });

  it('sends nothing when the confirm dialog is dismissed', async () => {
    mockConfirm.mockResolvedValue(false);
    const f = flight({ workflow_state: 'backlog', workspace: undefined });
    render(<FlightDisposeButton flight={f} size="sm" />);

    await userEvent.click(screen.getByRole('button', { name: 'Dispose' }));

    expect(api.disposeFlight).not.toHaveBeenCalled();
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
