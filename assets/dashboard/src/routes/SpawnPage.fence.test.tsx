import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { ConfigResponse, SpawnRequest, SpawnResult } from '../lib/types';
import { makeConfig, systemCapabilities } from '../lib/test-factories';

// --- Mocks ---

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

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: { value: string; onChange: (v: string) => void }) => (
    <textarea
      data-testid="spawn-prompt"
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

import SpawnPage from './SpawnPage';

function renderSpawnPage() {
  return render(
    <MemoryRouter initialEntries={['/spawn']}>
      <SpawnPage />
    </MemoryRouter>
  );
}

describe('SpawnPage fence toggle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    const cfg = makeConfig();
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('hides the fence toggle when fence is unavailable', async () => {
    const cfg = makeConfig({
      system_capabilities: systemCapabilities(),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeInTheDocument());

    expect(screen.queryByTestId('fence-toggle')).not.toBeInTheDocument();
  });

  it('shows the fence toggle when fence is available and target is local', async () => {
    const cfg = makeConfig({
      system_capabilities: systemCapabilities({ fence_available: true }),
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeInTheDocument());
  });

  it('hides the fence toggle when fence_mode is disabled even if available', async () => {
    const cfg = makeConfig({
      system_capabilities: systemCapabilities({ fence_available: true }),
      fence_mode: 'disabled',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeInTheDocument());
    expect(screen.queryByTestId('fence-toggle')).not.toBeInTheDocument();
  });

  it('defaults the fence toggle unchecked when fence_mode is optional_off', async () => {
    const cfg = makeConfig({
      system_capabilities: systemCapabilities({ fence_available: true }),
      fence_mode: 'optional_off',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeInTheDocument());
    expect(screen.getByTestId('fence-toggle')).not.toBeChecked();
  });

  it('defaults the fence toggle checked when fence_mode is optional_on', async () => {
    const cfg = makeConfig({
      system_capabilities: systemCapabilities({ fence_available: true }),
      fence_mode: 'optional_on',
    });
    configContextValue = cfg;
    mockGetConfig.mockResolvedValue(cfg);

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('fence-toggle')).toBeChecked());
  });
});
