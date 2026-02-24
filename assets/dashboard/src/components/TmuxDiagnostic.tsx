import { useState, useEffect } from 'react';

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
      {!collapsed &&
        rows.map((row) => (
          <div key={row.label} className="tmux-diag__row" title={row.description}>
            <span className="tmux-diag__dot" style={{ color: healthDotColor(row.health) }}>
              ●
            </span>
            <span className="tmux-diag__label">{row.label}</span>
            <span className="tmux-diag__value">{row.value}</span>
          </div>
        ))}
    </div>
  );
}
