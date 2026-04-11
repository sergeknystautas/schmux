import { useState, useEffect, useRef } from 'react';
import { getDetectionSummary } from '../lib/api';
import type { DetectionSummaryResponse } from '../lib/types.generated';

const MAX_RETRIES = 10;

type LoadState =
  | { phase: 'loading' }
  | { phase: 'ready'; data: DetectionSummaryResponse }
  | { phase: 'timeout' }
  | { phase: 'error'; message: string };

export function EnvironmentSummary() {
  const [state, setState] = useState<LoadState>({ phase: 'loading' });
  const retriesRef = useRef(0);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const poll = async () => {
      try {
        const data = await getDetectionSummary();
        if (cancelled) return;

        if (data.status === 'ready') {
          setState({ phase: 'ready', data });
          return;
        }

        // status === "pending" or other non-ready
        retriesRef.current += 1;
        if (retriesRef.current >= MAX_RETRIES) {
          setState({ phase: 'timeout' });
          return;
        }

        timer = setTimeout(poll, 1000);
      } catch {
        if (!cancelled) {
          setState({ phase: 'error', message: 'Failed to detect tools' });
        }
      }
    };

    poll();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, []);

  if (state.phase === 'loading') {
    return (
      <div className="env-summary" data-testid="env-summary">
        <span className="env-summary__loading" data-testid="env-summary-loading">
          Detecting tools...
        </span>
      </div>
    );
  }

  if (state.phase === 'timeout') {
    return (
      <div className="env-summary" data-testid="env-summary">
        <span className="env-summary__timeout" data-testid="env-summary-timeout">
          Detection timed out — some tools may not be shown. Refresh the page to retry.
        </span>
      </div>
    );
  }

  if (state.phase === 'error') {
    return (
      <div className="env-summary" data-testid="env-summary">
        <span className="env-summary__error">{state.message}</span>
      </div>
    );
  }

  const { data } = state;
  const hasAgents = data.agents.length > 0;
  const hasVcs = data.vcs.length > 0;
  const hasTmux = data.tmux.available;

  const warnings: string[] = [];
  if (!hasAgents) {
    warnings.push('No agents detected — install one or configure in Settings');
  }
  if (!hasVcs) {
    warnings.push('No version control found — install git to get started');
  }
  if (!hasTmux) {
    warnings.push('tmux not found — required to spawn sessions. Install: brew install tmux');
  }

  const categoryStyle: React.CSSProperties = {
    display: 'flex',
    alignItems: 'center',
    gap: 'var(--spacing-xs)',
    flexWrap: 'wrap',
  };

  const labelStyle: React.CSSProperties = {
    fontSize: '0.75rem',
    color: 'var(--color-text-muted)',
    textTransform: 'uppercase',
    letterSpacing: '0.05em',
    fontWeight: 600,
  };

  return (
    <div
      className="env-summary"
      data-testid="env-summary"
      style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-sm)' }}
    >
      {/* Found tools — grouped by category */}
      <div
        data-testid="env-summary-found"
        style={{ display: 'flex', flexDirection: 'column', gap: 'var(--spacing-xs)' }}
      >
        {hasAgents && (
          <div style={categoryStyle}>
            <span style={labelStyle}>Agents</span>
            {data.agents.map((agent) => (
              <span key={agent.name} className="badge badge--success" data-testid="env-badge-agent">
                {agent.name}
              </span>
            ))}
          </div>
        )}
        {(hasVcs || hasTmux) && (
          <div style={categoryStyle}>
            <span style={labelStyle}>Tools</span>
            {data.vcs.map((v) => (
              <span key={v.name} className="badge badge--success" data-testid="env-badge-vcs">
                {v.name}
              </span>
            ))}
            {hasTmux && (
              <span className="badge badge--success" data-testid="env-badge-tmux">
                tmux
              </span>
            )}
          </div>
        )}
      </div>

      {/* Warnings for missing tools */}
      {warnings.length > 0 && (
        <div className="env-summary__warnings" data-testid="env-summary-warnings">
          {warnings.map((msg) => (
            <div
              key={msg}
              className="env-summary__warning"
              data-testid="env-warning"
              style={{
                color: 'var(--color-warning)',
                fontSize: '0.8rem',
              }}
            >
              {msg}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
