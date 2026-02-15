import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useSync } from './useSync';

// --- Mocks ---

const navigate = vi.fn();
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

const alert = vi.fn().mockResolvedValue(undefined);
const confirm = vi.fn().mockResolvedValue(true);
const show = vi.fn().mockResolvedValue(true);
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert, confirm, show }),
}));

const toastError = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ error: toastError }),
}));

const clearLinearSyncResolveConflictState = vi.fn();
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ clearLinearSyncResolveConflictState }),
}));

const mockLinearSyncFromMain = vi.fn();
const mockLinearSyncToMain = vi.fn();
const mockDisposeWorkspaceAll = vi.fn();
const mockGetDevStatus = vi.fn();
vi.mock('../lib/api', () => ({
  linearSyncFromMain: (...args: unknown[]) => mockLinearSyncFromMain(...args),
  linearSyncToMain: (...args: unknown[]) => mockLinearSyncToMain(...args),
  pushToBranch: vi.fn(),
  linearSyncResolveConflict: vi.fn(),
  disposeWorkspaceAll: (...args: unknown[]) => mockDisposeWorkspaceAll(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
  getDevStatus: (...args: unknown[]) => mockGetDevStatus(...args),
  LinearSyncError: class LinearSyncError extends Error {
    isPreCommitHookError = false;
    preCommitErrorDetail = '';
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
});

// --- Tests ---

describe('useSync', () => {
  describe('handleLinearSyncFromMain', () => {
    it('does not show a success dialog when sync succeeds', async () => {
      mockLinearSyncFromMain.mockResolvedValue({
        success: true,
        branch: 'main',
        success_count: 3,
      });

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncFromMain('ws-1'));

      expect(alert).not.toHaveBeenCalled();
      expect(show).not.toHaveBeenCalled();
    });

    it('shows conflict dialog when sync returns conflicting_hash', async () => {
      mockLinearSyncFromMain.mockResolvedValue({
        success: false,
        conflicting_hash: 'abc123',
        success_count: 2,
      });

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncFromMain('ws-1'));

      expect(show).toHaveBeenCalledWith(
        'Unable to fully sync',
        expect.stringContaining('2 commits'),
        expect.objectContaining({ confirmText: 'Resolve' })
      );
    });

    it('shows error dialog on generic failure', async () => {
      mockLinearSyncFromMain.mockResolvedValue({
        success: false,
      });

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncFromMain('ws-1'));

      expect(alert).toHaveBeenCalledWith('Error', 'Sync failed.');
    });
  });

  describe('handleLinearSyncToMain', () => {
    it('does not show success dialog after dispose — navigates directly to /', async () => {
      mockLinearSyncToMain.mockResolvedValue({
        success: true,
        branch: 'main',
        success_count: 5,
      });
      confirm.mockResolvedValue(true);
      mockDisposeWorkspaceAll.mockResolvedValue(undefined);

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncToMain('ws-1'));

      expect(confirm).toHaveBeenCalled();
      expect(mockDisposeWorkspaceAll).toHaveBeenCalledWith('ws-1');
      // No success alert after dispose
      expect(alert).not.toHaveBeenCalled();
      expect(navigate).toHaveBeenCalledWith('/');
    });

    it('does not dispose when user cancels the confirm dialog', async () => {
      mockLinearSyncToMain.mockResolvedValue({
        success: true,
        branch: 'main',
        success_count: 1,
      });
      confirm.mockResolvedValue(false);

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncToMain('ws-1'));

      expect(mockDisposeWorkspaceAll).not.toHaveBeenCalled();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('shows dev-mode warning when workspace is live', async () => {
      mockLinearSyncToMain.mockResolvedValue({
        success: true,
        branch: 'main',
        success_count: 2,
      });
      mockGetDevStatus.mockResolvedValue({
        source_workspace: '/tmp/ws',
      });

      const { result } = renderHook(() => useSync());
      await act(() => result.current.handleLinearSyncToMain('ws-1', 'main', '/tmp/ws'));

      expect(alert).toHaveBeenCalledWith('Pushed', expect.stringContaining('dev mode'));
      expect(confirm).not.toHaveBeenCalled();
    });
  });
});
