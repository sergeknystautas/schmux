import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import type { ConfigResponse, SpawnRequest, SpawnResult, WorkspaceResponse } from '../lib/types';
import { makeConfig } from '../lib/test-factories';
import { resetSpawnInflightForTests } from '../lib/spawn-inflight';

// --- Mocks (same shape as SpawnPage.fence.test.tsx) ---

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
let workspacesContextValue: WorkspaceResponse[] = [];
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
    workspaces: workspacesContextValue,
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

import SpawnPage from './SpawnPage';

function chatRunners() {
  return {
    claude: { available: true, capabilities: ['interactive', 'oneshot', 'chat'] },
    gemini: { available: true, capabilities: ['interactive'] },
  };
}

function chatModels() {
  return [
    {
      id: 'claude',
      display_name: 'Claude Code',
      provider: 'anthropic',
      configured: true,
      runners: ['claude'],
    },
    {
      id: 'gemini',
      display_name: 'Gemini',
      provider: 'google',
      configured: true,
      runners: ['gemini'],
    },
  ];
}

function renderSpawnPage(entry = '/spawn') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <SpawnPage />
    </MemoryRouter>
  );
}

async function selectRepoAndBranch() {
  const repoSelect = (await screen.findByTestId('spawn-repo-select')) as HTMLSelectElement;
  fireEvent.change(repoSelect, { target: { value: 'https://github.com/user/gitrepo.git' } });
  const branchInput = await screen.findByPlaceholderText(/feature\//i);
  fireEvent.change(branchInput, { target: { value: 'feature/chat-test' } });
}

async function selectAgent(id: string) {
  const agentSelect = (await screen.findByTestId('agent-select')) as HTMLSelectElement;
  fireEvent.change(agentSelect, { target: { value: id } });
}

async function engage() {
  await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
  fireEvent.click(screen.getByTestId('spawn-submit'));
  await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
  return mockSpawnSessions.mock.calls[0][0];
}

describe('SpawnPage chat toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    const cfg = makeConfig();
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('hides the chat toggle when the chat_sessions flag is off', async () => {
    const cfg = makeConfig({ runners: chatRunners(), models: chatModels() });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');

    expect(screen.queryByTestId('chat-toggle')).not.toBeInTheDocument();
  });

  it('shows the toggle for a chat-capable target and sends kind chat when checked', async () => {
    const user = userEvent.setup();
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');

    const toggle = await screen.findByTestId('chat-toggle');
    await user.click(toggle);

    const payload = await engage();
    expect(payload.kind).toBe('chat');
  });

  it('omits kind when the toggle is not checked', async () => {
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await screen.findByTestId('chat-toggle');

    const payload = await engage();
    expect(payload.kind).toBeUndefined();
  });

  it('remembers the toggle within the tab (draft) and across tabs (last spawn)', async () => {
    const user = userEvent.setup();
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    // Toggle on, leave the page: the draft carries it back.
    const first = renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await user.click(await screen.findByTestId('chat-toggle'));
    first.unmount();

    // The draft restores repo and agent too, so only the branch is re-entered;
    // selecting the agent again would toggle it off.
    const second = renderSpawnPage();
    await selectRepoAndBranch();
    expect(await screen.findByTestId('chat-toggle')).toBeChecked();

    // Spawn clears the draft; the last spawned value carries it to a new tab.
    const payload = await engage();
    expect(payload.kind).toBe('chat');
    // onSuccess runs after the spawn promise resolves; wait for its write.
    await waitFor(() =>
      expect(localStorage.getItem('schmux:spawn-last-chat-enabled')).toBe('true')
    );
    sessionStorage.clear();
    mockSpawnSessions.mockClear();
    second.unmount();

    // A new tab: no draft, but the last spawn's agent and toggle come back.
    renderSpawnPage();
    await selectRepoAndBranch();
    expect(await screen.findByTestId('chat-toggle')).toBeChecked();
  });

  it('sends kind chat on /resume when the toggle is checked', async () => {
    const user = userEvent.setup();
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await user.click(await screen.findByTestId('chat-toggle'));

    fireEvent.click(screen.getByTestId('trigger-resume'));
    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.resume).toBe(true);
    expect(payload.kind).toBe('chat');
    await waitFor(() =>
      expect(localStorage.getItem('schmux:spawn-last-chat-enabled')).toBe('true')
    );
  });

  it('hides the toggle for a target without a chat mode', async () => {
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('gemini');

    expect(screen.queryByTestId('chat-toggle')).not.toBeInTheDocument();
    const payload = await engage();
    expect(payload.kind).toBeUndefined();
  });

  it('hides the toggle in a remote workspace', async () => {
    const cfg = makeConfig({
      chat_sessions: true,
      runners: chatRunners(),
      models: chatModels(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);
    workspacesContextValue = [
      {
        id: 'remote-ws-1',
        repo: 'repo',
        branch: 'branch',
        path: '/remote/workspace',
        session_count: 0,
        sessions: [],
        ahead: 0,
        behind: 0,
        lines_added: 0,
        lines_removed: 0,
        files_changed: 0,
        remote_host_id: 'remote-host-1',
      },
    ];

    renderSpawnPage('/spawn?workspace_id=remote-ws-1');
    await selectAgent('claude');

    expect(screen.queryByTestId('chat-toggle')).not.toBeInTheDocument();
  });
});
