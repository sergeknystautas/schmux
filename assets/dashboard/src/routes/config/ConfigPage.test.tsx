import { afterEach, describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ConfigPage from '../ConfigPage';
import type { ConfigResponse, ConfigUpdateRequest } from '../../lib/types';

// --- Mocks ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockUpdateConfig =
  vi.fn<
    (req: ConfigUpdateRequest) => Promise<{ status: string; warning?: string; warnings?: string[] }>
  >();
const mockGetAuthSecretsStatus = vi.fn();
const mockGetOverlays = vi.fn();
const mockGetBuiltinQuickLaunch = vi.fn();

vi.mock('../../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  updateConfig: (...args: unknown[]) => mockUpdateConfig(...(args as [ConfigUpdateRequest])),
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

const mockConfigCtx = {
  reloadConfig: vi.fn().mockResolvedValue(undefined),
};
vi.mock('../../contexts/ConfigContext', () => ({
  useConfig: () => mockConfigCtx,
}));

vi.mock('../../contexts/FeaturesContext', () => ({
  useFeatures: () => ({
    features: {
      tunnel: true,
      github: true,
      telemetry: true,
      update: true,
      dashboardsx: true,
      model_registry: true,
      repofeed: true,
      subreddit: true,
    },
    loading: false,
  }),
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
  nudgenik: { target: '', viewed_buffer_ms: 5000, seen_interval_ms: 2000 },
  branch_suggest: { target: '' },
  conflict_resolve: { target: '', timeout_ms: 120000 },
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
  system_capabilities: { iterm2_available: false },
  needs_restart: false,
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
      'access',
      'remote',
      'experimental',
      'advanced',
    ]) {
      expect(screen.getByTestId(`config-tab-${slug}`)).toBeInTheDocument();
    }

    // Click on the Sessions tab
    await userEvent.click(screen.getByTestId('config-tab-sessions'));
    await waitFor(() => {
      expect(screen.getByText('Command Targets')).toBeInTheDocument();
    });
  });

  it('falls back to Workspaces tab for dissolved slugs', async () => {
    render(
      <MemoryRouter initialEntries={['/config?tab=quicklaunch']}>
        <ConfigPage />
      </MemoryRouter>
    );
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });
    // The Workspaces tab should be active (fallback from dissolved slug)
    expect(screen.getByTestId('config-tab-workspaces')).toHaveAttribute('aria-selected', 'true');
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
});
