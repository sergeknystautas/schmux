import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { scanWorkspaces, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useRequireConfig } from '../contexts/ConfigContext';
import WorkspacesList from '../components/WorkspacesList';
import ScanResultsModal from '../components/ScanResultsModal';
import useLocalStorage, { SESSIONS_FILTERS_KEY } from '../hooks/useLocalStorage';
import type { ScanResult } from '../lib/types';

export default function SessionsPage() {
  useRequireConfig();
  const { error: toastError } = useToast();
  const [filters, setFilters] = useLocalStorage<{ status: string; repo: string }>(SESSIONS_FILTERS_KEY, { status: '', repo: '' });
  const [scanResult, setScanResult] = useState<ScanResult | null>(null);
  const [scanning, setScanning] = useState(false);

  const updateFilter = (key: 'status' | 'repo', value: string) => {
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
      toastError(`Failed to scan workspaces: ${getErrorMessage(err, 'Unknown error')}`);
    } finally {
      setScanning(false);
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
