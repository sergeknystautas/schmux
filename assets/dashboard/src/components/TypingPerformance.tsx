import { useState, useEffect, useRef } from 'react';
import { inputLatency } from '../lib/inputLatency';
import type { LatencyDistribution, LatencyBreakdown } from '../lib/inputLatency';

function colorForMs(ms: number): string {
  if (ms <= 10) return '#0dbc79';
  if (ms <= 50) return '#e5e510';
  return '#f14c4c';
}

// Map a bucket index (ms) to a bar color
function barColor(ms: number): string {
  if (ms < 5) return '#0dbc79';
  if (ms < 10) return '#0dbc79';
  if (ms < 25) return '#e5e510';
  if (ms < 50) return '#e5a010';
  return '#f14c4c';
}

export default function TypingPerformance() {
  const [, setTick] = useState(0);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('typing-perf-collapsed') === '1'
  );

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('typing-perf-collapsed', next ? '1' : '0');
      return next;
    });
  };

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 500);
    return () => clearInterval(id);
  }, []);

  const stats = inputLatency.getStats();
  const dist = inputLatency.getDistribution();

  const machineKey = inputLatency.getMachineKey();
  const sessionType = machineKey === 'local' ? 'local' : 'remote';

  return (
    <div className="typing-perf">
      <div className="typing-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">Typing Performance</span>
        </button>
        {sessionType && <span className="typing-perf__session-type">{sessionType}</span>}
        {!collapsed && stats && stats.count > 0 && (
          <button className="typing-perf__reset" onClick={() => inputLatency.reset()}>
            Reset
          </button>
        )}
      </div>

      {!collapsed &&
        (!stats || stats.count < 3 ? (
          <div className="typing-perf__empty">Type in a terminal to collect samples</div>
        ) : (
          <>
            {dist && (
              <Histogram
                dist={dist}
                median={stats.median}
                p99={stats.p99}
                p25={stats.p25}
                p75={stats.p75}
              />
            )}
            <LatencyBreakdownBars />
          </>
        ))}
    </div>
  );
}

function Histogram({
  dist,
  median,
  p99,
  p25,
  p75,
}: {
  dist: LatencyDistribution;
  median: number;
  p99: number;
  p25: number;
  p75: number;
}) {
  const { buckets, maxCount, maxMs } = dist;

  const padL = 0;
  const padR = 8;
  const chartW = 200;
  const chartH = 44;
  const marginBottom = 10;
  const plotW = chartW - padL - padR;
  const plotH = chartH - marginBottom;

  const barW = plotW / buckets.length;

  const toX = (ms: number) => padL + Math.min(Math.max(ms, 0) / maxMs, 1) * plotW;

  const medianX = toX(median);
  const p99X = toX(p99);
  const medianColor = colorForMs(median);
  const p99Color = colorForMs(p99);

  // IQR band (P25–P75)
  const iqrLoX = toX(p25);
  const iqrHiX = toX(p75);

  return (
    <div className="typing-perf__chart">
      <svg
        width="100%"
        viewBox={`0 0 ${chartW} ${chartH}`}
        style={{ display: 'block', overflow: 'visible' }}
      >
        {/* IQR shaded band (P25–P75) */}
        <rect
          x={iqrLoX}
          y={0}
          width={iqrHiX - iqrLoX}
          height={plotH}
          fill="rgba(255,255,255,0.06)"
        />

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
              fill={barColor(i)}
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
          {Math.round(median)}ms
        </text>

        {/* P99 vertical line (dashed) */}
        <line
          x1={p99X}
          y1={0}
          x2={p99X}
          y2={plotH}
          stroke={p99Color}
          strokeWidth={1}
          strokeDasharray="2,2"
          opacity={0.7}
        />
        <text
          x={p99X}
          y={-3}
          textAnchor="middle"
          fill={p99Color}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          P99
        </text>
        <text
          x={p99X}
          y={chartH - 1}
          textAnchor="middle"
          fill={p99Color}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {Math.round(p99)}ms
        </text>

        {/* P75 label on x-axis */}
        <text
          x={iqrHiX}
          y={chartH - 1}
          textAnchor="middle"
          fill="rgba(255,255,255,0.4)"
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {Math.round(p75)}ms
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

// Causal ordering: follows the keystroke's journey through the system
const SEGMENTS = [
  'handler', // schmux receives and decodes
  'tmuxCmd', // keystroke travels to tmux (transport)
  'paneOutput', // tmux + agent processes
  'wsWrite', // schmux sends output frame
  'jsQueue', // browser event loop picks it up
  'xterm', // terminal renders
  'network', // system jitter residual
] as const;

const SEGMENT_COLORS: Record<string, string> = {
  // schmux (ours) — green family
  handler: 'rgba(80, 170, 120, 0.7)',
  wsWrite: 'rgba(100, 190, 140, 0.7)',
  // host environment (theirs) — gray family
  tmuxCmd: 'rgba(160, 160, 160, 0.7)',
  paneOutput: 'rgba(130, 130, 130, 0.7)',
  // browser — blue family
  jsQueue: 'rgba(80, 130, 200, 0.7)',
  xterm: 'rgba(100, 150, 220, 0.7)',
  // catch-all
  network: 'rgba(190, 150, 80, 0.5)',
};

const SEGMENT_LABELS: Record<string, string> = {
  handler: 'handler',
  wsWrite: 'ws write',
  tmuxCmd: 'transport',
  paneOutput: 'tmux + agent',
  jsQueue: 'js queue',
  xterm: 'xterm',
  network: 'system',
};

function BreakdownRow({
  label,
  breakdown,
  scale,
}: {
  label: string;
  breakdown: LatencyBreakdown;
  scale: number;
}) {
  const [showTooltip, setShowTooltip] = useState(false);
  const rowRef = useRef<HTMLDivElement>(null);
  const { total, segmentSum } = breakdown;
  if (total <= 0) return null;

  return (
    <div
      className="typing-perf__bar-row"
      ref={rowRef}
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      <span className="typing-perf__bar-label">{label}</span>
      <span className="typing-perf__bar-total">{Math.round(total)}ms</span>
      <div className="typing-perf__bar-track">
        <div className="typing-perf__bar-fill" style={{ width: `${scale * 100}%` }}>
          {SEGMENTS.map((seg) => {
            const value = breakdown[seg as keyof LatencyBreakdown] as number;
            if (value == null) return null;
            const pct = segmentSum > 0 ? (value / segmentSum) * 100 : 0;
            if (pct < 0.5) return null;
            return (
              <div
                key={seg}
                className="typing-perf__bar-segment"
                style={{
                  width: `${pct}%`,
                  backgroundColor: SEGMENT_COLORS[seg],
                }}
              />
            );
          })}
        </div>
      </div>
      {showTooltip && (
        <div className="typing-perf__tooltip" data-testid="breakdown-tooltip">
          {SEGMENTS.map((seg) => {
            const value = breakdown[seg as keyof LatencyBreakdown] as number;
            if (value == null || value < 0.05) return null;
            return (
              <div key={seg} className="typing-perf__tooltip-row">
                <span
                  className="typing-perf__tooltip-swatch"
                  style={{ backgroundColor: SEGMENT_COLORS[seg] }}
                />
                <span className="typing-perf__tooltip-name">{SEGMENT_LABELS[seg]}</span>
                <span className="typing-perf__tooltip-value">{value.toFixed(1)}ms</span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

function LatencyBreakdownBars() {
  const typical = inputLatency.getBreakdown('typical');
  const outlier = inputLatency.getBreakdown('outlier');
  if (!typical && !outlier) return null;

  const maxTotal = Math.max(typical?.total ?? 0, outlier?.total ?? 0);

  return (
    <div className="typing-perf__breakdown" data-testid="latency-breakdown">
      {typical ? (
        <BreakdownRow
          label="Typical"
          breakdown={typical}
          scale={maxTotal > 0 ? typical.total / maxTotal : 1}
        />
      ) : (
        <div className="typing-perf__insufficient">Typical: insufficient data</div>
      )}
      {outlier ? (
        <BreakdownRow
          label="Outlier"
          breakdown={outlier}
          scale={maxTotal > 0 ? outlier.total / maxTotal : 1}
        />
      ) : (
        <div className="typing-perf__insufficient">Outlier: insufficient data</div>
      )}
    </div>
  );
}
