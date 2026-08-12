import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import GitHubConnectBanner from './GitHubConnectBanner';
import * as api from '../lib/api';
import type { GitHubConnectStatus } from '../lib/types.generated';

vi.mock('../lib/api');

const fullPlanStatus: GitHubConnectStatus = {
  eligible: true,
  gh: { available: true, username: 'sergeknystautas' },
  owners: ['sergeknystautas', 'lordbaltogames'],
  remote_reachable: false,
  remote_has_refs: false,
  config_url_is_local: true,
  state_repo_is_local: true,
  plan: [
    { step: 'set_origin', needed: true, reason: 'workspace has no origin remote' },
    { step: 'create_repo', needed: true, reason: 'no reachable remote repository' },
    { step: 'update_config', needed: true, reason: 'schmux config still records a local repo' },
    { step: 'link_workspaces', needed: true, reason: 'workspace still linked to the local repo' },
    { step: 'initial_push', needed: true, reason: 'remote has no branches yet' },
  ],
  name: 'talkback',
  default_branch: 'main',
};

describe('GitHubConnectBanner', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.getGitHubConnectStatus).mockResolvedValue(fullPlanStatus);
  });

  it('renders the banner and opens the dialog with create fields', async () => {
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    expect(
      screen.getByText('This repo only exists in this workspace — connect it to GitHub.')
    ).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));
    await waitFor(() => expect(api.getGitHubConnectStatus).toHaveBeenCalledWith('talkback-001'));

    expect(screen.getByLabelText(/owner/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/repository name/i)).toHaveValue('talkback');
    expect(screen.getByLabelText(/default branch/i)).toHaveValue('main');
  });

  it('repair-only plan hides create fields but keeps default branch when push is needed', async () => {
    vi.mocked(api.getGitHubConnectStatus).mockResolvedValue({
      ...fullPlanStatus,
      origin_url: 'https://github.com/sergeknystautas/talkback.git',
      remote_reachable: true,
      remote_has_refs: false,
      plan: [
        { step: 'set_origin', needed: false, reason: 'origin already set' },
        { step: 'create_repo', needed: false, reason: 'remote repository exists' },
        { step: 'update_config', needed: true, reason: 'schmux config still records a local repo' },
        {
          step: 'link_workspaces',
          needed: true,
          reason: 'workspace still linked to the local repo',
        },
        { step: 'initial_push', needed: true, reason: 'remote has no branches yet' },
      ],
    });
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));
    await waitFor(() => expect(api.getGitHubConnectStatus).toHaveBeenCalled());

    expect(screen.queryByLabelText(/owner/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/repository name/i)).not.toBeInTheDocument();
    expect(screen.getByLabelText(/default branch/i)).toBeInTheDocument();
  });

  it('submits and shows per-step results', async () => {
    vi.mocked(api.runGitHubConnect).mockResolvedValue({
      success: true,
      repo_url: 'https://github.com/sergeknystautas/talkback',
      steps: [
        { step: 'set_origin', status: 'done', detail: '' },
        { step: 'create_repo', status: 'done', detail: 'sergeknystautas/talkback' },
        { step: 'update_config', status: 'done', detail: '' },
        { step: 'link_workspaces', status: 'done', detail: '1 workspace(s) linked' },
        { step: 'initial_push', status: 'done', detail: 'pushed HEAD to main' },
      ],
    });
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));
    await waitFor(() => expect(api.getGitHubConnectStatus).toHaveBeenCalled());

    await userEvent.click(screen.getByRole('button', { name: /^connect$/i }));
    await waitFor(() =>
      expect(api.runGitHubConnect).toHaveBeenCalledWith('talkback-001', {
        owner: 'sergeknystautas',
        name: 'talkback',
        visibility: 'private',
        default_branch: 'main',
      })
    );
    expect(await screen.findByText(/connected/i)).toBeInTheDocument();
  });

  it('prefetches status on mount and reuses it when the dialog opens', async () => {
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    await waitFor(() => expect(api.getGitHubConnectStatus).toHaveBeenCalledWith('talkback-001'));

    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));
    // Dialog renders from the prefetched status without a second fetch.
    expect(screen.getByLabelText(/repository name/i)).toHaveValue('talkback');
    expect(api.getGitHubConnectStatus).toHaveBeenCalledTimes(1);
  });

  it('shows a loading message while the status fetch is in flight', async () => {
    let resolve!: (s: GitHubConnectStatus) => void;
    vi.mocked(api.getGitHubConnectStatus).mockImplementation(
      () => new Promise<GitHubConnectStatus>((r) => (resolve = r))
    );
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));

    expect(screen.getByText(/checking github/i)).toBeInTheDocument();
    resolve(fullPlanStatus);
    expect(await screen.findByLabelText(/repository name/i)).toHaveValue('talkback');
  });

  it('explains when gh is unavailable and creation is needed', async () => {
    vi.mocked(api.getGitHubConnectStatus).mockResolvedValue({
      ...fullPlanStatus,
      gh: { available: false, username: '' },
      owners: undefined,
    });
    render(<GitHubConnectBanner workspaceId="talkback-001" />);
    await userEvent.click(screen.getByRole('button', { name: /connect to github/i }));
    await waitFor(() => expect(api.getGitHubConnectStatus).toHaveBeenCalled());

    expect(screen.getByText(/gh CLI/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^connect$/i })).toBeDisabled();
  });
});
