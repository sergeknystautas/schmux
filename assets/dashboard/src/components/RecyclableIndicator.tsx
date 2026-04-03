import React, { useState, useEffect, useCallback } from 'react';
import { getRecyclableWorkspaces, purgeWorkspaces, getErrorMessage } from '../lib/api';
import { useToast } from './ToastProvider';

export default function RecyclableIndicator() {
  const [total, setTotal] = useState(0);
  const [purging, setPurging] = useState(false);
  const { success, error: toastError } = useToast();

  const fetchCount = useCallback(async () => {
    try {
      const data = await getRecyclableWorkspaces();
      setTotal(data.total);
    } catch {
      // Non-critical indicator — silently ignore errors
    }
  }, []);

  useEffect(() => {
    fetchCount();
  }, [fetchCount]);

  const handlePurge = async () => {
    setPurging(true);
    try {
      const result = await purgeWorkspaces();
      success(`Purged ${result.purged} recyclable workspace${result.purged !== 1 ? 's' : ''}`);
      setTotal(0);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to purge workspaces'));
      // Refresh count in case some were purged
      await fetchCount();
    } finally {
      setPurging(false);
    }
  };

  if (total === 0) return null;

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '6px 12px',
        background: 'var(--color-surface)',
        border: '1px solid var(--color-border)',
        borderRadius: 'var(--radius-md)',
        fontSize: '0.75rem',
        color: 'var(--color-text-muted)',
      }}
      data-testid="recyclable-indicator"
    >
      <span>
        {total} recyclable workspace{total !== 1 ? 's' : ''}
      </span>
      <button
        onClick={handlePurge}
        disabled={purging}
        style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '4px',
          padding: '2px 8px',
          background: 'transparent',
          border: '1px solid var(--color-border)',
          borderRadius: 'var(--radius-sm)',
          color: 'var(--color-text-muted)',
          fontSize: '0.7rem',
          fontWeight: 500,
          cursor: purging ? 'not-allowed' : 'pointer',
          opacity: purging ? 0.6 : 1,
        }}
        data-testid="purge-recyclable"
      >
        {purging ? 'Purging...' : 'Purge'}
      </button>
    </div>
  );
}
