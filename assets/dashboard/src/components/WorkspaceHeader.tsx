import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { openVSCode, disposeWorkspace, disposeWorkspaceAll, getErrorMessage } from '../lib/api';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig } from '../contexts/ConfigContext';
import { useSync } from '../hooks/useSync';
import useDevStatus from '../hooks/useDevStatus';
import Tooltip from './Tooltip';
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
  const { linearSyncResolveConflictStates } = useSessions();
  const { handleLinearSyncFromMain, handleLinearSyncToMain, startConflictResolution } = useSync();
  const [openingVSCode, setOpeningVSCode] = useState(false);
  const { devStatus } = useDevStatus();

  // Check if resolve conflict is in progress for this workspace
  const crState = linearSyncResolveConflictStates[workspace.id];
  const resolveInProgress = crState?.status === 'in_progress';

  // Dev mode guard: use explicit prop if provided, otherwise compute from hook
  const isDevLive =
    isDevLiveProp ??
    (devStatus?.source_workspace === workspace.path && !!devStatus?.source_workspace);

  // Git branch icon SVG
  const branchIcon = (
    <svg
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
    >
      <line x1="6" y1="3" x2="6" y2="15"></line>
      <circle cx="18" cy="6" r="3"></circle>
      <circle cx="6" cy="18" r="3"></circle>
      <path d="M18 9a9 9 0 0 1-9 9"></path>
    </svg>
  );

  const behind = workspace.git_behind ?? 0;
  const ahead = workspace.git_ahead ?? 0;

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
      toastError(getErrorMessage(err, 'Failed to dispose workspace'));
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
                  {isGit && branchIcon}
                  {displayBranch}
                </a>
              </Tooltip>
            ) : (
              <span className="app-header__branch">
                {isGit && branchIcon}
                {displayBranch}
              </span>
            )}
            {isGit && (
              <Tooltip content={`${behind} behind, ${ahead} ahead`}>
                <span className="app-header__git-status">
                  {behind} | {ahead}
                </span>
              </Tooltip>
            )}
          </span>
          <span className="app-header__name">{displayName}</span>
        </div>
        <div className="app-header__actions">
          <Tooltip content="Open in VS Code">
            <button
              className="btn btn--sm btn--ghost btn--bordered"
              disabled={openingVSCode}
              onClick={handleOpenVSCode}
              aria-label={`Open ${workspace.id} in VS Code`}
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
          <Tooltip
            content={
              isDevLive
                ? 'Cannot dispose workspace while live in dev mode'
                : 'Dispose workspace and all sessions'
            }
            variant={isDevLive ? undefined : 'warning'}
          >
            <button
              className="btn btn--sm btn--ghost btn--danger btn--bordered"
              onClick={handleDisposeWorkspace}
              disabled={resolveInProgress || isDevLive}
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
