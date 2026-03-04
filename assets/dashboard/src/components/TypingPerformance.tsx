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

  return (
    <div className="typing-perf">
      <div className="typing-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">Typing Performance</span>
        </button>
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
            {dist && <Histogram dist={dist} median={stats.median} p99={stats.p99} />}
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
}: {
  dist: LatencyDistribution;
  median: number;
  p99: number;
}) {
  const { buckets, maxCount, maxMs } = dist;

  // Generous padding so centered labels never clip
  const padL = 8;
  const padR = 8;
  const chartW = 200;
  const chartH = 44;
  const marginBottom = 10;
  const plotW = chartW - padL - padR;
  const plotH = chartH - marginBottom;

  const barW = plotW / buckets.length;

  const toX = (ms: number) => padL + Math.min(ms / maxMs, 1) * plotW;

  const medianX = toX(median);
  const p99X = toX(p99);
  const medianColor = colorForMs(median);
  const p99Color = colorForMs(p99);

  return (
    <div className="typing-perf__chart">
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

const SEGMENT_COLORS: Record<string, string> = {
  dispatch: 'rgba(160, 160, 160, 0.7)',
  sendKeys: 'rgba(100, 160, 220, 0.7)',
  echo: 'rgba(160, 100, 180, 0.7)',
  frameSend: 'rgba(80, 160, 180, 0.7)',
  eventLoopLag: 'rgba(180, 170, 80, 0.7)',
  wireResidual: 'rgba(190, 150, 80, 0.7)',
  render: 'rgba(80, 170, 120, 0.7)',
};

const SEGMENT_LABELS: Record<string, string> = {
  dispatch: 'dispatch',
  sendKeys: 'sendKeys',
  echo: 'echo',
  frameSend: 'frameSend',
  eventLoopLag: 'evtLoop',
  wireResidual: 'wire',
  render: 'render',
};

const SEGMENTS = [
  'dispatch',
  'sendKeys',
  'echo',
  'frameSend',
  'eventLoopLag',
  'wireResidual',
  'render',
] as const;

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
  const total = breakdown.total;
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
            const value = breakdown[seg];
            const pct = (value / total) * 100;
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
            const value = breakdown[seg];
            if (value < 0.05) return null;
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
  const p50 = inputLatency.getBreakdown('p50');
  const p99 = inputLatency.getBreakdown('p99');
  if (!p50 && !p99) return null;

  const maxTotal = Math.max(p50?.total ?? 0, p99?.total ?? 0);

  return (
    <div className="typing-perf__breakdown" data-testid="latency-breakdown">
      {p50 && (
        <BreakdownRow label="P50" breakdown={p50} scale={maxTotal > 0 ? p50.total / maxTotal : 1} />
      )}
      {p99 && (
        <BreakdownRow label="P99" breakdown={p99} scale={maxTotal > 0 ? p99.total / maxTotal : 1} />
      )}
    </div>
  );
}
