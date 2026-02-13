import { useState, useEffect } from 'react';
import { inputLatency } from '../lib/inputLatency';
import type { LatencyDistribution } from '../lib/inputLatency';

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

  useEffect(() => {
    const id = setInterval(() => setTick((t) => t + 1), 500);
    return () => clearInterval(id);
  }, []);

  const stats = inputLatency.getStats();
  const dist = inputLatency.getDistribution();

  return (
    <div className="typing-perf">
      <div className="typing-perf__header">
        <span className="nav-section-title">Typing Performance</span>
        {stats && stats.count > 0 && (
          <button className="typing-perf__reset" onClick={() => inputLatency.reset()}>
            Reset
          </button>
        )}
      </div>

      {!stats || stats.count < 3 ? (
        <div className="typing-perf__empty">Type in a terminal to collect samples</div>
      ) : (
        dist && <Histogram dist={dist} median={stats.median} p99={stats.p99} />
      )}
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
