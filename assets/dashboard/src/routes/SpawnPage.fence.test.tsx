import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse, SpawnRequest, SpawnResult } from '../lib/types';

// --- Fixtures ---

function makeConfig(overrides: Partial<ConfigResponse> = {}): ConfigResponse {
  return {
    workspace_path: '/home/user/ws',
    source_code_management: 'git-worktree',
    repos: [{ name: 'gitrepo', url: 'https://github.com/user/gitrepo.git', vcs: 'git' }],
    run_targets: [],
    runners: {
      claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
    },
    models: [
      {
        id: 'claude',
        display_name: 'Claude Code',
        provider: 'anthropic',
        configured: true,
        runners: ['claude'],
      },
    ],
    quick_launch: [],
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
    lore: { enabled: true, llm_target: '', curate_on_dispose: 'session', auto_pr: false },
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
    timelapse: {
      enabled: true,
      retention_days: 7,
      max_file_size_mb: 50,
      max_total_storage_mb: 500,
    },
    remote_access: {
      enabled: false,
      timeout_minutes: 0,
      password_hash_set: false,
      notify: { ntfy_topic: '', command: '' },
    },
    personas_enabled: false,
    comm_styles_enabled: false,
    clipboard_sync_enabled: true,
    fence_mode: 'optional_off',
    system_capabilities: { iterm2_available: false, fence_available: false },
    needs_restart: false,
    oneshot_targets: [],
    anthropic_oauth_token_set: false,
    ollama: { endpoint: '', reachable: false, models: [] },
    build_monitor: { enabled: false, repos: {} },
    ...overrides,
  };
}

// --- Mocks ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
const mockGetStyles = vi.fn<() => Promise<{ styles: unknown[] }>>();

vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  suggestBranch: (...args: unknown[]) => mockSuggestBranch(...args),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  getStyles: (...args: unknown[]) => mockGetStyles(...(args as [])),
}));

vi.mock('../lib/spawn-api', () => ({
  getSpawnEntries: vi.fn().mockResolvedValue([]),
  getPromptHistory: vi.fn().mockResolvedValue([]),
}));

vi.mock('../lib/quicklaunch', () => ({
  getQuickLaunchItems: () => [],
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn().mockResolvedValue(true), prompt: vi.fn() }),
}));

let configContextValue: ConfigResponse | null = null;
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: configContextValue,
    loading: false,
    error: null,
    reloadConfig: vi.fn(),
    getRepoName: (url: string) => url,
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [],
    loading: false,
    error: '',
    connected: true,
    waitForSession: vi.fn().mockResolvedValue(true),
    sessionsById: {},
    ackSession: vi.fn(),
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
    curatorEvents: {},
  }),
}));

vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
  }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="spawn-prompt"
      value={props.value}
      onChange={(e) => props.onChange(e.target.value)}
    />
  ),
}));
vi.mock('../components/Tooltip', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../components/RemoteHostSelector', () => ({
  default: () => <div data-testid="remote-host-selector" />,
}));

import SpawnPage from './SpawnPage';

function renderSpawnPage() {
  return render(
    <MemoryRouter initialEntries={['/spawn']}>
      <SpawnPage />
    </MemoryRouter>
  );
}

describe('SpawnPage fence toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    const cfg = makeConfig();
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('hides the fence toggle when fence is unavailable', async () => {
    const cfg = makeConfig({
      system_capabilities: { iterm2_available: false, fence_available: false },
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeInTheDocument());

    expect(screen.queryByTestId('fence-toggle')).not.toBeInTheDocument();
  });

  it('shows the fence toggle when fence is available and target is local', async () => {
    const cfg = makeConfig({
      system_capabilities: { iterm2_available: false, fence_available: true },
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeInTheDocument());
  });

  it('hides the fence toggle when fence_mode is disabled even if available', async () => {
    const cfg = makeConfig({
      system_capabilities: { iterm2_available: false, fence_available: true },
      fence_mode: 'disabled',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeInTheDocument());
    expect(screen.queryByTestId('fence-toggle')).not.toBeInTheDocument();
  });

  it('defaults the fence toggle unchecked when fence_mode is optional_off', async () => {
    const cfg = makeConfig({
      system_capabilities: { iterm2_available: false, fence_available: true },
      fence_mode: 'optional_off',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeInTheDocument());
    expect(screen.getByTestId('fence-toggle')).not.toBeChecked();
  });

  it('defaults the fence toggle checked when fence_mode is optional_on', async () => {
    const cfg = makeConfig({
      system_capabilities: { iterm2_available: false, fence_available: true },
      fence_mode: 'optional_on',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeChecked());
  });
});
