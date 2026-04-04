import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse } from '../lib/types';

// --- Mocks ---

const configFixture: ConfigResponse = {
  workspace_path: '/home/user/ws',
  source_code_management: 'git-worktree',
  repos: [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }],
  run_targets: [{ name: 'build', command: 'make build' }],
  runners: {
    claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
    codex: { available: true, capabilities: ['interactive', 'oneshot'] },
    opencode: { available: true, capabilities: ['interactive', 'oneshot'] },
  },
  models: [
    {
      id: 'claude',
      display_name: 'Claude Code',
      provider: 'anthropic',
      configured: true,
      runners: ['claude'],
    },
    {
      id: 'codex',
      display_name: 'Codex CLI',
      provider: 'openai',
      configured: true,
      runners: ['codex', 'opencode'],
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

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  spawnSessions: vi.fn(),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  suggestBranch: vi.fn(),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn().mockResolvedValue(true), prompt: vi.fn() }),
}));

vi.mock('../contexts/ConfigContext', () => ({
  useRequireConfig: () => {},
  useConfig: () => ({
    config: configFixture,
    loading: false,
    error: null,
    isNotConfigured: false,
    isFirstRun: false,
    completeFirstRun: vi.fn(),
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

// Stub child components that are complex and irrelevant to agent selection
vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="prompt-textarea"
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

// Now import the component under test (after mocks are set up)
import SpawnPage from './SpawnPage';

function renderSpawnPage() {
  return render(
    <MemoryRouter initialEntries={['/spawn']}>
      <SpawnPage />
    </MemoryRouter>
  );
}

describe('SpawnPage unified agent dropdown', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    mockGetConfig.mockResolvedValue(configFixture);
    mockGetPersonas.mockResolvedValue({ personas: [] });
  });

  it('renders the unified agent dropdown with agents and special options', async () => {
    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    const select = screen.getByTestId('agent-select') as HTMLSelectElement;
    const options = within(select).getAllByRole('option');

    // Should have: "Select agent..." + 2 agents + separator + "Multiple agents" + "Advanced"
    const optionTexts = options.map((o) => o.textContent);
    expect(optionTexts).toContain('Claude Code');
    expect(optionTexts).toContain('Codex CLI');
    expect(optionTexts).toContain('Multiple agents');
    expect(optionTexts).toContain('Advanced');
  });

  it('does NOT render the old mode selector dropdown with "Single Agent" text', async () => {
    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    // The old mode selector had options "Single Agent", "Multiple Agents", "Advanced"
    // Ensure the old-style "Single Agent" option text is NOT present
    expect(screen.queryByRole('option', { name: 'Single Agent' })).not.toBeInTheDocument();
    expect(screen.queryByRole('option', { name: 'Multiple Agents' })).not.toBeInTheDocument();
  });

  it('selecting "Multiple agents" shows the toggle grid and "Single agent" button', async () => {
    const user = userEvent.setup();
    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    const select = screen.getByTestId('agent-select') as HTMLSelectElement;
    await user.selectOptions(select, '__multiple__');

    await waitFor(() => {
      // Toggle grid should appear with agent buttons
      expect(screen.getByTestId('agent-claude')).toBeInTheDocument();
      expect(screen.getByTestId('agent-codex')).toBeInTheDocument();
      // "Single agent" button should appear
      expect(screen.getByRole('button', { name: 'Single agent' })).toBeInTheDocument();
      // The unified dropdown should NOT be rendered anymore
      expect(screen.queryByTestId('agent-select')).not.toBeInTheDocument();
    });
  });

  it('selecting "Advanced" shows the counter grid and "Single agent" button', async () => {
    const user = userEvent.setup();
    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    const select = screen.getByTestId('agent-select') as HTMLSelectElement;
    await user.selectOptions(select, '__advanced__');

    await waitFor(() => {
      // Counter grid should appear
      expect(screen.getByTestId('agent-claude')).toBeInTheDocument();
      expect(screen.getByTestId('agent-codex')).toBeInTheDocument();
      // "Single agent" button should appear
      expect(screen.getByRole('button', { name: 'Single agent' })).toBeInTheDocument();
      // The unified dropdown should NOT be rendered
      expect(screen.queryByTestId('agent-select')).not.toBeInTheDocument();
    });
  });

  it('clicking "Single agent" button returns to the unified dropdown', async () => {
    const user = userEvent.setup();
    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    // Switch to multiple mode
    const select = screen.getByTestId('agent-select') as HTMLSelectElement;
    await user.selectOptions(select, '__multiple__');

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Single agent' })).toBeInTheDocument();
    });

    // Click "Single agent" button to go back
    await user.click(screen.getByRole('button', { name: 'Single agent' }));

    await waitFor(() => {
      // The unified dropdown should be back
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
      // The "Single agent" button should be gone
      expect(screen.queryByRole('button', { name: 'Single agent' })).not.toBeInTheDocument();
    });
  });

  it('shows persona dropdown in same row as agent and repo when personas exist', async () => {
    // Mock personas to be returned
    mockGetPersonas.mockResolvedValue({
      personas: [
        {
          id: 'builder',
          name: 'Builder',
          icon: '🏗️',
          color: '#3498db',
          prompt: 'Build things',
          expectations: '',
          built_in: true,
        },
      ],
    });

    renderSpawnPage();

    await waitFor(() => {
      expect(screen.getByTestId('agent-select')).toBeInTheDocument();
    });

    // Persona should be visible when personas exist
    expect(screen.getByTestId('persona-select')).toBeInTheDocument();

    // All three (agent, persona, repo) should be in the agent-repo-row container
    const row = screen.getByTestId('agent-repo-row');
    expect(within(row).getByTestId('agent-select')).toBeInTheDocument();
    expect(within(row).getByTestId('persona-select')).toBeInTheDocument();
    expect(within(row).getByTestId('spawn-repo-select')).toBeInTheDocument();
  });
});
