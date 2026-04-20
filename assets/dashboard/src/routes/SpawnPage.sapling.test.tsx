import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse, SpawnRequest, SpawnResult } from '../lib/types';

// --- Fixtures ---

function makeConfig(overrides: Partial<ConfigResponse> = {}): ConfigResponse {
  return {
    workspace_path: '/home/user/ws',
    source_code_management: 'git-worktree',
    repos: [
      { name: 'saplingrepo', url: 'sl:saplingrepo', vcs: 'sapling' },
      { name: 'gitrepo', url: 'https://github.com/user/gitrepo.git', vcs: 'git' },
    ],
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
    system_capabilities: { iterm2_available: false },
    needs_restart: false,
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

// Stub child components that are complex and irrelevant to the sapling flow.
vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: {
    value: string;
    onChange: (v: string) => void;
    onSelectCommand?: (cmd: string) => void;
  }) => (
    <div>
      <textarea
        data-testid="spawn-prompt"
        value={props.value}
        onChange={(e) => props.onChange(e.target.value)}
      />
      <button
        data-testid="trigger-resume"
        type="button"
        onClick={() => props.onSelectCommand?.('/resume')}
      >
        Trigger /resume
      </button>
    </div>
  ),
}));
vi.mock('../components/Tooltip', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../components/RemoteHostSelector', () => ({
  default: () => <div data-testid="remote-host-selector" />,
}));

// Now import the component under test (after mocks are set up)
import SpawnPage from './SpawnPage';

function renderSpawnPage() {
  return render(
    <MemoryRouter initialEntries={['/spawn']}>
      <SpawnPage />
    </MemoryRouter>
  );
}

async function selectSaplingRepo() {
  const repoSelect = (await screen.findByTestId('spawn-repo-select')) as HTMLSelectElement;
  fireEvent.change(repoSelect, { target: { value: 'sl:saplingrepo' } });
}

async function selectClaudeAgent() {
  const agentSelect = (await screen.findByTestId('agent-select')) as HTMLSelectElement;
  fireEvent.change(agentSelect, { target: { value: 'claude' } });
}

describe('SpawnPage sapling flow', () => {
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
    mockSuggestBranch.mockResolvedValue({ branch: 'auto/from-llm' });
  });

  it('hides the branch input when a sapling repo is selected', async () => {
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();

    expect(screen.queryByPlaceholderText(/feature\/my-branch/i)).not.toBeInTheDocument();
  });

  it('shows the branch input when a git repo is selected', async () => {
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    const repoSelect = screen.getByTestId('spawn-repo-select') as HTMLSelectElement;
    fireEvent.change(repoSelect, {
      target: { value: 'https://github.com/user/gitrepo.git' },
    });

    expect(screen.getByPlaceholderText(/feature\/my-branch/i)).toBeInTheDocument();
  });

  it('renders the label input with the prospective workspace ID as placeholder', async () => {
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();

    const labelInput = await screen.findByTestId('workspace-label-input');
    expect(labelInput).toHaveAttribute('placeholder', expect.stringMatching(/^saplingrepo-\d{3}$/));
  });

  it('does not error on submit when sapling repo is selected and label is empty', async () => {
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();
    await selectClaudeAgent();

    fireEvent.click(screen.getByTestId('spawn-submit'));

    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.branch).toBe('');
    expect(payload.workspace_label).toBe('');
  });

  it('passes workspace_label to spawnSessions when typed', async () => {
    const user = userEvent.setup();
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();
    await selectClaudeAgent();

    const labelInput = await screen.findByTestId('workspace-label-input');
    await user.type(labelInput, 'Login bug fix');

    fireEvent.click(screen.getByTestId('spawn-submit'));

    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.workspace_label).toBe('Login bug fix');
    expect(payload.branch).toBe('');
  });

  it('does not call the LLM branch suggester for sapling repos', async () => {
    // Reset config with a non-empty branch_suggest target so it would normally fire.
    const cfg = makeConfig({ branch_suggest: { target: 'opus' } });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    const user = userEvent.setup();
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();
    await selectClaudeAgent();

    await user.type(screen.getByTestId('spawn-prompt'), 'fix the login bug');
    fireEvent.click(screen.getByTestId('spawn-submit'));

    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    expect(mockSuggestBranch).not.toHaveBeenCalled();
  });

  it('/resume sends empty branch for sapling repos', async () => {
    renderSpawnPage();
    await waitFor(() => {
      expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument();
    });
    await selectSaplingRepo();
    await selectClaudeAgent();

    // Trigger /resume via the stubbed PromptTextarea button, which calls
    // onSelectCommand('/resume') and exercises handleSlashCommandSelect.
    fireEvent.click(screen.getByTestId('trigger-resume'));

    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.branch).toBe('');
    expect(payload.resume).toBe(true);
  });
});
