import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { useSync } from './useSync';

const pushCommits = vi.fn();
const pushToBranch = vi.fn();
const linearSyncToMain = vi.fn();
const getConfig = vi.fn();
const getDevStatus = vi.fn();
const disposeWorkspaceAll = vi.fn();

vi.mock('../lib/api', () => ({
  linearSyncFromMain: vi.fn(),
  linearSyncToMain: (...args: unknown[]) => linearSyncToMain(...args),
  pushToBranch: (...args: unknown[]) => pushToBranch(...args),
  pushCommits: (...args: unknown[]) => pushCommits(...args),
  linearSyncResolveConflict: vi.fn(),
  disposeWorkspaceAll: (...args: unknown[]) => disposeWorkspaceAll(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  getDevStatus: (...args: unknown[]) => getDevStatus(...args),
  getConfig: (...args: unknown[]) => getConfig(...args),
  LinearSyncError: class LinearSyncError extends Error {},
}));

const confirm = vi.fn();
const confirmWithCheckbox = vi.fn();
const alert = vi.fn();
const show = vi.fn();
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert, confirm, confirmWithCheckbox, show }),
}));

const toastSuccess = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ error: vi.fn(), success: toastSuccess }),
}));

vi.mock('../contexts/SyncContext', () => ({
  useSyncState: () => ({ clearLinearSyncResolveConflictState: vi.fn() }),
}));
vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({ setPendingNavigation: vi.fn() }),
}));

const navigate = vi.fn();
vi.mock('react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-router')>()),
  useNavigate: () => navigate,
}));

// Probe component: exposes the hook's return value to the tests.
let sync: ReturnType<typeof useSync>;
function Probe() {
  sync = useSync();
  return null;
}

function renderSync() {
  render(
    <MemoryRouter>
      <Probe />
    </MemoryRouter>
  );
}

function successResult(overrides: Record<string, unknown> = {}) {
  return {
    success: true,
    target_branch: 'main',
    per_commit: false,
    total_commits: 2,
    pushes_succeeded: 1,
    ...overrides,
  };
}

const baseOpts = {
  hash: 'a'.repeat(40),
  target: 'default' as const,
  perCommit: false,
  targetBranchName: 'main',
  headCommit: true,
  disposeContext: {
    workspaceId: 'ws-1',
    workspacePath: '/tmp/ws',
    branch: 'feature',
    defaultBranch: 'main',
    remoteBranchExists: false,
    remoteBranchIsFork: false,
  },
};

beforeEach(() => {
  vi.clearAllMocks();
  getConfig.mockResolvedValue({ notifications: { suggest_dispose_after_push: true } });
  getDevStatus.mockResolvedValue({ source_workspace: '/somewhere/else' });
});

describe('handlePushCommits dispose suggestion', () => {
  it('offers workspace cleanup after a full push to main, like the Push to main button', async () => {
    pushCommits.mockResolvedValue(successResult());
    confirm.mockResolvedValue(true);
    renderSync();

    const pushed = await sync.handlePushCommits('ws-1', baseOpts);

    expect(pushed).toBe(true);
    expect(confirm).toHaveBeenCalledWith(
      expect.stringMatching(/Pushed 2 commits in 1 push to origin\/main\..*dispose this workspace/)
    );
    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })
    );
    expect(navigate).toHaveBeenCalledWith('/');
  });

  it('does not dispose when the cleanup prompt is declined', async () => {
    pushCommits.mockResolvedValue(successResult());
    confirm.mockResolvedValue(false);
    renderSync();

    await sync.handlePushCommits('ws-1', baseOpts);

    expect(confirm).toHaveBeenCalled();
    expect(disposeWorkspaceAll).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('only toasts for a partial push to main (unpushed commits remain)', async () => {
    pushCommits.mockResolvedValue(successResult());
    renderSync();

    await sync.handlePushCommits('ws-1', { ...baseOpts, headCommit: false });

    expect(confirm).not.toHaveBeenCalled();
    expect(toastSuccess).toHaveBeenCalledWith(expect.stringContaining('Pushed 2 commits'));
  });

  it('only toasts for a push to the branch target', async () => {
    pushCommits.mockResolvedValue(successResult({ target_branch: 'feature' }));
    renderSync();

    await sync.handlePushCommits('ws-1', {
      ...baseOpts,
      target: 'branch',
      targetBranchName: 'feature',
    });

    expect(confirm).not.toHaveBeenCalled();
    expect(toastSuccess).toHaveBeenCalledWith(expect.stringContaining('origin/feature'));
  });

  it('respects notifications.suggest_dispose_after_push = false', async () => {
    getConfig.mockResolvedValue({ notifications: { suggest_dispose_after_push: false } });
    pushCommits.mockResolvedValue(successResult());
    renderSync();

    await sync.handlePushCommits('ws-1', baseOpts);

    expect(confirm).not.toHaveBeenCalled();
    expect(toastSuccess).toHaveBeenCalled();
  });

  it('does not offer to dispose the live dev workspace', async () => {
    getDevStatus.mockResolvedValue({ source_workspace: '/tmp/ws' });
    pushCommits.mockResolvedValue(successResult());
    renderSync();

    await sync.handlePushCommits('ws-1', baseOpts);

    expect(confirm).not.toHaveBeenCalled();
    expect(toastSuccess).toHaveBeenCalledWith(expect.stringContaining('live in dev mode'));
  });
});

describe('handlePushToBranch', () => {
  beforeEach(() => {
    pushToBranch.mockReset();
    alert.mockClear();
    toastSuccess.mockClear();
  });

  it('toasts on success', async () => {
    pushToBranch.mockResolvedValue({ success: true });
    renderSync();
    await sync.handlePushToBranch('ws-1', 'feature/foo');
    expect(pushToBranch).toHaveBeenCalledWith('ws-1');
    expect(toastSuccess).toHaveBeenCalledWith('Pushed to origin/feature/foo');
    expect(alert).not.toHaveBeenCalled();
  });

  it('needs_confirm shows the Shift hint', async () => {
    pushToBranch.mockResolvedValue({ success: false, needs_confirm: true });
    renderSync();
    await sync.handlePushToBranch('ws-1', 'feature/foo');
    expect(alert).toHaveBeenCalledWith('Push rejected', expect.stringContaining('Shift'));
    expect(alert).toHaveBeenCalledWith(
      'Push rejected',
      expect.stringContaining('origin/feature/foo')
    );
  });

  it('behind failure shows the pull/merge message without the Shift hint', async () => {
    pushToBranch.mockResolvedValue({
      success: false,
      message: 'local branch is behind origin - pull or merge first',
    });
    renderSync();
    await sync.handlePushToBranch('ws-1', 'feature/foo');
    expect(alert).toHaveBeenCalledWith('Error', expect.stringContaining('behind'));
    const [, message] = alert.mock.calls[0];
    expect(message).not.toContain('Shift');
  });

  it('API error alerts with the error message', async () => {
    pushToBranch.mockRejectedValue(new Error('network down'));
    renderSync();
    await sync.handlePushToBranch('ws-1', 'feature/foo');
    expect(alert).toHaveBeenCalledWith('Error', expect.any(String));
  });
});

// Build a context literal for tests. `over` lets each case tweak one field.
const ctx = (over: Record<string, unknown> = {}) => ({
  workspaceId: 'ws-1',
  branch: 'feature/x',
  defaultBranch: 'main',
  remoteBranchExists: true,
  remoteBranchIsFork: false,
  ...over,
});

describe('post-push cleanup: delete remote branch', () => {
  beforeEach(() => {
    linearSyncToMain.mockResolvedValue({ success: true, branch: 'main', success_count: 2 });
  });

  it('offers the checkbox, checked, for an eligible workspace', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: true });
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    expect(confirmWithCheckbox).toHaveBeenCalledWith(
      expect.stringContaining('Are you done?'),
      expect.objectContaining({
        checkbox: {
          label: 'Delete remote branch',
          code: 'origin/feature/x',
          note: undefined,
          defaultChecked: true,
        },
      })
    );
    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: true })
    );
    expect(navigate).toHaveBeenCalledWith('/');
  });

  it('names the PR in the label when one is open', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: true });
    renderSync();

    await sync.handleLinearSyncToMain(ctx({ prNumber: 123 }));

    expect(confirmWithCheckbox).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({
        checkbox: {
          label: 'Delete remote branch',
          code: 'origin/feature/x',
          note: '(closes PR #123)',
          defaultChecked: true,
        },
      })
    );
  });

  it('passes deleteRemoteBranch false when the user clears the box', async () => {
    confirmWithCheckbox.mockResolvedValue({ confirmed: true, checked: false });
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })
    );
  });

  it.each([
    ['no remote branch', { remoteBranchExists: false }],
    ['fork branch', { remoteBranchIsFork: true }],
    ['on the default branch', { branch: 'main' }],
    ['remote host workspace', { remoteHostId: 'host-1' }],
    ['sapling workspace', { vcs: 'sapling' }],
  ])('falls back to a plain confirm when %s', async (_name, over) => {
    confirm.mockResolvedValue(true);
    renderSync();

    await sync.handleLinearSyncToMain(ctx(over));

    expect(confirm).toHaveBeenCalled();
    expect(confirmWithCheckbox).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(disposeWorkspaceAll).toHaveBeenCalledWith('ws-1', { deleteRemoteBranch: false })
    );
  });

  it('does not dispose when the user cancels', async () => {
    confirmWithCheckbox.mockResolvedValue(null);
    renderSync();

    await sync.handleLinearSyncToMain(ctx());

    expect(confirmWithCheckbox).toHaveBeenCalled();
    expect(disposeWorkspaceAll).not.toHaveBeenCalled();
  });
});
