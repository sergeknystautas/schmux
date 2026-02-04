import React from 'react';
import { dismissLinearSyncResolveConflictState } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import type { LinearSyncResolveConflictStatePayload, LinearSyncResolveConflictStep } from '../lib/types';

type LinearSyncResolveConflictProgressProps = {
  workspaceId: string;
};

function StepIcon({ status }: { status: string }) {
  if (status === 'in_progress') {
    return (
      <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="var(--color-text-muted)" strokeWidth="2">
        <circle cx="8" cy="8" r="5.5" />
      </svg>
    );
  }
  if (status === 'done') {
    return (
      <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="var(--color-success)" strokeWidth="2.5">
        <polyline points="3 8 6.5 11.5 13 4.5" />
      </svg>
    );
  }
  // failed
  return (
    <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="var(--color-error)" strokeWidth="2.5">
      <line x1="4" y1="4" x2="12" y2="12" />
      <line x1="12" y1="4" x2="4" y2="12" />
    </svg>
  );
}

function formatElapsed(ms: number) {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes === 0) {
    return `${seconds}s`;
  }
  return `${minutes}m ${seconds}s`;
}

function StepRow({ step }: { step: LinearSyncResolveConflictStep }) {
  const [now, setNow] = React.useState(Date.now());

  React.useEffect(() => {
    if (step.status !== 'in_progress') return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [step.status]);

  const startedAt = step.at ? Date.parse(step.at) : NaN;
  const elapsed = step.status === 'in_progress' && !Number.isNaN(startedAt)
    ? formatElapsed(now - startedAt)
    : null;

  return (
    <div style={{
      display: 'flex',
      alignItems: 'flex-start',
      gap: 8,
      padding: '3px 0',
      opacity: step.status === 'in_progress' ? 1 : 0.8,
    }}>
      <div style={{ marginTop: 2, flexShrink: 0, minWidth: 32, textAlign: 'right' }}>
        {elapsed
          ? <span style={{ fontSize: '0.7rem', color: 'var(--color-text-muted)', fontFamily: 'monospace' }}>{elapsed}</span>
          : <span style={{ display: 'inline-block', width: 12 }}><StepIcon status={step.status} /></span>
        }
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: '0.85rem' }}>{step.message}</span>
        {step.files && step.files.length > 0 && (
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginTop: 2 }}>
            {step.files.join(', ')}
          </div>
        )}
      </div>
    </div>
  );
}

export default function LinearSyncResolveConflictProgress({ workspaceId }: LinearSyncResolveConflictProgressProps) {
  const { linearSyncResolveConflictStates } = useSessions();
  const state: LinearSyncResolveConflictStatePayload | undefined = linearSyncResolveConflictStates[workspaceId];

  if (!state) return null;

  const isActive = state.status === 'in_progress';
  const isDone = state.status === 'done';
  const isFailed = state.status === 'failed';

  const handleDismiss = async () => {
    try {
      await dismissLinearSyncResolveConflictState(workspaceId);
    } catch {
      // State will be cleared via next WS broadcast
    }
  };

  const borderColor = isActive ? 'var(--color-border)' : isDone ? 'var(--color-success)' : 'var(--color-error)';

  return (
    <div style={{ fontSize: '0.85rem' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {isActive && <div className="spinner--small" style={{ width: 14, height: 14, borderWidth: 2 }} />}
          <strong>
            {isActive ? 'Resolving conflicts...' : isDone ? 'Conflict resolution complete' : 'Conflict resolution failed'}
          </strong>
          {state.hash && (
            <span style={{ color: 'var(--color-text-muted)', fontFamily: 'monospace', fontSize: '0.8rem' }}>
              {state.hash.substring(0, 7)}
            </span>
          )}
        </div>
        {!isActive && (
          <button
            className="btn btn--sm btn--ghost"
            onClick={handleDismiss}
            style={{ padding: '2px 8px', fontSize: '0.75rem' }}
          >
            dismiss
          </button>
        )}
      </div>

      {/* Final message for done/failed */}
      {!isActive && state.message && (
        <div style={{
          padding: '6px 10px',
          marginBottom: 6,
          borderRadius: 4,
          background: isDone ? 'rgba(0, 180, 100, 0.08)' : 'rgba(220, 50, 50, 0.08)',
          fontSize: '0.85rem',
        }}>
          {state.message}
        </div>
      )}

      {/* Steps */}
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {state.steps.map((step, i) => (
          <StepRow key={i} step={step} />
        ))}
      </div>

      {/* Resolutions summary */}
      {state.resolutions && state.resolutions.length > 0 && (
        <div style={{ marginTop: 8, borderTop: '1px solid var(--color-border)', paddingTop: 6 }}>
          <div style={{ fontSize: '0.75rem', color: 'var(--color-text-muted)', marginBottom: 4 }}>
            Resolutions:
          </div>
          {state.resolutions.map((res, i) => (
            <div key={i} style={{ fontSize: '0.8rem', padding: '2px 0' }}>
              <span style={{ fontFamily: 'monospace' }}>{res.local_commit.substring(0, 7)}</span>
              {' '}<span style={{ color: 'var(--color-text-muted)' }}>{res.local_commit_message}</span>
              {res.summary && <> — {res.summary}</>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
