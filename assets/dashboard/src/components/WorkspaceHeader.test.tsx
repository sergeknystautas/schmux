import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
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

describe('WorkspaceHeader (new repo) badge', () => {
  it('shows (new repo) linking to the commit graph for local: workspaces', async () => {
    await renderHeader(makeWorkspace({ repo: 'local:talkback' }));
    const badge = screen.getByText('new repo');
    expect(badge).toBeInTheDocument();
    expect(badge.closest('a')).toHaveAttribute('href', expect.stringContaining('/commits/'));
    // ahead/behind pair and (local only) are suppressed
    expect(screen.queryByText('(local only)')).not.toBeInTheDocument();
  });

  it('does not show (new repo) for remote-backed workspaces', async () => {
    await renderHeader(makeWorkspace({ repo: 'https://github.com/x/y' }));
    expect(screen.queryByText('new repo')).not.toBeInTheDocument();
  });
});

describe('WorkspaceHeader GitHub button', () => {
  it('links HTTPS GitHub remotes to the repository page', async () => {
    await renderHeader(makeWorkspace({ repo: 'https://github.com/acme/widget.git' }));

    expect(screen.getByRole('link', { name: 'Open ws-1 on GitHub' })).toHaveAttribute(
      'href',
      'https://github.com/acme/widget'
    );
  });

  it('links SSH GitHub remotes to the repository page', async () => {
    await renderHeader(makeWorkspace({ repo: 'git@github.com:acme/widget.git' }));

    expect(screen.getByRole('link', { name: 'Open ws-1 on GitHub' })).toHaveAttribute(
      'href',
      'https://github.com/acme/widget'
    );
  });

  it('is hidden for non-GitHub remotes', async () => {
    await renderHeader(makeWorkspace({ repo: 'https://gitlab.com/acme/widget.git' }));

    expect(screen.queryByRole('link', { name: 'Open ws-1 on GitHub' })).not.toBeInTheDocument();
  });
});

describe('WorkspaceHeader CI and PR indicators', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockConfig = {};
    mockWorkspaceLockStates = {};
  });

  it.each([
    ['success', 'CI: passing'],
    ['failure', 'CI: failing'],
    ['pending', 'CI: running'],
    ['none', 'CI: no runs yet'],
  ])('renders the %s badge with a link to ci_url', async (status, label) => {
    await renderHeader(makeWorkspace({ ci_status: status, ci_url: 'https://example.com/run' }));
    const badge = screen.getByLabelText(label);
    expect(badge).toBeInTheDocument();
    expect(badge.closest('a')).toHaveAttribute('href', 'https://example.com/run');
  });

  it('renders the badge without a link when ci_url is absent', async () => {
    await renderHeader(makeWorkspace({ ci_status: 'none' }));
    const badge = screen.getByLabelText('CI: no runs yet');
    expect(badge.closest('a')).toBeNull();
  });

  it('hides the CI badge when ci_status is absent', async () => {
    await renderHeader(makeWorkspace({}));
    expect(screen.queryByLabelText(/^CI:/)).toBeNull();
  });

  it('renders the PR link when an open PR exists', async () => {
    await renderHeader(
      makeWorkspace({ pr_number: 42, pr_url: 'https://github.com/acme/widget/pull/42' })
    );
    const link = screen.getByRole('link', { name: 'PR #42' });
    expect(link).toHaveAttribute('href', 'https://github.com/acme/widget/pull/42');
  });

  it('hides the PR link when no open PR exists', async () => {
    await renderHeader(makeWorkspace({}));
    expect(screen.queryByText(/^PR #/)).toBeNull();
  });
});
