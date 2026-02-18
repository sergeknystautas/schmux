import React, { useState, useEffect, useRef } from 'react';
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

const thinkingMessages = [
  'Thinking...',
  'Reading conflict markers...',
  'Comparing both sides...',
  'Understanding the intent...',
  'Analyzing changes...',
  'Figuring out the merge...',
  'Resolving differences...',
  'Almost there...',
  'Reviewing the resolution...',
  'Double-checking...',
];

function ThinkingIndicator() {
  const [index, setIndex] = React.useState(0);

  React.useEffect(() => {
    const timer = window.setInterval(() => {
      setIndex((prev) => (prev + 1) % thinkingMessages.length);
    }, 3000);
    return () => window.clearInterval(timer);
  }, []);

  return (
    <div
      style={{
        marginTop: 4,
        padding: '6px 8px',
        background: 'var(--color-surface-alt, rgba(0, 0, 0, 0.15))',
        borderRadius: 4,
        fontSize: '0.7rem',
        fontFamily: 'var(--font-mono)',
        color: 'var(--color-text-muted)',
        animation: 'pulse 2s infinite',
      }}
    >
      {thinkingMessages[index]}
    </div>
  );
}

function StepRow({ step }: { step: LinearSyncResolveConflictStep }) {
  const [now, setNow] = React.useState(Date.now());

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

  const showSummary = step.summary && step.status !== 'in_progress';

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
        {step.message.map((line, i) => (
          <div key={i} style={{ fontSize: '0.85rem' }}>
            {line}
          </div>
        ))}
        {step.conflict_diffs && step.files && step.files.length > 0 ? (
          <div style={{ marginTop: 4 }}>
            {step.files.map((file) => (
              <div key={file} style={{ marginBottom: 6 }}>
                <div
                  style={{
                    fontSize: '0.75rem',
                    fontFamily: 'var(--font-mono)',
                    color: 'var(--color-text-muted)',
                    marginBottom: 2,
                  }}
                >
                  {file}
                </div>
                {step.conflict_diffs![file]?.map((hunk, i) => (
                  <div key={i} style={{ marginBottom: 4 }}>
                    <div style={{ fontSize: '0.85rem', marginBottom: 2 }}>Hunk {i + 1}:</div>
                    <pre
                      style={{
                        margin: 0,
                        marginTop: 2,
                        marginBottom: 4,
                        padding: '6px 8px',
                        background: 'var(--color-surface-alt, rgba(0, 0, 0, 0.15))',
                        borderRadius: 4,
                        fontSize: '0.7rem',
                        fontFamily: 'var(--font-mono)',
                        lineHeight: 1.4,
                        color: 'var(--color-text)',
                        overflowX: 'auto',
                        overflowY: 'auto',
                        maxHeight: 200,
                        whiteSpace: 'pre',
                      }}
                    >
                      {hunk}
                    </pre>
                  </div>
                ))}
              </div>
            ))}
          </div>
        ) : (
          step.files &&
          step.files.length > 0 && (
            <div style={{ marginTop: 2 }}>
              {step.files.map((file) => (
                <div
                  key={file}
                  style={{
                    fontSize: '0.75rem',
                    fontFamily: 'var(--font-mono)',
                    color: 'var(--color-text-muted)',
                  }}
                >
                  {file}
                </div>
              ))}
            </div>
          )
        )}
        {showSummary ? (
          <pre
            style={{
              margin: 0,
              marginTop: 4,
              padding: '6px 8px',
              background: 'var(--color-surface-alt, rgba(0, 0, 0, 0.15))',
              borderRadius: 4,
              fontSize: '0.7rem',
              fontFamily: 'var(--font-mono)',
              lineHeight: 1.4,
              color: 'var(--color-text)',
              whiteSpace: 'pre-wrap',
              wordBreak: 'break-word',
            }}
          >
            {step.summary}
          </pre>
        ) : step.action === 'llm_call' && step.status === 'in_progress' ? (
          <ThinkingIndicator />
        ) : null}
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
  const bottomRef = useRef<HTMLDivElement>(null);

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

  // Auto-scroll to bottom as new steps arrive or existing steps update
  useEffect(() => {
    if (bottomRef.current && typeof bottomRef.current.scrollIntoView === 'function') {
      bottomRef.current.scrollIntoView({ behavior: 'smooth', block: 'end' });
    }
  }, [state?.steps]);

  if (!state) return null;

  const isActive = state.status === 'in_progress';
  const isDone = state.status === 'done';
  const isFailed = state.status === 'failed';

  const handleContinue = async () => {
    setContinuing(true);
    await handleLinearSyncFromMain(workspaceId);
    setContinuing(false);
  };

  return (
    <div style={{ fontSize: '0.85rem' }}>
      {/* Header */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 6 }}>
        {isActive && (
          <div
            className="spinner spinner--small"
            style={{ width: 14, height: 14, borderWidth: 2, flexShrink: 0, marginTop: 2 }}
          />
        )}
        <div>
          <strong>
            {isActive
              ? (() => {
                  const conflictFiles = state.steps
                    .filter((s) => s.action === 'conflict_detected' && s.files)
                    .flatMap((s) => s.files!);
                  return conflictFiles.length > 0
                    ? `Resolving ${conflictFiles.length} file conflict${conflictFiles.length !== 1 ? 's' : ''} with...`
                    : 'Resolving conflicts...';
                })()
              : isDone
                ? 'Conflict resolution completed'
                : 'Conflict resolution failed'}
          </strong>
          {state.hash && (
            <div
              style={{
                color: 'var(--color-text-muted)',
                fontFamily: 'monospace',
                fontSize: '0.8rem',
                marginTop: 2,
              }}
            >
              {state.hash.substring(0, 7)}
              {state.hash_message ? ` ${state.hash_message}` : ''}
            </div>
          )}
        </div>
      </div>

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
      <div ref={bottomRef} />
    </div>
  );
}
