import { afterEach, describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import ConfigPage from '../ConfigPage';
import type { ConfigResponse, ConfigUpdateRequest, Repo, RepoAddRequest } from '../../lib/types';
import { systemCapabilities } from '../../lib/test-factories';

// --- Mocks ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockUpdateConfig =
  vi.fn<
    (req: ConfigUpdateRequest) => Promise<{ status: string; warning?: string; warnings?: string[] }>
  >();
const mockAddRepo = vi.fn<(req: RepoAddRequest) => Promise<Repo>>();
const mockRemoveRepo = vi.fn<(name: string) => Promise<void>>();
const mockGetAuthSecretsStatus = vi.fn();
const mockGetOverlays = vi.fn();
const mockGetBuiltinQuickLaunch = vi.fn();

vi.mock('../../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  updateConfig: (...args: unknown[]) => mockUpdateConfig(...(args as [ConfigUpdateRequest])),
  addRepo: (...args: unknown[]) => mockAddRepo(...(args as [RepoAddRequest])),
  removeRepo: (...args: unknown[]) => mockRemoveRepo(...(args as [string])),
  getAuthSecretsStatus: (...args: unknown[]) => mockGetAuthSecretsStatus(...args),
  getOverlays: (...args: unknown[]) => mockGetOverlays(...args),
  getBuiltinQuickLaunch: (...args: unknown[]) => mockGetBuiltinQuickLaunch(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  configureModelSecrets: vi.fn(),
  removeModelSecrets: vi.fn(),
  saveAuthSecrets: vi.fn(),
  setRemoteAccessPassword: vi.fn(),
  testRemoteAccessNotification: vi.fn(),
  getPersonas: vi.fn().mockResolvedValue({ personas: [] }),
  getStyles: vi.fn().mockResolvedValue({ styles: [] }),
  validateTLS: vi.fn(),
  getChatStorageStatus: vi.fn().mockResolvedValue({
    retention_days: 30,
    max_total_storage_mb: 1024,
    used_bytes: 0,
    active_bytes: 0,
    ended_bytes: 0,
    session_directories: 0,
    active_directories: 0,
  }),
}));

const mockSuccess = vi.fn();
const mockToastError = vi.fn();
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: mockSuccess, error: mockToastError }),
}));

const mockShow = vi.fn().mockResolvedValue(true);
const mockConfirm = vi.fn().mockResolvedValue(true);
const mockPrompt = vi.fn().mockResolvedValue(null);
const mockAlert = vi.fn().mockResolvedValue(true);
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ show: mockShow, alert: mockAlert, confirm: mockConfirm, prompt: mockPrompt }),
}));

vi.mock('../../contexts/SessionsContext', () => ({
  useSessions: () => ({ workspaces: [] }),
}));

const mockConfigCtx = {
  reloadConfig: vi.fn().mockResolvedValue(undefined),
};
vi.mock('../../contexts/ConfigContext', () => ({
  useConfig: () => mockConfigCtx,
}));

const mockFeatures: Record<string, boolean> = {
  tunnel: true,
  github: true,
  telemetry: true,
  update: true,
  dashboardsx: true,
  model_registry: true,
  repofeed: true,
  subreddit: true,
  build_monitor: false,
};
vi.mock('../../contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: mockFeatures, loading: false }),
}));

// Minimal full config response fixture
const configFixture: ConfigResponse = {
  workspace_path: '/home/user/ws',
  source_code_management: 'git-worktree',
  repos: [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }],
  run_targets: [
    { name: 'my-agent', command: 'my-agent --prompt' },
    { name: 'build', command: 'make build' },
  ],
  runners: {},
  models: [],
  quick_launch: [{ name: 'ql1', target: 'claude', prompt: 'hello' }],
  nudgenik: { targets: [], viewed_buffer_ms: 5000, seen_interval_ms: 2000 },
  branch_suggest: { targets: [] },
  conflict_resolve: { target: '', timeout_ms: 120000 },
  flight_name: { target: '' },
  flight_pilot: { target: '' },
  sessions: {
    dashboard_poll_interval_ms: 5000,
    git_status_poll_interval_ms: 10000,
    git_clone_timeout_ms: 300000,
    git_status_timeout_ms: 30000,
  },
  xterm: {
    query_timeout_ms: 5000,
    operation_timeout_ms: 10000,
    use_webgl: true,
  },
  network: {
    bind_address: '127.0.0.1',
    port: 7337,
    public_base_url: '',
    tls: { cert_path: '', key_path: '' },
  },
  access_control: { enabled: false, provider: 'github', session_ttl_minutes: 1440 },
  pr_review: { target: '' },
  commit_message: { target: '' },
  desync: { enabled: false, target: '' },
  io_workspace_telemetry: { enabled: false, target: '' },
  client_performance: { enabled: false, repo: '', target: '' },
  fence_analyze: { enabled: false, target: '' },
  notifications: {
    sound_disabled: false,
    confirm_before_close: false,
    suggest_dispose_after_push: true,
  },
  lore: {
    enabled: true,
    llm_target: '',
    curate_on_dispose: 'session',
    auto_pr: false,
    public_rule_mode: 'direct_push',
  },
  subreddit: {
    enabled: false,
    target: '',
    interval: 30,
    checking_range: 48,
    max_posts: 30,
    max_age: 14,
    repos: {},
  },
  repofeed: {
    enabled: false,
    publish_interval_seconds: 30,
    fetch_interval_seconds: 60,
    completed_retention_hours: 48,
    repos: {},
  },
  floor_manager: { enabled: false, target: '', rotation_threshold: 150, debounce_ms: 2000 },
  timelapse: { enabled: true, retention_days: 7, max_file_size_mb: 50, max_total_storage_mb: 500 },
  remote_access: {
    enabled: false,
    timeout_minutes: 0,
    password_hash_set: false,
    notify: { ntfy_topic: '', command: '' },
  },
  clipboard_sync_enabled: true,
  chat_storage: { retention_days: 7, max_total_storage_mb: 4096 },
  system_capabilities: systemCapabilities(),
  needs_restart: false,
  oneshot_targets: [],
  anthropic_oauth_token_set: false,
  typesafe_api_key_set: false,
  ollama: { endpoint: '', reachable: false, models: [] },
  build_monitor: { enabled: false, repos: {} },
};

function renderConfigPage() {
  return render(
    <MemoryRouter initialEntries={['/config?tab=workspaces']}>
      <ConfigPage />
    </MemoryRouter>
  );
}

describe('ConfigPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetConfig.mockResolvedValue(configFixture);
    mockUpdateConfig.mockResolvedValue({ status: 'ok' });
    mockGetAuthSecretsStatus.mockResolvedValue({ client_id_set: false, client_secret_set: false });
    mockGetOverlays.mockResolvedValue({ overlays: [] });
    mockGetBuiltinQuickLaunch.mockResolvedValue([]);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it.each([
    { stored: false, button: 'Set TypeSafe Key', key: 'new-key' },
    { stored: true, button: 'Update TypeSafe Key', key: 'updated-key' },
    { stored: true, button: 'Remove TypeSafe Key', key: '' },
  ])('saves $button through the standard config API', async ({ stored, button, key }) => {
    vi.useRealTimers();
    mockGetConfig
      .mockResolvedValueOnce({ ...configFixture, typesafe_api_key_set: stored })
      .mockResolvedValue({ ...configFixture, typesafe_api_key_set: key !== '' });
    if (key) mockPrompt.mockResolvedValueOnce(` ${key} `);
    render(
      <MemoryRouter initialEntries={['/config?tab=flights']}>
        <ConfigPage />
      </MemoryRouter>
    );
    await userEvent.click(await screen.findByRole('button', { name: button }));
    await waitFor(() =>
      expect(mockSuccess).toHaveBeenCalledWith(
        key ? 'TypeSafe API key saved' : 'TypeSafe API key removed'
      )
    );
    expect(mockUpdateConfig).toHaveBeenCalledWith({ typesafe_api_key: key });
    expect(mockConfigCtx.reloadConfig).toHaveBeenCalled();
    if (key) {
      expect(mockPrompt).toHaveBeenCalledWith(
        expect.stringContaining('TypeSafe API Key'),
        expect.objectContaining({ password: true })
      );
    } else {
      expect(mockConfirm).toHaveBeenCalledWith(
        'Remove TypeSafe API Key?',
        expect.objectContaining({ danger: true })
      );
    }
    expect(screen.queryByDisplayValue(key || 'new-key')).not.toBeInTheDocument();
  });

  it('preserves the TypeSafe key when removal is cancelled', async () => {
    vi.useRealTimers();
    mockGetConfig.mockResolvedValue({ ...configFixture, typesafe_api_key_set: true });
    mockConfirm.mockResolvedValueOnce(false);
    render(
      <MemoryRouter initialEntries={['/config?tab=flights']}>
        <ConfigPage />
      </MemoryRouter>
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Remove TypeSafe Key' }));
    expect(mockConfirm).toHaveBeenCalled();
    expect(mockUpdateConfig).not.toHaveBeenCalledWith(
      expect.objectContaining({ typesafe_api_key: expect.anything() })
    );
  });

  it('loads config and renders the Workspaces tab', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByText('Workspaces')).toBeInTheDocument();
    });
    expect(mockGetConfig).toHaveBeenCalled();
    expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    // Make getConfig hang
    mockGetConfig.mockReturnValue(new Promise(() => {}));
    renderConfigPage();
    expect(screen.getByText('Loading configuration...')).toBeInTheDocument();
  });

  it('shows error state when config load fails', async () => {
    mockGetConfig.mockRejectedValue(new Error('Network error'));
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByText('Failed to load config')).toBeInTheDocument();
    });
  });

  it('has no Save button (auto-save)', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // No Save Changes button
    expect(screen.queryByText('Save Changes')).not.toBeInTheDocument();

    // Error indicator only shown on failure, not visible by default
    expect(screen.queryByTestId('config-save-status')).not.toBeInTheDocument();
  });

  it('auto-saves after a change with debounce', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // Make a change — modify workspace path via the edit prompt
    mockPrompt.mockResolvedValueOnce('/home/user/new-ws');
    await userEvent.click(screen.getByText('Edit'));

    // Wait for the state update
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/new-ws')).toBeInTheDocument();
    });

    // Also need getConfig for reload after save
    mockGetConfig.mockResolvedValue({ ...configFixture, workspace_path: '/home/user/new-ws' });

    // Advance past the 300ms debounce
    vi.advanceTimersByTime(350);

    await waitFor(() => {
      expect(mockUpdateConfig).toHaveBeenCalledTimes(1);
    });

    const payload = mockUpdateConfig.mock.calls[0][0];
    expect(payload.workspace_path).toBe('/home/user/new-ws');
  });

  it('switches tabs via tab buttons', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // All 7 tabs should be present
    for (const slug of [
      'workspaces',
      'sessions',
      'agents',
      'flights',
      'access',
      'experimental',
      'advanced',
    ]) {
      expect(screen.getByTestId(`config-tab-${slug}`)).toBeInTheDocument();
    }
    expect(screen.queryByTestId('config-tab-remote')).not.toBeInTheDocument();

    // Click on the Sessions tab
    await userEvent.click(screen.getByTestId('config-tab-sessions'));
    await waitFor(() => {
      expect(screen.getByText('Command Targets')).toBeInTheDocument();
    });
  });

  it.each([
    ['remote', 'workspaces'],
    ['experimental', 'experimental'],
    ['advanced', 'advanced'],
  ])('?tab=%s selects the %s tab', async (slug, selected) => {
    render(
      <MemoryRouter initialEntries={[`/config?tab=${slug}`]}>
        <ConfigPage />
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(screen.getByTestId(`config-tab-${selected}`)).toHaveAttribute('aria-selected', 'true');
    });
  });

  it('renders the Experimental and Advanced panels under their own tabs', async () => {
    render(
      <MemoryRouter initialEntries={['/config?tab=experimental']}>
        <ConfigPage />
      </MemoryRouter>
    );
    expect(await screen.findByText('Experimental Features')).toBeInTheDocument();
    await userEvent.click(screen.getByTestId('config-tab-advanced'));
    expect(await screen.findByText('Feature Settings')).toBeInTheDocument();
  });

  it('redirects quicklaunch slug to Sessions tab', async () => {
    render(
      <MemoryRouter initialEntries={['/config?tab=quicklaunch']}>
        <ConfigPage />
      </MemoryRouter>
    );
    // Wait for config to load, then check that the Sessions tab is active
    await waitFor(() => {
      expect(screen.getByTestId('config-tab-sessions')).toHaveAttribute('aria-selected', 'true');
    });
  });

  it('opens the Access tab when ?tab=access is requested', async () => {
    render(
      <MemoryRouter initialEntries={['/config?tab=access']}>
        <ConfigPage />
      </MemoryRouter>
    );
    // Wait for config to load, then check that the Access tab is active
    await waitFor(() => {
      expect(screen.getByTestId('config-tab-access')).toHaveAttribute('aria-selected', 'true');
    });
  });

  it('shows error toast when auto-save fails', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // Make a change
    mockPrompt.mockResolvedValueOnce('/home/user/fail');
    await userEvent.click(screen.getByText('Edit'));
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/fail')).toBeInTheDocument();
    });

    mockUpdateConfig.mockRejectedValueOnce(new Error('Server error'));

    // Advance past debounce
    vi.advanceTimersByTime(350);

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith('Failed to save config');
    });
  });

  describe('repos', () => {
    beforeEach(() => {
      mockAddRepo.mockResolvedValue({
        name: 'my-repo',
        url: 'git@github.com:user/repo.git',
        bare_path: 'my-repo.git',
      } as Repo);
      mockRemoveRepo.mockResolvedValue(undefined);
    });

    it('registers a repo without sending a name for a git repo', async () => {
      vi.useRealTimers();
      renderConfigPage();

      await userEvent.type(await screen.findByPlaceholderText('Name'), 'my-repo');
      await userEvent.type(
        await screen.findByPlaceholderText('git@github.com:user/repo.git'),
        'git@github.com:user/repo.git'
      );
      await userEvent.click(await screen.findByTestId('add-repo'));

      await waitFor(() => {
        expect(mockAddRepo).toHaveBeenCalledWith({
          url: 'git@github.com:user/repo.git',
        });
      });
    });

    it('sends the name for a sapling repo', async () => {
      vi.useRealTimers();
      renderConfigPage();

      await userEvent.type(await screen.findByPlaceholderText('Name'), 'my-repo');
      await userEvent.selectOptions(await screen.findByRole('combobox'), 'sapling');
      await userEvent.type(await screen.findByPlaceholderText('Repo Identifier'), 'sl:my-repo');
      await userEvent.click(await screen.findByTestId('add-repo'));

      await waitFor(() => {
        expect(mockAddRepo).toHaveBeenCalledWith({
          url: 'sl:my-repo',
          vcs: 'sapling',
          name: 'my-repo',
        });
      });
    });

    it('sends nothing when the name box is empty', async () => {
      vi.useRealTimers();
      renderConfigPage();

      await userEvent.type(
        await screen.findByPlaceholderText('git@github.com:user/repo.git'),
        'git@github.com:user/repo.git'
      );
      await userEvent.click(await screen.findByTestId('add-repo'));

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalledWith('Repo name is required');
      });
      expect(mockAddRepo).not.toHaveBeenCalled();
    });

    it('reports a failed add and leaves the form alone', async () => {
      vi.useRealTimers();
      mockAddRepo.mockRejectedValueOnce(new Error('boom'));
      renderConfigPage();

      await userEvent.type(await screen.findByPlaceholderText('Name'), 'my-repo');
      await userEvent.type(
        await screen.findByPlaceholderText('git@github.com:user/repo.git'),
        'git@github.com:user/repo.git'
      );
      await userEvent.click(await screen.findByTestId('add-repo'));

      await waitFor(() => {
        expect(mockAlert).toHaveBeenCalledWith('Add Repo Failed', 'Failed to add repo');
      });
      expect(screen.getByDisplayValue('my-repo')).toBeInTheDocument();
    });

    it('reports a failed refresh, not a failed add, after the add succeeds', async () => {
      vi.useRealTimers();
      renderConfigPage();

      await userEvent.type(await screen.findByPlaceholderText('Name'), 'my-repo');
      await userEvent.type(
        await screen.findByPlaceholderText('git@github.com:user/repo.git'),
        'git@github.com:user/repo.git'
      );
      // The page has loaded; the next config read is the refresh after the add.
      mockGetConfig.mockRejectedValueOnce(new Error('offline'));
      await userEvent.click(await screen.findByTestId('add-repo'));

      await waitFor(() => {
        expect(mockAlert).toHaveBeenCalledWith('Reload Repos Failed', 'Failed to reload repos');
      });
      expect(mockAddRepo).toHaveBeenCalledTimes(1);
      expect(mockAlert).not.toHaveBeenCalledWith('Add Repo Failed', 'Failed to add repo');
    });

    it('reports a failed refresh, not a failed remove, after the remove succeeds', async () => {
      vi.useRealTimers();
      mockConfirm.mockResolvedValueOnce(true);
      renderConfigPage();

      const removeButton = await screen.findByRole('button', { name: /remove/i });
      mockGetConfig.mockRejectedValueOnce(new Error('offline'));
      await userEvent.click(removeButton);

      await waitFor(() => {
        expect(mockAlert).toHaveBeenCalledWith('Reload Repos Failed', 'Failed to reload repos');
      });
      expect(mockRemoveRepo).toHaveBeenCalledTimes(1);
      expect(mockAlert).not.toHaveBeenCalledWith('Remove Repo Failed', 'Failed to remove repo');
    });

    it('sends no remove request when the confirmation is declined', async () => {
      vi.useRealTimers();
      mockConfirm.mockResolvedValueOnce(false);
      renderConfigPage();

      await userEvent.click(await screen.findByRole('button', { name: /remove/i }));
      await waitFor(() => expect(mockConfirm).toHaveBeenCalled());
      // The handler awaited this very promise before this line did, so its
      // continuation (the decline) has run by the time this await returns.
      await mockConfirm.mock.results[0].value;

      expect(mockRemoveRepo).not.toHaveBeenCalled();
    });

    it('removes a repo after the confirmation', async () => {
      vi.useRealTimers();
      mockConfirm.mockResolvedValueOnce(true);
      renderConfigPage();

      await userEvent.click(await screen.findByRole('button', { name: /remove/i }));
      await waitFor(() => {
        expect(mockRemoveRepo).toHaveBeenCalledWith('my-repo');
      });
    });

    it('reports a failed remove and keeps the repo listed', async () => {
      vi.useRealTimers();
      mockRemoveRepo.mockRejectedValueOnce(new Error('boom'));
      renderConfigPage();

      await userEvent.click(await screen.findByRole('button', { name: /remove/i }));
      await waitFor(() => {
        expect(mockAlert).toHaveBeenCalledWith('Remove Repo Failed', 'Failed to remove repo');
      });
      expect(screen.getByText('my-repo')).toBeInTheDocument();
    });
  });
});
