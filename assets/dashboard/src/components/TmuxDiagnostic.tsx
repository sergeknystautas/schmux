import { useState, useEffect, useRef } from 'react';
import { getTmuxHealth, getTmuxHealthVersion, computeDistribution } from '../lib/tmuxHealth';
import type { TmuxHealthData, TmuxHealthDistribution } from '../lib/tmuxHealth';

type TmuxCounts = {
  sessions: number;
  attach: number;
  tmux: number;
};

type HealthColor = 'green' | 'yellow' | 'red' | 'neutral';

function healthDotColor(health: HealthColor): string {
  switch (health) {
    case 'green':
      return '#0dbc79';
    case 'yellow':
      return '#e5e510';
    case 'red':
      return '#f14c4c';
    case 'neutral':
      return 'var(--color-text-tertiary)';
  }
}

function attachHealth(counts: TmuxCounts): HealthColor {
  if (counts.attach <= counts.sessions) return 'green';
  if (counts.attach <= counts.sessions + 2) return 'yellow';
  return 'red';
}

function tmuxProcHealth(counts: TmuxCounts): HealthColor {
  const expected = counts.sessions + counts.attach + 2;
  if (counts.tmux <= expected) return 'green';
  if (counts.tmux <= expected + 3) return 'yellow';
  return 'red';
}

export default function TmuxDiagnostic() {
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('tmux-diag-collapsed') === '1'
  );
  const [counts, setCounts] = useState<TmuxCounts | null>(null);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('tmux-diag-collapsed', next ? '1' : '0');
      return next;
    });
  };

  // Poll tmux leak API
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return;
      try {
        const res = await fetch('/api/debug/tmux-leak', { credentials: 'same-origin' });
        if (!res.ok) return;
        const data = await res.json();
        if (cancelled) return;
        setCounts({
          sessions: data?.tmux_sessions?.count ?? 0,
          attach: data?.os_processes?.attach_session_process_count ?? 0,
          tmux: data?.os_processes?.tmux_process_count ?? 0,
        });
      } catch {
        // Best-effort dev diagnostics only.
      }
    };
    load();
    const id = setInterval(load, 1000);
    const onVisibility = () => {
      if (document.visibilityState === 'visible') {
        void load();
      }
    };
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      cancelled = true;
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, []);

  // Poll health probe data (arrives via WebSocket stats, stored in singleton)
  const [, setHealthTick] = useState(0);
  const healthVersionRef = useRef(getTmuxHealthVersion());
  useEffect(() => {
    const id = setInterval(() => {
      const v = getTmuxHealthVersion();
      if (v !== healthVersionRef.current) {
        healthVersionRef.current = v;
        setHealthTick((t) => t + 1);
      }
    }, 500);
    return () => clearInterval(id);
  }, []);

  if (!counts) return null;

  const rows: { label: string; value: number; health: HealthColor; description: string }[] = [
    {
      label: 'Sessions',
      value: counts.sessions,
      health: 'neutral',
      description: 'Active tmux sessions on this machine',
    },
    {
      label: 'Attach procs',
      value: counts.attach,
      health: attachHealth(counts),
      description: 'Control-mode processes watching sessions (expect ≤ sessions)',
    },
    {
      label: 'Tmux procs',
      value: counts.tmux,
      health: tmuxProcHealth(counts),
      description: 'Total OS processes with "tmux" in command line',
    },
  ];

  return (
    <div className="tmux-diag">
      <button className="diag-pane__toggle" onClick={toggleCollapsed}>
        <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
          ▶
        </span>
        <span className="nav-section-title">Tmux</span>
      </button>
      {!collapsed && (
        <>
          {rows.map((row) => (
            <div
              key={row.label}
              className="tmux-diag__row"
              title={row.description}
              data-testid="tmux-diag-row"
              data-label={row.label.toLowerCase().replace(/\s+/g, '-')}
            >
              <span className="tmux-diag__dot" style={{ color: healthDotColor(row.health) }}>
                ●
              </span>
              <span className="tmux-diag__label">{row.label}</span>
              <span className="tmux-diag__value">{row.value}</span>
            </div>
          ))}
          <ProbeHistogram />
        </>
      )}
    </div>
  );
}

function rttColor(us: number): string {
  if (us <= 2000) return '#0dbc79'; // ≤2ms green
  if (us <= 10000) return '#e5e510'; // ≤10ms yellow
  if (us <= 50000) return '#e5a010'; // ≤50ms orange
  return '#f14c4c'; // >50ms red
}

function barColorForRtt(us: number): string {
  if (us < 1000) return '#0dbc79';
  if (us < 5000) return '#0dbc79';
  if (us < 10000) return '#e5e510';
  if (us < 25000) return '#e5a010';
  return '#f14c4c';
}

function formatRtt(us: number): string {
  if (us >= 1000) return `${(us / 1000).toFixed(1)}ms`;
  return `${Math.round(us)}μs`;
}

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.round((seconds % 3600) / 60);
  return `${h}h${m}m`;
}

function ProbeHistogram() {
  const health = getTmuxHealth();
  if (!health || health.count < 3) {
    return (
      <div
        style={{
          fontSize: '10px',
          color: 'var(--color-text-tertiary)',
          padding: '4px 8px 2px',
        }}
      >
        Collecting RTT probes...
      </div>
    );
  }

  return (
    <div style={{ padding: '4px 0 0' }}>
      <div
        style={{
          fontSize: '9px',
          color: 'var(--color-text-muted)',
          padding: '0 8px 2px',
          fontFamily: 'var(--font-mono)',
          display: 'flex',
          justifyContent: 'space-between',
        }}
      >
        <span>
          RTT P50 <span style={{ color: rttColor(health.p50_us) }}>{formatRtt(health.p50_us)}</span>
        </span>
        <span>
          P99 <span style={{ color: rttColor(health.p99_us) }}>{formatRtt(health.p99_us)}</span>
        </span>
        <span>
          {health.count} / {formatUptime(health.uptime_s)}
        </span>
      </div>
      <RttHistogram health={health} dist={computeDistribution(health)} />
    </div>
  );
}

function RttHistogram({
  health,
  dist,
}: {
  health: TmuxHealthData;
  dist: TmuxHealthDistribution | null;
}) {
  if (!dist) return null;
  const { buckets, maxCount, maxUs, bucketUs } = dist;
  const { p50_us, p99_us } = health;

  const padL = 8;
  const padR = 8;
  const chartW = 200;
  const chartH = 44;
  const marginBottom = 10;
  const plotW = chartW - padL - padR;
  const plotH = chartH - marginBottom;
  const barW = plotW / buckets.length;
  const toX = (us: number) => padL + Math.min(us / maxUs, 1) * plotW;

  const p50X = toX(p50_us);
  const p99X = toX(p99_us);
  const p50C = rttColor(p50_us);
  const p99C = rttColor(p99_us);

  return (
    <div className="typing-perf__chart">
      <svg
        width="100%"
        viewBox={`0 0 ${chartW} ${chartH}`}
        style={{ display: 'block', overflow: 'visible' }}
      >
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
              fill={barColorForRtt(i * bucketUs)}
              opacity={0.85}
            />
          );
        })}
        <line x1={p50X} y1={0} x2={p50X} y2={plotH} stroke={p50C} strokeWidth={1} opacity={0.7} />
        <text
          x={p50X}
          y={-3}
          textAnchor="middle"
          fill={p50C}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          P50
        </text>
        <text
          x={p50X}
          y={chartH - 1}
          textAnchor="middle"
          fill={p50C}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {formatRtt(p50_us)}
        </text>
        <line
          x1={p99X}
          y1={0}
          x2={p99X}
          y2={plotH}
          stroke={p99C}
          strokeWidth={1}
          strokeDasharray="2,2"
          opacity={0.7}
        />
        <text
          x={p99X}
          y={-3}
          textAnchor="middle"
          fill={p99C}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          P99
        </text>
        <text
          x={p99X}
          y={chartH - 1}
          textAnchor="middle"
          fill={p99C}
          fontSize={7}
          fontFamily="Menlo, Monaco, 'Courier New', monospace"
        >
          {formatRtt(p99_us)}
        </text>
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
