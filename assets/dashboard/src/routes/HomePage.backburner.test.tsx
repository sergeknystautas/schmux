import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse, WorkspaceResponse } from '../lib/types';

// --- Mutable state for mocks (must be declared before vi.mock factories) ---

let currentWorkspaces: WorkspaceResponse[] = [];
let currentConfig: Partial<ConfigResponse>;

// --- Mocks (vi.mock is hoisted — factories must not reference const declarations) ---

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: currentConfig,
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
    versionInfo: { version: '0.0.0-test', dev_mode: false },
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
}));

import HomePage from './HomePage';

const baseConfig: Partial<ConfigResponse> = {
  workspace_path: '/home/user/ws',
  source_code_management: 'git-worktree',
  repos: [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }],
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
};

function makeWorkspace(overrides: Partial<WorkspaceResponse> & { id: string }): WorkspaceResponse {
  return {
    repo: 'https://github.com/user/repo.git',
    branch: overrides.id,
    path: `/home/user/ws/${overrides.id}`,
    ahead: 0,
    behind: 0,
    session_count: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    sessions: [],
    ...overrides,
  };
}

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
  currentConfig = { ...baseConfig };
});

describe('backburner workspaces', () => {
  it('dims backburnered workspace rows when backburner_enabled', () => {
    currentConfig = { ...baseConfig, backburner_enabled: true };
    currentWorkspaces = [
      makeWorkspace({ id: 'ws-normal' }),
      makeWorkspace({ id: 'ws-bb', backburner: true }),
    ];
    renderPage();

    // There are two workspace-list containers (sidebar + right column);
    // use getAllByTestId and check the right-column ones (last occurrence).
    const normalRows = screen.getAllByTestId('workspace-ws-normal');
    const bbRows = screen.getAllByTestId('workspace-ws-bb');

    // Check that at least one rendered row (right column) has opacity on the bb row
    const bbRow = bbRows[bbRows.length - 1];
    expect(bbRow.style.opacity).toBe('0.38');

    // Normal row should NOT have opacity set
    const normalRow = normalRows[normalRows.length - 1];
    expect(normalRow.style.opacity).toBe('');
  });

  it('does not dim rows when backburner_enabled is false', () => {
    currentConfig = { ...baseConfig, backburner_enabled: false };
    currentWorkspaces = [makeWorkspace({ id: 'ws-bb', backburner: true })];
    renderPage();

    const bbRows = screen.getAllByTestId('workspace-ws-bb');
    const bbRow = bbRows[bbRows.length - 1];
    expect(bbRow.style.opacity).toBe('');
  });

  it('sorts backburnered workspaces to bottom', () => {
    currentConfig = { ...baseConfig, backburner_enabled: true };
    // ws-a is not backburnered, ws-b is backburnered, ws-c is not backburnered
    currentWorkspaces = [
      makeWorkspace({ id: 'ws-a', branch: 'feature-a' }),
      makeWorkspace({ id: 'ws-b', branch: 'feature-b', backburner: true }),
      makeWorkspace({ id: 'ws-c', branch: 'feature-c' }),
    ];
    renderPage();

    // Find the right-column workspace-list and get the order
    const lists = screen.getAllByTestId('workspace-list');
    const rightList = lists[lists.length - 1];
    const rows = rightList.querySelectorAll('[data-testid^="workspace-"]');

    // Order should be: ws-a, ws-c, ws-b (non-backburnered first, then backburnered)
    const ids = Array.from(rows).map((row) => row.getAttribute('data-testid'));
    expect(ids).toEqual(['workspace-ws-a', 'workspace-ws-c', 'workspace-ws-b']);
  });

  it('does not sort when backburner_enabled is false', () => {
    currentConfig = { ...baseConfig, backburner_enabled: false };
    currentWorkspaces = [
      makeWorkspace({ id: 'ws-a', branch: 'feature-a' }),
      makeWorkspace({ id: 'ws-b', branch: 'feature-b', backburner: true }),
      makeWorkspace({ id: 'ws-c', branch: 'feature-c' }),
    ];
    renderPage();

    const lists = screen.getAllByTestId('workspace-list');
    const rightList = lists[lists.length - 1];
    const rows = rightList.querySelectorAll('[data-testid^="workspace-"]');

    // Order should remain as provided when feature is disabled
    const ids = Array.from(rows).map((row) => row.getAttribute('data-testid'));
    expect(ids).toEqual(['workspace-ws-a', 'workspace-ws-b', 'workspace-ws-c']);
  });
});
