import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import FlightDetailPage from './FlightDetailPage';
import type { FlightDetailResponse, FlightSummaryResponse } from '../lib/types.generated';

const mockNavigate = vi.fn();
vi.mock('react-router', async (importOriginal) => {
  const mod = await importOriginal<typeof import('react-router')>();
  return { ...mod, useNavigate: () => mockNavigate };
});
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), prompt: vi.fn() }),
}));
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ loading: false, config: { repos: [], models: [] } }),
}));

let mockFlightsById: Record<string, FlightSummaryResponse> = {};
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ flightsById: mockFlightsById, workspaces: [] }),
}));

const api = { getFlight: vi.fn(), takeoffFlight: vi.fn(), disposeFlight: vi.fn() };
vi.mock('../lib/api', () => ({
  getFlight: (...a: unknown[]) => api.getFlight(...a),
  updateFlight: vi.fn(),
  takeoffFlight: (...a: unknown[]) => api.takeoffFlight(...a),
  disposeFlight: (...a: unknown[]) => api.disposeFlight(...a),
  enterFlightExecution: vi.fn(),
  enterFlightCompletion: vi.fn(),
  markFlightDone: vi.fn(),
  cancelFlight: vi.fn(),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const T = '2026-01-01T00:00:00Z';

const backlogFlight: FlightDetailResponse = {
  id: 'fl-1',
  repo: 'git@example.com:a.git',
  short_name: 'Login fix',
  prompt: 'Fix login.\n\n  Keep the tests.',
  planner_model: 'claude',
  executor_model: 'codex',
  workflow_state: 'backlog',
  cleanup_state: 'idle',
  order: 0,
  created_at: T,
  updated_at: T,
};

const startedFlight: FlightDetailResponse = {
  ...backlogFlight,
  workflow_state: 'cancelled',
  cancel_reason: 'superseded',
  cancelled_at: T,
  artifact_path: '/w/ws-7/.schmux/flights/fl-1/plan.md',
  workspace: {
    id: 'wa',
    requested_branch: 'schmux/flight/fl-1',
    workspace_id: 'ws-7',
    path: '/w/ws-7',
    branch: 'schmux/flight/fl-1',
    outcome: 'succeeded',
    available: false,
    created_at: T,
    updated_at: T,
  },
  sessions: [
    {
      id: 'sa-p',
      role: 'planner',
      target: 'claude',
      session_id: 'ws-7-p2',
      prior_session_ids: ['ws-7-p1'],
      outcome: 'succeeded',
      available: true,
      created_at: T,
      updated_at: T,
    },
    {
      id: 'sa-e',
      role: 'executor',
      target: 'codex',
      session_id: 'ws-7-e1',
      outcome: 'succeeded',
      available: false,
      created_at: T,
      updated_at: T,
    },
  ],
  operations: [
    {
      id: 'op-1',
      action: 'takeoff',
      correlation_id: 'c1',
      status: 'failed',
      error: 'spawn planner: boom',
      resources: [{ kind: 'session', status: 'failed', error: 'boom' }],
      created_at: T,
      updated_at: T,
    },
    {
      id: 'op-2',
      action: 'takeoff',
      correlation_id: 'c2',
      status: 'succeeded',
      created_at: T,
      updated_at: T,
    },
    {
      id: 'op-3',
      action: 'cancel',
      correlation_id: 'c3',
      status: 'succeeded',
      created_at: T,
      updated_at: T,
    },
  ],
};

function summaryOf(f: FlightDetailResponse): FlightSummaryResponse {
  const { prompt: _p, operations: _o, artifact_path: _a, ...rest } = f;
  return rest;
}

function page() {
  return (
    <MemoryRouter initialEntries={['/flights/fl-1']}>
      <Routes>
        <Route path="/flights/:flightId" element={<FlightDetailPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockFlightsById = {};
});

describe('FlightDetailPage', () => {
  it('shows the complete prompt verbatim with models and backlog takeoff', async () => {
    api.getFlight.mockResolvedValue(backlogFlight);
    render(page());

    const prompt = await screen.findByTestId('flight-detail-prompt');
    expect(prompt.textContent).toBe('Fix login.\n\n  Keep the tests.');
    expect(screen.getByTestId('flight-detail-title')).toHaveTextContent('Login fix');
    // The title row is a breadcrumb: "Flights" links back to the list.
    const crumb = screen.getByRole('link', { name: 'Flights' });
    expect(crumb).toHaveAttribute('href', '/flights');
    expect(screen.queryByRole('link', { name: 'All flights' })).not.toBeInTheDocument();
    expect(screen.getByText('claude')).toBeInTheDocument();
    expect(screen.getByText('codex')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Take off' })).toBeInTheDocument();
    expect(api.getFlight).toHaveBeenCalledWith('fl-1');
  });

  it('shows resources, the planning artifact, and unavailable resources as history', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    expect(await screen.findByTestId('flight-artifact-path')).toHaveTextContent(
      '/w/ws-7/.schmux/flights/fl-1/plan.md'
    );
    expect(screen.getByTestId('flight-cancel-reason')).toHaveTextContent('superseded');
    expect(screen.getByTestId('flight-workspace-unavailable')).toHaveTextContent('ws-7');
    expect(screen.getByTestId('flight-session-link-planner')).toHaveAttribute(
      'href',
      '/sessions/ws-7-p2'
    );
    expect(screen.getByTestId('flight-session-planner')).toHaveTextContent('ws-7-p1');
    expect(screen.getByTestId('flight-session-unavailable-executor')).toHaveTextContent(
      'ws-7-e1 (unavailable)'
    );
    // Terminal workflow states still own resources and can be disposed.
    expect(screen.getByRole('button', { name: 'Dispose' })).toBeInTheDocument();
  });

  it('offers Dispose on a backlog flight that owns no resources', async () => {
    api.getFlight.mockResolvedValue(backlogFlight);
    render(page());
    await screen.findByTestId('flight-detail-title');
    expect(screen.getByRole('button', { name: 'Dispose' })).toBeInTheDocument();
  });

  it('puts the workflow (resources, operations) above the prompt', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    const prompt = await screen.findByTestId('flight-detail-prompt');
    const resources = screen.getByTestId('flight-assignment-links');
    const operations = screen.getByTestId('flight-operation-history');
    const before = (a: HTMLElement, b: HTMLElement) =>
      Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    expect(before(resources, operations)).toBe(true);
    expect(before(operations, prompt)).toBe(true);
  });

  it('lists operations most recent first with their resource outcomes', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    const history = await screen.findByTestId('flight-operation-history');
    const items = within(history).getAllByRole('row').slice(1);
    expect(items.map((li) => li.getAttribute('data-testid'))).toEqual([
      'flight-operation-op-3',
      'flight-operation-op-2',
      'flight-operation-op-1',
    ]);
    expect(screen.getByTestId('flight-operation-error-op-1')).toHaveTextContent(
      'spawn planner: boom'
    );
  });

  it('shows why the flight cannot be loaded', async () => {
    api.getFlight.mockRejectedValue(new Error('flight not found'));
    render(page());

    expect(await screen.findByTestId('flight-detail-error')).toHaveTextContent('flight not found');
  });

  it('renders the transition response without waiting for a snapshot', async () => {
    api.getFlight.mockResolvedValue(backlogFlight);
    api.takeoffFlight.mockResolvedValue({
      flight: {
        ...backlogFlight,
        workflow_state: 'planning',
        sessions: [{ ...startedFlight.sessions![0] }],
      },
    });
    render(page());

    await userEvent.click(await screen.findByRole('button', { name: 'Take off' }));

    expect(screen.getByTestId('flight-state-fl-1')).toHaveTextContent('planning');
    expect(screen.getByRole('button', { name: 'Enter execution' })).toBeInTheDocument();
  });

  it('returns to the flights list once dispose removes the record', async () => {
    api.getFlight.mockResolvedValue({ ...startedFlight, workflow_state: 'done' });
    api.disposeFlight.mockResolvedValue({ flight: startedFlight, removed: true });
    render(page());

    await userEvent.click(await screen.findByRole('button', { name: 'Dispose' }));
    await userEvent.click(screen.getByTestId('flight-dispose-confirm'));

    expect(api.disposeFlight).toHaveBeenCalledWith('fl-1', {
      workspace_id: 'ws-7',
      confirm: true,
    });
    expect(mockNavigate).toHaveBeenCalledWith('/flights');
  });

  it('refetches only when the snapshot summary for this flight changes', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    mockFlightsById = { 'fl-1': summaryOf(startedFlight) };
    const { rerender } = render(page());
    await screen.findByTestId('flight-detail-prompt');
    expect(api.getFlight).toHaveBeenCalledTimes(1);

    // A new snapshot with identical content (e.g. after a reconnect) does not refetch.
    mockFlightsById = { 'fl-1': { ...summaryOf(startedFlight) } };
    rerender(page());
    expect(api.getFlight).toHaveBeenCalledTimes(1);

    mockFlightsById = {
      'fl-1': { ...summaryOf(startedFlight), cleanup_state: 'disposing', updated_at: T },
    };
    api.getFlight.mockResolvedValue({ ...startedFlight, cleanup_state: 'disposing' });
    rerender(page());

    expect(await screen.findByTestId('flight-cleanup-fl-1')).toHaveTextContent('disposing');
    expect(api.getFlight).toHaveBeenCalledTimes(2);
  });

  it('renders the name-error line when name_error is set', async () => {
    api.getFlight.mockResolvedValue({ ...startedFlight, name_error: 'target not found: x' });
    render(page());
    const err = await screen.findByTestId('flight-detail-name-error');
    expect(err).toHaveTextContent('Name not generated: target not found: x');
  });
});
