import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { openVSCode, disposeWorkspace, disposeWorkspaceAll, getErrorMessage } from '../lib/api';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import { useSyncState } from '../contexts/SyncContext';
import { useRemoteAccess } from '../contexts/RemoteAccessContext';
import { useConfig } from '../contexts/ConfigContext';
import { useSync } from '../hooks/useSync';
import useDevStatus from '../hooks/useDevStatus';
import Tooltip from './Tooltip';
import { ArrowDownIcon, ArrowUpIcon } from './Icons';
import type { WorkspaceResponse } from '../lib/types';

type WorkspaceHeaderProps = {
  workspace: WorkspaceResponse;
  isDevLive?: boolean;
};

export default function WorkspaceHeader({
  workspace,
  isDevLive: isDevLiveProp,
}: WorkspaceHeaderProps) {
  const navigate = useNavigate();
  const { alert, confirm } = useModal();
  const { success, error: toastError } = useToast();
  const { config } = useConfig();
  const { linearSyncResolveConflictStates, workspaceLockStates } = useSyncState();
  const { simulateRemote } = useRemoteAccess();
  const { handleLinearSyncFromMain, handleLinearSyncToMain, startConflictResolution } = useSync();
  const [openingVSCode, setOpeningVSCode] = useState(false);
  const { devStatus } = useDevStatus();

  // Check if workspace is locked (resolve conflict or clean sync in progress)
  const crState = linearSyncResolveConflictStates[workspace.id];
  const resolveInProgress = crState?.status === 'in_progress';
  const lockState = workspaceLockStates[workspace.id];
  const isLocked = resolveInProgress || lockState?.locked;

  // Dev mode guard: use explicit prop if provided, otherwise compute from hook
  const isDevLive =
    isDevLiveProp ??
    (devStatus?.source_workspace === workspace.path && !!devStatus?.source_workspace);

  const arrowDown = ArrowDownIcon;
  const arrowUp = ArrowUpIcon;

  // Git branch icon SVG
  const branchIcon = (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="var(--color-text)"
      strokeWidth="2"
      style={{ marginRight: 4 }}
    >
      <line x1="6" y1="3" x2="6" y2="15"></line>
      <circle cx="18" cy="6" r="3"></circle>
      <circle cx="6" cy="18" r="3"></circle>
      <path d="M18 9a9 9 0 0 1-9 9"></path>
    </svg>
  );

  // Remote tracking icon SVG (merge/PR arrow style)
  const remoteIcon = (
    <svg
      width="14"
      height="14"
      viewBox="-1 -1 26 26"
      fill="none"
      stroke="var(--color-text)"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ marginRight: 4 }}
    >
      <circle cx="3" cy="5" r="3" />
      <line x1="3" y1="8" x2="3" y2="17" />
      <circle cx="3" cy="20" r="3" />
      <circle cx="21" cy="20" r="3" />
      <line x1="21" y1="17" x2="21" y2="12" />
      <path d="M21 12c0-5-7-7-10-7" />
      <polyline points="13,3 11,5 13,7" />
    </svg>
  );

  const behind = workspace.git_behind ?? 0;
  const ahead = workspace.git_ahead ?? 0;
  const remoteBranchExists = workspace.remote_branch_exists ?? false;
  const localUnique = workspace.local_unique_commits ?? 0;
  const remoteUnique = workspace.remote_unique_commits ?? 0;

  const handleOpenVSCode = async () => {
    setOpeningVSCode(true);
    try {
      const result = await openVSCode(workspace.id);
      if (!result.success) {
        await alert('Unable to open VS Code', result.message);
      }
    } catch (err) {
      await alert('Unable to open VS Code', getErrorMessage(err, 'Failed to open VS Code'));
    } finally {
      setOpeningVSCode(false);
    }
  };

  const handleDisposeWorkspace = async () => {
    const accepted = await confirm(`Dispose workspace ${workspace.id}?`, { danger: true });
    if (!accepted) return;

    try {
      // For disconnected remote workspaces, dispose all sessions too
      const isRemoteDisconnected =
        workspace.remote_host_id && workspace.remote_host_status !== 'connected';
      if (isRemoteDisconnected) {
        await disposeWorkspaceAll(workspace.id);
      } else {
        await disposeWorkspace(workspace.id);
      }
      success('Workspace disposed');
      navigate('/');
    } catch (err) {
      await alert('Dispose Failed', getErrorMessage(err, 'Failed to dispose workspace'));
    }
  };

  // For remote workspaces, use hostname from sessions if branch matches repo (fallback case)
  const isRemote = workspace.sessions?.some((s) => s.remote_host_id);
  const remoteHostname = workspace.sessions?.find((s) => s.remote_hostname)?.remote_hostname;
  const displayBranch =
    isRemote && remoteHostname && workspace.branch === workspace.repo
      ? remoteHostname
      : workspace.branch;

  // Build the workspace name line: include flavor for remote workspaces
  const remoteFlavorName = workspace.remote_flavor_name;
  const remoteFlavor = workspace.remote_flavor;
  const displayName =
    isRemote && (remoteFlavorName || remoteFlavor)
      ? remoteFlavorName || remoteFlavor
      : workspace.id;

  // Git-specific UI should only appear for git-managed workspaces
  const isGit = !workspace.vcs || workspace.vcs === 'git';

  // Hide local-only actions (VS Code) when accessing remotely
  const isRemoteClient =
    window.location.hostname !== 'localhost' && window.location.hostname !== '127.0.0.1';
  const isRemoteAccess = isRemoteClient || simulateRemote;

  const hasRunningSessions = workspace.sessions?.some((s) => s.running) ?? false;

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <span className="app-header__meta">
            {workspace.branch_url ? (
              <Tooltip content="View branch in git">
                <a
                  href={workspace.branch_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="app-header__branch-link"
                >
                  {displayBranch}
                </a>
              </Tooltip>
            ) : (
              <span className="app-header__branch">{displayBranch}</span>
            )}
            {isGit && (
              <>
                <Tooltip content={`${behind} behind main, ${ahead} ahead of main`}>
                  <span className="app-header__git-status">
                    {remoteIcon}
                    <span className="app-header__git-pair">
                      {behind}
                      {arrowDown}
                    </span>{' '}
                    <span className="app-header__git-pair">
                      {ahead}
                      {arrowUp}
                    </span>
                  </span>
                </Tooltip>
                {!remoteBranchExists ? (
                  <Tooltip content="Branch does not exist on remote">
                    <span className="app-header__git-status">
                      {branchIcon}
                      <span style={{ opacity: 0.6 }}>(local only)</span>
                    </span>
                  </Tooltip>
                ) : (
                  <Tooltip
                    content={`${remoteUnique} behind remote, ${localUnique} ahead of remote`}
                  >
                    <span className="app-header__git-status">
                      {branchIcon}
                      <span className="app-header__git-pair">
                        {remoteUnique}
                        {arrowDown}
                      </span>{' '}
                      <span className="app-header__git-pair">
                        {localUnique}
                        {arrowUp}
                      </span>
                    </span>
                  </Tooltip>
                )}
              </>
            )}
          </span>
          <span className="app-header__name">{displayName}</span>
        </div>
        <div className="app-header__actions">
          {!isRemoteAccess && (
            <Tooltip content="Open in VS Code">
              <button
                className="btn btn--sm btn--ghost btn--bordered vscode-btn"
                disabled={openingVSCode}
                onClick={handleOpenVSCode}
                aria-label={`Open ${workspace.id} in VS Code`}
                data-tour="vscode-btn"
              >
                {openingVSCode ? (
                  <div className="spinner spinner--small"></div>
                ) : (
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    xmlns="http://www.w3.org/2000/svg"
                  >
                    <path
                      d="M23.15 2.587L18.21.21a1.494 1.494 0 0 0-1.705.29l-9.46 8.63-4.12-3.128a.999.999 0 0 0-1.276.057L.327 7.261A1 1 0 0 0 .326 8.74L3.899 12 .326 15.26a1 1 0 0 0 .001 1.479L1.65 17.94a.999.999 0 0 0 1.276.057l4.12-3.128 9.46 8.63a1.492 1.492 0 0 0 1.704.29l4.942-2.377A1.5 1.5 0 0 0 24 20.06V3.939a1.5 1.5 0 0 0-.85-1.352zm-5.146 14.861L10.826 12l7.178-5.448v10.896z"
                      fill="#007ACC"
                    />
                  </svg>
                )}
              </button>
            </Tooltip>
          )}
          <Tooltip
            content={
              isDevLive
                ? 'Cannot dispose workspace while live in dev mode'
                : hasRunningSessions
                  ? 'Stop all sessions before disposing'
                  : 'Dispose workspace and all sessions'
            }
            variant={isDevLive || hasRunningSessions ? undefined : 'warning'}
          >
            <button
              className="btn btn--sm btn--ghost btn--danger btn--bordered"
              onClick={handleDisposeWorkspace}
              disabled={isLocked || isDevLive || hasRunningSessions}
              aria-label={`Dispose ${workspace.id}`}
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </button>
          </Tooltip>
        </div>
      </div>
    </>
  );
}
