import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightActions from './FlightActions';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';

const mockAlert = vi.fn();
const mockPrompt = vi.fn();
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, prompt: mockPrompt }),
}));
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

const api = {
  takeoffFlight: vi.fn(),
  enterFlightExecution: vi.fn(),
  enterFlightCompletion: vi.fn(),
  markFlightDone: vi.fn(),
  cancelFlight: vi.fn(),
  terminateFlight: vi.fn(),
};
vi.mock('../../lib/api', () => ({
  takeoffFlight: (...a: unknown[]) => api.takeoffFlight(...a),
  enterFlightExecution: (...a: unknown[]) => api.enterFlightExecution(...a),
  enterFlightCompletion: (...a: unknown[]) => api.enterFlightCompletion(...a),
  markFlightDone: (...a: unknown[]) => api.markFlightDone(...a),
  cancelFlight: (...a: unknown[]) => api.cancelFlight(...a),
  terminateFlight: (...a: unknown[]) => api.terminateFlight(...a),
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

describe('FlightActions', () => {
  it('retries a failed takeoff through the same takeoff endpoint', async () => {
    const f = flight({
      workflow_state: 'planning',
      sessions: [
        {
          id: 'sa',
          role: 'planner',
          target: 'm',
          outcome: 'failed',
          error: 'spawn failed',
          available: false,
          created_at: T,
          updated_at: T,
        },
      ],
    });
    api.takeoffFlight.mockResolvedValue(transition(f));
    const onTransition = vi.fn();
    render(<FlightActions flight={f} onTransition={onTransition} />);

    await userEvent.click(screen.getByRole('button', { name: 'Retry takeoff' }));

    expect(api.takeoffFlight).toHaveBeenCalledWith('fl-1');
    expect(onTransition).toHaveBeenCalledWith(transition(f));
    expect(screen.queryByRole('button', { name: 'Enter execution' })).not.toBeInTheDocument();
  });

  it('shows a failed transition and keeps the control available for retry', async () => {
    api.markFlightDone.mockRejectedValue(new Error('flight state conflict: not completing'));
    render(<FlightActions flight={flight({ workflow_state: 'completing' })} />);

    await userEvent.click(screen.getByRole('button', { name: 'Mark done' }));

    expect(mockAlert).toHaveBeenCalledWith(
      'Mark done failed',
      'flight state conflict: not completing'
    );
    expect(screen.getByRole('button', { name: 'Mark done' })).toBeEnabled();
  });

  it('cancels with the reason the user enters', async () => {
    mockPrompt.mockResolvedValue('  superseded by fl-2 ');
    api.cancelFlight.mockResolvedValue(transition(flight({ workflow_state: 'cancelled' })));
    render(<FlightActions flight={flight()} />);

    await userEvent.click(screen.getByRole('button', { name: 'Cancel flight' }));

    expect(api.cancelFlight).toHaveBeenCalledWith('fl-1', 'superseded by fl-2');
  });

  it('does not cancel when the reason prompt is dismissed', async () => {
    mockPrompt.mockResolvedValue(null);
    render(<FlightActions flight={flight()} />);

    await userEvent.click(screen.getByRole('button', { name: 'Cancel flight' }));

    expect(api.cancelFlight).not.toHaveBeenCalled();
  });

  it('enters completion directly: one click starts the commit agent', async () => {
    api.enterFlightCompletion.mockResolvedValue(
      transition(flight({ workflow_state: 'completing' }))
    );
    render(<FlightActions flight={flight()} />);

    await userEvent.click(screen.getByRole('button', { name: 'Enter completion' }));

    expect(api.enterFlightCompletion).toHaveBeenCalledWith('fl-1');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('shows the backend rejection when completion fails', async () => {
    api.enterFlightCompletion.mockRejectedValue(new Error('flight fl-1 is planning'));
    render(<FlightActions flight={flight()} />);

    await userEvent.click(screen.getByRole('button', { name: 'Enter completion' }));

    expect(mockAlert).toHaveBeenCalledWith('Enter completion failed', 'flight fl-1 is planning');
  });

  describe('terminate', () => {
    it('confirms with the workspace named and sends it as the expected workspace', async () => {
      const f = flight({ workflow_state: 'done' });
      api.terminateFlight.mockResolvedValue(transition(f, true));
      const onTransition = vi.fn();
      render(<FlightActions flight={f} onTransition={onTransition} />);

      await userEvent.click(screen.getByRole('button', { name: 'Terminate' }));
      const dialog = screen.getByTestId('flight-terminate-modal');
      expect(dialog).toHaveTextContent('ws-7');
      expect(within(dialog).getByTestId('flight-terminate-consequences')).toHaveTextContent(
        'every uncommitted file change'
      );
      expect(api.terminateFlight).not.toHaveBeenCalled();

      await userEvent.click(screen.getByTestId('flight-terminate-confirm'));

      expect(api.terminateFlight).toHaveBeenCalledWith('fl-1', {
        workspace_id: 'ws-7',
        confirm: true,
      });
      expect(onTransition).toHaveBeenCalledWith(transition(f, true));
    });

    it('sends nothing when the confirmation is dismissed', async () => {
      render(<FlightActions flight={flight({ workflow_state: 'cancelled' })} />);

      await userEvent.click(screen.getByRole('button', { name: 'Terminate' }));
      await userEvent.click(screen.getByRole('button', { name: 'Keep flight' }));

      expect(api.terminateFlight).not.toHaveBeenCalled();
      expect(screen.queryByTestId('flight-terminate-modal')).not.toBeInTheDocument();
    });

    it('reports a partial cleanup failure and offers terminate again', async () => {
      api.terminateFlight.mockRejectedValue(new Error('dispose session ws-7-abc: tmux gone'));
      render(
        <FlightActions flight={flight({ workflow_state: 'done', cleanup_state: 'terminating' })} />
      );

      await userEvent.click(screen.getByRole('button', { name: 'Retry terminate' }));
      await userEvent.click(screen.getByTestId('flight-terminate-confirm'));

      expect(mockAlert).toHaveBeenCalledWith(
        'Retry terminate failed',
        'dispose session ws-7-abc: tmux gone'
      );
      expect(screen.getByRole('button', { name: 'Retry terminate' })).toBeEnabled();
    });
  });
});
