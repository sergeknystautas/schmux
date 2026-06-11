import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import userEvent from '@testing-library/user-event';
import BuildMonitorConfig from './BuildMonitorConfig';
import type { ConfigPanelProps } from './ConfigPanelProps';

const baseState = {
  authEnabled: true,
  authClientIdSet: true,
  authClientSecretSet: true,
  repos: [
    { name: 'My Repo', url: 'https://github.com/owner/repo' },
    { name: 'Other', url: 'https://gitlab.com/x/y' },
  ],
  buildMonitorRepos: {},
  buildMonitorInterval: 5,
} as any;

const baseProps: ConfigPanelProps = {
  state: baseState,
  dispatch: vi.fn(),
  models: [],
};

beforeEach(() => {
  vi.restoreAllMocks();
});

function mockIdentities(logins: string[]) {
  vi.spyOn(globalThis, 'fetch').mockImplementation((url: string | URL | Request) => {
    if (url.toString().includes('/api/build-monitor/identities')) {
      return Promise.resolve(Response.json({ logins }));
    }
    return Promise.reject(new Error('unknown'));
  });
}

function renderPanel(props: Partial<ConfigPanelProps> = {}) {
  return render(
    <MemoryRouter>
      <BuildMonitorConfig {...baseProps} {...props} />
    </MemoryRouter>
  );
}

describe('BuildMonitorConfig', () => {
  it('disables Authorize and explains the Access tab prerequisite when sign-in is not configured', async () => {
    mockIdentities([]);
    renderPanel({ state: { ...baseState, authEnabled: false } });
    const authorize = await screen.findByRole('button', { name: /authorize github/i });
    expect(authorize).toBeDisabled();
    expect(screen.getByText(/requires github sign-in.*access tab/i)).toBeInTheDocument();
  });

  it('enables Authorize and greys out the repos section when there are no identities yet', async () => {
    mockIdentities([]);
    renderPanel();
    const authorize = await screen.findByRole('button', { name: /authorize github/i });
    expect(authorize).toBeEnabled();
    const reposSection = screen.getByTestId('build-monitor-section-repos');
    expect(reposSection).toHaveStyle({ pointerEvents: 'none' });
  });

  it('shows authorized identities and an option to add another', async () => {
    mockIdentities(['octocat']);
    renderPanel();
    expect(await screen.findByText('Authorized')).toBeInTheDocument();
    expect(screen.getByText(/octocat/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /authorize another identity/i })).toBeEnabled();
    const reposSection = screen.getByTestId('build-monitor-section-repos');
    expect(reposSection).toHaveStyle({ pointerEvents: 'auto' });
  });

  it('lists GitHub repos with an enable checkbox and hides non-GitHub repos', async () => {
    mockIdentities(['octocat']);
    renderPanel();
    expect(await screen.findByTestId('build-monitor-enable-my-repo')).toBeInTheDocument();
    expect(screen.queryByTestId('build-monitor-enable-other')).not.toBeInTheDocument();
  });

  it('binds the single identity automatically when a repo is enabled', async () => {
    mockIdentities(['octocat']);
    const dispatch = vi.fn();
    const user = userEvent.setup();
    renderPanel({ dispatch });
    await user.click(await screen.findByTestId('build-monitor-enable-my-repo'));
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'buildMonitorRepos',
      value: { 'My Repo': { enabled: true, github_login: 'octocat' } },
    });
    // No identity selector and no warning with a single identity
    expect(screen.queryByLabelText('GitHub identity')).not.toBeInTheDocument();
    expect(screen.queryByText(/select an identity/i)).not.toBeInTheDocument();
  });

  it('heals an enabled repo saved without an identity when only one identity exists', async () => {
    mockIdentities(['octocat']);
    const dispatch = vi.fn();
    renderPanel({
      dispatch,
      state: {
        ...baseState,
        buildMonitorRepos: { 'My Repo': { enabled: true, github_login: '' } },
      },
    });
    await screen.findByTestId('build-monitor-enable-my-repo');
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'buildMonitorRepos',
      value: { 'My Repo': { enabled: true, github_login: 'octocat' } },
    });
  });

  it('shows an identity selector with a warning when several identities exist and none is chosen', async () => {
    mockIdentities(['octocat', 'hubot']);
    renderPanel({
      state: {
        ...baseState,
        buildMonitorRepos: { 'My Repo': { enabled: true, github_login: '' } },
      },
    });
    expect(await screen.findByLabelText('GitHub identity')).toBeInTheDocument();
    expect(screen.getByTestId('build-monitor-identity-my-repo')).toBeInTheDocument();
    expect(screen.getByText(/select an identity to start monitoring/i)).toBeInTheDocument();
  });

  it('clears the warning once an identity is chosen', async () => {
    mockIdentities(['octocat', 'hubot']);
    renderPanel({
      state: {
        ...baseState,
        buildMonitorRepos: { 'My Repo': { enabled: true, github_login: 'hubot' } },
      },
    });
    const select = await screen.findByLabelText('GitHub identity');
    expect(select).toHaveValue('hubot');
    expect(screen.queryByText(/select an identity to start monitoring/i)).not.toBeInTheDocument();
  });

  it('links to the Build Monitor page once a repo is enabled', async () => {
    mockIdentities(['octocat']);
    renderPanel({
      state: {
        ...baseState,
        buildMonitorRepos: { 'My Repo': { enabled: true, github_login: 'octocat' } },
      },
    });
    const link = await screen.findByRole('link', { name: /build monitor/i });
    expect(link).toHaveAttribute('href', '/build-monitor');
  });

  it('shows no GitHub repos message when all repos are non-GitHub', async () => {
    mockIdentities(['octocat']);
    renderPanel({
      state: { ...baseState, repos: [{ name: 'Other', url: 'https://gitlab.com/x/y' }] },
    });
    expect(await screen.findByText(/No GitHub repositories/i)).toBeInTheDocument();
  });

  it('renders the check interval input', async () => {
    mockIdentities(['octocat']);
    const dispatch = vi.fn();
    renderPanel({ dispatch });
    const input = await screen.findByLabelText(/check interval/i);
    expect(input).toHaveValue(5);
    // Type into the input — the controlled value won't actually update since
    // dispatch is a mock, but verify that changes dispatch SET_FIELD actions.
    const user = userEvent.setup();
    await user.type(input, '0');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'SET_FIELD',
        field: 'buildMonitorInterval',
      })
    );
  });
});
