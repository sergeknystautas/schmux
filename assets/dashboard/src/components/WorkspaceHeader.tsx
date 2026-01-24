import React, { useState } from 'react';
import { openVSCode, disposeWorkspace, getErrorMessage } from '../lib/api';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useSessions } from '../contexts/SessionsContext';
import Tooltip from './Tooltip';
import VSCodeResultModal from './VSCodeResultModal';
import type { WorkspaceResponse, OpenVSCodeResponse } from '../lib/types';

type WorkspaceHeaderProps = {
  workspace: WorkspaceResponse;
};

export default function WorkspaceHeader({ workspace }: WorkspaceHeaderProps) {
  const { refresh } = useSessions();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const [vsCodeResult, setVSCodeResult] = useState<OpenVSCodeResponse | null>(null);
  const [openingVSCode, setOpeningVSCode] = useState(false);

  // Git branch icon SVG
  const branchIcon = (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
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
      setVSCodeResult(result);
    } catch (err) {
      setVSCodeResult({ success: false, message: getErrorMessage(err, 'Failed to open VS Code') });
    } finally {
      setOpeningVSCode(false);
    }
  };

  const handleDisposeWorkspace = async () => {
    const accepted = await confirm(`Dispose workspace ${workspace.id}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeWorkspace(workspace.id);
      success('Workspace disposed');
      refresh();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to dispose workspace'));
    }
  };

  return (
    <>
      <div className="workspace-header">
        <div className="workspace-header__info">
          <span className="workspace-header__name">{workspace.id}</span>
          <span className="workspace-header__meta">
            {workspace.branch_url ? (
              <Tooltip content="View branch in git">
                <a
                  href={workspace.branch_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="workspace-header__branch-link"
                >
                  {branchIcon}
                  {workspace.branch}
                </a>
              </Tooltip>
            ) : (
              <span className="workspace-header__branch">
                {branchIcon}
                {workspace.branch}
              </span>
            )}
            <Tooltip content={`${behind} behind, ${ahead} ahead`}>
              <span className="workspace-header__git-status">
                {behind} | {ahead}
              </span>
            </Tooltip>
          </span>
        </div>
        <div className="workspace-header__actions">
          <Tooltip content="Open in VS Code">
            <button
              className="btn btn--sm btn--ghost btn--bordered"
              disabled={openingVSCode}
              onClick={handleOpenVSCode}
              aria-label={`Open ${workspace.id} in VS Code`}
            >
              {openingVSCode ? (
                <div className="spinner--small"></div>
              ) : (
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                  <path d="M23.15 2.587L18.21.21a1.494 1.494 0 0 0-1.705.29l-9.46 8.63-4.12-3.128a.999.999 0 0 0-1.276.057L.327 7.261A1 1 0 0 0 .326 8.74L3.899 12 .326 15.26a1 1 0 0 0 .001 1.479L1.65 17.94a.999.999 0 0 0 1.276.057l4.12-3.128 9.46 8.63a1.492 1.492 0 0 0 1.704.29l4.942-2.377A1.5 1.5 0 0 0 24 20.06V3.939a1.5 1.5 0 0 0-.85-1.352zm-5.146 14.861L10.826 12l7.178-5.448v10.896z" fill="#007ACC"/>
                </svg>
              )}
            </button>
          </Tooltip>
          <Tooltip content="Dispose workspace and all sessions" variant="warning">
            <button
              className="btn btn--sm btn--ghost btn--danger btn--bordered"
              onClick={handleDisposeWorkspace}
              aria-label={`Dispose ${workspace.id}`}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
            </button>
          </Tooltip>
        </div>
      </div>

      {vsCodeResult && (
        <VSCodeResultModal
          success={vsCodeResult.success}
          message={vsCodeResult.message}
          onClose={() => setVSCodeResult(null)}
        />
      )}
    </>
  );
}
