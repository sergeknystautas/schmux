import { useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  linearSyncFromMain,
  linearSyncToMain,
  linearSyncResolveConflict,
  disposeWorkspaceAll,
  getErrorMessage,
  getDevStatus,
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
        await alert('Error', err instanceof Error ? err.message : 'Failed to sync from main');
      }
    },
    [alert, show, startConflictResolution]
  );

  const handleLinearSyncToMain = useCallback(
    async (workspaceId: string, workspacePath?: string): Promise<void> => {
      try {
        const result = await linearSyncToMain(workspaceId);
        if (result.success) {
          const branch = result.branch || 'main';
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
              await alert('Success', 'Workspace and sessions disposed');
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
    handleSmartSync,
  };
}
