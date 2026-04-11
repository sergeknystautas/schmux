import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse } from '../lib/types';

// --- Config fixture ---

const baseConfig: ConfigResponse = {
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

// --- Mocks ---

let currentConfig: ConfigResponse = { ...baseConfig };

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
      dashboardsx: true,
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

function renderPage() {
  return render(
    <MemoryRouter>
      <HomePage />
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  currentConfig = { ...baseConfig };
});

describe('HomePage dashboard.sx alerts', () => {
  it('does not render alerts section when no dashboard_sx_status', () => {
    currentConfig = { ...baseConfig };
    renderPage();
    expect(screen.queryByTestId('dashboardsx-alerts')).not.toBeInTheDocument();
  });

  it('does not render alerts when heartbeat is 200 and cert not expiring', () => {
    const farFuture = new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString();
    currentConfig = {
      ...baseConfig,
      dashboard_sx_status: {
        last_heartbeat_status: 200,
        last_heartbeat_time: new Date().toISOString(),
        cert_domain: 'example.com',
        cert_expires_at: farFuture,
      },
    };
    renderPage();
    expect(screen.queryByTestId('dashboardsx-alerts')).not.toBeInTheDocument();
  });

  it('renders heartbeat alert when status is not 200', () => {
    currentConfig = {
      ...baseConfig,
      dashboard_sx_status: {
        last_heartbeat_status: 502,
        last_heartbeat_time: '2026-03-15T10:00:00Z',
        last_heartbeat_error: 'Bad Gateway',
      },
    };
    renderPage();
    const alerts = screen.getByTestId('dashboardsx-alerts');
    expect(alerts).toBeInTheDocument();
    expect(alerts).toHaveTextContent('dashboard.sx alerts');
    expect(alerts).toHaveTextContent('heartbeat:');
    expect(alerts).toHaveTextContent('502');
    expect(alerts).toHaveTextContent('Bad Gateway');
  });

  it('renders cert expiry alert when within 30 days', () => {
    const soon = new Date(Date.now() + 10 * 24 * 60 * 60 * 1000).toISOString();
    currentConfig = {
      ...baseConfig,
      dashboard_sx_status: {
        last_heartbeat_status: 200,
        cert_domain: 'mysite.dashboard.sx',
        cert_expires_at: soon,
      },
    };
    renderPage();
    const alerts = screen.getByTestId('dashboardsx-alerts');
    expect(alerts).toBeInTheDocument();
    expect(alerts).toHaveTextContent('certificate: mysite.dashboard.sx expires in');
    expect(alerts).toHaveTextContent('days');
  });

  it('renders both alerts when heartbeat failed and cert expiring', () => {
    const soon = new Date(Date.now() + 5 * 24 * 60 * 60 * 1000).toISOString();
    currentConfig = {
      ...baseConfig,
      dashboard_sx_status: {
        last_heartbeat_status: 500,
        last_heartbeat_time: '2026-03-20T12:00:00Z',
        last_heartbeat_error: 'Internal Server Error',
        cert_domain: 'prod.dashboard.sx',
        cert_expires_at: soon,
      },
    };
    renderPage();
    const alerts = screen.getByTestId('dashboardsx-alerts');
    expect(alerts).toBeInTheDocument();
    expect(alerts).toHaveTextContent('heartbeat:');
    expect(alerts).toHaveTextContent('500');
    expect(alerts).toHaveTextContent('Internal Server Error');
    expect(alerts).toHaveTextContent('certificate: prod.dashboard.sx expires in');
  });
});
