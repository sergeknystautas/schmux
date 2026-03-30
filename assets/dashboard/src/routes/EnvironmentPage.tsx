import { useState, useEffect } from 'react';
import { getEnvironment, syncEnvironmentVar, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type { EnvironmentVar } from '../lib/types.generated';

function statusLabel(status: string): string {
  switch (status) {
    case 'in_sync':
      return 'in sync';
    case 'differs':
      return 'differs';
    case 'system_only':
      return 'system only';
    case 'tmux_only':
      return 'tmux only';
    default:
      return status;
  }
}

function statusBadgeClass(status: string): string {
  switch (status) {
    case 'in_sync':
      return 'env-badge env-badge--in-sync';
    case 'differs':
      return 'env-badge env-badge--differs';
    case 'system_only':
      return 'env-badge env-badge--system-only';
    case 'tmux_only':
      return 'env-badge env-badge--tmux-only';
    default:
      return 'env-badge';
  }
}

function canSync(status: string): boolean {
  return status === 'differs' || status === 'system_only';
}

export default function EnvironmentPage() {
  const [vars, setVars] = useState<EnvironmentVar[]>([]);
  const [blocked, setBlocked] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [syncing, setSyncing] = useState<string | null>(null);
  const { success: toastSuccess, error: toastError } = useToast();

  const loadEnvironment = async () => {
    try {
      setLoading(true);
      const data = await getEnvironment();
      setVars(data.vars);
      setBlocked(data.blocked);
      setError('');
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to fetch environment'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadEnvironment();
  }, []);

  const handleSync = async (key: string) => {
    try {
      setSyncing(key);
      await syncEnvironmentVar(key);
      toastSuccess(`Synced ${key}`);
      await loadEnvironment();
    } catch (err) {
      toastError(getErrorMessage(err, `Failed to sync ${key}`));
    } finally {
      setSyncing(null);
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading environment...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">!</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{error}</p>
      </div>
    );
  }

  return (
    <>
      <div className="app-header">
        <div className="app-header__info">
          <h1 className="app-header__meta">Environment</h1>
        </div>
      </div>

      <div className="spawn-content">
        <p className="mb-lg text-muted">
          Compare the current system environment against the tmux server environment. Sync
          individual variables so new sessions pick up the changes.
        </p>

        <table className="session-table env-table">
          <thead>
            <tr>
              <th>Key</th>
              <th>Status</th>
              <th>Action</th>
            </tr>
          </thead>
          <tbody>
            {vars.map((v) => (
              <tr key={v.key}>
                <td>
                  <code>{v.key}</code>
                </td>
                <td>
                  <span className={statusBadgeClass(v.status)}>{statusLabel(v.status)}</span>
                </td>
                <td>
                  {canSync(v.status) && (
                    <button
                      className="btn btn--sm btn--primary"
                      onClick={() => handleSync(v.key)}
                      disabled={syncing === v.key}
                    >
                      {syncing === v.key ? 'Syncing...' : 'Sync'}
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {blocked.length > 0 && (
          <div className="env-blocked">
            <h3 className="env-blocked__title">Blocked Keys</h3>
            <p className="env-blocked__desc">
              Excluded from comparison — tmux-internal, session-transient, or terminal-specific.
            </p>
            <p className="env-blocked__keys">
              {blocked.map((key) => (
                <code key={key}>{key}</code>
              ))}
            </p>
          </div>
        )}
      </div>
    </>
  );
}
