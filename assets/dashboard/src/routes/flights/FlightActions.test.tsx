import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import FlightActions from './FlightActions';
import type { FlightSummaryResponse, FlightTransitionResponse } from '../../lib/types.generated';

const mockAlert = vi.fn();
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: vi.fn() }),
}));
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

const api = {
  takeoffFlight: vi.fn(),
  enterFlightExecution: vi.fn(),
  enterFlightCompletion: vi.fn(),
  markFlightDone: vi.fn(),
  disposeFlight: vi.fn(),
};
vi.mock('../../lib/api', () => ({
  takeoffFlight: (...a: unknown[]) => api.takeoffFlight(...a),
  enterFlightExecution: (...a: unknown[]) => api.enterFlightExecution(...a),
  enterFlightCompletion: (...a: unknown[]) => api.enterFlightCompletion(...a),
  markFlightDone: (...a: unknown[]) => api.markFlightDone(...a),
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

describe('FlightActions', () => {
  it.each(['backlog', 'planning', 'executing', 'completing', 'done'])(
    'offers only Dispose for removal in %s',
    (workflow_state) => {
      render(<FlightActions flight={flight({ workflow_state })} />);
      expect(screen.getByRole('button', { name: 'Dispose' })).toBeInTheDocument();
      expect(
        screen.queryByRole('button', { name: /^(Delete|Cancel flight|Terminate)$/ })
      ).not.toBeInTheDocument();
    }
  );

  it('does not offer takeoff after a planner spawn failure', () => {
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
    const onTransition = vi.fn();
    render(<FlightActions flight={f} onTransition={onTransition} />);

    expect(screen.queryByTestId('flight-action-takeoff-fl-1')).not.toBeInTheDocument();
    expect(api.takeoffFlight).not.toHaveBeenCalled();
    expect(onTransition).not.toHaveBeenCalled();
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

  it('offers Dispose for a backlog flight with no workspace', () => {
    render(<FlightActions flight={flight({ workflow_state: 'backlog', workspace: undefined })} />);

    expect(screen.getByRole('button', { name: 'Dispose' })).toBeInTheDocument();
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
});
