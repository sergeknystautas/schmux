import React, { useEffect, useState } from 'react';
import { getChatStorageStatus } from '../../lib/api';
import type { ChatStorageStatusResponse } from '../../lib/types.generated';
import type { ConfigFormAction } from './useConfigForm';

type ChatStorageConfigProps = {
  retentionDays: number;
  maxTotalStorageMB: number;
  dispatch: React.Dispatch<ConfigFormAction>;
};

export default function ChatStorageConfig({
  retentionDays,
  maxTotalStorageMB,
  dispatch,
}: ChatStorageConfigProps) {
  const [status, setStatus] = useState<ChatStorageStatusResponse | null>(null);
  const [statusError, setStatusError] = useState<string | null>(null);
  const [retentionInput, setRetentionInput] = useState(String(retentionDays));
  const [budgetInput, setBudgetInput] = useState(String(maxTotalStorageMB));

  useEffect(() => {
    setRetentionInput(String(retentionDays));
  }, [retentionDays]);

  useEffect(() => {
    setBudgetInput(String(maxTotalStorageMB));
  }, [maxTotalStorageMB]);

  useEffect(() => {
    let cancelled = false;
    getChatStorageStatus()
      .then((s) => {
        if (!cancelled) setStatus(s);
      })
      .catch((err: Error) => {
        if (!cancelled) setStatusError(err.message || 'Failed to load');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const setField = (field: string, value: unknown) =>
    dispatch({ type: 'SET_FIELD', field: field as never, value });

  const retentionValue = parseWholeNumber(retentionInput);
  const budgetValue = parseWholeNumber(budgetInput);
  const retentionInvalid = retentionValue === null || retentionValue < 0;
  const budgetInvalid = budgetValue === null || budgetValue < 1;

  return (
    <div className="form-row">
      <div className="form-group">
        <label className="form-group__label" htmlFor="chat-storage-retention">
          Transcript retention (days)
        </label>
        <input
          id="chat-storage-retention"
          type="number"
          className="input input--compact"
          min={0}
          step={1}
          value={retentionInput}
          onChange={(e) => {
            const next = e.target.value;
            setRetentionInput(next);
            const value = parseWholeNumber(next);
            if (value !== null && value >= 0) {
              setField('chatStorageRetentionDays', value);
            }
          }}
          aria-invalid={retentionInvalid}
          data-testid="chat-storage-retention"
        />
        {retentionInvalid && (
          <p className="form-group__error">Enter a whole number of days, 0 or greater.</p>
        )}
        <p className="form-group__hint">
          Ended chat transcripts are kept for this many days, plus a fixed one-hour grace period.{' '}
          <code>0</code> means transcripts become age-eligible on the next pass after grace.
        </p>
      </div>
      <div className="form-group">
        <label className="form-group__label" htmlFor="chat-storage-budget">
          Maximum total storage (MiB)
        </label>
        <input
          id="chat-storage-budget"
          type="number"
          className="input input--compact"
          min={1}
          step={1}
          value={budgetInput}
          onChange={(e) => {
            const next = e.target.value;
            setBudgetInput(next);
            const value = parseWholeNumber(next);
            if (value !== null && value >= 1) {
              setField('chatStorageMaxTotalStorageMB', value);
            }
          }}
          aria-invalid={budgetInvalid}
          data-testid="chat-storage-budget"
        />
        {budgetInvalid && (
          <p className="form-group__error">Enter a whole number of MiB, 1 or greater.</p>
        )}
        <p className="form-group__hint">
          When chat storage exceeds this, the cleaner deletes oldest grace-eligible directories
          first.
        </p>
      </div>
      <div className="form-group" data-testid="chat-storage-status">
        <label className="form-group__label">Status</label>
        {statusError ? (
          <p className="form-group__hint text-muted">Status unavailable: {statusError}</p>
        ) : status ? (
          <p className="form-group__hint">
            Current use: {formatBytes(status.used_bytes)} of {status.max_total_storage_mb} MiB
            {status.last_cleanup ? (
              <>
                {' '}
                — last cleanup {formatTimestamp(status.last_cleanup.at)}:{' '}
                {status.last_cleanup.age_deleted + status.last_cleanup.budget_deleted} dirs removed,{' '}
                {formatBytes(status.last_cleanup.bytes_reclaimed)} reclaimed
              </>
            ) : (
              <> — no cleanup pass yet (initial scan runs after startup)</>
            )}
          </p>
        ) : (
          <p className="form-group__hint text-muted">Loading…</p>
        )}
      </div>
    </div>
  );
}

function parseWholeNumber(value: string): number | null {
  if (!/^\d+$/.test(value)) {
    return null;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function formatBytes(b: number): string {
  if (b === 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let v = b;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 ? 0 : 1)} ${units[i]}`;
}

function formatTimestamp(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso;
  }
}
