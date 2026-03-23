import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ActionDropdown from './ActionDropdown';
import type { WorkspaceResponse } from '../lib/types';
import type { SpawnEntry } from '../lib/types.generated';

// Track navigations
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

// Mock API
const mockSpawnSessions = vi.fn();
vi.mock('../lib/api', () => ({
  spawnSessions: (...args: unknown[]) => mockSpawnSessions(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
}));

// Mock emergence API
const mockGetSpawnEntries = vi.fn().mockResolvedValue([]);
const mockRecordSpawnEntryUse = vi.fn().mockResolvedValue(undefined);
vi.mock('../lib/emergence-api', () => ({
  getSpawnEntries: (...args: unknown[]) => mockGetSpawnEntries(...args),
  recordSpawnEntryUse: (...args: unknown[]) => mockRecordSpawnEntryUse(...args),
}));

// Mock toast
const mockSuccess = vi.fn();
const mockError = vi.fn();
vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: mockSuccess, error: mockError }),
}));

// Mock sessions context
const mockWaitForSession = vi.fn().mockResolvedValue(undefined);
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ waitForSession: mockWaitForSession }),
}));

// Mock config context — controlled per-test
let mockConfig: Record<string, unknown> = {};
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: mockConfig }),
}));

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'git@github.com:test/repo.git',
    repo_name: 'test-repo',
    branch: 'main',
    path: '/tmp/ws',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  };
}

function makeEntry(overrides: Partial<SpawnEntry> = {}): SpawnEntry {
  return {
    id: 'se-1',
    name: 'Fix lint errors',
    type: 'skill',
    source: 'emerged',
    state: 'pinned',
    use_count: 5,
    ...overrides,
  };
}

async function renderDropdown(workspace?: WorkspaceResponse) {
  const onClose = vi.fn();
  const ws = workspace || makeWorkspace();
  await act(async () => {
    render(
      <MemoryRouter>
        <ActionDropdown workspace={ws} onClose={onClose} />
      </MemoryRouter>
    );
  });
  return { onClose };
}

describe('ActionDropdown', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConfig = {};
    mockGetSpawnEntries.mockResolvedValue([]);
  });

  // --- Section structure ---

  it('renders both section headers', async () => {
    await renderDropdown();

    expect(screen.getByText('Quick Launch')).toBeInTheDocument();
    expect(screen.getByText('Emerged')).toBeInTheDocument();
  });

  it('renders "Spawn a session..." button', async () => {
    await renderDropdown();

    expect(screen.getByText('Spawn a session...')).toBeInTheDocument();
    expect(screen.getByText('wizard')).toBeInTheDocument();
  });

  it('renders two manage links', async () => {
    await renderDropdown();

    const manageLinks = screen.getAllByText('manage');
    expect(manageLinks).toHaveLength(2);
  });

  // --- Empty states ---

  it('shows empty state when no quick launch presets configured', async () => {
    await renderDropdown();

    expect(screen.getByText('No presets configured')).toBeInTheDocument();
  });

  it('shows empty state when no emerged actions exist', async () => {
    await renderDropdown();

    expect(screen.getByText('No emerged actions yet')).toBeInTheDocument();
  });

  // --- Quick Launch items ---

  it('renders quick launch items from global config', async () => {
    mockConfig = {
      quick_launch: [{ name: 'Claude Code' }, { name: 'Codex' }],
    };
    await renderDropdown();

    expect(screen.getByText('Claude Code')).toBeInTheDocument();
    expect(screen.getByText('Codex')).toBeInTheDocument();
    expect(screen.queryByText('No presets configured')).not.toBeInTheDocument();
  });

  it('renders quick launch items from workspace config', async () => {
    const ws = makeWorkspace({ quick_launch: ['Custom Agent'] });
    await renderDropdown(ws);

    expect(screen.getByText('Custom Agent')).toBeInTheDocument();
  });

  it('merges global and workspace quick launch items', async () => {
    mockConfig = {
      quick_launch: [{ name: 'Claude Code' }],
    };
    const ws = makeWorkspace({ quick_launch: ['Custom Agent'] });
    await renderDropdown(ws);

    expect(screen.getByText('Claude Code')).toBeInTheDocument();
    expect(screen.getByText('Custom Agent')).toBeInTheDocument();
  });

  // --- Emerged entries ---

  it('renders spawn entries from API', async () => {
    mockGetSpawnEntries.mockResolvedValue([
      makeEntry({ id: 'se-1', name: 'Fix lint errors' }),
      makeEntry({ id: 'se-2', name: 'Run tests', source: 'manual' }),
    ]);
    await renderDropdown();

    expect(screen.getByText('Fix lint errors')).toBeInTheDocument();
    expect(screen.getByText('Run tests')).toBeInTheDocument();
    expect(screen.queryByText('No emerged actions yet')).not.toBeInTheDocument();
  });

  it('fetches entries using repo_name, not repo URL', async () => {
    await renderDropdown(
      makeWorkspace({ repo: 'git@github.com:org/my-repo.git', repo_name: 'my-repo' })
    );

    expect(mockGetSpawnEntries).toHaveBeenCalledWith('my-repo');
  });

  it('falls back to repo when repo_name is missing', async () => {
    await renderDropdown(makeWorkspace({ repo: 'local-repo', repo_name: undefined }));

    expect(mockGetSpawnEntries).toHaveBeenCalledWith('local-repo');
  });

  // --- Navigation ---

  it('"Spawn a session..." navigates to spawn wizard', async () => {
    const user = userEvent.setup();
    const { onClose } = await renderDropdown();

    await user.click(screen.getByText('Spawn a session...'));

    expect(onClose).toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith('/spawn?workspace_id=ws-1');
  });

  it('Quick Launch manage link navigates to config quicklaunch tab', async () => {
    const user = userEvent.setup();
    const { onClose } = await renderDropdown();

    const manageLinks = screen.getAllByText('manage');
    await user.click(manageLinks[0]); // first manage = Quick Launch

    expect(onClose).toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith('/config?tab=quicklaunch');
  });

  it('Emerged manage link navigates to lore actions tab with repo', async () => {
    const user = userEvent.setup();
    const { onClose } = await renderDropdown();

    const manageLinks = screen.getAllByText('manage');
    await user.click(manageLinks[1]); // second manage = Emerged

    expect(onClose).toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith('/lore?repo=test-repo&tab=actions');
  });

  // --- Spawning ---

  it('quick launch item spawns session with quick_launch_name', async () => {
    const user = userEvent.setup();
    mockConfig = { quick_launch: [{ name: 'Claude Code' }] };
    mockSpawnSessions.mockResolvedValue([{ session_id: 's-123' }]);

    await renderDropdown();
    await user.click(screen.getByText('Claude Code'));

    expect(mockSpawnSessions).toHaveBeenCalledWith(
      expect.objectContaining({
        repo: 'git@github.com:test/repo.git',
        branch: 'main',
        workspace_id: 'ws-1',
        quick_launch_name: 'Claude Code',
        prompt: '',
        nickname: 'Claude Code',
      })
    );
  });

  it('spawn entry spawns session and records usage', async () => {
    const user = userEvent.setup();
    mockGetSpawnEntries.mockResolvedValue([
      makeEntry({ id: 'se-42', name: 'Run tests', type: 'agent', prompt: 'Run all tests' }),
    ]);
    mockSpawnSessions.mockResolvedValue([{ session_id: 's-456' }]);

    await renderDropdown();
    await user.click(screen.getByText('Run tests'));

    expect(mockSpawnSessions).toHaveBeenCalledWith(
      expect.objectContaining({
        prompt: 'Run all tests',
        nickname: 'Run tests',
      })
    );
    expect(mockRecordSpawnEntryUse).toHaveBeenCalledWith('test-repo', 'se-42');
  });

  // --- Create action ---

  it('"+ Create action" navigates to lore actions tab', async () => {
    const user = userEvent.setup();
    const { onClose } = await renderDropdown();

    await user.click(screen.getByText('+ Create action'));

    expect(onClose).toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith('/lore?repo=test-repo&tab=actions&create=1');
  });
});
