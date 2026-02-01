import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { openVSCode, disposeWorkspaceAll, disposeWorkspace, getErrorMessage, linearSyncFromMain, linearSyncToMain, linearSyncResolveConflict } from '../lib/api';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig } from '../contexts/ConfigContext';
import Tooltip from './Tooltip';
import VSCodeResultModal from './VSCodeResultModal';
import SyncResultModal from './SyncResultModal';
import type { WorkspaceResponse, OpenVSCodeResponse } from '../lib/types';

type WorkspaceHeaderProps = {
  workspace: WorkspaceResponse;
};

export default function WorkspaceHeader({ workspace }: WorkspaceHeaderProps) {
  const navigate = useNavigate();
  const { confirm } = useModal();
  const { success, error: toastError } = useToast();
  const { config } = useConfig();
  const [vsCodeResult, setVSCodeResult] = useState<OpenVSCodeResponse | null>(null);
  const [syncResult, setSyncResult] = useState<{ success: boolean; message: string; navigateTo?: string } | null>(null);
  const [openingVSCode, setOpeningVSCode] = useState(false);

  // Git status dropdown state
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const [rebasing, setRebasing] = useState(false);
  const [merging, setMerging] = useState(false);
  const [resolving, setResolving] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const gitStatusRef = useRef<HTMLDivElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

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
      if (!result.success) {
        setVSCodeResult(result);
      }
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
      navigate('/');
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to dispose workspace'));
    }
  };

  // Calculate menu position when dropdown opens
  useEffect(() => {
    if (isDropdownOpen && gitStatusRef.current) {
      const rect = gitStatusRef.current.getBoundingClientRect();
      const gap = 4;
      const estimatedMenuHeight = 120; // Approximate height for single menu item

      const spaceBelow = window.innerHeight - rect.bottom - gap;
      const spaceAbove = rect.top - gap;

      const shouldPlaceAbove = spaceBelow < estimatedMenuHeight && spaceAbove > spaceBelow;
      setPlacementAbove(shouldPlaceAbove);

      if (shouldPlaceAbove) {
        setMenuPosition({
          top: rect.top - gap,
          left: rect.right,
        });
      } else {
        setMenuPosition({
          top: rect.bottom + gap,
          left: rect.right,
        });
      }
    }
  }, [isDropdownOpen]);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (gitStatusRef.current?.contains(target)) return;
      if (menuRef.current?.contains(target)) return;
      setIsDropdownOpen(false);
    };

    if (isDropdownOpen) {
      document.addEventListener('click', handleClickOutside, true);
    }

    return () => {
      document.removeEventListener('click', handleClickOutside, true);
    };
  }, [isDropdownOpen]);

  const handleLinearSyncFromMain = async () => {
    setIsDropdownOpen(false);
    setRebasing(true);

    try {
      const result = await linearSyncFromMain(workspace.id);
      setSyncResult({ success: result.success, message: result.message });
    } catch (err) {
      setSyncResult({ success: false, message: getErrorMessage(err, 'Failed to sync from main') });
    } finally {
      setRebasing(false);
    }
  };

  const handleLinearSyncToMain = async () => {
    setIsDropdownOpen(false);
    setMerging(true);

    try {
      const result = await linearSyncToMain(workspace.id);
      if (result.success) {
        const disposeConfirmed = await confirm('Are you done? Shall I dispose this workspace and sessions?', {
          detailedMessage: result.message
        });
        if (disposeConfirmed) {
          await disposeWorkspaceAll(workspace.id);
          setSyncResult({ success: true, message: 'Workspace and sessions disposed' });
          navigate('/');
        }
      } else {
        setSyncResult({ success: false, message: result.message });
      }
    } catch (err) {
      setSyncResult({ success: false, message: getErrorMessage(err, 'Failed to sync or dispose') });
    } finally {
      setMerging(false);
    }
  };

  const handleLinearSyncResolveConflict = async () => {
    setIsDropdownOpen(false);
    setResolving(true);

    try {
      const result = await linearSyncResolveConflict(workspace.id);
      if (result.success) {
        setSyncResult({
          success: true,
          message: result.message + (result.session_id ? ' - spawned session for conflict resolution' : ''),
          navigateTo: result.session_id
        });
      } else {
        setSyncResult({ success: false, message: result.message });
      }
    } catch (err) {
      setSyncResult({ success: false, message: getErrorMessage(err, 'Failed to resolve conflict') });
    } finally {
      setResolving(false);
    }
  };

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
                  {branchIcon}
                  {workspace.branch}
                </a>
              </Tooltip>
            ) : (
              <span className="app-header__branch">
                {branchIcon}
                {workspace.branch}
              </span>
            )}
            <div style={{ display: 'inline-flex' }} ref={gitStatusRef}>
              <Tooltip content={`${behind} behind, ${ahead} ahead`}>
                <span
                  className="app-header__git-status"
                  onClick={() => setIsDropdownOpen(!isDropdownOpen)}
                  style={{ cursor: 'pointer' }}
                >
                  {behind} | {ahead}
                </span>
              </Tooltip>
            </div>
          </span>
          <span className="app-header__name">{workspace.id}</span>
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

      {syncResult && (
        <SyncResultModal
          success={syncResult.success}
          message={syncResult.message}
          navigateTo={syncResult.navigateTo}
          onClose={() => setSyncResult(null)}
        />
      )}

      {isDropdownOpen && !rebasing && !merging && !resolving && createPortal(
        <div
          ref={menuRef}
          className={`spawn-dropdown__menu spawn-dropdown__menu--portal${placementAbove ? ' spawn-dropdown__menu--above' : ''}`}
          role="menu"
          style={{
            position: 'fixed',
            top: placementAbove ? 'auto' : `${menuPosition.top}px`,
            bottom: placementAbove ? `${window.innerHeight - menuPosition.top}px` : 'auto',
            right: `${window.innerWidth - menuPosition.left}px`,
          }}
        >
          <button
            className="spawn-dropdown__item"
            onClick={handleLinearSyncFromMain}
            role="menuitem"
            disabled={behind === 0}
            aria-disabled={behind === 0}
          >
            <span className="spawn-dropdown__item-label">sync from main clean</span>
          </button>
          <button
            className="spawn-dropdown__item"
            onClick={handleLinearSyncResolveConflict}
            role="menuitem"
            disabled={behind === 0 || !config.conflict_resolve?.target}
            aria-disabled={behind === 0 || !config.conflict_resolve?.target}
          >
            <span className="spawn-dropdown__item-label">sync from main conflict</span>
          </button>
          <button
            className="spawn-dropdown__item"
            onClick={handleLinearSyncToMain}
            role="menuitem"
            disabled={workspace.git_lines_added !== 0 || workspace.git_lines_removed !== 0 || workspace.git_files_changed !== 0 || behind !== 0 || ahead < 1}
            aria-disabled={workspace.git_lines_added !== 0 || workspace.git_lines_removed !== 0 || workspace.git_files_changed !== 0 || behind !== 0 || ahead < 1}
          >
            <span className="spawn-dropdown__item-label">sync to main</span>
          </button>
        </div>,
        document.body
      )}
    </>
  );
}
