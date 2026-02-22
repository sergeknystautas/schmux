import { useState, useRef, useEffect } from 'react';
import type { SequenceBreakRecord } from '../lib/streamDiagnostics';

export interface BackendStats {
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
  recentBreaks?: SequenceBreakRecord[];
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
  const [showBreakDetails, setShowBreakDetails] = useState(false);
  const panelRef = useRef<HTMLDivElement>(null);
  const frames = frontendStats?.framesReceived ?? 0;
  const bytes = frontendStats?.bytesReceived ?? 0;
  const drops = backendStats?.eventsDropped ?? 0;
  const seqBreaks = frontendStats?.sequenceBreaks ?? 0;

  // Close dropdown on outside click
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

  return (
    <div className="stream-metrics" ref={panelRef} style={{ position: 'relative' }}>
      <div
        className="connection-pill"
        onClick={() => setExpanded(!expanded)}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        <span>{formatCount(frames)} frames</span>
        <span className={drops > 0 ? 'warning' : ''}>{drops} drops</span>
        <span className={seqBreaks > 0 ? 'warning' : ''}>{seqBreaks} seq breaks</span>
      </div>
      {onDiagnosticCapture && (
        <button className="btn btn--sm" onClick={onDiagnosticCapture}>
          Diagnose
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
            minWidth: '280px',
            boxShadow: '0 4px 12px rgba(0,0,0,0.3)',
          }}
        >
          <table style={{ width: '100%', borderCollapse: 'collapse' }}>
            <tbody>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Frames received
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>{frames}</td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Bytes received
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>{formatBytes(bytes)}</td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Events delivered (backend)
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {backendStats?.eventsDelivered ?? '—'}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Events dropped
                </td>
                <td
                  className={drops > 0 ? 'warning' : ''}
                  style={{ padding: '2px 0', textAlign: 'right' }}
                >
                  {drops}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Bytes delivered (backend)
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {formatBytes(backendStats?.bytesDelivered ?? 0)}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Throughput
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {backendStats?.bytesPerSec ? formatBytes(backendStats.bytesPerSec) + '/s' : '—'}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Bootstrap count
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {frontendStats?.bootstrapCount ?? '—'}
                </td>
              </tr>
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Sequence breaks
                </td>
                <td
                  className={seqBreaks > 0 ? 'warning' : ''}
                  style={{ padding: '2px 0', textAlign: 'right' }}
                >
                  {seqBreaks}
                </td>
              </tr>
              {seqBreaks > 0 && (frontendStats?.recentBreaks?.length ?? 0) > 0 && (
                <tr>
                  <td colSpan={2} style={{ padding: '0' }}>
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setShowBreakDetails(!showBreakDetails);
                      }}
                      style={{
                        background: 'none',
                        border: 'none',
                        color: 'var(--color-text-muted)',
                        cursor: 'pointer',
                        fontSize: '0.65rem',
                        padding: '2px 0',
                        textDecoration: 'underline',
                      }}
                      data-testid="toggle-break-details"
                    >
                      {showBreakDetails ? 'hide details' : 'show details'}
                    </button>
                    {showBreakDetails && (
                      <table
                        data-testid="break-details-table"
                        style={{
                          width: '100%',
                          borderCollapse: 'collapse',
                          fontSize: '0.65rem',
                          marginTop: '2px',
                          marginBottom: '4px',
                        }}
                      >
                        <thead>
                          <tr style={{ color: 'var(--color-text-muted)' }}>
                            <th
                              style={{
                                padding: '1px 4px',
                                textAlign: 'left',
                                fontWeight: 'normal',
                              }}
                            >
                              Frame
                            </th>
                            <th
                              style={{
                                padding: '1px 4px',
                                textAlign: 'right',
                                fontWeight: 'normal',
                              }}
                            >
                              Offset
                            </th>
                            <th
                              style={{
                                padding: '1px 4px',
                                textAlign: 'left',
                                fontWeight: 'normal',
                              }}
                            >
                              Tail (hex)
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          {frontendStats!.recentBreaks!.map((brk, idx) => (
                            <tr key={idx}>
                              <td style={{ padding: '1px 4px' }}>{brk.frameIndex}</td>
                              <td style={{ padding: '1px 4px', textAlign: 'right' }}>
                                {formatBytes(brk.byteOffset)}
                              </td>
                              <td
                                style={{
                                  padding: '1px 4px',
                                  fontFamily: 'monospace',
                                  maxWidth: '140px',
                                  overflow: 'hidden',
                                  textOverflow: 'ellipsis',
                                  whiteSpace: 'nowrap',
                                }}
                              >
                                {brk.tail}
                              </td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </td>
                </tr>
              )}
              <tr>
                <td style={{ padding: '2px 8px 2px 0', color: 'var(--color-text-muted)' }}>
                  Control mode reconnects
                </td>
                <td style={{ padding: '2px 0', textAlign: 'right' }}>
                  {backendStats?.controlModeReconnects ?? 0}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
