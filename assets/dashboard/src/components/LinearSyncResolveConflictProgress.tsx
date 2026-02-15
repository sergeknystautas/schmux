import React, { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { dismissLinearSyncResolveConflictState } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import { useSync } from '../hooks/useSync';
import type {
  LinearSyncResolveConflictStatePayload,
  LinearSyncResolveConflictStep,
} from '../lib/types';

type LinearSyncResolveConflictProgressProps = {
  workspaceId: string;
};

function StepIcon({ status }: { status: string }) {
  if (status === 'in_progress') {
    return (
      <svg
        width="12"
        height="12"
        viewBox="0 0 16 16"
        fill="none"
        stroke="var(--color-text-muted)"
        strokeWidth="2"
      >
        <circle cx="8" cy="8" r="5.5" />
      </svg>
    );
  }
  if (status === 'done') {
    return (
      <svg
        width="12"
        height="12"
        viewBox="0 0 16 16"
        fill="none"
        stroke="var(--color-success)"
        strokeWidth="2.5"
      >
        <polyline points="3 8 6.5 11.5 13 4.5" />
      </svg>
    );
  }
  // failed
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 16 16"
      fill="none"
      stroke="var(--color-error)"
      strokeWidth="2.5"
    >
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
  const [expanded, setExpanded] = React.useState(false);

  React.useEffect(() => {
    if (step.status !== 'in_progress') return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [step.status]);

  const startedAt = step.at ? Date.parse(step.at) : NaN;
  const elapsed =
    step.status === 'in_progress' && !Number.isNaN(startedAt)
      ? formatElapsed(now - startedAt)
      : null;

  const showSummary = step.status === 'failed' && step.summary;

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'flex-start',
        gap: 8,
        padding: '3px 0',
        opacity: step.status === 'in_progress' ? 1 : 0.8,
      }}
    >
      <div
        style={{
          marginTop: 2,
          flexShrink: 0,
          minWidth: 32,
          textAlign: 'right',
        }}
      >
        {elapsed ? (
          <span
            style={{
              fontSize: '0.7rem',
              color: 'var(--color-text-muted)',
              fontFamily: 'monospace',
            }}
          >
            {elapsed}
          </span>
        ) : (
          <span style={{ display: 'inline-block', width: 12 }}>
            <StepIcon status={step.status} />
          </span>
        )}
      </div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: '0.85rem' }}>{step.message}</span>
        {step.files && step.files.length > 0 && (
          <div
            style={{
              fontSize: '0.75rem',
              color: 'var(--color-text-muted)',
              marginTop: 2,
            }}
          >
            {step.files.join(', ')}
          </div>
        )}
        {showSummary && (
          <div style={{ marginTop: 4 }}>
            <button
              onClick={() => setExpanded(!expanded)}
              style={{
                background: 'none',
                border: 'none',
                color: 'var(--color-text-muted)',
                cursor: 'pointer',
                fontSize: '0.75rem',
                padding: 0,
                textDecoration: 'underline',
              }}
            >
              {expanded ? 'Hide' : 'Show'} LLM response
            </button>
            {expanded && (
              <pre
                style={{
                  marginTop: 4,
                  padding: '8px 10px',
                  background: 'rgba(0, 0, 0, 0.15)',
                  borderRadius: 4,
                  fontSize: '0.75rem',
                  fontFamily: 'monospace',
                  color: 'var(--color-text-muted)',
                  maxHeight: 200,
                  overflowY: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                }}
              >
                {step.summary}
              </pre>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

export default function LinearSyncResolveConflictProgress({
  workspaceId,
}: LinearSyncResolveConflictProgressProps) {
  const { linearSyncResolveConflictStates, workspaces, clearLinearSyncResolveConflictState } =
    useSessions();
  const navigate = useNavigate();
  const state: LinearSyncResolveConflictStatePayload | undefined =
    linearSyncResolveConflictStates[workspaceId];
  const [continuing, setContinuing] = useState(false);
  const { handleLinearSyncFromMain } = useSync();

  // Auto-dismiss when resolution completes and there are no more commits to sync
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const hasMoreCommits = (workspace?.git_behind ?? 0) > 0;
  useEffect(() => {
    if (state?.status === 'done' && !hasMoreCommits) {
      clearLinearSyncResolveConflictState(workspaceId);
      const firstSession = workspace?.sessions?.[0];
      navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
      dismissLinearSyncResolveConflictState(workspaceId).catch(() => {});
    }
  }, [
    state?.status,
    hasMoreCommits,
    workspaceId,
    clearLinearSyncResolveConflictState,
    navigate,
    workspace?.sessions,
  ]);

  if (!state) return null;

  const isActive = state.status === 'in_progress';
  const isDone = state.status === 'done';
  const isFailed = state.status === 'failed';

  const handleDismiss = async () => {
    clearLinearSyncResolveConflictState(workspaceId);
    const firstSession = workspace?.sessions?.[0];
    navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
    try {
      await dismissLinearSyncResolveConflictState(workspaceId);
    } catch {
      // State will be cleared via next WS broadcast
    }
  };

  const handleContinue = async () => {
    setContinuing(true);
    await handleLinearSyncFromMain(workspaceId);
    setContinuing(false);
  };

  const borderColor = isActive
    ? 'var(--color-border)'
    : isDone
      ? 'var(--color-success)'
      : 'var(--color-error)';

  return (
    <div style={{ fontSize: '0.85rem' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 6,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          {isActive && (
            <div
              className="spinner spinner--small"
              style={{ width: 14, height: 14, borderWidth: 2 }}
            />
          )}
          <strong>
            {isActive
              ? 'Resolving conflicts...'
              : isDone
                ? 'Conflict resolution complete'
                : 'Conflict resolution failed'}
          </strong>
          {state.hash && (
            <span
              style={{
                color: 'var(--color-text-muted)',
                fontFamily: 'monospace',
                fontSize: '0.8rem',
              }}
            >
              {state.hash.substring(0, 7)}
            </span>
          )}
        </div>
        {!isActive && !(isDone && hasMoreCommits) && (
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
        <div
          style={{
            padding: '6px 10px',
            marginBottom: 6,
            borderRadius: 4,
            background: isDone ? 'rgba(0, 180, 100, 0.08)' : 'rgba(220, 50, 50, 0.08)',
            fontSize: '0.85rem',
          }}
        >
          {state.message}
        </div>
      )}

      {/* Steps */}
      <div style={{ display: 'flex', flexDirection: 'column' }}>
        {state.steps.map((step) => (
          <StepRow key={step.action} step={step} />
        ))}
      </div>

      {/* Next steps */}
      {!isActive && isDone && hasMoreCommits && (
        <div
          style={{
            marginTop: 8,
            borderTop: '1px solid var(--color-border)',
            paddingTop: 6,
          }}
        >
          <strong style={{ marginBottom: 4, display: 'block' }}>Next steps</strong>
          <div style={{ fontSize: '0.85rem', marginBottom: 8 }}>
            There are {workspace?.git_behind} commits left to sync.
          </div>
          <button className="btn btn--primary" onClick={handleContinue} disabled={continuing}>
            {continuing && (
              <div
                className="spinner spinner--small"
                style={{
                  width: 12,
                  height: 12,
                  borderWidth: 2,
                  display: 'inline-block',
                  marginRight: 6,
                }}
              />
            )}
            {continuing ? 'Starting...' : 'Continue syncing'}
          </button>
        </div>
      )}
    </div>
  );
}
