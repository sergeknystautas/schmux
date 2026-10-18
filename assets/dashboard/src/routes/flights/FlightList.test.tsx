import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import FlightList from './FlightList';
import type { FlightSummaryResponse } from '../../lib/types.generated';

const mockNavigate = vi.fn();
vi.mock('react-router', async (importOriginal) => {
  const mod = await importOriginal<typeof import('react-router')>();
  return { ...mod, useNavigate: () => mockNavigate };
});
vi.mock('../../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [{ id: 'ws-7', sessions: [{ id: 'ws-7-planner' }] }],
  }),
}));
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), prompt: vi.fn() }),
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

function renderList(flights: FlightSummaryResponse[]) {
  const handlers = { onMove: vi.fn(), onEdit: vi.fn(), onDelete: vi.fn() };
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
  it('shows empty backlog, active, and finished sections', () => {
    renderList([]);
    expect(screen.getByTestId('flight-backlog')).toHaveTextContent('No flights in the backlog.');
    expect(screen.getByTestId('flight-active')).toHaveTextContent('No flights are in progress.');
    expect(screen.getByTestId('flight-finished')).toHaveTextContent(
      'No finished flights are holding a workspace.'
    );
  });

  it('splits started flights into Active (planning to completing) and Finished (done, cancelled)', () => {
    renderList([
      started('fl-p', 'planning'),
      started('fl-x', 'executing'),
      started('fl-m', 'completing'),
      started('fl-d', 'done'),
      started('fl-c', 'cancelled'),
    ]);

    const rowIds = (section: string) =>
      within(screen.getByTestId(section))
        .getAllByRole('row')
        .slice(1)
        .map((r) => r.getAttribute('data-testid'));
    expect(rowIds('flight-active')).toEqual([
      'flight-row-fl-p',
      'flight-row-fl-x',
      'flight-row-fl-m',
    ]);
    expect(rowIds('flight-finished')).toEqual(['flight-row-fl-d', 'flight-row-fl-c']);
  });

  it('orders the backlog by its order field and offers backlog-only controls', () => {
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
    expect(screen.getByTestId('flight-delete-fl-a')).toBeInTheDocument();
    expect(screen.getByTestId('flight-action-takeoff-fl-a')).toHaveTextContent('Take off');
  });

  it('reports reorder, edit, and delete intents for the row', async () => {
    const flights = [backlog('fl-a', 0), backlog('fl-b', 1)];
    const handlers = renderList(flights);

    await userEvent.click(screen.getByRole('button', { name: 'Move fl-a down' }));
    await userEvent.click(screen.getByTestId('flight-edit-fl-b'));
    await userEvent.click(screen.getByTestId('flight-delete-fl-a'));

    expect(handlers.onMove).toHaveBeenCalledWith('fl-a', 1);
    expect(handlers.onEdit).toHaveBeenCalledWith(flights[1]);
    expect(handlers.onDelete).toHaveBeenCalledWith(flights[0]);
  });

  it('exposes a Delete button on started rows so any flight can be removed', async () => {
    const flights = [started('fl-p', 'planning'), started('fl-d', 'done')];
    const handlers = renderList(flights);

    await userEvent.click(screen.getByTestId('flight-delete-fl-p'));
    await userEvent.click(screen.getByTestId('flight-delete-fl-d'));

    expect(handlers.onDelete).toHaveBeenNthCalledWith(1, flights[0]);
    expect(handlers.onDelete).toHaveBeenNthCalledWith(2, flights[1]);
  });

  it('labels the list columns for scanning flights and their actions', () => {
    renderList([backlog('fl-a', 0)]);
    const table = screen.getByRole('table', { name: 'Backlog flights' });
    expect(
      within(table)
        .getAllByRole('columnheader')
        .map((cell) => cell.textContent)
    ).toEqual(['Flight', 'Repository', 'Actions']);
  });

  it('lists every started flight, including done and cancelled ones that still own resources', () => {
    renderList([
      started('fl-p', 'planning'),
      started('fl-d', 'done'),
      started('fl-c', 'cancelled', { cleanup_state: 'terminating' }),
    ]);

    expect(within(screen.getByTestId('flight-active')).getAllByRole('row').slice(1)).toHaveLength(
      1
    );
    expect(within(screen.getByTestId('flight-finished')).getAllByRole('row').slice(1)).toHaveLength(
      2
    );
    expect(screen.getByTestId('flight-state-fl-d')).toHaveTextContent('done');
    expect(screen.getByTestId('flight-cleanup-fl-c')).toHaveTextContent('terminating');
    expect(screen.getByTestId('flight-action-terminate-fl-d')).toHaveTextContent('Terminate');
    expect(screen.getByTestId('flight-action-terminate-fl-c')).toHaveTextContent('Retry terminate');
    expect(screen.queryByTestId('flight-action-cancel-fl-c')).not.toBeInTheDocument();
    expect(screen.getByTestId('flight-edit-fl-p')).toBeInTheDocument();
  });

  it('lists each resource on its own labelled line', () => {
    renderList([started('fl-p', 'planning')]);

    const resources = within(screen.getByTestId('flight-resources-fl-p'));
    const terms = resources.getAllByRole('term').map((t) => t.textContent);
    expect(terms).toEqual(['Workspace', 'Planner']);
    expect(resources.getAllByRole('definition')).toHaveLength(2);
  });

  it('links the workspace and role sessions to their existing pages', async () => {
    renderList([started('fl-p', 'planning')]);

    expect(screen.getByTestId('flight-session-link-planner')).toHaveAttribute(
      'href',
      '/sessions/ws-7-planner'
    );
    // The workspace is a real link (underlined, right-clickable), not a button.
    const workspace = screen.getByTestId('flight-workspace-link');
    expect(workspace.tagName).toBe('A');
    expect(workspace).toHaveAttribute('href', '/sessions/ws-7-planner');
  });

  it('shows resources that no longer exist as unavailable history', () => {
    const f = started('fl-p', 'executing');
    renderList([
      {
        ...f,
        workspace: { ...f.workspace!, available: false },
        sessions: [{ ...f.sessions![0], available: false }],
      },
    ]);

    expect(screen.getByTestId('flight-workspace-unavailable')).toHaveTextContent(
      'ws-7 (unavailable)'
    );
    expect(screen.getByTestId('flight-session-unavailable-planner')).toHaveTextContent(
      'ws-7-planner (unavailable)'
    );
    expect(screen.queryByTestId('flight-workspace-link')).not.toBeInTheDocument();
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
});
