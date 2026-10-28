import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import FlightList from './FlightList';
import { nudgeStateEmoji } from '../../lib/utils';
import type { FlightSummaryResponse } from '../../lib/types.generated';
import type { SessionWithWorkspace } from '../../lib/types';

const mockNavigate = vi.fn();
vi.mock('react-router', async (importOriginal) => {
  const mod = await importOriginal<typeof import('react-router')>();
  return { ...mod, useNavigate: () => mockNavigate };
});
// Each test sets the sessions the list resolves through useSessions().
const sessions = vi.hoisted(() => ({ byId: {} as Record<string, SessionWithWorkspace> }));
vi.mock('../../contexts/SessionsContext', () => ({
  useSessions: () => ({ sessionsById: sessions.byId, workspaces: [] }),
}));
vi.mock('../../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: { run_targets: [], nudgenik: { targets: ['m'] } } }),
}));

// FlightDisposeButton pulls in ModalProvider and ToastProvider; provide stubs.
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn() }),
}));
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

const T = '2026-01-01T00:00:00Z';

function backlog(id: string, order: number): FlightSummaryResponse {
  return {
    id,
    repo: 'git@example.com:a.git',
    planner_model: 'claude',
    executor_model: 'codex',
    workflow_state: 'backlog',
    cleanup_state: 'idle',
    order,
    created_at: T,
    updated_at: T,
  };
}

function started(
  id: string,
  state: string,
  overrides: Partial<FlightSummaryResponse> = {}
): FlightSummaryResponse {
  return {
    ...backlog(id, 0),
    workflow_state: state,
    workspace: {
      id: `wa-${id}`,
      requested_branch: `schmux/flight/${id}`,
      workspace_id: 'ws-7',
      path: '/w/ws-7',
      outcome: 'succeeded',
      available: true,
      created_at: T,
      updated_at: T,
    },
    sessions: [
      {
        id: `sa-${id}`,
        role: 'planner',
        target: 'claude',
        session_id: 'ws-7-planner',
        outcome: 'succeeded',
        available: true,
        created_at: T,
        updated_at: T,
      },
    ],
    ...overrides,
  };
}

/** A started flight whose only role session is a planner with the given ID. */
function withPlanner(id: string, sessionId: string, createdAt = T): FlightSummaryResponse {
  const f = started(id, 'planning', { created_at: createdAt });
  return { ...f, sessions: [{ ...f.sessions![0], id: `sa-${id}`, session_id: sessionId }] };
}

function session(id: string, overrides: Partial<SessionWithWorkspace> = {}): SessionWithWorkspace {
  return {
    id,
    target: 'claude',
    branch: 'main',
    created_at: T,
    running: true,
    attach_cmd: '',
    nudge_state: 'Idle',
    workspace_id: 'ws-7',
    workspace_path: '/w/ws-7',
    repo: 'git@example.com:a.git',
    ...overrides,
  };
}

function renderList(
  flights: FlightSummaryResponse[],
  sessionsById: Record<string, SessionWithWorkspace> = {}
) {
  sessions.byId = sessionsById;
  const handlers = { onMove: vi.fn(), onEdit: vi.fn() };
  render(
    <MemoryRouter>
      <FlightList
        flights={flights}
        repoNames={{ 'git@example.com:a.git': 'alpha' }}
        {...handlers}
      />
    </MemoryRouter>
  );
  return handlers;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('FlightList', () => {
  it('shows empty Backlog and In the air sections', () => {
    renderList([]);
    expect(screen.getByTestId('flight-backlog')).toHaveTextContent('No flights in the backlog.');
    expect(screen.getByTestId('flight-air')).toHaveTextContent('No flights are in the air.');
  });

  it('lists every started flight, including completing and disposing, under In the air', () => {
    renderList([
      started('fl-p', 'planning'),
      started('fl-d', 'completing'),
      started('fl-c', 'executing', { cleanup_state: 'disposing' }),
    ]);

    const inAir = within(screen.getByTestId('flight-air'));
    expect(inAir.getByTestId('flight-row-fl-d')).toBeInTheDocument();
    expect(inAir.getByTestId('flight-row-fl-c')).toBeInTheDocument();
    expect(inAir.getByTestId('flight-row-fl-p')).toBeInTheDocument();
    expect(screen.getByTestId('flight-activity-fl-d')).toHaveTextContent('Completing');
    expect(screen.getByTestId('flight-activity-fl-c')).toHaveTextContent(/^Disposing$/);
    expect(screen.getByTestId('flight-row-fl-c').querySelector('.status-pill')).toBeNull();
  });

  it('orders the backlog by order and offers arrows, Edit, and Dispose only', () => {
    renderList([backlog('fl-b', 1), backlog('fl-a', 0)]);

    const rows = within(screen.getByRole('table', { name: 'Backlog flights' }))
      .getAllByRole('row')
      .slice(1);
    expect(rows.map((r) => r.getAttribute('data-testid'))).toEqual([
      'flight-row-fl-a',
      'flight-row-fl-b',
    ]);
    expect(screen.getByRole('button', { name: 'Move fl-a up' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Move fl-b down' })).toBeDisabled();
    expect(screen.getByTestId('flight-edit-fl-a')).toBeInTheDocument();
    // Workflow transitions, Take off included, live on the detail page.
    expect(screen.queryByTestId('flight-action-takeoff-fl-a')).not.toBeInTheDocument();
    // No Delete button; only Edit + Dispose.
    expect(screen.queryByTestId('flight-delete-fl-a')).not.toBeInTheDocument();
    // Dispose button rendered on each row.
    expect(screen.getAllByRole('button', { name: 'Dispose' })).toHaveLength(2);
  });

  it('renders no workflow transition buttons on started rows', () => {
    renderList([
      started('fl-p', 'planning'),
      started('fl-x', 'executing'),
      started('fl-d', 'completing'),
    ]);

    for (const id of ['fl-p', 'fl-x', 'fl-d']) {
      const row = within(screen.getByTestId(`flight-row-${id}`));
      expect(row.getAllByRole('button').map((b) => b.textContent)).toEqual(['Edit', 'Dispose']);
    }
  });

  it('shows no resource lines on a backlog flight without a workspace', () => {
    renderList([backlog('fl-a', 0)]);
    expect(screen.queryByTestId('flight-resources-fl-a')).not.toBeInTheDocument();
  });

  it('keeps a backlog flight with resources compact and links to its detail', () => {
    renderList([started('fl-b', 'backlog')]);
    const row = within(screen.getByTestId('flight-backlog')).getByTestId('flight-row-fl-b');
    expect(within(row).queryByRole('definition')).not.toBeInTheDocument();
    expect(within(row).getByRole('link', { name: 'fl-b' })).toHaveAttribute(
      'href',
      '/flights/fl-b'
    );
  });

  it('shows the activity line for the session with the greatest adjusted time', () => {
    const f = started('fl-p', 'executing');
    const flight = {
      ...f,
      sessions: [
        { ...f.sessions![0], session_id: 'p' },
        { ...f.sessions![0], id: 'sa-x', role: 'executor', session_id: 'x' },
      ],
    };
    // The working planner is aged an hour, so the stopped executor wins.
    renderList([flight], {
      p: session('p', { nudge_state: 'Working', last_output_at: '2026-01-01T01:00:00Z' }),
      x: session('x', { running: false, last_output_at: '2026-01-01T00:30:00Z' }),
    });

    const line = screen.getByTestId('flight-activity-fl-p');
    expect(line).toHaveTextContent(/^Executing · Executor · Stopped · /);
    expect(within(line).getByRole('link')).toHaveAttribute('href', '/sessions/x');
  });

  it('reads Working with a spinner for any session the sort treats as busy', () => {
    renderList([withPlanner('fl-p', 'p')], {
      p: session('p', { nudge_state: 'Background', last_output_at: T }),
    });

    const line = screen.getByTestId('flight-activity-fl-p');
    expect(line).toHaveTextContent(/^Planning · Planner · Working · /);
    expect(line.querySelector('.spinner')).not.toBeNull();
  });

  it('renders the nudge emoji and summary for a waiting session', () => {
    renderList([withPlanner('fl-p', 'p')], {
      p: session('p', {
        nudge_state: 'Needs Input',
        nudge_summary: 'Which port?',
        last_output_at: T,
      }),
    });

    expect(screen.getByTestId('flight-activity-fl-p')).toHaveTextContent(
      `Planner · ${nudgeStateEmoji['Needs Input']} Which port?`
    );
  });

  it('shows an unlinked "Planner session gone" line when no session resolves', () => {
    renderList([withPlanner('fl-p', 'missing')]);

    const line = screen.getByTestId('flight-activity-fl-p');
    expect(line).toHaveTextContent('Planner session gone');
    expect(within(line).queryByRole('link')).not.toBeInTheDocument();
  });

  it('orders In the air by activity time with timeless flights last by newest created_at', () => {
    renderList(
      [
        withPlanner('fl-old', 'old'),
        withPlanner('fl-gone-early', 'missing-1', '2026-01-01T00:00:00Z'),
        withPlanner('fl-new', 'new'),
        withPlanner('fl-gone-late', 'missing-2', '2026-01-02T00:00:00Z'),
      ],
      {
        old: session('old', { running: false, last_output_at: '2026-01-01T00:10:00Z' }),
        new: session('new', { running: false, last_output_at: '2026-01-01T00:50:00Z' }),
      }
    );

    const rows = within(screen.getByRole('table', { name: 'Flights in the air' }))
      .getAllByRole('row')
      .slice(1);
    expect(rows.map((r) => r.getAttribute('data-testid'))).toEqual([
      'flight-row-fl-new',
      'flight-row-fl-old',
      'flight-row-fl-gone-late',
      'flight-row-fl-gone-early',
    ]);
  });

  it('shows one linked status instead of pills and a resource inventory', () => {
    renderList([withPlanner('fl-p', 'p')], { p: session('p') });
    const row = screen.getByTestId('flight-row-fl-p');
    expect(row.querySelector('.status-pill')).toBeNull();
    expect(within(row).queryByRole('definition')).not.toBeInTheDocument();
    expect(
      within(row)
        .getAllByRole('link')
        .map((link) => link.getAttribute('href'))
    ).toEqual(['/flights/fl-p', '/sessions/p']);
    expect(screen.getByTestId('flight-activity-fl-p')).toHaveTextContent(
      /^Planning · Planner · Idle$/
    );
  });

  it('shows a status even when a started flight has no session assignments', () => {
    renderList([started('fl-p', 'planning', { sessions: [] })]);
    const line = screen.getByTestId('flight-activity-fl-p');
    expect(line).toHaveTextContent(/^Planning · No session$/);
    expect(within(line).queryByRole('link')).not.toBeInTheDocument();
  });

  it('shows the review reading and the pilot latest guidance under the status', () => {
    renderList([
      started('fl-r', 'reviewing', {
        review_status: {
          reviewer_session_id: 'ws-7-r1',
          state: 'fixing',
          tests: 'run_failed',
          test_rules_review: 'not_run',
          changed_work: 'changed',
          artifacts_named: 'incomplete',
          updated_at: T,
        },
        pilot_guidance: {
          recipient_role: 'reviewer',
          recipient_session_id: 'ws-7-r1',
          text: 'Rerun the tests after the fix.',
          status: 'queued',
          created_at: T,
        },
      }),
    ]);
    expect(screen.getByTestId('flight-activity-fl-r')).toHaveTextContent(
      'Reviewer reports: still fixing, tests run and failed, test-rules-review not run, changed the work, artifacts incomplete'
    );
    expect(screen.getByTestId('flight-pilot-guidance-fl-r')).toHaveTextContent(
      'Pilot to Reviewer (queued): Rerun the tests after the fix.'
    );
  });

  it('shows no guidance line when the pilot has sent none', () => {
    renderList([started('fl-p', 'planning')]);
    expect(screen.queryByTestId('flight-pilot-guidance-fl-p')).not.toBeInTheDocument();
  });

  it('shows disposal instead of a stale session status and offers retry', () => {
    renderList([started('fl-p', 'planning', { cleanup_state: 'disposing' })], {
      'ws-7-planner': session('ws-7-planner', { nudge_state: 'Working' }),
    });
    const line = screen.getByTestId('flight-activity-fl-p');
    expect(line).toHaveTextContent(/^Disposing$/);
    expect(within(line).queryByRole('link')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry dispose' })).toBeEnabled();
  });

  it('explains both sort orders on the page', () => {
    renderList([]);
    expect(screen.getByTestId('flight-backlog')).toHaveTextContent(
      'Manual order · use the arrows to reorder.'
    );
    expect(screen.getByTestId('flight-air')).toHaveTextContent(
      'Latest activity first · working sessions rank one hour older. No activity goes last, newest flight first.'
    );
  });

  it('shows the last failed attempt on the row', () => {
    renderList([
      started('fl-p', 'planning', {
        last_error: 'spawn planner: target claude has no chat mode',
      }),
    ]);
    expect(screen.getByTestId('flight-error-fl-p')).toHaveTextContent(
      'spawn planner: target claude has no chat mode'
    );
  });

  it('reports reorder and edit intents for the row', async () => {
    const flights = [backlog('fl-a', 0), backlog('fl-b', 1)];
    const handlers = renderList(flights);

    await userEvent.click(screen.getByRole('button', { name: 'Move fl-a down' }));
    await userEvent.click(screen.getByTestId('flight-edit-fl-b'));

    expect(handlers.onMove).toHaveBeenCalledWith('fl-a', 1);
    expect(handlers.onEdit).toHaveBeenCalledWith(flights[1]);
  });

  it('labels the backlog table columns', () => {
    renderList([backlog('fl-a', 0)]);
    const table = screen.getByRole('table', { name: 'Backlog flights' });
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((cell) => cell.textContent)
    ).toEqual(['Flight', 'Repository', 'Actions']);
  });

  it('labels the In the air table columns', () => {
    renderList([started('fl-p', 'planning')]);
    const table = screen.getByRole('table', { name: 'Flights in the air' });
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((cell) => cell.textContent)
    ).toEqual(['Flight', 'Status', 'Actions']);
  });

  it('renders the name-error line under the label of a backlog flight', () => {
    renderList([{ ...backlog('fl-n', 0), name_error: 'target not found: x' }]);
    const line = screen.getByTestId('flight-name-error-fl-n');
    expect(line).toHaveTextContent('Name not generated: target not found: x');
    expect(line.previousElementSibling).toHaveAttribute('href', '/flights/fl-n');
  });

  it('renders the name-error line under the label of a started flight', () => {
    renderList([started('fl-n', 'planning', { name_error: 'target not found: x' })]);
    const line = screen.getByTestId('flight-name-error-fl-n');
    expect(line).toHaveTextContent('Name not generated: target not found: x');
    expect(line.previousElementSibling).toHaveAttribute('href', '/flights/fl-n');
  });

  it('does not render the name-error line when there is none', () => {
    renderList([started('fl-n', 'planning')]);
    expect(screen.queryByTestId('flight-name-error-fl-n')).toBeNull();
  });
});
