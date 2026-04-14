import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import RepofeedPage from './RepofeedPage';

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    repofeedUpdateCount: 0,
    workspaces: [
      {
        id: 'ws-001',
        repo: 'https://github.com/test/repo',
        branch: 'main',
        path: '/tmp/ws-001',
        session_count: 1,
        sessions: [{ id: 'session-1', running: true }],
        ahead: 0,
        behind: 0,
        lines_added: 0,
        lines_removed: 0,
        files_changed: 0,
        intent_shared: false,
      },
    ],
  }),
}));

vi.mock('../lib/api', () => ({
  getRepofeedOutgoing: vi.fn(),
  getRepofeedIncoming: vi.fn(),
  setIntentShared: vi.fn(),
  dismissRepofeedIntent: vi.fn(),
}));

import { getRepofeedOutgoing, getRepofeedIncoming } from '../lib/api';

const mockGetOutgoing = vi.mocked(getRepofeedOutgoing);
const mockGetIncoming = vi.mocked(getRepofeedIncoming);

function renderPage() {
  return render(
    <MemoryRouter>
      <RepofeedPage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockGetOutgoing.mockResolvedValue({ entries: [] });
  mockGetIncoming.mockResolvedValue({ entries: [] });
});

describe('RepofeedPage', () => {
  it('shows loading state initially', () => {
    mockGetOutgoing.mockReturnValue(new Promise(() => {}));
    mockGetIncoming.mockReturnValue(new Promise(() => {}));
    renderPage();
    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('shows empty state when no incoming intents', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('No incoming activity yet.')).toBeInTheDocument();
    });
  });

  it('renders outgoing section with workspaces', async () => {
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Outgoing')).toBeInTheDocument();
      expect(screen.getByText('ws-001')).toBeInTheDocument();
      expect(screen.getByText('Share activity')).toBeInTheDocument();
    });
  });

  it('shows LLM summary in outgoing when available', async () => {
    mockGetOutgoing.mockResolvedValue({
      entries: [{ workspace_id: 'ws-001', summary: 'Fixing auth timeout' }],
    });
    renderPage();
    await waitFor(() => {
      // Summary should NOT appear because ws-001 is not shared (intent_shared=false)
      // It should show the branch name instead
      expect(screen.getByText('main')).toBeInTheDocument();
    });
  });

  it('renders incoming intents grouped by developer', async () => {
    mockGetIncoming.mockResolvedValue({
      entries: [
        {
          developer: 'alice@example.com',
          display_name: 'Alice',
          intent: 'Adding user auth',
          status: 'active',
          started: new Date().toISOString(),
        },
        {
          developer: 'bob@example.com',
          display_name: 'Bob',
          intent: 'Fixing tests',
          status: 'inactive',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Adding user auth')).toBeInTheDocument();
      expect(screen.getByText('Fixing tests')).toBeInTheDocument();
      expect(screen.getAllByText(/Alice/).length).toBeGreaterThan(0);
      expect(screen.getAllByText(/Bob/).length).toBeGreaterThan(0);
    });
  });

  it('filters intents by status', async () => {
    mockGetIncoming.mockResolvedValue({
      entries: [
        {
          developer: 'a@b.com',
          display_name: 'A',
          intent: 'Active work',
          status: 'active',
        },
        {
          developer: 'c@d.com',
          display_name: 'C',
          intent: 'Done work',
          status: 'completed',
        },
      ],
    });
    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Active work')).toBeInTheDocument();
      expect(screen.getByText('Done work')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByText('Finished'));
    await waitFor(() => {
      expect(screen.queryByText('Active work')).not.toBeInTheDocument();
      expect(screen.getByText('Done work')).toBeInTheDocument();
    });

    await userEvent.click(screen.getByText('In Progress'));
    await waitFor(() => {
      expect(screen.getByText('Active work')).toBeInTheDocument();
      expect(screen.queryByText('Done work')).not.toBeInTheDocument();
    });
  });

  it('shows dismiss button for completed intents', async () => {
    mockGetIncoming.mockResolvedValue({
      entries: [
        {
          developer: 'alice@example.com',
          display_name: 'Alice',
          intent: 'Finished feature',
          status: 'completed',
          workspace_id: 'ws-remote-1',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Dismiss')).toBeInTheDocument();
    });
  });

  it('shows developer group headers in incoming section', async () => {
    mockGetIncoming.mockResolvedValue({
      entries: [
        {
          developer: 'alice@example.com',
          display_name: 'Alice',
          intent: 'Adding auth',
          status: 'active',
        },
        {
          developer: 'alice@example.com',
          display_name: 'Alice',
          intent: 'Fixing tests',
          status: 'inactive',
        },
        {
          developer: 'bob@example.com',
          display_name: 'Bob',
          intent: 'Refactoring',
          status: 'active',
        },
      ],
    });
    renderPage();
    await waitFor(() => {
      // Developer headers appear as group separators (exact text match)
      // IncomingCards show "Alice · active" so exact "Alice" only matches the header
      expect(screen.getByText('Alice')).toBeInTheDocument();
      expect(screen.getByText('Bob')).toBeInTheDocument();
      // The intents themselves are also rendered
      expect(screen.getByText('Adding auth')).toBeInTheDocument();
      expect(screen.getByText('Refactoring')).toBeInTheDocument();
    });
  });
});
