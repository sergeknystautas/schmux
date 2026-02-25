import { useState, useRef, useEffect } from 'react';

export interface IOWorkspaceStats {
  totalCommands: number;
  totalDurationMs: number;
  triggerCounts: Record<string, number>;
  counters: Record<string, number>;
}

interface Props {
  stats: IOWorkspaceStats | null;
  onCapture?: () => void;
}

function formatDuration(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(1)}s`;
  return `${ms}ms`;
}

export function IOWorkspaceMetricsPanel({ stats, onCapture }: Props) {
  const [expanded, setExpanded] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!expanded) return;
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setExpanded(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [expanded]);

  const totalCmds = stats?.totalCommands ?? 0;
  const totalMs = stats?.totalDurationMs ?? 0;
  const counters = stats?.counters ?? {};
  const triggerCounts = stats?.triggerCounts ?? {};

  return (
    <div className="stream-metrics" ref={panelRef} style={{ position: 'relative' }}>
      <div
        className="connection-pill"
        onClick={() => setExpanded(!expanded)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
        title="IO Workspace Telemetry"
      >
        <span>{totalCmds} git cmds</span>
        <span>{formatDuration(totalMs)}</span>
      </div>
      {onCapture && (
        <button className="btn btn--sm" onClick={onCapture}>
          Capture
        </button>
      )}
      {expanded && (
        <div
          className="stream-metrics__dropdown"
          style={{
            position: 'absolute',
            top: '100%',
            left: 0,
            zIndex: 100,
            marginTop: '4px',
            background: 'var(--color-surface)',
            border: '1px solid var(--color-border)',
            borderRadius: 'var(--radius-md)',
            padding: 'var(--spacing-sm)',
            fontSize: '0.75rem',
            minWidth: '240px',
            boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
          }}
        >
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <tbody>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Total commands
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>{totalCmds}</td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Total duration
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>{formatDuration(totalMs)}</td>
              </tr>
              {Object.keys(triggerCounts).length > 0 && (
                <>
                  <tr>
                    <td
                      colSpan={2}
                      style={{
                        padding: '6px 0 2px 0',
                        color: 'var(--color-text-muted)',
                        fontSize: '0.65rem',
                        textTransform: 'uppercase',
                        letterSpacing: '0.05em',
                      }}
                    >
                      By trigger
                    </td>
                  </tr>
                  {Object.entries(triggerCounts).map(([trigger, count]) => (
                    <tr key={trigger}>
                      <td style={{ padding: '2px 8px 2px 8px', color: 'var(--color-text-muted)' }}>
                        {trigger}
                      </td>
                      <td style={{ padding: '2px 0', textAlign: 'right' }}>{count}</td>
                    </tr>
                  ))}
                </>
              )}
              {Object.keys(counters).length > 0 && (
                <>
                  <tr>
                    <td
                      colSpan={2}
                      style={{
                        padding: '6px 0 2px 0',
                        color: 'var(--color-text-muted)',
                        fontSize: '0.65rem',
                        textTransform: 'uppercase',
                        letterSpacing: '0.05em',
                      }}
                    >
                      By command
                    </td>
                  </tr>
                  {Object.entries(counters).map(([cmd, count]) => (
                    <tr key={cmd}>
                      <td style={{ padding: '2px 8px 2px 8px', color: 'var(--color-text-muted)' }}>
                        {cmd}
                      </td>
                      <td style={{ padding: '2px 0', textAlign: 'right' }}>{count}</td>
                    </tr>
                  ))}
                </>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
