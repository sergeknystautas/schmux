import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import RepofeedPage from './RepofeedPage';

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    repofeedUpdateCount: 0,
  }),
}));

vi.mock('../lib/api', () => ({
  getRepofeedList: vi.fn(),
  getRepofeedRepo: vi.fn(),
}));

import { getRepofeedList, getRepofeedRepo } from '../lib/api';

const mockGetRepofeedList = vi.mocked(getRepofeedList);
const mockGetRepofeedRepo = vi.mocked(getRepofeedRepo);

function renderPage() {
  return render(
    <MemoryRouter>
      <RepofeedPage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('RepofeedPage', () => {
  it('shows loading state initially', () => {
    mockGetRepofeedList.mockReturnValue(new Promise(() => {})); // never resolves
    renderPage();
    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('shows empty state when no repos', async () => {
    mockGetRepofeedList.mockResolvedValue({ repos: [] });
    renderPage();
    await waitFor(() => {
      expect(
        screen.getByText('No repofeed data yet. Enable repofeed in settings to start publishing.')
      ).toBeInTheDocument();
    });
  });

  it('renders repo tabs', async () => {
    mockGetRepofeedList.mockResolvedValue({
      repos: [
        { name: 'frontend', slug: 'frontend', active_intents: 2, landed_count: 0 },
        { name: 'backend', slug: 'backend', active_intents: 0, landed_count: 1 },
      ],
    });
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'frontend',
      slug: 'frontend',
      intents: [],
      landed: [],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('frontend')).toBeInTheDocument();
      expect(screen.getByText('backend')).toBeInTheDocument();
    });
  });

  it('shows badge for active intents', async () => {
    mockGetRepofeedList.mockResolvedValue({
      repos: [{ name: 'frontend', slug: 'frontend', active_intents: 3, landed_count: 0 }],
    });
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'frontend',
      slug: 'frontend',
      intents: [],
      landed: [],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('3')).toBeInTheDocument();
    });
  });

  it('fetches and renders intent cards', async () => {
    mockGetRepofeedList.mockResolvedValue({
      repos: [{ name: 'my-repo', slug: 'my-repo', active_intents: 1, landed_count: 0 }],
    });
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'my-repo',
      slug: 'my-repo',
      intents: [
        {
          developer: 'alice@example.com',
          display_name: 'Alice',
          intent: 'Adding user auth',
          status: 'active',
          started: new Date().toISOString(),
          branches: ['feature/auth'],
          session_count: 2,
          agents: ['claude-code'],
        },
      ],
      landed: [],
    });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Adding user auth')).toBeInTheDocument();
      expect(screen.getByText(/Alice/)).toBeInTheDocument();
      expect(screen.getByText('feature/auth')).toBeInTheDocument();
    });
  });

  it('filters intents by status', async () => {
    mockGetRepofeedList.mockResolvedValue({
      repos: [{ name: 'repo', slug: 'repo', active_intents: 1, landed_count: 0 }],
    });
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'repo',
      slug: 'repo',
      intents: [
        {
          developer: 'a@b.com',
          display_name: 'A',
          intent: 'Active work',
          status: 'active',
          started: '',
          branches: [],
          session_count: 1,
          agents: [],
        },
        {
          developer: 'c@d.com',
          display_name: 'C',
          intent: 'Done work',
          status: 'completed',
          started: '',
          branches: [],
          session_count: 0,
          agents: [],
        },
      ],
      landed: [],
    });
    renderPage();

    // Both visible by default (All filter)
    await waitFor(() => {
      expect(screen.getByText('Active work')).toBeInTheDocument();
      expect(screen.getByText('Done work')).toBeInTheDocument();
    });

    // Click "Landed" filter — only completed shown
    await userEvent.click(screen.getByText('Landed'));
    await waitFor(() => {
      expect(screen.queryByText('Active work')).not.toBeInTheDocument();
      expect(screen.getByText('Done work')).toBeInTheDocument();
    });

    // Click "In Progress" filter — only active shown
    await userEvent.click(screen.getByText('In Progress'));
    await waitFor(() => {
      expect(screen.getByText('Active work')).toBeInTheDocument();
      expect(screen.queryByText('Done work')).not.toBeInTheDocument();
    });
  });

  it('selects first repo by default and allows switching', async () => {
    mockGetRepofeedList.mockResolvedValue({
      repos: [
        { name: 'repo-a', slug: 'repo-a', active_intents: 0, landed_count: 0 },
        { name: 'repo-b', slug: 'repo-b', active_intents: 0, landed_count: 0 },
      ],
    });
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'repo-a',
      slug: 'repo-a',
      intents: [],
      landed: [],
    });
    renderPage();

    await waitFor(() => {
      // First repo should be auto-selected, triggering a detail fetch
      expect(mockGetRepofeedRepo).toHaveBeenCalledWith('repo-a');
    });

    // Click second repo tab
    mockGetRepofeedRepo.mockResolvedValue({
      name: 'repo-b',
      slug: 'repo-b',
      intents: [],
      landed: [],
    });
    await userEvent.click(screen.getByText('repo-b'));

    await waitFor(() => {
      expect(mockGetRepofeedRepo).toHaveBeenCalledWith('repo-b');
    });
  });
});
