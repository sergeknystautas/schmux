import { useCallback } from 'react';
import { useNavigate } from 'react-router';
import {
  linearSyncFromMain,
  linearSyncToMain,
  pushToBranch,
  pushCommits,
  linearSyncResolveConflict,
  disposeWorkspaceAll,
  getErrorMessage,
  getDevStatus,
  getConfig,
  LinearSyncError,
} from '../lib/api';
import { useModal } from '../components/ModalProvider';
import { useToast } from '../components/ToastProvider';
import { useSyncState } from '../contexts/SyncContext';
import { usePendingNavigation } from '../lib/navigation';
import type { WorkspaceResponse } from '../lib/types';

/** Everything the post-push cleanup prompt needs about a workspace. */
export interface DisposeSuggestionContext {
  workspaceId: string;
  workspacePath?: string;
  branch: string;
  defaultBranch: string;
  remoteBranchExists: boolean;
  remoteBranchIsFork: boolean;
  remoteHostId?: string;
  vcs?: string;
  prNumber?: number;
}

export function useSync() {
  const navigate = useNavigate();
  const { alert, confirm, show, confirmWithCheckbox } = useModal();
  const { error: toastError, success: toastSuccess } = useToast();
  const { clearLinearSyncResolveConflictState } = useSyncState();
  const { setPendingNavigation } = usePendingNavigation();

  const startConflictResolution = useCallback(
    async (workspaceId: string, conflictHash?: string): Promise<void> => {
      clearLinearSyncResolveConflictState(workspaceId);
      if (conflictHash) {
        const shortHash = conflictHash.slice(0, 7);
        const route = `/resolve-conflict/${workspaceId}/sys-resolve-conflict-${shortHash}`;
        setPendingNavigation({ type: 'tab', workspaceId, tabRoute: route });
      }
      try {
        await linearSyncResolveConflict(workspaceId);
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to start conflict resolution'));
      }
    },
    [setPendingNavigation, toastError, clearLinearSyncResolveConflictState]
  );

  const handleLinearSyncFromMain = useCallback(
    async (workspaceId: string, hash: string): Promise<void> => {
      try {
        const result = await linearSyncFromMain(workspaceId, hash);
        if (result.in_progress) {
          return;
        }
        if (result.success) {
          const branch = result.branch || 'main';
          const count = result.success_count ?? 0;
          toastSuccess(`Synced ${count} commit${count === 1 ? '' : 's'} from ${branch}.`);
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
            await startConflictResolution(workspaceId, result.conflicting_hash);
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
    [alert, show, startConflictResolution, toastSuccess]
  );

  // Post-push-to-main flow shared by the "Push to main" button and the
  // per-commit push modal: honors notifications.suggest_dispose_after_push,
  // refuses to suggest disposing the live dev workspace, and on confirm
  // disposes the workspace and navigates home. `summary` is the completed
  // "Pushed …" sentence (including trailing period).
  const suggestDisposeAfterPush = useCallback(
    async (ctx: DisposeSuggestionContext, summary: string): Promise<void> => {
      // Check config flag for dispose suggestion
      let suggestDispose = true;
      try {
        const config = await getConfig();
        suggestDispose = config.notifications?.suggest_dispose_after_push ?? true;
      } catch {
        // Config fetch failed — default to showing the prompt
      }

      if (!suggestDispose) {
        toastSuccess(summary);
        return;
      }

      // Check if this workspace is the live dev workspace
      let isDevLive = false;
      if (ctx.workspacePath) {
        try {
          const devStatus = await getDevStatus();
          isDevLive = devStatus.source_workspace === ctx.workspacePath;
        } catch {
          // Not in dev mode or dev status unavailable — ignore
        }
      }

      if (isDevLive) {
        toastSuccess(`${summary} This workspace is live in dev mode — switch before disposing.`);
        return;
      }

      const message = `${summary} Are you done? Shall I dispose this workspace and sessions?`;

      // The branch is deletable only when it exists on origin (not a fork), is
      // not the default branch, and lives in a local git workspace. The backend
      // re-checks all of this and holds the real safety property; this gate only
      // decides whether to offer the choice.
      const canDeleteRemoteBranch =
        ctx.remoteBranchExists &&
        !ctx.remoteBranchIsFork &&
        ctx.branch !== ctx.defaultBranch &&
        !ctx.remoteHostId &&
        (!ctx.vcs || ctx.vcs === 'git');

      let disposeConfirmed = false;
      let deleteRemoteBranch = false;

      if (canDeleteRemoteBranch) {
        // Split so the modal can render the ref in mono: the style guide reserves
        // --font-mono for branch names, and it separates the ref from the prose.
        const result = await confirmWithCheckbox(message, {
          checkbox: {
            label: 'Delete remote branch',
            code: `origin/${ctx.branch}`,
            note: ctx.prNumber ? `(closes PR #${ctx.prNumber})` : undefined,
            defaultChecked: true,
          },
        });
        disposeConfirmed = result?.confirmed ?? false;
        deleteRemoteBranch = disposeConfirmed && (result?.checked ?? false);
      } else {
        disposeConfirmed = (await confirm(message)) ?? false;
      }

      if (disposeConfirmed) {
        await disposeWorkspaceAll(ctx.workspaceId, { deleteRemoteBranch });
        navigate('/');
      }
    },
    [confirm, confirmWithCheckbox, navigate, toastSuccess]
  );

  const handleLinearSyncToMain = useCallback(
    async (ctx: DisposeSuggestionContext): Promise<void> => {
      try {
        const result = await linearSyncToMain(ctx.workspaceId);
        if (result.success) {
          const branch = ctx.defaultBranch || result.branch || 'main';
          const count = result.success_count ?? 0;
          await suggestDisposeAfterPush(
            ctx,
            `Pushed ${count} commit${count === 1 ? '' : 's'} to ${branch}.`
          );
        } else {
          await alert('Error', 'Sync failed.');
        }
      } catch (err) {
        await alert('Error', getErrorMessage(err, 'Failed to sync or dispose'));
      }
    },
    [alert, suggestDisposeAfterPush]
  );

  const handlePushToBranch = useCallback(
    async (workspaceId: string, branchName?: string): Promise<void> => {
      try {
        const result = await pushToBranch(workspaceId);
        if (result.success) {
          const branch = branchName || 'current branch';
          toastSuccess(`Pushed to origin/${branch}`);
        } else if (result.needs_confirm) {
          await alert(
            'Push rejected',
            `origin/${branchName || 'the branch'} has commits that aren't in your local branch. ` +
              'Hold Shift and click Push to branch to review what would be overwritten and force push.'
          );
        } else {
          await alert(
            'Error',
            result.message ||
              'Push failed. The remote branch may have commits that are not in your local branch.'
          );
        }
      } catch (err) {
        await alert('Error', getErrorMessage(err, 'Failed to push to branch'));
      }
    },
    [alert, toastSuccess]
  );

  const handlePushCommits = useCallback(
    async (
      workspaceId: string,
      opts: {
        hash: string;
        target: 'default' | 'branch';
        perCommit: boolean;
        targetBranchName: string;
        /** the selected commit is the branch head — a full push */
        headCommit: boolean;
        /** everything the post-push cleanup prompt needs; also skips the prompt for the live dev workspace */
        disposeContext: DisposeSuggestionContext;
      }
    ): Promise<boolean> => {
      const { hash, target, perCommit, targetBranchName } = opts;
      try {
        let result = await pushCommits(workspaceId, {
          hash,
          target,
          per_commit: perCommit,
          confirm: false,
        });

        if (result.needs_confirm) {
          const confirmed = await show(
            'Force push required',
            `origin/${targetBranchName} has commits that are not in your local branch. Force pushing will overwrite them:`,
            {
              confirmText: 'Force push',
              cancelText: 'Cancel',
              danger: true,
              detailedMessage: (result.diverged_commits ?? []).join('\n'),
            }
          );
          if (!confirmed) return false;
          result = await pushCommits(workspaceId, {
            hash,
            target,
            per_commit: perCommit,
            confirm: true,
          });
        }

        if (result.success) {
          const c = result.total_commits;
          const p = result.pushes_succeeded;
          const summary = `Pushed ${c} commit${c === 1 ? '' : 's'} in ${p} push${p === 1 ? '' : 'es'} to origin/${targetBranchName}.`;
          if (target === 'default' && opts.headCommit) {
            // A full push to main is the same milestone as the "Push to main"
            // button — offer the same workspace cleanup. Partial pushes leave
            // unpushed commits behind, so no dispose suggestion there.
            await suggestDisposeAfterPush(opts.disposeContext, summary);
          } else {
            toastSuccess(summary);
          }
          return true;
        }

        if (result.reason === 'push_rejected' && result.pushes_succeeded > 0) {
          // Partial: remote sits at the last successful push — a consistent state.
          await show(
            'Push partially completed',
            `Pushed ${result.pushes_succeeded} of ${result.total_commits} commits to origin/${targetBranchName}. Push of ${(result.failed_hash ?? '').slice(0, 7)} was rejected:`,
            {
              confirmText: 'OK',
              cancelText: null,
              detailedMessage: result.message || '',
              wide: true,
            }
          );
          return true;
        }

        await alert('Push failed', result.message || 'Push failed.');
        return false;
      } catch (err) {
        await alert('Push failed', getErrorMessage(err, 'Failed to push commits'));
        return false;
      }
    },
    [alert, show, toastSuccess, suggestDisposeAfterPush]
  );

  // Smart sync: chooses clean or conflict resolution based on workspace state
  const handleSmartSync = useCallback(
    async (workspace: WorkspaceResponse, hash: string): Promise<void> => {
      const hasKnownConflict =
        workspace.conflict_on_branch && workspace.conflict_on_branch === workspace.branch;

      if (hasKnownConflict) {
        // Known conflict on current branch - go straight to conflict resolution
        await startConflictResolution(workspace.id, hash);
      } else {
        // Try clean sync first
        await handleLinearSyncFromMain(workspace.id, hash);
      }
    },
    [startConflictResolution, handleLinearSyncFromMain]
  );

  return {
    startConflictResolution,
    handleLinearSyncFromMain,
    handleLinearSyncToMain,
    handlePushToBranch,
    handlePushCommits,
    handleSmartSync,
  };
}
