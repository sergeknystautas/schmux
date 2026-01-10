import React, { useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { disposeWorkspace, scanWorkspaces } from '../lib/api.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig, useRequireConfig } from '../contexts/ConfigContext.jsx';
import WorkspacesList from '../components/WorkspacesList.jsx';
import Tooltip from '../components/Tooltip.jsx';
import ScanResultsModal from '../components/ScanResultsModal.jsx';
import useLocalStorage from '../hooks/useLocalStorage.js';

export default function SessionsPage() {
  const { config } = useConfig();
  useRequireConfig();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const navigate = useNavigate();
  const [filters, setFilters] = useLocalStorage('sessions-filters', { status: '', repo: '' });
  const [scanResult, setScanResult] = useState(null);
  const [scanning, setScanning] = useState(false);

  const updateFilter = (key, value) => {
    setFilters((prev) => ({
      ...prev,
      [key]: value || ''
    }));
  };

  const handleScan = async () => {
    setScanning(true);
    try {
      const result = await scanWorkspaces();
      setScanResult(result);
    } catch (err) {
      toastError(`Failed to scan workspaces: ${err.message}`);
    } finally {
      setScanning(false);
    }
  };

  const handleDisposeWorkspace = async (workspaceId) => {
    const accepted = await confirm(`Dispose workspace ${workspaceId}?`, { danger: true });
    if (!accepted) return;
    try {
      await disposeWorkspace(workspaceId);
      success('Workspace disposed');
      // WorkspacesList will auto-refresh
    } catch (err) {
      toastError(`Failed to dispose workspace: ${err.message}`);
    }
  };

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Sessions</h1>
        <div className="page-header__actions">
          <button className="btn btn--ghost" onClick={handleScan} disabled={scanning}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="12"></line>
              <line x1="12" y1="16" x2="12.01" y2="16"></line>
            </svg>
            Scan
          </button>
          <Link to="/spawn" className="btn btn--primary">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="16"></line>
              <line x1="8" y1="12" x2="16" y2="12"></line>
            </svg>
            Spawn
          </Link>
        </div>
      </div>

      <WorkspacesList
        filters={filters}
        onFilterChange={updateFilter}
        renderActions={(workspace) => (
          <>
            <Tooltip content="View git diff">
              <button
                className="btn btn--sm btn--ghost"
                onClick={(event) => {
                  event.stopPropagation();
                  navigate(`/diff/${workspace.id}`);
                }}
                aria-label={`View diff for ${workspace.id}`}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                  <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                </svg>
                Diff
              </button>
            </Tooltip>
            <Tooltip content="Spawn session in this workspace">
              <button
                className="btn btn--sm btn--primary"
                onClick={(event) => {
                  event.stopPropagation();
                  navigate(`/spawn?workspace_id=${workspace.id}`);
                }}
                aria-label={`Spawn in ${workspace.id}`}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <circle cx="12" cy="12" r="10"></circle>
                  <line x1="12" y1="8" x2="12" y2="16"></line>
                  <line x1="8" y1="12" x2="16" y2="12"></line>
                </svg>
                Spawn
              </button>
            </Tooltip>
            <Tooltip content="Dispose workspace and all sessions" variant="warning">
              <button
                className="btn btn--sm btn--ghost btn--danger"
                onClick={(event) => {
                  event.stopPropagation();
                  handleDisposeWorkspace(workspace.id);
                }}
                aria-label={`Dispose ${workspace.id}`}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <polyline points="3 6 5 6 21 6"></polyline>
                  <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                </svg>
                Dispose
              </button>
            </Tooltip>
          </>
        )}
      />

      {scanResult && (
        <ScanResultsModal
          result={scanResult}
          onClose={() => setScanResult(null)}
        />
      )}
    </>
  );
}
