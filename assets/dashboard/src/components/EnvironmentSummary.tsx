import { useState, useEffect } from 'react';
import { getDependencies } from '../lib/api';
import type { DependenciesResponse } from '../lib/types.generated';

type LoadState =
  | { phase: 'loading' }
  | { phase: 'ready'; data: DependenciesResponse }
  | { phase: 'error'; message: string };

export function EnvironmentSummary() {
  const [state, setState] = useState<LoadState>({ phase: 'loading' });

  useEffect(() => {
    let cancelled = false;
    getDependencies()
      .then((data) => {
        if (!cancelled) setState({ phase: 'ready', data });
      })
      .catch(() => {
        if (!cancelled) setState({ phase: 'error', message: 'Failed to detect tools' });
      });
    return () => {
      cancelled = true;
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

  if (state.phase === 'error') {
    return (
      <div className="env-summary" data-testid="env-summary">
        <span className="env-summary__error">{state.message}</span>
      </div>
    );
  }

  const { data } = state;
  const detectedIn = (groupId: string) =>
    data.groups.find((g) => g.id === groupId)?.dependencies.filter((d) => d.detected) ?? [];

  // Agents in their own row; every other detected tool (vcs, tmux, fence,
  // editors, ...) shares the "Tools" row so nothing the daemon found is dropped.
  const agents = detectedIn('agents');
  const tools = data.groups
    .filter((g) => g.id !== 'agents')
    .flatMap((g) => g.dependencies.filter((d) => d.detected));

  const hasAgents = agents.length > 0;
  const hasTools = tools.length > 0;
  const hasVcs = detectedIn('vcs').length > 0;
  const hasTmux = detectedIn('terminal').some((d) => d.id === 'tmux');

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
            {agents.map((agent) => (
              <span key={agent.id} className="badge badge--success" data-testid="env-badge-agent">
                {agent.display_name}
              </span>
            ))}
          </div>
        )}
        {hasTools && (
          <div style={categoryStyle}>
            <span style={labelStyle}>Tools</span>
            {tools.map((t) => (
              <span key={t.id} className="badge badge--success" data-testid={`env-badge-${t.id}`}>
                {t.display_name}
              </span>
            ))}
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
