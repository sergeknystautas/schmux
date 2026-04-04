import { useState, useRef, useEffect } from 'react';
import type { SequenceBreakRecord } from '../lib/streamDiagnostics';
import type { FrameSizeStats, FrameSizeDistribution } from '../lib/streamDiagnostics';

export interface BackendStats {
  eventsDelivered: number;
  eventsDropped: number;
  bytesDelivered: number;
  controlModeReconnects: number;
  bytesPerSec?: number;
  clientFanOutDrops?: number;
  fanOutDrops?: number;
  currentSeq?: number;
  logOldestSeq?: number;
  logTotalBytes?: number;
}

interface FrontendStats {
  framesReceived: number;
  bytesReceived: number;
  bootstrapCount: number;
  sequenceBreaks: number;
  recentBreaks?: SequenceBreakRecord[];
  frameSizeStats?: FrameSizeStats | null;
  frameSizeDist?: FrameSizeDistribution | null;
  followLostCount?: number;
  scrollSuppressedCount?: number;
  scrollCoalesceHits?: number;
  resizeCount?: number;
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
  const parserDrops = backendStats?.eventsDropped ?? 0;
  const clientDrops = backendStats?.clientFanOutDrops ?? 0;
  const trackerDrops = backendStats?.fanOutDrops ?? 0;
  const drops = parserDrops + clientDrops + trackerDrops;
  const seqBreaks = frontendStats?.sequenceBreaks ?? 0;
  const followLost = frontendStats?.followLostCount ?? 0;

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
    <div className="stream-metrics relative" ref={panelRef}>
      <div className="connection-pill cursor-pointer" onClick={() => setExpanded(!expanded)}>
        <span>{formatCount(frames)} frames</span>
        <span
          className={drops > 0 ? 'warning' : ''}
          data-severity={drops > 0 ? 'warning' : undefined}
        >
          {drops} drops
        </span>
        <span className={seqBreaks > 0 ? 'warning' : ''}>{seqBreaks} seq breaks</span>
        {followLost > 0 && (
          <span className="warning" data-testid="follow-lost-pill">
            {followLost} follow lost
          </span>
        )}
      </div>
      {onDiagnosticCapture && (
        <button className="btn btn--sm btn--secondary" onClick={onDiagnosticCapture}>
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M6 18h8"></path>
            <path d="M3 22h18"></path>
            <path d="M14 22a7 7 0 1 0 0-14h-1"></path>
            <path d="M9 14h2"></path>
            <path d="M9 12a2 2 0 0 1-2-2V6h6v4a2 2 0 0 1-2 2Z"></path>
            <path d="M12 6V3a1 1 0 0 0-1-1H9a1 1 0 0 0-1 1v3"></path>
          </svg>
          <span>Diagnose</span>
        </button>
      )}
      {expanded && (
        <div className="stream-metrics__dropdown">
          <table className="stream-metrics__table">
            <tbody>
              <tr>
                <td className="stream-metrics__label">Frames received</td>
                <td className="stream-metrics__value">{frames}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Bytes received</td>
                <td className="stream-metrics__value">{formatBytes(bytes)}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Events delivered (backend)</td>
                <td className="stream-metrics__value">{backendStats?.eventsDelivered ?? '—'}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Events dropped (parser)</td>
                <td className={`stream-metrics__value${parserDrops > 0 ? ' warning' : ''}`}>
                  {parserDrops}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Drops (client fan-out)</td>
                <td className={`stream-metrics__value${clientDrops > 0 ? ' warning' : ''}`}>
                  {clientDrops}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Drops (tracker fan-out)</td>
                <td className={`stream-metrics__value${trackerDrops > 0 ? ' warning' : ''}`}>
                  {trackerDrops}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Bytes delivered (backend)</td>
                <td className="stream-metrics__value">
                  {formatBytes(backendStats?.bytesDelivered ?? 0)}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Throughput</td>
                <td className="stream-metrics__value">
                  {backendStats?.bytesPerSec ? formatBytes(backendStats.bytesPerSec) + '/s' : '—'}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Bootstrap count</td>
                <td className="stream-metrics__value">{frontendStats?.bootstrapCount ?? '—'}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Sequence breaks</td>
                <td className={`stream-metrics__value${seqBreaks > 0 ? ' warning' : ''}`}>
                  {seqBreaks}
                </td>
              </tr>
              {seqBreaks > 0 && (frontendStats?.recentBreaks?.length ?? 0) > 0 && (
                <tr>
                  <td colSpan={2} className="p-0">
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        setShowBreakDetails(!showBreakDetails);
                      }}
                      className="stream-metrics__toggle-btn"
                      data-testid="toggle-break-details"
                    >
                      {showBreakDetails ? 'hide details' : 'show details'}
                    </button>
                    {showBreakDetails && (
                      <table
                        data-testid="break-details-table"
                        className="stream-metrics__break-table"
                      >
                        <thead>
                          <tr className="text-muted">
                            <th className="text-left">Frame</th>
                            <th className="text-right">Offset</th>
                            <th className="text-left">Tail (hex)</th>
                          </tr>
                        </thead>
                        <tbody>
                          {frontendStats!.recentBreaks!.map((brk, idx) => (
                            <tr key={idx}>
                              <td>{brk.frameIndex}</td>
                              <td className="text-right">{formatBytes(brk.byteOffset)}</td>
                              <td className="stream-metrics__break-tail">{brk.tail}</td>
                            </tr>
                          ))}
                        </tbody>
                      </table>
                    )}
                  </td>
                </tr>
              )}
              <tr>
                <td className="stream-metrics__label">Server seq (output log)</td>
                <td className="stream-metrics__value">{backendStats?.currentSeq ?? '—'}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Log oldest seq</td>
                <td className="stream-metrics__value">{backendStats?.logOldestSeq ?? '—'}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Log total bytes</td>
                <td className="stream-metrics__value">
                  {backendStats?.logTotalBytes != null
                    ? formatBytes(backendStats.logTotalBytes)
                    : '—'}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Control mode reconnects</td>
                <td className="stream-metrics__value">
                  {backendStats?.controlModeReconnects ?? 0}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Follow lost (true→false)</td>
                <td className={`stream-metrics__value${followLost > 0 ? ' warning' : ''}`}>
                  {followLost}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Scroll suppressed</td>
                <td className="stream-metrics__value">
                  {frontendStats?.scrollSuppressedCount ?? 0}
                </td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Write coalesce hits</td>
                <td className="stream-metrics__value">{frontendStats?.scrollCoalesceHits ?? 0}</td>
              </tr>
              <tr>
                <td className="stream-metrics__label">Resizes</td>
                <td className="stream-metrics__value">{frontendStats?.resizeCount ?? 0}</td>
              </tr>
              {frontendStats?.frameSizeDist && frontendStats?.frameSizeStats && (
                <tr>
                  <td colSpan={2} style={{ padding: '12px 0 2px 0' }}>
                    <div className="stream-metrics__dist-header">Frame size distribution</div>
                    <FrameSizeHistogram
                      dist={frontendStats.frameSizeDist}
                      median={frontendStats.frameSizeStats.median}
                      p90={frontendStats.frameSizeStats.p90}
                    />
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

// Map a bucket's byte range to a bar color
function frameSizeBarColor(bytes: number): string {
  if (bytes < 1024) return '#0dbc79'; // green: <1KB
  if (bytes < 4096) return '#e5e510'; // yellow: <4KB
  if (bytes < 16384) return '#e5a010'; // orange: <16KB
  return '#f14c4c'; // red: ≥16KB
}

function frameSizeColor(bytes: number): string {
  if (bytes < 1024) return '#0dbc79';
  if (bytes < 4096) return '#e5e510';
  return '#f14c4c';
}

function FrameSizeHistogram({
  dist,
  median,
  p90,
}: {
  dist: FrameSizeDistribution;
  median: number;
  p90: number;
}) {
  const { buckets, maxCount, maxBytes } = dist;

  const padL = 8;
  const padR = 8;
  const chartW = 200;
  const chartH = 44;
  const marginBottom = 10;
  const plotW = chartW - padL - padR;
  const plotH = chartH - marginBottom;

  const barW = plotW / buckets.length;
  const bucketSize = maxBytes / buckets.length;

  const toX = (bytes: number) => padL + Math.min(bytes / maxBytes, 1) * plotW;

  const medianX = toX(median);
  const p90X = toX(p90);
  const medianColor = frameSizeColor(median);
  const p90Color = frameSizeColor(p90);

  return (
    <div data-testid="frame-size-histogram">
      <svg
        width="100%"
        viewBox={`0 0 ${chartW} ${chartH}`}
        style={{ display: 'block', overflow: 'visible' }}
      >
        {/* Bars */}
        {buckets.map((count, i) => {
          if (count === 0) return null;
          const h = maxCount > 0 ? (count / maxCount) * plotH : 0;
          const x = padL + i * barW;
          const y = plotH - h;
          return (
            <rect
              key={i}
              x={x}
              y={y}
              width={Math.max(barW - 0.5, 0.5)}
              height={h}
              fill={frameSizeBarColor(i * bucketSize)}
              opacity={0.85}
            />
          );
        })}

        {/* P50 vertical line (solid) */}
        <line
          x1={medianX}
          y1={0}
          x2={medianX}
          y2={plotH}
          stroke={medianColor}
          strokeWidth={1}
          opacity={0.7}
        />
        <text
          x={medianX}
          y={-3}
          textAnchor="middle"
          fill={medianColor}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          P50
        </text>
        <text
          x={medianX}
          y={chartH - 1}
          textAnchor="middle"
          fill={medianColor}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {formatBytes(median)}
        </text>

        {/* P90 vertical line (dashed) */}
        <line
          x1={p90X}
          y1={0}
          x2={p90X}
          y2={plotH}
          stroke={p90Color}
          strokeWidth={1}
          strokeDasharray="2,2"
          opacity={0.7}
        />
        <text
          x={p90X}
          y={-3}
          textAnchor="middle"
          fill={p90Color}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          P90
        </text>
        <text
          x={p90X}
          y={chartH - 1}
          textAnchor="middle"
          fill={p90Color}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {formatBytes(p90)}
        </text>

        {/* X-axis baseline */}
        <line
          x1={padL}
          y1={plotH}
          x2={padL + plotW}
          y2={plotH}
          stroke="rgba(255,255,255,0.15)"
          strokeWidth={0.5}
        />
      </svg>
    </div>
  );
}
