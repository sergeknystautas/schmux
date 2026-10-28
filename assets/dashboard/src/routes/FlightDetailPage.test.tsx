import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import FlightDetailPage from './FlightDetailPage';
import type { FlightDetailResponse, FlightSummaryResponse } from '../lib/types.generated';
import type { SessionWithWorkspace } from '../lib/types';

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
let mockSessionsById: Record<string, SessionWithWorkspace> = {};
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    flightsById: mockFlightsById,
    sessionsById: mockSessionsById,
    workspaces: [],
  }),
}));

const api = { getFlight: vi.fn(), takeoffFlight: vi.fn(), disposeFlight: vi.fn() };
vi.mock('../lib/api', () => ({
  getFlight: (...a: unknown[]) => api.getFlight(...a),
  updateFlight: vi.fn(),
  takeoffFlight: (...a: unknown[]) => api.takeoffFlight(...a),
  disposeFlight: (...a: unknown[]) => api.disposeFlight(...a),
  enterFlightExecution: vi.fn(),
  enterFlightCompletion: vi.fn(),
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
  workflow_state: 'executing',
  spec_path: '/w/ws-7/docs/superpowers/specs/2026-10-19-login-design.md',
  plan_path: '/w/ws-7/docs/superpowers/plans/2026-10-19-login.md',
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
      action: 'execution',
      correlation_id: 'c3',
      status: 'succeeded',
      created_at: T,
      updated_at: T,
    },
  ],
};

function summaryOf(f: FlightDetailResponse): FlightSummaryResponse {
  const { prompt: _p, operations: _o, spec_path: _s, plan_path: _pl, ...rest } = f;
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

function liveSession(id: string, opts: Partial<SessionWithWorkspace> = {}): SessionWithWorkspace {
  return {
    id,
    target: 'claude',
    branch: 'main',
    created_at: T,
    last_output_at: T,
    running: true,
    attach_cmd: '',
    nudge_state: '',
    workspace_id: 'ws-7',
    workspace_path: '/w/ws-7',
    repo: 'r',
    ...opts,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockFlightsById = {};
  mockSessionsById = {};
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

  it('shows resources, the verified spec and plan, and unavailable resources as history', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    expect(await screen.findByTestId('flight-spec-path')).toHaveTextContent(
      '/w/ws-7/docs/superpowers/specs/2026-10-19-login-design.md'
    );
    expect(screen.getByTestId('flight-plan-path')).toHaveTextContent(
      '/w/ws-7/docs/superpowers/plans/2026-10-19-login.md'
    );
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

  it('shows the report target as not yet verified until the daemon verifies it', async () => {
    const report = '/w/ws-7/docs/superpowers/plans/2026-10-19-login-execution.md';
    api.getFlight.mockResolvedValue({ ...startedFlight, report_target: report });
    const { unmount } = render(page());

    const row = await screen.findByTestId('flight-report-path');
    expect(row).toHaveTextContent(`${report} (not yet verified)`);
    expect(screen.getByText('Report')).toBeInTheDocument();
    unmount();

    api.getFlight.mockResolvedValue({
      ...startedFlight,
      report_target: report,
      report_path: report,
    });
    render(page());
    const verified = await screen.findByTestId('flight-report-path');
    expect(verified).toHaveTextContent(report);
    expect(verified).not.toHaveTextContent('not yet verified');
  });

  it('shows the same activity line as the list, led by a session waiting on input', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    mockSessionsById = {
      'ws-7-p2': liveSession('ws-7-p2', { last_output_at: '2026-01-01T02:00:00Z' }),
      'ws-7-e1': liveSession('ws-7-e1', {
        nudge_state: 'Needs Input',
        nudge_summary: 'Asked a question',
        last_output_at: '2026-01-01T01:00:00Z',
      }),
    };
    render(page());

    const line = await screen.findByTestId('flight-activity-fl-1');
    expect(line).toHaveTextContent('Executing · Executor');
    expect(within(line).getByRole('link')).toHaveAttribute('href', '/sessions/ws-7-e1');
  });

  it('offers Dispose on a backlog flight that owns no resources', async () => {
    api.getFlight.mockResolvedValue(backlogFlight);
    render(page());
    await screen.findByTestId('flight-detail-title');
    expect(screen.getByRole('button', { name: 'Dispose' })).toBeInTheDocument();
  });

  it('shows the pilot stop with its reason', async () => {
    api.getFlight.mockResolvedValue({
      ...startedFlight,
      pilot_stop: {
        workflow_state: 'executing',
        reason: 'the reviewer keeps changing the same file',
        source_turn_id: 'turn-1',
        created_at: T,
      },
    });
    render(page());

    const stop = await screen.findByTestId('flight-detail-pilot-stop');
    expect(stop).toHaveTextContent('Pilot stopped');
    expect(stop).toHaveTextContent('the reviewer keeps changing the same file');
  });

  it('shows the latest review reading in words', async () => {
    api.getFlight.mockResolvedValue({
      ...startedFlight,
      workflow_state: 'reviewing',
      review_status: {
        reviewer_session_id: 'ws-7-r1',
        state: 'finished',
        tests: 'run_passed',
        test_rules_review: 'compliant',
        changed_work: 'unchanged',
        artifacts_named: 'named',
        updated_at: T,
      },
    });
    render(page());

    expect(await screen.findByTestId('flight-detail-review-reading')).toHaveTextContent(
      'Reviewer reports: finished, tests run and passed, test-rules-review compliant, changed nothing, artifacts named'
    );
  });

  it('shows the pilot latest guidance beside the review reading', async () => {
    api.getFlight.mockResolvedValue({
      ...startedFlight,
      workflow_state: 'reviewing',
      pilot_guidance: {
        recipient_role: 'reviewer',
        recipient_session_id: 'ws-7-r1',
        text: 'Name the summary path.',
        status: 'accepted',
        created_at: T,
      },
    });
    render(page());

    expect(await screen.findByTestId('flight-detail-pilot-guidance')).toHaveTextContent(
      'Pilot to Reviewer: Name the summary path.'
    );
  });

  it('leads with the work, then how it is going, then the story, then the details', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    const prompt = await screen.findByTestId('flight-detail-prompt');
    const progress = screen.getByTestId('flight-progress');
    const story = screen.getByTestId('flight-story');
    const details = screen.getByTestId('flight-detail-metadata');
    const before = (a: HTMLElement, b: HTMLElement) =>
      Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
    expect(before(prompt, progress)).toBe(true);
    expect(before(progress, story)).toBe(true);
    expect(before(story, details)).toBe(true);
    // The progress bar marks the phases behind the flight and the current one,
    // and offers nothing to click.
    expect(screen.getByTestId('flight-phase-executing')).toHaveAttribute('aria-current', 'step');
    expect(screen.getByTestId('flight-phase-executing')).toHaveAttribute('data-state', 'current');
    expect(screen.getByTestId('flight-phase-planning')).toHaveAttribute('data-state', 'done');
    expect(screen.getByTestId('flight-phase-reviewing')).toHaveAttribute('data-state', 'next');
    expect(within(screen.getByTestId('flight-phase-strip')).queryByRole('button')).toBeNull();
    // A short prompt is shown whole, with no toggle.
    expect(screen.queryByTestId('flight-detail-prompt-toggle')).not.toBeInTheDocument();
    // Resources live under the phase strip.
    expect(within(progress).getByTestId('flight-assignment-links')).toBeInTheDocument();
  });

  it('collapses a long prompt to its first lines until the user opens it', async () => {
    const prompt = Array.from({ length: 40 }, (_, i) => `Line ${i + 1} of the brief.`).join('\n');
    api.getFlight.mockResolvedValue({ ...backlogFlight, prompt });
    render(page());

    const text = await screen.findByTestId('flight-detail-prompt');
    expect(text).toHaveTextContent('Line 40 of the brief.');
    // The card header is the disclosure, with the shared chevron.
    const toggle = screen.getByTestId('flight-detail-prompt-toggle');
    expect(toggle).toHaveTextContent('The work');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle.querySelector('.diag-pane__chevron--open')).toBeNull();
    expect(text.className).toMatch(/promptClamped/);

    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(toggle.querySelector('.diag-pane__chevron--open')).not.toBeNull();
    expect(text.className).not.toMatch(/promptClamped/);
  });

  it('tells the operations as a story, newest first, with failures in words', async () => {
    api.getFlight.mockResolvedValue(startedFlight);
    render(page());

    const story = await screen.findByTestId('flight-story');
    const entries = within(story).getAllByTestId('flight-story-entry');
    expect(entries.map((e) => e.getAttribute('data-entry-id'))).toEqual(['op-3', 'op-2', 'op-1']);
    expect(
      within(story)
        .getAllByTestId('flight-story-headline')
        .map((e) => e.textContent)
    ).toEqual([
      'Entered execution: executor started',
      'Took off: workspace allocated, pilot and planner started',
      'Takeoff failed: spawn planner: boom',
    ]);
    expect(entries[2]).toHaveAttribute('data-tone', 'error');
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
    api.getFlight.mockResolvedValue({ ...startedFlight, workflow_state: 'completing' });
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
