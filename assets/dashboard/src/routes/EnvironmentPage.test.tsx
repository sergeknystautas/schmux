import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import EnvironmentPage from './EnvironmentPage';

vi.mock('../lib/api', () => ({
  getEnvironment: vi.fn(),
  syncEnvironmentVar: vi.fn(),
  getErrorMessage: vi.fn((err: unknown, fallback: string) => fallback),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

import { getEnvironment, syncEnvironmentVar } from '../lib/api';

const mockGetEnvironment = vi.mocked(getEnvironment);
const mockSyncEnvironmentVar = vi.mocked(syncEnvironmentVar);

function renderPage() {
  return render(
    <MemoryRouter>
      <EnvironmentPage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('EnvironmentPage', () => {
  it('shows loading state initially', () => {
    mockGetEnvironment.mockReturnValue(new Promise(() => {}));
    renderPage();
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it('renders environment variables with statuses', async () => {
    mockGetEnvironment.mockResolvedValue({
      vars: [
        { key: 'GOPATH', status: 'in_sync' },
        { key: 'NVM_DIR', status: 'system_only' },
        { key: 'PATH', status: 'differs' },
      ],
      blocked: ['TMUX', 'SHLVL'],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('PATH')).toBeInTheDocument();
    });

    expect(screen.getByText('GOPATH')).toBeInTheDocument();
    expect(screen.getByText('NVM_DIR')).toBeInTheDocument();
  });

  it('shows sync button for differs and system_only, not for in_sync or tmux_only', async () => {
    mockGetEnvironment.mockResolvedValue({
      vars: [
        { key: 'PATH', status: 'differs' },
        { key: 'GOPATH', status: 'in_sync' },
        { key: 'NVM_DIR', status: 'system_only' },
        { key: 'OLD_VAR', status: 'tmux_only' },
      ],
      blocked: ['TMUX'],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('PATH')).toBeInTheDocument();
    });

    const syncButtons = screen.getAllByRole('button', { name: /sync/i });
    expect(syncButtons).toHaveLength(2);
  });

  it('calls syncEnvironmentVar and reloads on sync click', async () => {
    const user = userEvent.setup();

    mockGetEnvironment.mockResolvedValue({
      vars: [{ key: 'PATH', status: 'differs' }],
      blocked: [],
    });
    mockSyncEnvironmentVar.mockResolvedValue(undefined);

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('PATH')).toBeInTheDocument();
    });

    mockGetEnvironment.mockResolvedValue({
      vars: [{ key: 'PATH', status: 'in_sync' }],
      blocked: [],
    });

    const syncBtn = screen.getByRole('button', { name: /sync/i });
    await user.click(syncBtn);

    expect(mockSyncEnvironmentVar).toHaveBeenCalledWith('PATH');

    await waitFor(() => {
      expect(mockGetEnvironment).toHaveBeenCalledTimes(2);
    });
  });

  it('shows blocked keys section', async () => {
    mockGetEnvironment.mockResolvedValue({
      vars: [{ key: 'PATH', status: 'in_sync' }],
      blocked: ['TMUX', 'SHLVL'],
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('TMUX')).toBeInTheDocument();
    });
    expect(screen.getByText('SHLVL')).toBeInTheDocument();
  });

  it('shows error state on fetch failure', async () => {
    mockGetEnvironment.mockRejectedValue(new Error('Network error'));

    renderPage();

    await waitFor(() => {
      expect(screen.getByText('Failed to fetch environment')).toBeInTheDocument();
    });
  });
});
