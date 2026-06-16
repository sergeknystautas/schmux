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
    <div className="stream-metrics relative" ref={panelRef}>
      <div
        className="connection-pill cursor-pointer"
        onClick={() => setExpanded(!expanded)}
        title="IO Workspace Telemetry"
      >
        <span>{totalCmds} git cmds</span>
        <span>{formatDuration(totalMs)}</span>
      </div>
      {onCapture && (
        <button className="btn btn--sm btn--secondary" onClick={onCapture}>
          Diagnose
        </button>
      )}
      {expanded && (
        <div className="stream-metrics__dropdown">
          <table className="stream-metrics__table">
            <tbody>
              <tr>
                <td className="stream-metrics__label">Total commands</td>
                <td className="stream-metrics__value">{totalCmds}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Total duration</td>
                <td className="stream-metrics__value">{formatDuration(totalMs)}</td>
              </tr>
              {Object.keys(triggerCounts).length > 0 && (
                <>
                  <tr>
                    <td colSpan={2} className="stream-metrics__subheader">
                      By trigger
                    </td>
                  </tr>
                  {Object.entries(triggerCounts).map(([trigger, count]) => (
                    <tr key={trigger}>
                      <td className="stream-metrics__label stream-metrics__label--indented">
                        {trigger}
                      </td>
                      <td className="stream-metrics__value">{count}</td>
                    </tr>
                  ))}
                </>
              )}
              {Object.keys(counters).length > 0 && (
                <>
                  <tr>
                    <td colSpan={2} className="stream-metrics__subheader">
                      By command
                    </td>
                  </tr>
                  {Object.entries(counters).map(([cmd, count]) => (
                    <tr key={cmd}>
                      <td className="stream-metrics__label stream-metrics__label--indented">
                        {cmd}
                      </td>
                      <td className="stream-metrics__value">{count}</td>
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
