import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  linearSyncFromMain,
  linearSyncToMain,
  pushToBranch,
  linearSyncResolveConflict,
  disposeWorkspaceAll,
  getErrorMessage,
  getDevStatus,
  LinearSyncError,
} from '../lib/api';
import { useModal } from '../components/ModalProvider';
import { useToast } from '../components/ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import type { WorkspaceResponse } from '../lib/types';

export function useSync() {
  const navigate = useNavigate();
  const { alert, confirm, show } = useModal();
  const { error: toastError } = useToast();
  const { clearLinearSyncResolveConflictState } = useSessions();

  const startConflictResolution = useCallback(
    async (workspaceId: string): Promise<void> => {
      clearLinearSyncResolveConflictState(workspaceId);
      navigate(`/resolve-conflict/${workspaceId}`);
      try {
        await linearSyncResolveConflict(workspaceId);
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to start conflict resolution'));
      }
    },
    [navigate, toastError, clearLinearSyncResolveConflictState]
  );

  const handleLinearSyncFromMain = useCallback(
    async (workspaceId: string): Promise<void> => {
      try {
        const result = await linearSyncFromMain(workspaceId);
        if (result.success) {
          const branch = result.branch || 'main';
          const count = result.success_count ?? 0;
          await alert('Success', `Synced ${count} commit${count === 1 ? '' : 's'} from ${branch}.`);
        } else if (result.conflicting_hash) {
          const commitCount = result.success_count ?? 0;
          const resolveConfirmed = await show(
            'Unable to fully sync',
            `We were able to fast forward ${commitCount} commits cleanly. You can have an agent resolve the conflict at ${result.conflicting_hash}.`,
            {
              confirmText: 'Resolve',
              cancelText: 'Close',
              danger: true,
            }
          );
          if (resolveConfirmed) {
            await startConflictResolution(workspaceId);
          }
        } else {
          await alert('Error', 'Sync failed.');
        }
      } catch (err) {
        // Check if it's a pre-commit hook error (using custom error type)
        if (err instanceof LinearSyncError && err.isPreCommitHookError) {
          await show(
            'Pre-commit Hook Failed',
            'To rebase commits, we create a WIP commit which is triggering pre-commit hooks that fail. Fix the errors shown below and try again.',
            {
              confirmText: 'OK',
              cancelText: null,
              detailedMessage: err.preCommitErrorDetail || '',
              wide: true,
            }
          );
          return;
        }
        await alert('Error', getErrorMessage(err, 'Failed to sync from main'));
      }
    },
    [alert, show, startConflictResolution]
  );

  const handleLinearSyncToMain = useCallback(
    async (workspaceId: string, defaultBranch?: string, workspacePath?: string): Promise<void> => {
      try {
        const result = await linearSyncToMain(workspaceId);
        if (result.success) {
          const branch = defaultBranch || result.branch || 'main';
          const count = result.success_count ?? 0;

          // Check if this workspace is the live dev workspace
          let isDevLive = false;
          if (workspacePath) {
            try {
              const devStatus = await getDevStatus();
              isDevLive = devStatus.source_workspace === workspacePath;
            } catch {
              // Not in dev mode or dev status unavailable — ignore
            }
          }

          if (isDevLive) {
            await alert(
              'Pushed',
              `Pushed ${count} commit${count === 1 ? '' : 's'} to ${branch}. This workspace is currently live in dev mode — switch to another workspace before disposing it.`
            );
          } else {
            const disposeConfirmed = await confirm(
              `Pushed ${count} commit${count === 1 ? '' : 's'} to ${branch}. Are you done? Shall I dispose this workspace and sessions?`
            );
            if (disposeConfirmed) {
              await disposeWorkspaceAll(workspaceId);
              navigate('/');
            }
          }
        } else {
          await alert('Error', 'Sync failed.');
        }
      } catch (err) {
        await alert('Error', getErrorMessage(err, 'Failed to sync or dispose'));
      }
    },
    [alert, confirm, navigate]
  );

  const handlePushToBranch = useCallback(
    async (workspaceId: string, branchName?: string): Promise<void> => {
      try {
        const result = await pushToBranch(workspaceId);
        if (result.success) {
          const branch = branchName || 'current branch';
          await alert('Success', `Pushed to origin/${branch}`);
        } else {
          await alert(
            'Error',
            'Push failed. The remote branch may have commits that are not in your local branch.'
          );
        }
      } catch (err) {
        await alert('Error', getErrorMessage(err, 'Failed to push to branch'));
      }
    },
    [alert]
  );

  // Smart sync: chooses clean or conflict resolution based on workspace state
  const handleSmartSync = useCallback(
    async (workspace: WorkspaceResponse): Promise<void> => {
      const hasKnownConflict =
        workspace.conflict_on_branch && workspace.conflict_on_branch === workspace.branch;

      if (hasKnownConflict) {
        // Known conflict on current branch - go straight to conflict resolution
        await startConflictResolution(workspace.id);
      } else {
        // Try clean sync first
        await handleLinearSyncFromMain(workspace.id);
      }
    },
    [startConflictResolution, handleLinearSyncFromMain]
  );

  return {
    startConflictResolution,
    handleLinearSyncFromMain,
    handleLinearSyncToMain,
    handlePushToBranch,
    handleSmartSync,
  };
}
