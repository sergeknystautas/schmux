import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { ConfigResponse, SpawnRequest, SpawnResult, WorkspaceResponse } from '../lib/types';
import { makeConfig } from '../lib/test-factories';
import { resetSpawnInflightForTests } from '../lib/spawn-inflight';

// --- Mocks (same shape as SpawnPage.chat.test.tsx) ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
const mockGetStyles = vi.fn<() => Promise<{ styles: unknown[] }>>();
const mockAlert = vi.fn();

vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
  suggestBranch: (...args: unknown[]) => mockSuggestBranch(...(args as [])),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  getStyles: (...args: unknown[]) => mockGetStyles(...(args as [])),
  uploadSpawnAttachment: vi.fn().mockResolvedValue({ id: 'unused', name: 'unused' }),
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
  useModal: () => ({
    alert: mockAlert,
    confirm: vi.fn().mockResolvedValue(true),
    prompt: vi.fn(),
  }),
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
      <button
        data-testid="trigger-command"
        type="button"
        onClick={() => props.onSelectCommand?.('lint')}
      >
        Trigger lint
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

const BUILDER = {
  id: 'builder',
  name: 'Builder',
  icon: 'B',
  color: '#000',
  prompt: 'p',
  expectations: '',
  built_in: true,
};
const REVIEWER = {
  id: 'reviewer',
  name: 'Reviewer',
  icon: 'R',
  color: '#111',
  prompt: 'p',
  expectations: '',
  built_in: true,
};
const AUDITOR = {
  id: 'auditor',
  name: 'Auditor',
  icon: 'A',
  color: '#222',
  prompt: 'p',
  expectations: '',
  built_in: true,
};

const LAST_PERSONA_KEY = 'schmux:spawn-last-persona-id';

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
  fireEvent.change(branchInput, { target: { value: 'feature/persona-test' } });
}

async function selectAgent(id: string) {
  const agentSelect = (await screen.findByTestId('agent-select')) as HTMLSelectElement;
  fireEvent.change(agentSelect, { target: { value: id } });
}

async function selectPersona(id: string) {
  const personaSelect = (await screen.findByTestId('persona-select')) as HTMLSelectElement;
  fireEvent.change(personaSelect, { target: { value: id } });
}

async function engage() {
  await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
  fireEvent.click(screen.getByTestId('spawn-submit'));
  await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
  return mockSpawnSessions.mock.calls[0][0];
}

// The spawn runner calls onSuccess (the last-used write) and then enters the
// 'waiting' phase, which the submit button shows as "Downloading session...".
async function spawnSucceeded() {
  await waitFor(() => expect(screen.getByText('Downloading session...')).toBeInTheDocument());
}

// A draft that already selects claude, so a test that seeds it must not
// re-select the agent (the single-mode agent dropdown toggles the pick off).
function draftWithPersona(personaId: string) {
  return JSON.stringify({
    prompt: '',
    targetCounts: { claude: 1 },
    modelSelectionMode: 'single',
    repo: 'https://github.com/user/gitrepo.git',
    personaId,
  });
}

describe('SpawnPage persona memory', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    const cfg = makeConfig({ personas_enabled: true, models: chatModels() });
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [BUILDER, REVIEWER] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('restores the persona from the tab draft', async () => {
    const first = renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('builder');
    first.unmount();

    // The draft restores repo and agent too, so only the branch is re-entered.
    renderSpawnPage();
    await selectRepoAndBranch();

    expect(await screen.findByTestId('persona-select')).toHaveValue('builder');
    const payload = await engage();
    expect(payload.persona_id).toBe('builder');
  });

  it('an explicit None in the draft beats last-used', async () => {
    localStorage.setItem(LAST_PERSONA_KEY, 'builder');
    sessionStorage.setItem('spawn-draft-fresh', draftWithPersona(''));

    renderSpawnPage();
    await selectRepoAndBranch();

    expect(await screen.findByTestId('persona-select')).toHaveValue('');
    const payload = await engage();
    expect(payload.persona_id).toBeUndefined();
  });

  it('remembers the spawned persona for a new tab', async () => {
    const first = renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('builder');

    const payload = await engage();
    expect(payload.persona_id).toBe('builder');
    await spawnSucceeded();
    expect(localStorage.getItem(LAST_PERSONA_KEY)).toBe('builder');

    // A new tab: no draft, but the last spawn's persona comes back.
    sessionStorage.clear();
    mockSpawnSessions.mockClear();
    first.unmount();
    renderSpawnPage();
    await selectRepoAndBranch();

    expect(await screen.findByTestId('persona-select')).toHaveValue('builder');
  });

  it('spawning with None records None', async () => {
    localStorage.setItem(LAST_PERSONA_KEY, 'builder');

    const first = renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('');

    const payload = await engage();
    expect(payload.persona_id).toBeUndefined();
    await spawnSucceeded();
    expect(localStorage.getItem(LAST_PERSONA_KEY)).toBe('');

    sessionStorage.clear();
    mockSpawnSessions.mockClear();
    first.unmount();
    renderSpawnPage();
    await selectRepoAndBranch();

    expect(await screen.findByTestId('persona-select')).toHaveValue('');
  });

  it('/resume writes the last-used persona', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('builder');

    fireEvent.click(screen.getByTestId('trigger-resume'));
    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());

    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.resume).toBe(true);
    expect(payload.persona_id).toBe('builder');
    await spawnSucceeded();
    expect(localStorage.getItem(LAST_PERSONA_KEY)).toBe('builder');
  });

  it('command target writes the last-used persona', async () => {
    const cfg = makeConfig({
      personas_enabled: true,
      models: chatModels(),
      run_targets: [{ name: 'lint', command: 'make lint' }],
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('builder');

    fireEvent.click(screen.getByTestId('trigger-command'));
    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());

    const payload = mockSpawnSessions.mock.calls[0][0];
    expect(payload.targets).toEqual({ lint: 1 });
    expect(payload.persona_id).toBe('builder');
    await spawnSucceeded();
    expect(localStorage.getItem(LAST_PERSONA_KEY)).toBe('builder');
  });

  it('workspace mode restores from its own draft, else last-used', async () => {
    // Three distinct personas so each source is identifiable: the workspace
    // draft, the fresh-spawn draft, and last-used must not be confused.
    mockGetPersonas.mockResolvedValue({ personas: [BUILDER, REVIEWER, AUDITOR] });
    workspacesContextValue = [
      {
        id: 'ws-1',
        repo: 'https://github.com/user/gitrepo.git',
        branch: 'main',
        sessions: [],
      } as unknown as WorkspaceResponse,
    ];
    localStorage.setItem(LAST_PERSONA_KEY, 'builder');
    sessionStorage.setItem('spawn-draft-ws-1', draftWithPersona('reviewer'));
    sessionStorage.setItem('spawn-draft-fresh', draftWithPersona('auditor'));

    // The workspace's own draft wins; the fresh-spawn draft does not leak in.
    const first = renderSpawnPage('/spawn?workspace_id=ws-1');
    expect(await screen.findByTestId('persona-select')).toHaveValue('reviewer');
    first.unmount();

    // Without a workspace draft, last-used wins; the fresh draft still does not leak.
    sessionStorage.removeItem('spawn-draft-ws-1');
    const second = renderSpawnPage('/spawn?workspace_id=ws-1');
    expect(await screen.findByTestId('persona-select')).toHaveValue('builder');
    second.unmount();

    // The workspace draft does not leak into a fresh-spawn render either.
    sessionStorage.removeItem('spawn-draft-fresh');
    renderSpawnPage('/spawn');
    expect(await screen.findByTestId('persona-select')).toHaveValue('builder');
  });

  it('a remembered persona that no longer exists shows None and is not sent', async () => {
    localStorage.setItem(LAST_PERSONA_KEY, 'gone');

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');

    expect(await screen.findByTestId('persona-select')).toHaveValue('');
    const payload = await engage();
    expect(payload.persona_id).toBeUndefined();
  });

  it('personas disabled: no dropdown, no persona sent', async () => {
    const cfg = makeConfig({ personas_enabled: false, models: chatModels() });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);
    localStorage.setItem(LAST_PERSONA_KEY, 'builder');

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');

    expect(screen.queryByTestId('persona-select')).toBeNull();
    const payload = await engage();
    expect(payload.persona_id).toBeUndefined();
  });

  it('a failed persona fetch keeps the draft pick', async () => {
    sessionStorage.setItem('spawn-draft-fresh', draftWithPersona('builder'));
    mockGetPersonas.mockRejectedValue(new Error('personas unavailable'));

    const first = renderSpawnPage();
    fireEvent.change(await screen.findByTestId('spawn-prompt'), {
      target: { value: 'do the thing' },
    });

    // The seeded draft already holds 'builder'; wait for the re-save the prompt
    // edit triggers, then check the persona survived it.
    const savedDraft = () => JSON.parse(sessionStorage.getItem('spawn-draft-fresh') ?? '{}');
    await waitFor(() => expect(savedDraft().prompt).toBe('do the thing'));
    expect(savedDraft().personaId).toBe('builder');
    first.unmount();

    // Once the personas arrive, the remembered pick shows again.
    mockGetPersonas.mockResolvedValue({ personas: [BUILDER, REVIEWER] });
    renderSpawnPage();
    expect(await screen.findByTestId('persona-select')).toHaveValue('builder');
  });

  it('a failed persona fetch sends no persona', async () => {
    sessionStorage.setItem('spawn-draft-fresh', draftWithPersona('builder'));
    mockGetPersonas.mockRejectedValue(new Error('personas unavailable'));

    renderSpawnPage();
    await selectRepoAndBranch();

    const payload = await engage();
    expect(payload.persona_id).toBeUndefined();
  });

  it('a failed spawn does not record last-used', async () => {
    mockSpawnSessions.mockRejectedValue(new Error('boom'));

    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await selectPersona('builder');

    await engage();
    // The failure is the spawn's terminal state; wait for it to surface.
    await waitFor(() =>
      expect(mockAlert).toHaveBeenCalledWith('Spawn Failed', 'Failed to spawn: boom')
    );

    expect(localStorage.getItem(LAST_PERSONA_KEY)).toBeNull();
  });
});
