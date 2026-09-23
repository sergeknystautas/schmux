import { useEffect, useState } from 'react';
import { getUsage } from '../lib/api';
import type { UsageSnapshotResponse, UsageProviderInfo, UsageWindow } from '../lib/types.generated';

const PROVIDER_NAMES: Record<string, string> = {
  anthropic: 'Claude',
  openai: 'Codex',
  moonshot: 'Kimi',
  zai: 'GLM',
  minimax: 'MiniMax',
};

const WINDOWS_WITHOUT_REPORTED_DURATION = new Set([
  'five_hour',
  'seven_day',
  'seven_day_opus',
  'seven_day_sonnet',
  'primary',
  'secondary',
  'overage',
]);

function providerName(id: string): string {
  return PROVIDER_NAMES[id] ?? id;
}

function windowName(window: UsageWindow): string {
  if (window.duration_minutes) {
    const minutes = window.duration_minutes;
    if (minutes % 1440 === 0) return `${minutes / 1440}-day`;
    if (minutes % 60 === 0) return `${minutes / 60}-hour`;
    return `${minutes}-minute`;
  }
  const names: Record<string, string> = {
    five_hour: '5-hour',
    seven_day: '7-day',
    seven_day_opus: '7-day Opus',
    seven_day_sonnet: '7-day Sonnet',
    primary: 'Primary',
    secondary: 'Secondary',
    overage: 'Extra usage',
  };
  return names[window.id] ?? window.id;
}

function windowDurationMinutes(window: UsageWindow): number | undefined {
  if (window.duration_minutes && window.duration_minutes > 0) return window.duration_minutes;
  if (window.id === 'five_hour') return 300;
  if (['seven_day', 'seven_day_opus', 'seven_day_sonnet'].includes(window.id)) {
    return 10080;
  }
  return undefined;
}

function isDisplayableWindow(window: UsageWindow): boolean {
  return (
    (window.duration_minutes && window.duration_minutes > 0) ||
    WINDOWS_WITHOUT_REPORTED_DURATION.has(window.id)
  );
}

function isExhaustedWindow(window: UsageWindow, now: number): boolean {
  return (
    window.resets_at != null &&
    window.resets_at * 1000 > now &&
    window.used_percent != null &&
    window.used_percent >= 100
  );
}

function WindowBalance({ window, now }: { window: UsageWindow; now: number }) {
  const remaining = window.resets_at ? window.resets_at * 1000 - now : undefined;
  const duration = windowDurationMinutes(window);
  const expired = remaining != null && remaining <= 0;
  const exhausted = isExhaustedWindow(window, now);
  const balance =
    !expired && !exhausted && remaining != null && duration && window.used_percent != null
      ? Math.max(0, 1 - remaining / (duration * 60_000)) * 100 - window.used_percent
      : undefined;
  const amount = balance == null ? undefined : Math.round(Math.abs(balance));
  const label = expired
    ? 'Awaiting update'
    : exhausted
      ? 'Limit reached'
      : amount == null
        ? 'N/A'
        : amount === 0
          ? 'On pace'
          : `${amount}% ${balance! > 0 ? 'reserve' : 'deficit'}`;
  const tone = exhausted
    ? 'exhausted'
    : amount
      ? balance! > 0
        ? 'reserve'
        : 'deficit'
      : 'unknown';
  const timeLeft =
    remaining == null
      ? 'N/A'
      : remaining <= 0
        ? '0h'
        : remaining >= 86_400_000
          ? `${Math.floor(remaining / 86_400_000)}d`
          : remaining >= 3_600_000
            ? `${Math.floor(remaining / 3_600_000)}h`
            : '<1h';
  return (
    <div className="plan-usage__balance">
      <span>{windowName(window)}</span>
      <span className={`plan-usage__balance--${tone}`}>{label}</span>
      <span className="plan-usage__remaining">
        {timeLeft}
        {remaining != null && ' left'}
      </span>
    </div>
  );
}

function ProviderCard({ provider, now }: { provider: UsageProviderInfo; now: number }) {
  const displayable = provider.windows.filter(isDisplayableWindow);
  const maxExhaustedKnownDuration = displayable.reduce((max, window) => {
    if (!isExhaustedWindow(window, now)) return max;
    const duration = windowDurationMinutes(window);
    if (duration == null) return max;
    return Math.max(max, duration);
  }, 0);
  const windows =
    maxExhaustedKnownDuration === 0
      ? displayable
      : displayable.filter((window) => {
          const duration = windowDurationMinutes(window);
          if (duration == null) return true;
          return duration >= maxExhaustedKnownDuration;
        });
  return (
    <div className="plan-usage__provider">
      <div className="plan-usage__name">{providerName(provider.provider)}</div>
      {windows.map((window) => (
        <div key={window.id} className="plan-usage__meta">
          <WindowBalance window={window} now={now} />
        </div>
      ))}
      {windows.length === 0 && <div className="plan-usage__meta">Window unavailable</div>}
    </div>
  );
}

export default function PlanUsagePanel() {
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('plan-usage-collapsed') === '1'
  );
  const [snapshot, setSnapshot] = useState<UsageSnapshotResponse | null>(null);
  const [error, setError] = useState(false);
  const [now, setNow] = useState(Date.now);

  // Poll /api/usage every 60s, paused while the tab is hidden — the same
  // shape as TmuxDiagnostic's poll loop.
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return;
      setNow(Date.now());
      try {
        const data = await getUsage();
        if (!cancelled) {
          setSnapshot(data);
          setError(false);
        }
      } catch {
        if (!cancelled) setError(true);
      }
    };
    load();
    const id = setInterval(load, 60_000);
    const onVisibility = () => {
      if (document.visibilityState === 'visible') void load();
    };
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      cancelled = true;
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, []);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('plan-usage-collapsed', next ? '1' : '0');
      return next;
    });
  };

  return (
    <div className="typing-perf">
      <div className="typing-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed} aria-expanded={!collapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">Plan Usage</span>
        </button>
      </div>
      {!collapsed && error && (
        <div className="typing-perf__empty" role="status">
          Unable to refresh plan usage.
        </div>
      )}
      {!collapsed &&
        (!snapshot ? (
          !error && <div className="typing-perf__empty">Loading plan usage…</div>
        ) : snapshot.providers.length === 0 ? (
          <div className="typing-perf__empty">No plan quota reported yet.</div>
        ) : (
          snapshot.providers.map((p) => <ProviderCard key={p.provider} provider={p} now={now} />)
        ))}
    </div>
  );
}
