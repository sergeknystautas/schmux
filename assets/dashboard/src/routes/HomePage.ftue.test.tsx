import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse, WorkspaceResponse } from '../lib/types';

// --- Mutable state for mocks (must be declared before vi.mock factories) ---

let currentWorkspaces: WorkspaceResponse[] = [];
let currentRepos: Array<{ name: string; url: string }> = [];
let currentDevMode = false;

// --- Mocks (vi.mock is hoisted — factories must not reference const declarations) ---

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: {
      workspace_path: '/home/user/ws',
      source_code_management: 'git-worktree',
      repos: currentRepos,
      run_targets: [],
      runners: {
        claude: { available: true, capabilities: ['interactive', 'oneshot', 'streaming'] },
      },
      models: [],
      quick_launch: [],
      nudgenik: { target: '', viewed_buffer_ms: 5000, seen_interval_ms: 2000 },
      sessions: {
        dashboard_poll_interval_ms: 5000,
        git_status_poll_interval_ms: 10000,
        git_clone_timeout_ms: 300000,
        git_status_timeout_ms: 30000,
      },
      xterm: { query_timeout_ms: 5000, operation_timeout_ms: 10000, use_webgl: true },
      network: {
        bind_address: '127.0.0.1',
        port: 7337,
        public_base_url: '',
        tls: { cert_path: '', key_path: '' },
      },
      notifications: {
        sound_disabled: false,
        confirm_before_close: false,
        suggest_dispose_after_push: true,
      },
      pr_review: { target: '' },
      needs_restart: false,
    } as Partial<ConfigResponse>,
    loading: false,
    error: null,
    reloadConfig: vi.fn(),
    getRepoName: (url: string) => url,
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: currentWorkspaces,
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
    subredditUpdateCount: 0,
    repofeedUpdateCount: 0,
  }),
}));

vi.mock('../contexts/FeaturesContext', () => ({
  useFeatures: () => ({
    features: {
      tunnel: true,
      github: true,
      telemetry: true,
      update: true,
      dashboardsx: false,
      model_registry: true,
      repofeed: false,
      subreddit: false,
    },
    loading: false,
  }),
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn().mockResolvedValue(true), prompt: vi.fn() }),
}));

vi.mock('../hooks/useVersionInfo', () => ({
  default: () => ({
    versionInfo: { version: '0.0.0-test', dev_mode: currentDevMode },
    loading: false,
  }),
}));

vi.mock('../hooks/useFloorManager', () => ({
  useFloorManager: () => ({
    enabled: false,
    tmuxSession: '',
    running: false,
    injectionCount: 0,
    rotationThreshold: 150,
  }),
}));

vi.mock('../hooks/useTerminalStream', () => ({
  useTerminalStream: () => ({
    containerRef: { current: null },
    streamRef: { current: null },
  }),
}));

vi.mock('../lib/navigation', () => ({
  navigateToWorkspace: vi.fn(),
  usePendingNavigation: () => ({
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
  }),
}));

vi.mock('../lib/api', () => ({
  scanWorkspaces: vi.fn().mockResolvedValue({ workspaces: [] }),
  getRecentBranches: vi.fn().mockResolvedValue([]),
  refreshRecentBranches: vi.fn().mockResolvedValue([]),
  prepareBranchSpawn: vi.fn().mockResolvedValue({}),
  getPRs: vi.fn().mockResolvedValue([]),
  refreshPRs: vi.fn().mockResolvedValue([]),
  checkoutPR: vi.fn().mockResolvedValue({}),
  getOverlays: vi.fn().mockResolvedValue({ overlays: [] }),
  dismissOverlayNudge: vi.fn().mockResolvedValue({}),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  linearSyncFromMain: vi.fn().mockResolvedValue({}),
  getCommitGraph: vi.fn().mockResolvedValue({ commits: [] }),
  getSubreddit: vi.fn().mockResolvedValue({ repos: [] }),
  getRepofeedList: vi.fn().mockResolvedValue({ repos: [] }),
  getDetectionSummary: vi.fn().mockResolvedValue({
    status: 'ready',
    agents: [{ name: 'claude', version: '1.0' }],
    vcs: [{ name: 'git', version: '2.40' }],
    tmux: { available: true, version: '3.4' },
  }),
  scanLocalRepos: vi.fn().mockResolvedValue([]),
  probeRepo: vi.fn().mockResolvedValue({ accessible: true }),
  getConfig: vi.fn().mockResolvedValue({}),
  updateConfig: vi.fn().mockResolvedValue({}),
}));

import HomePage from './HomePage';

function renderPage() {
  return render(
    <MemoryRouter>
      <HomePage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  currentWorkspaces = [];
  currentRepos = [];
  currentDevMode = false;
});

describe('HomePage FTUE (zero workspaces)', () => {
  it('renders EnvironmentSummary when zero workspaces', () => {
    currentWorkspaces = [];
    renderPage();
    expect(screen.getByTestId('env-summary')).toBeInTheDocument();
  });

  it('renders "+ Add Repository" CTA when no repos configured', () => {
    currentWorkspaces = [];
    currentRepos = [];
    renderPage();
    const cta = screen.getByTestId('add-workspace-cta');
    expect(cta).toBeInTheDocument();
    expect(cta).toHaveTextContent('+ Add Repository');
    expect(cta).toHaveTextContent('Add a repository to start spawning AI coding sessions');
  });

  it('hides "+ Add Repository" CTA when a repo is configured', () => {
    currentWorkspaces = [];
    currentRepos = [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }];
    renderPage();
    expect(screen.queryByTestId('add-workspace-cta')).not.toBeInTheDocument();
  });

  it('does NOT render branches section when zero workspaces', () => {
    currentWorkspaces = [];
    renderPage();
    expect(screen.queryByTestId('recent-branches')).not.toBeInTheDocument();
  });

  it('does NOT render pull requests section when zero workspaces', () => {
    currentWorkspaces = [];
    renderPage();
    expect(screen.queryByText('Pull Requests')).not.toBeInTheDocument();
  });

  it('opens AddRepoModal when CTA is clicked', () => {
    currentWorkspaces = [];
    currentRepos = [];
    renderPage();
    const cta = screen.getByTestId('add-workspace-cta');
    fireEvent.click(cta);
    expect(screen.getByText('Add Repository')).toBeInTheDocument();
    expect(screen.getByLabelText('Clone from')).toBeInTheDocument();
  });
});

describe('HomePage with workspaces (active user)', () => {
  const mockWorkspace: WorkspaceResponse = {
    id: 'ws-1',
    repo: 'https://github.com/user/repo.git',
    branch: 'main',
    path: '/home/user/ws/ws-1',
    ahead: 0,
    behind: 0,
    session_count: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    sessions: [],
  };

  it('renders branches section when workspaces exist and dev mode is on', () => {
    currentWorkspaces = [mockWorkspace];
    currentDevMode = true;
    renderPage();
    expect(screen.getByTestId('recent-branches')).toBeInTheDocument();
  });

  it('does NOT render branches section when dev mode is off', () => {
    currentWorkspaces = [mockWorkspace];
    currentDevMode = false;
    renderPage();
    expect(screen.queryByTestId('recent-branches')).not.toBeInTheDocument();
  });

  it('renders EnvironmentSummary even when workspaces exist', () => {
    currentWorkspaces = [mockWorkspace];
    renderPage();
    expect(screen.getByTestId('env-summary')).toBeInTheDocument();
  });

  it('hides add-repo CTA when repos are configured', () => {
    currentWorkspaces = [mockWorkspace];
    currentRepos = [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }];
    renderPage();
    expect(screen.queryByTestId('add-workspace-cta')).not.toBeInTheDocument();
  });

  it('renders workspace list when workspaces exist', () => {
    currentWorkspaces = [mockWorkspace];
    renderPage();
    expect(screen.getByTestId('workspace-list')).toBeInTheDocument();
  });
});
