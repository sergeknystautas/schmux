import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router';
import {
  getRecentBranches,
  refreshRecentBranches,
  prepareBranchSpawn,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import type { RecentBranch } from '../lib/types';
import styles from '../styles/branches.module.css';

// Copied from HomePage.tsx (local helper there; the card keeps its own copy)
function formatRelativeDate(isoDate: string): string {
  const date = new Date(isoDate);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  const diffHours = Math.floor(diffMs / 3600000);
  const diffDays = Math.floor(diffMs / 86400000);

  if (diffMins < 1) return 'just now';
  if (diffMins < 60) return `${diffMins}m ago`;
  if (diffHours < 24) return `${diffHours}h ago`;
  if (diffDays < 7) return `${diffDays}d ago`;
  if (diffDays < 30) return `${Math.floor(diffDays / 7)}w ago`;
  return date.toLocaleDateString();
}

const RefreshIcon = () => (
  <svg
    width="14"
    height="14"
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth="1.5"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <path d="M2 8a6 6 0 0 1 10.3-4.2L14 2v4h-4l1.7-1.7A4.5 4.5 0 0 0 3.5 8" />
    <path d="M14 8a6 6 0 0 1-10.3 4.2L2 14v-4h4l-1.7 1.7A4.5 4.5 0 0 0 12.5 8" />
  </svg>
);

export default function BranchesPage() {
  const navigate = useNavigate();
  const { success } = useToast();
  const { alert } = useModal();
  const [branches, setBranches] = useState<RecentBranch[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [preparingBranch, setPreparingBranch] = useState<string | null>(null);

  const loadBranches = useCallback(async () => {
    try {
      setLoading(true);
      setError('');
      const data = await getRecentBranches(50);
      setBranches(data || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load branches'));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadBranches();
  }, [loadBranches]);

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      const result = await refreshRecentBranches();
      setBranches(result.branches || []);
      setError('');
      success(
        `Found ${result.fetched_count} recent branch${result.fetched_count !== 1 ? 'es' : ''}`
      );
    } catch (err) {
      alert('Branch Refresh Failed', getErrorMessage(err, 'Failed to refresh branches'));
    } finally {
      setRefreshing(false);
    }
  };

  const handleLaunch = async (repoName: string, branchName: string) => {
    if (preparingBranch) return;
    const key = `${repoName}:${branchName}`;
    setPreparingBranch(key);
    try {
      const result = await prepareBranchSpawn(repoName, branchName);
      navigate('/spawn', { state: result });
    } catch (err) {
      alert('Branch Spawn Failed', getErrorMessage(err, 'Failed to prepare branch spawn'));
      setPreparingBranch(null);
    }
  };

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Remote Branches</h1>
        </div>
        <div className="app-header__actions">
          <button
            className="btn btn--secondary"
            data-testid="branches-refresh"
            onClick={handleRefresh}
            disabled={refreshing}
            title="Refresh branches from remote"
          >
            <RefreshIcon />
            {refreshing ? 'Refreshing...' : 'Refresh'}
          </button>
        </div>
      </div>

      <div className="card">
        {loading ? (
          <div className="loading-state">
            <div className="spinner"></div>
            <span>Loading branches...</span>
          </div>
        ) : error ? (
          <div className="empty-state">
            <div className="empty-state__icon">!</div>
            <h3 className="empty-state__title">Error</h3>
            <p className="empty-state__description">{error}</p>
          </div>
        ) : branches.length === 0 ? (
          <div className="empty-state">
            <h3 className="empty-state__title">No branches found yet</h3>
            <p className="empty-state__description">
              Branches will appear after the first fetch completes.
            </p>
          </div>
        ) : (
          <table className={`session-table ${styles.branchTable}`} data-testid="branch-list">
            <thead>
              <tr>
                <th>Branch</th>
                <th>Repo</th>
                <th>Updated</th>
                <th>Last commit</th>
              </tr>
            </thead>
            <tbody>
              {branches.map((branch, idx) => {
                const key = `${branch.repo_name}:${branch.branch}`;
                const isPreparing = preparingBranch === key;
                return (
                  <tr
                    key={`${branch.repo_name}-${branch.branch}-${idx}`}
                    data-testid="branch-item"
                    className={`cursor-pointer ${styles.branchRow}`}
                    role="button"
                    tabIndex={0}
                    aria-busy={isPreparing}
                    title={`Spawn a session on ${branch.branch}`}
                    onClick={() => handleLaunch(branch.repo_name, branch.branch)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleLaunch(branch.repo_name, branch.branch);
                      }
                    }}
                  >
                    <td>
                      <span className={styles.branchCell}>
                        <code className={styles.branchName}>{branch.branch}</code>
                        {isPreparing && <div className="spinner spinner--small" />}
                      </span>
                    </td>
                    <td className={styles.meta}>{branch.repo_name}</td>
                    <td className={styles.meta}>{formatRelativeDate(branch.commit_date)}</td>
                    <td className={styles.subject} title={branch.subject}>
                      {branch.subject}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </>
  );
}
