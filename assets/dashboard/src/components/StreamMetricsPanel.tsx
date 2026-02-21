import { useState } from 'react';

interface BackendStats {
  eventsDelivered: number;
  eventsDropped: number;
  bytesDelivered: number;
  controlModeReconnects: number;
  bytesPerSec?: number;
}

interface FrontendStats {
  framesReceived: number;
  bytesReceived: number;
  bootstrapCount: number;
  sequenceBreaks: number;
}

interface Props {
  backendStats: BackendStats | null;
  frontendStats: FrontendStats | null;
  onDiagnosticCapture?: () => void;
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatBytes(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}MB`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}KB`;
  return `${n}B`;
}

export function StreamMetricsPanel({ backendStats, frontendStats, onDiagnosticCapture }: Props) {
  const [expanded, setExpanded] = useState(false);
  const frames = frontendStats?.framesReceived ?? 0;
  const bytes = frontendStats?.bytesReceived ?? 0;
  const drops = backendStats?.eventsDropped ?? 0;
  const seqBreaks = frontendStats?.sequenceBreaks ?? 0;

  return (
    <div className="stream-metrics-panel">
      <div className="stream-metrics-panel__summary" onClick={() => setExpanded(!expanded)}>
        <span>Stream: {formatCount(frames)} frames</span>
        <span> | {formatBytes(bytes)}</span>
        <span className={drops > 0 ? 'warning' : ''}> | {drops} drops</span>
        <span className={seqBreaks > 0 ? 'warning' : ''}> | {seqBreaks} seq breaks</span>
        {onDiagnosticCapture && (
          <button
            className="stream-metrics-panel__diagnose-btn"
            onClick={(e) => {
              e.stopPropagation();
              onDiagnosticCapture();
            }}
          >
            Diagnose Desync
          </button>
        )}
      </div>
      {expanded && (
        <div className="stream-metrics-panel__details">
          <table>
            <thead>
              <tr>
                <th>Metric</th>
                <th>Value</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>Frames received</td>
                <td>{frames}</td>
              </tr>
              <tr>
                <td>Bytes received</td>
                <td>{formatBytes(bytes)}</td>
              </tr>
              <tr>
                <td>Events delivered (backend)</td>
                <td>{backendStats?.eventsDelivered ?? '—'}</td>
              </tr>
              <tr>
                <td>Events dropped</td>
                <td className={drops > 0 ? 'warning' : ''}>{drops}</td>
              </tr>
              <tr>
                <td>Bytes delivered (backend)</td>
                <td>{formatBytes(backendStats?.bytesDelivered ?? 0)}</td>
              </tr>
              <tr>
                <td>Throughput</td>
                <td>
                  {backendStats?.bytesPerSec ? formatBytes(backendStats.bytesPerSec) + '/s' : '—'}
                </td>
              </tr>
              <tr>
                <td>Bootstrap count</td>
                <td>{frontendStats?.bootstrapCount ?? '—'}</td>
              </tr>
              <tr>
                <td>Sequence breaks</td>
                <td className={seqBreaks > 0 ? 'warning' : ''}>{seqBreaks}</td>
              </tr>
              <tr>
                <td>Control mode reconnects</td>
                <td>{backendStats?.controlModeReconnects ?? 0}</td>
              </tr>
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
