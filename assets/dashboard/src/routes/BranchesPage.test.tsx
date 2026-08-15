import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { RecentBranch } from '../lib/types';

const { mockNavigate, mockAlert } = vi.hoisted(() => ({
  mockNavigate: vi.fn(),
  mockAlert: vi.fn(),
}));

vi.mock('react-router', async () => {
  const actual = await vi.importActual<typeof import('react-router')>('react-router');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('../lib/api', () => ({
  getRecentBranches: vi.fn(),
  refreshRecentBranches: vi.fn(),
  prepareBranchSpawn: vi.fn(),
  getErrorMessage: vi.fn((_err: unknown, fallback: string) => fallback),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: mockAlert, confirm: vi.fn() }),
}));

import BranchesPage from './BranchesPage';
import { getRecentBranches, refreshRecentBranches, prepareBranchSpawn } from '../lib/api';

const mockGetRecentBranches = vi.mocked(getRecentBranches);
const mockRefreshRecentBranches = vi.mocked(refreshRecentBranches);
const mockPrepareBranchSpawn = vi.mocked(prepareBranchSpawn);

const branches: RecentBranch[] = [
  {
    repo_name: 'schmux',
    repo_url: 'https://github.com/user/schmux.git',
    branch: 'feature/load-remote-branch',
    commit_date: '2026-08-13T10:00:00Z',
    subject: 'feat: remote branches page',
  },
  {
    repo_name: 'schmux',
    repo_url: 'https://github.com/user/schmux.git',
    branch: 'fix/spawn-race',
    commit_date: '2026-08-12T09:00:00Z',
    subject: 'fix: spawn race',
  },
];

function renderPage() {
  return render(
    <MemoryRouter>
      <BranchesPage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('BranchesPage', () => {
  it('shows loading state initially', () => {
    mockGetRecentBranches.mockReturnValue(new Promise(() => {}));
    renderPage();
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('fetches with limit 50 and renders all rows', async () => {
    mockGetRecentBranches.mockResolvedValue(branches);
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('branch-item')).toHaveLength(2);
    });
    expect(mockGetRecentBranches).toHaveBeenCalledWith(50);
    expect(screen.getByText('feature/load-remote-branch')).toBeInTheDocument();
    expect(screen.getByText('fix: spawn race')).toBeInTheDocument();
  });

  it('shows empty state when no branches', async () => {
    mockGetRecentBranches.mockResolvedValue([]);
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('No branches found yet')).toBeInTheDocument();
    });
  });

  it('renders rows inside the shared session-table primitive', async () => {
    mockGetRecentBranches.mockResolvedValue(branches);
    renderPage();
    await waitFor(() => {
      expect(screen.getByTestId('branch-list')).toBeInTheDocument();
    });
    const table = screen.getByTestId('branch-list');
    expect(table.tagName).toBe('TABLE');
    expect(table).toHaveClass('session-table');
    expect(screen.getByRole('columnheader', { name: 'Branch' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'Last commit' })).toBeInTheDocument();
  });

  it('shows error state on fetch failure, keeping Refresh rendered', async () => {
    mockGetRecentBranches.mockRejectedValue(new Error('boom'));
    renderPage();
    await waitFor(() => {
      expect(screen.getByText('Failed to load branches')).toBeInTheDocument();
    });
    expect(screen.getByTestId('branches-refresh')).toBeInTheDocument();
  });

  it('refresh replaces the list', async () => {
    mockGetRecentBranches.mockResolvedValue([branches[0]]);
    mockRefreshRecentBranches.mockResolvedValue({ branches, fetched_count: 2 });
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('branch-item')).toHaveLength(1);
    });
    fireEvent.click(screen.getByTestId('branches-refresh'));
    await waitFor(() => {
      expect(screen.getAllByTestId('branch-item')).toHaveLength(2);
    });
  });

  it('row click prepares spawn and navigates with the result as state', async () => {
    mockGetRecentBranches.mockResolvedValue(branches);
    const prepared = {
      repo: 'https://github.com/user/schmux.git',
      branch: 'feature/load-remote-branch',
      prompt: 'Recent commits:\n...',
    };
    mockPrepareBranchSpawn.mockResolvedValue(prepared);
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('branch-item')).toHaveLength(2);
    });
    fireEvent.click(screen.getAllByTestId('branch-item')[0]);
    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/spawn', { state: prepared });
    });
    expect(mockPrepareBranchSpawn).toHaveBeenCalledWith('schmux', 'feature/load-remote-branch');
  });

  it('prepare failure shows alert and does not navigate', async () => {
    mockGetRecentBranches.mockResolvedValue(branches);
    mockPrepareBranchSpawn.mockRejectedValue(new Error('nope'));
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('branch-item')).toHaveLength(2);
    });
    fireEvent.click(screen.getAllByTestId('branch-item')[0]);
    await waitFor(() => {
      expect(mockAlert).toHaveBeenCalledWith(
        'Branch Spawn Failed',
        'Failed to prepare branch spawn'
      );
    });
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
