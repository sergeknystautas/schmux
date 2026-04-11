import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import AddRepoModal from './AddRepoModal';
import type { LocalRepo, ProbeRepoResult } from '../lib/api';
import type { ConfigResponse } from '../lib/types';

// Mock navigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Mock ConfigContext
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    reloadConfig: vi.fn().mockResolvedValue(undefined),
  }),
}));

// Mock API
const mockScanLocalRepos = vi.fn<() => Promise<LocalRepo[]>>();
const mockProbeRepo = vi.fn<(url: string) => Promise<ProbeRepoResult>>();
const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockUpdateConfig = vi.fn();

vi.mock('../lib/api', () => ({
  scanLocalRepos: (...args: unknown[]) => mockScanLocalRepos(...(args as [])),
  probeRepo: (...args: unknown[]) => mockProbeRepo(...(args as [string])),
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  updateConfig: (...args: unknown[]) => mockUpdateConfig(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
}));

const baseConfig = {
  workspace_path: '/tmp/workspaces',
  repos: [],
  run_targets: [],
} as unknown as ConfigResponse;

function renderModal(onClose = vi.fn()) {
  return render(
    <MemoryRouter>
      <AddRepoModal onClose={onClose} />
    </MemoryRouter>
  );
}

describe('AddRepoModal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockScanLocalRepos.mockResolvedValue([]);
    mockGetConfig.mockResolvedValue({ ...baseConfig });
  });

  it('renders with "Clone from" label and subtext', () => {
    renderModal();

    expect(screen.getByText('Clone from')).toBeInTheDocument();
    expect(screen.getByText(/Each workspace gets its own isolated copy/)).toBeInTheDocument();
  });

  it('shows scan results on focus', async () => {
    const user = userEvent.setup();
    mockScanLocalRepos.mockResolvedValue([
      {
        name: 'my-project',
        path: '/home/user/my-project',
        vcs: 'git',
        remote_url: 'https://github.com/user/my-project.git',
      },
      { name: 'other-repo', path: '/home/user/other-repo', vcs: 'sapling' },
    ]);

    renderModal();

    const input = screen.getByRole('combobox');
    await user.click(input);

    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument();
      expect(screen.getByText('other-repo')).toBeInTheDocument();
    });

    // VCS type should be visible
    expect(screen.getByText('git')).toBeInTheDocument();
    expect(screen.getByText('sapling')).toBeInTheDocument();
  });

  it('shows spinner during probe', async () => {
    const user = userEvent.setup();
    // Never resolve so we stay in the probing state
    mockProbeRepo.mockReturnValue(new Promise(() => {}));

    renderModal();

    const input = screen.getByRole('combobox');
    await user.type(input, 'https://github.com/user/repo.git');

    const addButton = screen.getByRole('button', { name: /add/i });
    await user.click(addButton);

    await waitFor(() => {
      expect(screen.getByText('Checking repository access...')).toBeInTheDocument();
    });
  });

  it('on success: calls updateConfig and navigates to /spawn', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    mockProbeRepo.mockResolvedValue({
      accessible: true,
      default_branch: 'main',
    });
    mockGetConfig.mockResolvedValue({
      ...baseConfig,
      repos: [{ name: 'existing', url: 'https://example.com/existing.git' }],
    });
    mockUpdateConfig.mockResolvedValue({ status: 'ok' });

    renderModal(onClose);

    const input = screen.getByRole('combobox');
    await user.type(input, 'https://github.com/user/my-repo.git');

    const addButton = screen.getByRole('button', { name: /add/i });
    await user.click(addButton);

    await waitFor(() => {
      expect(mockUpdateConfig).toHaveBeenCalledWith(
        expect.objectContaining({
          repos: [
            { name: 'existing', url: 'https://example.com/existing.git' },
            { name: 'my-repo', url: 'https://github.com/user/my-repo.git' },
          ],
        })
      );
    });

    await waitFor(() => {
      expect(mockNavigate).toHaveBeenCalledWith('/spawn', {
        state: { repo: 'https://github.com/user/my-repo.git', branch: 'main' },
      });
    });
  });

  it('on failure: shows inline error', async () => {
    const user = userEvent.setup();

    mockProbeRepo.mockResolvedValue({
      accessible: false,
      default_branch: '',
      error: 'Repository not found or access denied',
    });

    renderModal();

    const input = screen.getByRole('combobox');
    await user.type(input, 'https://github.com/user/bad-repo.git');

    const addButton = screen.getByRole('button', { name: /add/i });
    await user.click(addButton);

    await waitFor(() => {
      expect(screen.getByText('Repository not found or access denied')).toBeInTheDocument();
    });

    // Should not navigate
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('cancel button calls onClose', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderModal(onClose);

    const cancelButton = screen.getByRole('button', { name: /cancel/i });
    await user.click(cancelButton);

    expect(onClose).toHaveBeenCalled();
  });

  it('shows remote URL when selecting a local repo', async () => {
    const user = userEvent.setup();
    mockScanLocalRepos.mockResolvedValue([
      {
        name: 'my-project',
        path: '/home/user/my-project',
        vcs: 'git',
        remote_url: 'https://github.com/user/my-project.git',
      },
    ]);

    renderModal();

    const input = screen.getByRole('combobox');
    await user.click(input);

    await waitFor(() => {
      expect(screen.getByText('my-project')).toBeInTheDocument();
    });

    await user.click(screen.getByText('my-project'));

    await waitFor(() => {
      expect(screen.getByText('https://github.com/user/my-project.git')).toBeInTheDocument();
    });
  });

  it('cancel button during probe aborts and resets', async () => {
    const user = userEvent.setup();
    // Never resolve so we stay in the probing state
    mockProbeRepo.mockReturnValue(new Promise(() => {}));

    renderModal();

    const input = screen.getByRole('combobox');
    await user.type(input, 'https://github.com/user/repo.git');

    const addButton = screen.getByRole('button', { name: /add/i });
    await user.click(addButton);

    await waitFor(() => {
      expect(screen.getByText('Checking repository access...')).toBeInTheDocument();
    });

    // Click Cancel during probe
    const cancelButton = screen.getByRole('button', { name: /cancel/i });
    await user.click(cancelButton);

    // Should reset back to the input state (no spinner)
    await waitFor(() => {
      expect(screen.queryByText('Checking repository access...')).not.toBeInTheDocument();
    });
  });
});
