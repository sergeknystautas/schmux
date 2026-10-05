import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import type { ConfigResponse, SpawnRequest, SpawnResult, WorkspaceResponse } from '../lib/types';
import { makeConfig } from '../lib/test-factories';
import { resetSpawnInflightForTests } from '../lib/spawn-inflight';

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
const mockGetStyles = vi.fn<() => Promise<{ styles: unknown[] }>>();
const mockUploadSpawnAttachment = vi.fn<(file: File) => Promise<{ id: string; name: string }>>();

vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
  suggestBranch: (...args: unknown[]) => mockSuggestBranch(...args),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  getStyles: (...args: unknown[]) => mockGetStyles(...(args as [])),
  uploadSpawnAttachment: (file: File) => mockUploadSpawnAttachment(file),
}));

vi.mock('../lib/spawn-api', () => ({
  getSpawnEntries: vi.fn().mockResolvedValue([]),
  getPromptHistory: vi.fn().mockResolvedValue([]),
}));

vi.mock('../lib/quicklaunch', () => ({
  getQuickLaunchItems: () => [],
}));

const mockToastError = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: mockToastError }),
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
  default: (props: { onChange: (v: unknown) => void }) => (
    <button
      type="button"
      data-testid="go-remote"
      onClick={() => props.onChange({ type: 'remote', profileId: 'p1', profile: {}, flavor: 'f1' })}
    >
      Remote
    </button>
  ),
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

const csv = (name = 'users.csv') => new File(['id\n'], name, { type: 'text/csv' });
const jpeg = (name = 'photo.jpg') => new File(['j'], name, { type: 'image/jpeg' });

function filesTransfer(files: File[]) {
  return { types: ['Files'], files, dropEffect: 'none' };
}

describe('SpawnPage attachments', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    const cfg = makeConfig({ runners: chatRunners(), models: chatModels() });
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    mockUploadSpawnAttachment.mockImplementation(async (f) => ({
      id: `id-${f.name}`,
      name: f.name,
    }));
  });

  it('sends picked files by staging id and images with their media type', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), [csv(), jpeg()]);
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('users.csv');
    expect(await screen.findByTestId('spawn-image-chip')).toBeInTheDocument();

    const req = await engage();
    expect(req.file_attachments).toEqual(['id-users.csv']);
    expect(req.images).toEqual([{ media_type: 'image/jpeg', data: 'ag==' }]);
  });

  it('disables Spawn until the upload finishes', async () => {
    let finish!: (v: { id: string; name: string }) => void;
    mockUploadSpawnAttachment.mockImplementation(() => new Promise((r) => (finish = r)));
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    finish({ id: 'id-users.csv', name: 'users.csv' });
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
  });

  it('attaches pasted files and leaves plain-text paste alone', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const textPaste = new Event('paste', { bubbles: true, cancelable: true });
    Object.assign(textPaste, { clipboardData: { files: [], items: [] } });
    document.dispatchEvent(textPaste);
    expect(textPaste.defaultPrevented).toBe(false);

    const filePaste = new Event('paste', { bubbles: true, cancelable: true });
    Object.assign(filePaste, { clipboardData: { files: [csv('pasted.csv')], items: [] } });
    document.dispatchEvent(filePaste);
    expect(filePaste.defaultPrevented).toBe(true);
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('pasted.csv');
  });

  it('attaches dropped files and shows the shared overlay while dragging', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const zone = screen.getByTestId('spawn-drop-zone');
    fireEvent.dragEnter(zone, { dataTransfer: filesTransfer([csv('dropped.csv')]) });
    expect(screen.getByTestId('spawn-file-drop-overlay')).toBeInTheDocument();
    fireEvent.drop(zone, { dataTransfer: filesTransfer([csv('dropped.csv')]) });
    expect(screen.queryByTestId('spawn-file-drop-overlay')).not.toBeInTheDocument();
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('dropped.csv');
  });

  it('restores both chip kinds from the draft without uploading again', async () => {
    sessionStorage.setItem(
      'spawn-draft-fresh',
      JSON.stringify({
        prompt: 'go',
        targetCounts: {},
        modelSelectionMode: 'single',
        images: [{ media_type: 'image/png', data: 'AA==' }],
        files: [{ id: 'id-restored.csv', name: 'restored.csv' }],
      })
    );
    renderSpawnPage();
    expect(await screen.findByTestId('spawn-file-chip')).toHaveTextContent('restored.csv');
    expect(screen.getByTestId('spawn-image-chip')).toBeInTheDocument();
    expect(mockUploadSpawnAttachment).not.toHaveBeenCalled();
  });

  it('clears attachments after a successful spawn', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    await engage();
    await waitFor(() => expect(screen.queryByTestId('spawn-file-chip')).not.toBeInTheDocument());
  });

  it('keeps attachments when every target fails', async () => {
    mockSpawnSessions.mockResolvedValue([
      { error: 'attachment no longer available: id-users.csv' },
    ]);
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    await engage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeEnabled());
    expect(screen.getByTestId('spawn-file-chip')).toHaveTextContent('users.csv');
  });

  it('blocks a remote spawn while attachments are present', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    fireEvent.click(screen.getByTestId('go-remote'));
    expect(screen.getByTestId('spawn-attach')).toBeDisabled();
    fireEvent.click(screen.getByTestId('spawn-submit'));
    expect(mockToastError).toHaveBeenCalledWith("Attachments aren't supported for remote spawns");
    expect(mockSpawnSessions).not.toHaveBeenCalled();
  });

  it('allows a remote spawn once the last attachment is removed', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    fireEvent.click(screen.getByTestId('go-remote'));
    fireEvent.click(screen.getByRole('button', { name: 'Remove users.csv' }));
    expect(screen.queryByTestId('spawn-file-chip')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('spawn-submit'));
    expect(mockToastError).not.toHaveBeenCalledWith(
      "Attachments aren't supported for remote spawns"
    );
    await waitFor(() => expect(mockSpawnSessions).toHaveBeenCalled());
    expect(mockSpawnSessions.mock.calls[0][0].file_attachments).toBeUndefined();
  });

  it('blocks /resume while attachments are present', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    await selectAgent('claude');
    await userEvent.upload(screen.getByTestId('spawn-file-input'), csv());
    await screen.findByTestId('spawn-file-chip');
    fireEvent.click(screen.getByTestId('trigger-resume'));
    expect(mockToastError).toHaveBeenCalledWith('Remove attachments to run /resume');
    expect(mockSpawnSessions).not.toHaveBeenCalled();
  });

  it('caps images at five and names the rejected file', async () => {
    renderSpawnPage();
    await selectRepoAndBranch();
    const six = [1, 2, 3, 4, 5, 6].map((n) => jpeg(`${n}.jpg`));
    await userEvent.upload(screen.getByTestId('spawn-file-input'), six);
    expect(await screen.findByRole('alert')).toHaveTextContent('6.jpg: maximum 5 images');
    expect(screen.getAllByTestId('spawn-image-chip')).toHaveLength(5);
  });
});
