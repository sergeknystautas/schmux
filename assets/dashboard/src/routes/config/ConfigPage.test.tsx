import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import ConfigPage from '../ConfigPage';
import type { ConfigResponse, ConfigUpdateRequest } from '../../lib/types';

// --- Mocks ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockUpdateConfig = vi.fn<
  (req: ConfigUpdateRequest) => Promise<{ status: string; warning?: string; warnings?: string[] }>
>();
const mockGetAuthSecretsStatus = vi.fn();
const mockGetOverlays = vi.fn();
const mockGetBuiltinQuickLaunch = vi.fn();

vi.mock('../../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  updateConfig: (...args: unknown[]) => mockUpdateConfig(...(args as [ConfigUpdateRequest])),
  getAuthSecretsStatus: (...args: unknown[]) => mockGetAuthSecretsStatus(...args),
  getOverlays: (...args: unknown[]) => mockGetOverlays(...args),
  getBuiltinQuickLaunch: (...args: unknown[]) => mockGetBuiltinQuickLaunch(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  configureModelSecrets: vi.fn(),
  removeModelSecrets: vi.fn(),
  saveAuthSecrets: vi.fn(),
  setRemoteAccessPassword: vi.fn(),
  testRemoteAccessNotification: vi.fn(),
}));

const mockSuccess = vi.fn();
const mockToastError = vi.fn();
vi.mock('../../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: mockSuccess, error: mockToastError }),
}));

const mockShow = vi.fn().mockResolvedValue(true);
const mockConfirm = vi.fn().mockResolvedValue(true);
const mockPrompt = vi.fn().mockResolvedValue(null);
vi.mock('../../components/ModalProvider', () => ({
  useModal: () => ({ show: mockShow, alert: vi.fn(), confirm: mockConfirm, prompt: mockPrompt }),
}));

const mockConfigCtx = {
  isNotConfigured: false,
  isFirstRun: false,
  completeFirstRun: vi.fn(),
  reloadConfig: vi.fn().mockResolvedValue(undefined),
};
vi.mock('../../contexts/ConfigContext', () => ({
  useConfig: () => mockConfigCtx,
}));

// Minimal full config response fixture
const configFixture: ConfigResponse = {
  workspace_path: '/home/user/ws',
  source_code_management: 'git-worktree',
  repos: [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }],
  run_targets: [
    { name: 'claude', command: 'claude', type: 'promptable', source: 'detected' },
    { name: 'my-agent', command: 'my-agent --prompt', type: 'promptable', source: 'user' },
    { name: 'build', command: 'make build', type: 'command', source: 'user' },
  ],
  models: [],
  quick_launch: [{ name: 'ql1', target: 'claude', prompt: 'hello' }],
  nudgenik: { target: '', viewed_buffer_ms: 5000, seen_interval_ms: 2000 },
  branch_suggest: { target: '' },
  conflict_resolve: { target: '', timeout_ms: 120000 },
  sessions: {
    dashboard_poll_interval_ms: 5000,
    git_status_poll_interval_ms: 10000,
    git_clone_timeout_ms: 300000,
    git_status_timeout_ms: 30000,
  },
  xterm: { query_timeout_ms: 5000, operation_timeout_ms: 10000 },
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
  notifications: { sound_disabled: false, confirm_before_close: false },
  lore: { enabled: true, llm_target: '', curate_on_dispose: 'session', auto_pr: false },
  remote_access: {
    enabled: false,
    timeout_minutes: 0,
    password_hash_set: false,
    notify: { ntfy_topic: '', command: '' },
  },
  needs_restart: false,
};

function renderConfigPage() {
  return render(
    <MemoryRouter initialEntries={['/config?tab=workspaces']}>
      <ConfigPage />
    </MemoryRouter>
  );
}

describe('ConfigPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetConfig.mockResolvedValue(configFixture);
    mockUpdateConfig.mockResolvedValue({ status: 'ok' });
    mockGetAuthSecretsStatus.mockResolvedValue({ client_id_set: false, client_secret_set: false });
    mockGetOverlays.mockResolvedValue({ overlays: [] });
    mockGetBuiltinQuickLaunch.mockResolvedValue([]);
    mockConfigCtx.isFirstRun = false;
    mockConfigCtx.isNotConfigured = false;
  });

  it('loads config and renders the Workspaces tab', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByText('Workspaces')).toBeInTheDocument();
    });
    expect(mockGetConfig).toHaveBeenCalled();
    expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
  });

  it('shows loading state initially', () => {
    // Make getConfig hang
    mockGetConfig.mockReturnValue(new Promise(() => {}));
    renderConfigPage();
    expect(screen.getByText('Loading configuration...')).toBeInTheDocument();
  });

  it('shows error state when config load fails', async () => {
    mockGetConfig.mockRejectedValue(new Error('Network error'));
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByText('Failed to load config')).toBeInTheDocument();
    });
  });

  it('sends correct save payload on Save Changes click', async () => {
    renderConfigPage();

    // Wait for config to load
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // Make a change to trigger hasChanges — modify workspace path via the edit prompt
    mockPrompt.mockResolvedValueOnce('/home/user/new-ws');
    await userEvent.click(screen.getByText('Edit'));

    // Wait for the state update
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/new-ws')).toBeInTheDocument();
    });

    // The Save button should now be enabled
    const saveBtn = screen.getByTestId('config-save');
    expect(saveBtn).not.toBeDisabled();

    // Also need getConfig for reload after save
    mockGetConfig.mockResolvedValue({ ...configFixture, workspace_path: '/home/user/new-ws' });

    await userEvent.click(saveBtn);

    await waitFor(() => {
      expect(mockUpdateConfig).toHaveBeenCalledTimes(1);
    });

    const payload = mockUpdateConfig.mock.calls[0][0];

    // Verify key fields in the payload
    expect(payload.workspace_path).toBe('/home/user/new-ws');
    expect(payload.source_code_management).toBe('git-worktree');
    expect(payload.repos).toEqual([{ name: 'my-repo', url: 'https://github.com/user/repo.git' }]);
    expect(payload.run_targets).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ name: 'my-agent', type: 'promptable' }),
        expect.objectContaining({ name: 'build', type: 'command' }),
      ])
    );
    // Detected targets are NOT included in run_targets (filtered out)
    expect(payload.run_targets?.find((t) => t.name === 'claude')).toBeUndefined();
    expect(payload.quick_launch).toEqual([{ name: 'ql1', target: 'claude', prompt: 'hello' }]);
    expect(payload.nudgenik).toEqual({
      target: '',
      viewed_buffer_ms: 5000,
      seen_interval_ms: 2000,
    });
    expect(payload.sessions).toEqual({
      dashboard_poll_interval_ms: 5000,
      git_status_poll_interval_ms: 10000,
      git_clone_timeout_ms: 300000,
      git_status_timeout_ms: 30000,
    });
    expect(payload.xterm).toEqual({ query_timeout_ms: 5000, operation_timeout_ms: 10000 });
    expect(payload.network).toEqual({
      bind_address: '127.0.0.1',
      public_base_url: '',
      tls: { cert_path: '', key_path: '' },
    });
    expect(payload.access_control).toEqual({
      enabled: false,
      provider: 'github',
      session_ttl_minutes: 1440,
    });
    expect(payload.notifications).toEqual({ sound_disabled: false, confirm_before_close: false });
    expect(payload.lore).toEqual({
      enabled: true,
      llm_target: '',
      curate_on_dispose: 'session',
      auto_pr: false,
    });
    expect(payload.desync).toEqual({ enabled: false, target: '' });
  });

  it('shows success toast after save', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // Make a change
    mockPrompt.mockResolvedValueOnce('/home/user/changed');
    await userEvent.click(screen.getByText('Edit'));
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/changed')).toBeInTheDocument();
    });

    mockGetConfig.mockResolvedValue({ ...configFixture, workspace_path: '/home/user/changed' });
    await userEvent.click(screen.getByTestId('config-save'));

    await waitFor(() => {
      expect(mockSuccess).toHaveBeenCalledWith('Configuration saved');
    });
  });

  it('shows error toast when save fails', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    mockPrompt.mockResolvedValueOnce('/home/user/fail');
    await userEvent.click(screen.getByText('Edit'));
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/fail')).toBeInTheDocument();
    });

    mockUpdateConfig.mockRejectedValueOnce(new Error('Server error'));
    await userEvent.click(screen.getByTestId('config-save'));

    await waitFor(() => {
      expect(mockToastError).toHaveBeenCalledWith('Server error');
    });
  });

  it('switches tabs via tab buttons', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    // Click on the Sessions tab
    await userEvent.click(screen.getByTestId('config-tab-sessions'));
    await waitFor(() => {
      expect(screen.getByText('Detected Run Targets (Read-only)')).toBeInTheDocument();
    });
  });

  it('renders first-run wizard mode', async () => {
    mockConfigCtx.isFirstRun = true;
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByText('Setup schmux')).toBeInTheDocument();
    });
    expect(screen.getByText(/Welcome to schmux!/)).toBeInTheDocument();
  });

  it('disables Save Changes when no changes made', async () => {
    renderConfigPage();
    await waitFor(() => {
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
    });

    const saveBtn = screen.getByTestId('config-save');
    expect(saveBtn).toBeDisabled();
  });

  describe('onboarding wizard flow', () => {
    beforeEach(() => {
      mockConfigCtx.isFirstRun = true;
    });

    async function waitForWizardLoaded() {
      await waitFor(() => {
        expect(screen.queryByText('Loading configuration...')).not.toBeInTheDocument();
      });
      await waitFor(() => {
        expect(screen.getByText('Setup schmux')).toBeInTheDocument();
      });
    }

    it('shows wizard header and welcome banner', async () => {
      renderConfigPage();
      await waitForWizardLoaded();
      expect(screen.getByText('Setup schmux')).toBeInTheDocument();
      expect(screen.getByText(/Welcome to schmux!/)).toBeInTheDocument();
    });

    it('does not show sticky header or Save Changes button', async () => {
      renderConfigPage();
      await waitForWizardLoaded();
      expect(screen.queryByTestId('config-save')).not.toBeInTheDocument();
    });

    it('shows Next button but no Back button on step 1', async () => {
      renderConfigPage();
      await waitForWizardLoaded();
      expect(screen.getByRole('button', { name: /Next/ })).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /Back/ })).not.toBeInTheDocument();
    });

    it('advances to step 2 on Next click and saves', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Click Next — should save step 1 and advance
      await userEvent.click(screen.getByRole('button', { name: /Next/ }));

      await waitFor(() => {
        expect(mockUpdateConfig).toHaveBeenCalledTimes(1);
      });

      // Step 2 content should now be visible
      await waitFor(() => {
        expect(screen.getByText('Detected Run Targets (Read-only)')).toBeInTheDocument();
      });
    });

    it('shows Back button on step 2 and navigates back', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Advance to step 2
      await userEvent.click(screen.getByRole('button', { name: /Next/ }));
      await waitFor(() => {
        expect(screen.getByText('Detected Run Targets (Read-only)')).toBeInTheDocument();
      });

      // Back button should be visible
      const backBtn = screen.getByRole('button', { name: /Back/ });
      expect(backBtn).toBeInTheDocument();

      // Click Back — should go to step 1
      await userEvent.click(backBtn);
      await waitFor(() => {
        expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
      });
    });

    it('shows Finish Setup on the last step', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Advance through all 5 intermediate steps to reach step 6
      for (let i = 0; i < 5; i++) {
        await userEvent.click(screen.getByRole('button', { name: /Next/ }));
        await waitFor(() => {
          expect(mockUpdateConfig).toHaveBeenCalledTimes(i + 1);
        });
      }

      // On step 6, the button should say "Finish Setup"
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Finish Setup/ })).toBeInTheDocument();
      });
    });

    it('completes wizard on Finish Setup and navigates to /spawn', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Advance to step 6
      for (let i = 0; i < 5; i++) {
        await userEvent.click(screen.getByRole('button', { name: /Next/ }));
        await waitFor(() => {
          expect(mockUpdateConfig).toHaveBeenCalledTimes(i + 1);
        });
      }

      // Click Finish Setup
      await userEvent.click(screen.getByRole('button', { name: /Finish Setup/ }));

      await waitFor(() => {
        expect(mockUpdateConfig).toHaveBeenCalledTimes(6);
      });

      // completeFirstRun should have been called
      await waitFor(() => {
        expect(mockConfigCtx.completeFirstRun).toHaveBeenCalled();
      });

      // The "Setup Complete" modal should have been shown
      expect(mockShow).toHaveBeenCalledWith(
        expect.stringContaining('Setup Complete'),
        expect.any(String),
        expect.objectContaining({ confirmText: 'Go to Spawn', cancelText: null })
      );
    });

    it('does not advance when save fails', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Make updateConfig fail
      mockUpdateConfig.mockRejectedValueOnce(new Error('Save failed'));

      await userEvent.click(screen.getByRole('button', { name: /Next/ }));

      await waitFor(() => {
        expect(mockToastError).toHaveBeenCalledWith('Save failed');
      });

      // Should still be on step 1
      expect(screen.getByDisplayValue('/home/user/ws')).toBeInTheDocument();
      expect(screen.queryByText('Detected Run Targets (Read-only)')).not.toBeInTheDocument();
    });

    it('wizard step indicators allow clicking to navigate', async () => {
      renderConfigPage();
      await waitForWizardLoaded();

      // Click on the "Sessions" step indicator directly
      await userEvent.click(screen.getByText('Sessions'));

      // Should switch to step 2
      await waitFor(() => {
        expect(screen.getByText('Detected Run Targets (Read-only)')).toBeInTheDocument();
      });
    });
  });

  it('validates step 1 requires workspace path', async () => {
    // Return config with empty workspace but with repos so we don't hit the repos error
    mockGetConfig.mockResolvedValue({ ...configFixture, workspace_path: '' });
    mockConfigCtx.isFirstRun = true;
    renderConfigPage();

    // Wait for loading to complete
    await waitFor(() => {
      expect(screen.queryByText('Loading configuration...')).not.toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getByText('Setup schmux')).toBeInTheDocument();
    });

    // Click Next — should validate and show error in the form
    const nextBtn = screen.getByRole('button', { name: /Next/ });
    await userEvent.click(nextBtn);
    await waitFor(() => {
      expect(screen.getByText('Workspace path is required')).toBeInTheDocument();
    });

    // Should NOT advance to step 2
    expect(screen.queryByText('Detected Run Targets (Read-only)')).not.toBeInTheDocument();
  });
});
