import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import WorkspaceHeader from './WorkspaceHeader';
import type { WorkspaceResponse } from '../lib/types';

// ---- Context mocks ----

vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));

vi.mock('./ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn().mockResolvedValue(null) }),
}));

vi.mock('../contexts/ConfigContext', () => {
  const mod = { useConfig: () => ({ config: mockConfig }) };
  return mod;
});

let mockWorkspaceLockStates: Record<string, { locked: boolean }> = {};
vi.mock('../contexts/SyncContext', () => ({
  useSyncState: () => ({
    linearSyncResolveConflictStates: {},
    clearLinearSyncResolveConflictState: vi.fn(),
    workspaceLockStates: mockWorkspaceLockStates,
    syncResultEvents: [],
    clearSyncResultEvents: vi.fn(),
  }),
}));

vi.mock('../contexts/RemoteAccessContext', () => ({
  useRemoteAccess: () => ({ simulateRemote: false }),
}));

vi.mock('../hooks/useSync', () => ({
  useSync: () => ({
    handleLinearSyncFromMain: vi.fn(),
    handleLinearSyncToMain: vi.fn(),
    startConflictResolution: vi.fn(),
  }),
}));

vi.mock('../hooks/useDevStatus', () => ({
  default: () => ({ devStatus: null }),
}));

// Mock API
const mockSetBackburner = vi.fn().mockResolvedValue({ status: 'ok' });
vi.mock('../lib/api', () => ({
  openVSCode: vi.fn().mockResolvedValue({ success: true }),
  disposeWorkspace: vi.fn().mockResolvedValue(undefined),
  disposeWorkspaceAll: vi.fn().mockResolvedValue(undefined),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  setBackburner: (...args: unknown[]) => mockSetBackburner(...args),
}));

// ---- Controlled config ----
let mockConfig: Record<string, unknown> = {};

// ---- Factory ----

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

async function renderHeader(workspace?: WorkspaceResponse) {
  const ws = workspace || makeWorkspace();
  await act(async () => {
    render(
      <MemoryRouter>
        <WorkspaceHeader workspace={ws} />
      </MemoryRouter>
    );
  });
}

describe('WorkspaceHeader backburner button', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConfig = {};
    mockWorkspaceLockStates = {};
  });

  it('renders when feature enabled and workspace not backburnered', async () => {
    mockConfig = { backburner_enabled: true };
    await renderHeader(makeWorkspace({ backburner: false }));

    const btn = screen.getByLabelText('Backburner');
    expect(btn).toBeInTheDocument();
  });

  it('shows wake up label when workspace is backburnered', async () => {
    mockConfig = { backburner_enabled: true };
    await renderHeader(makeWorkspace({ backburner: true }));

    const btn = screen.getByLabelText('Wake up');
    expect(btn).toBeInTheDocument();
  });

  it('hidden when feature disabled', async () => {
    mockConfig = { backburner_enabled: false };
    await renderHeader(makeWorkspace({ backburner: false }));

    expect(screen.queryByLabelText('Backburner')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Wake up')).not.toBeInTheDocument();
  });

  it('hidden when feature flag is absent', async () => {
    mockConfig = {};
    await renderHeader(makeWorkspace({ backburner: false }));

    expect(screen.queryByLabelText('Backburner')).not.toBeInTheDocument();
    expect(screen.queryByLabelText('Wake up')).not.toBeInTheDocument();
  });
});

describe('WorkspaceHeader branch display (workspaceDisplayLabel wiring)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConfig = {};
    mockWorkspaceLockStates = {};
  });

  it('renders the branch when set (git workspace, no label)', async () => {
    await renderHeader(makeWorkspace({ id: 'myrepo-001', branch: 'feature/login', vcs: 'git' }));

    expect(screen.getByText('feature/login')).toBeInTheDocument();
  });

  it('renders the workspace ID for sapling workspace with empty branch and empty label', async () => {
    await renderHeader(
      makeWorkspace({
        id: 'myrepo-008',
        branch: '',
        vcs: 'sapling',
        sessions: [],
      })
    );

    // The workspace ID appears in both the branch slot (via the helper
    // fallback) and the workspace-name slot. We assert specifically on
    // the branch slot to verify the helper wired through.
    const branchEl = document.querySelector('.app-header__branch');
    expect(branchEl).not.toBeNull();
    expect(branchEl?.textContent).toBe('myrepo-008');
  });

  it('renders the label when set (preferred over branch)', async () => {
    await renderHeader(
      makeWorkspace({
        id: 'myrepo-009',
        branch: '',
        vcs: 'sapling',
        label: 'My Custom Label',
      })
    );

    expect(screen.getByText('My Custom Label')).toBeInTheDocument();
  });
});
