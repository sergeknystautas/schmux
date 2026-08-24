import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import {
  openVSCode,
  disposeWorkspace,
  disposeWorkspaceAll,
  getErrorMessage,
  setBackburner,
} from '../lib/api';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import { useSyncState } from '../contexts/SyncContext';
import { useRemoteAccess } from '../contexts/RemoteAccessContext';
import { useConfig } from '../contexts/ConfigContext';
import { useSync } from '../hooks/useSync';
import useDevStatus from '../hooks/useDevStatus';
import Tooltip from './Tooltip';
import CIStatusChip from './CIStatusChip';
import { ArrowDownIcon, ArrowUpIcon } from './Icons';
import type { WorkspaceResponse } from '../lib/types';
import { workspaceDisplayLabel } from '../lib/workspace-display';

type WorkspaceHeaderProps = {
  workspace: WorkspaceResponse;
  isDevLive?: boolean;
};

function githubWebUrl(repo: string): string | null {
  const match = repo.match(
    /^(?:https?:\/\/github\.com\/|git@github\.com:)([^/]+\/[^/]+?)(?:\.git)?\/?$/i
  );
  return match ? `https://github.com/${match[1]}` : null;
}

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
  const [togglingBackburner, setTogglingBackburner] = useState(false);
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

  const behind = workspace.behind ?? 0;
  const ahead = workspace.ahead ?? 0;
  const remoteBranchExists = workspace.remote_branch_exists ?? false;
  const remoteBranchIsFork = workspace.remote_branch_is_fork ?? false;
  const localUnique = workspace.local_unique_commits ?? 0;
  const remoteUnique = workspace.remote_unique_commits ?? 0;

  // Detect if the dashboard is being accessed from a different machine
  const isRemoteClient =
    window.location.hostname !== 'localhost' && window.location.hostname !== '127.0.0.1';
  const isRemoteAccess = isRemoteClient || simulateRemote;

  const handleOpenVSCode = async () => {
    setOpeningVSCode(true);
    try {
      if (isRemoteAccess) {
        // Remote client: request a vscode:// URI and open it locally
        const result = await openVSCode(workspace.id, { mode: 'uri' });
        if (!result.success) {
          await alert('Unable to open VS Code', result.message);
          return;
        }

        if (result.vscode_uri) {
          // Open the vscode:// URI — this triggers VS Code on the user's machine
          // to connect via SSH Remote to the server
          window.open(result.vscode_uri, '_blank');
          success('Opening VS Code Remote...');
        }

        // If a VS Code web server is running, offer that as an alternative
        if (result.server_info?.web_server_url) {
          success(`VS Code Server also available at ${result.server_info.web_server_url}`);
        }
      } else {
        // Local client: execute VS Code on the server (original behavior)
        const result = await openVSCode(workspace.id);
        if (!result.success) {
          await alert('Unable to open VS Code', result.message);
        }
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

  const handleToggleBackburner = async () => {
    setTogglingBackburner(true);
    try {
      await setBackburner(workspace.id, !workspace.backburner);
    } catch {
      // Error will show via WebSocket state update failure
    } finally {
      setTogglingBackburner(false);
    }
  };

  // For remote workspaces, use hostname from sessions if branch matches repo (fallback case)
  const isRemote = workspace.sessions?.some((s) => s.remote_host_id);
  const remoteHostname = workspace.sessions?.find((s) => s.remote_hostname)?.remote_hostname;
  const remoteAwareBranch =
    isRemote && remoteHostname && workspace.branch === workspace.repo
      ? remoteHostname
      : workspace.branch;
  // Compose label-aware fallback on top of remote-aware branch
  const displayBranch = workspaceDisplayLabel(workspace, remoteAwareBranch);

  // Build the workspace name line: include flavor for remote workspaces
  const remoteFlavorName = workspace.remote_flavor_name;
  const remoteFlavor = workspace.remote_flavor;
  const displayName =
    isRemote && (remoteFlavorName || remoteFlavor)
      ? remoteFlavorName || remoteFlavor
      : workspace.id;

  // Git-specific UI should only appear for git-managed workspaces
  const isGit = !workspace.vcs || workspace.vcs === 'git';
  const isNewRepo = isGit && (workspace.repo?.startsWith('local:') ?? false);

  const hasRunningSessions = workspace.sessions?.some((s) => s.running) ?? false;

  const githubUrl = githubWebUrl(workspace.repo);
  const vsCodeTooltip = isRemoteAccess ? 'Open in VS Code (Remote-SSH)' : 'Open in VS Code';

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
            {isNewRepo && (
              <Tooltip content="Not connected to a remote repository yet — connect it from the commit graph">
                <Link to={`/commits/${workspace.id}`} className="app-header__git-status">
                  {branchIcon}
                  <span className="text-muted">new repo</span>
                </Link>
              </Tooltip>
            )}
            {isGit && !isNewRepo && (
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
                ) : remoteBranchIsFork ? (
                  <Tooltip content={`${remoteUnique} behind fork, ${localUnique} ahead of fork`}>
                    <span className="app-header__git-status">
                      {branchIcon}
                      <span className="app-header__git-pair">
                        {remoteUnique}
                        {arrowDown}
                      </span>{' '}
                      <span className="app-header__git-pair">
                        {localUnique}
                        {arrowUp}
                      </span>{' '}
                      <span style={{ opacity: 0.6 }}>(fork)</span>
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
            {workspace.ci_status && (
              <CIStatusChip status={workspace.ci_status} url={workspace.ci_url} />
            )}
            {workspace.pr_number && workspace.pr_url ? (
              <Tooltip content={`Open pull request #${workspace.pr_number}`}>
                <a
                  href={workspace.pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="app-header__git-status app-header__pr-link"
                >
                  PR #{workspace.pr_number}
                </a>
              </Tooltip>
            ) : null}
          </span>
          <span className="app-header__name">{displayName}</span>
        </div>
        <div className="app-header__actions">
          {config.backburner_enabled && (
            <Tooltip content={workspace.backburner ? 'Wake up' : 'Backburner'}>
              <button
                className="btn btn--sm btn--ghost btn--bordered"
                style={
                  workspace.backburner
                    ? {
                        background: 'rgba(184,169,224,0.1)',
                        borderColor: 'rgba(184,169,224,0.4)',
                      }
                    : undefined
                }
                disabled={togglingBackburner}
                onClick={handleToggleBackburner}
                aria-label={workspace.backburner ? 'Wake up' : 'Backburner'}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke={workspace.backburner ? '#b8a9e0' : 'currentColor'}
                  strokeWidth="2.5"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <path d="M2 10h6l-6 8h6" />
                  <path d="M10 5h5l-5 7h5" />
                  <path d="M16 2h5l-5 6h5" />
                </svg>
              </button>
            </Tooltip>
          )}
          {githubUrl && (
            <Tooltip content="View on GitHub">
              <a
                href={githubUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="btn btn--sm btn--ghost btn--bordered"
                aria-label={`Open ${workspace.id} on GitHub`}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  style={{ fill: 'currentColor' }}
                  aria-hidden="true"
                >
                  <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                </svg>
              </a>
            </Tooltip>
          )}
          <Tooltip content={vsCodeTooltip}>
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
              disabled={
                isLocked || isDevLive || hasRunningSessions || workspace.status === 'disposing'
              }
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
