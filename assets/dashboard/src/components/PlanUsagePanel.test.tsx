import { render, screen, fireEvent, act, cleanup } from '@testing-library/react';
import PlanUsagePanel from './PlanUsagePanel';
import { getUsage } from '../lib/api';
import type { UsageSnapshotResponse, UsageWindow } from '../lib/types.generated';

vi.mock('../lib/api', () => ({ getUsage: vi.fn() }));

const now = new Date('2026-09-18T12:00:00Z').getTime();
const resetIn = (hours: number) => now / 1000 + hours * 3600;
function report(windows: UsageWindow[], provider: string = 'openai'): UsageSnapshotResponse {
  return {
    providers: [
      {
        provider,
        updated_at: '2026-09-18T11:00:00Z',
        plan_type: 'prolite',
        limit_id: 'codex',
        status: 'allowed',
        credits: { has_credits: true, unlimited: false, balance: '1447.9074337500' },
        windows,
      },
    ],
  };
}
async function show() {
  await act(async () => {
    render(<PlanUsagePanel />);
  });
}
beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  vi.mocked(getUsage)
    .mockReset()
    .mockResolvedValue(
      report([{ id: 'primary', used_percent: 29, duration_minutes: 10080, resets_at: resetIn(84) }])
    );
  localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

test.each([
  [29, '21% reserve'],
  [60, '10% deficit'],
  [50, 'On pace'],
  [0, '50% reserve'],
])('at halfway through the window, %s%% used displays %s', async (used, label) => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'primary', used_percent: used, duration_minutes: 10080, resets_at: resetIn(84) }])
  );
  await show();
  expect(screen.getByText(label)).toBeInTheDocument();
  expect(screen.getByText('3d left')).toBeInTheDocument();
});

test('shows provider once and hides plan, quota, credits, receipt time and usage percentage', async () => {
  await show();
  expect(screen.getAllByText('Codex')).toHaveLength(1);
  expect(screen.getByText('21% reserve')).toBeInTheDocument();
  expect(
    screen.queryByText(/prolite|codex|Credits|Last received|% used|Status:|Resets/)
  ).not.toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test('uses named Claude windows and omits unexplained buckets', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', used_percent: 29, resets_at: resetIn(2.5) },
      { id: 'seven_day', used_percent: 60, resets_at: resetIn(84) },
      { id: 'seven_day_overage_included', used_percent: 45, resets_at: resetIn(84) },
    ])
  );
  await show();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('21% reserve')).toBeInTheDocument();
  expect(screen.getByText('2h left')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('10% deficit')).toBeInTheDocument();
  expect(screen.queryByText('7-day overage')).not.toBeInTheDocument();
  expect(screen.queryByText('seven_day_overage_included')).not.toBeInTheDocument();
  expect(screen.queryByText('N/A')).not.toBeInTheDocument();
  expect(
    Array.from(screen.getByText('5-hour').parentElement!.children).map((cell) => cell.textContent)
  ).toEqual(['5-hour', '21% reserve', '2h left']);
  expect(
    Array.from(screen.getByText('7-day').parentElement!.children).map((cell) => cell.textContent)
  ).toEqual(['7-day', '10% deficit', '3d left']);
});

test.each([
  [53, '2d left'],
  [24, '1d left'],
  [23, '23h left'],
  [5, '5h left'],
  [0.5, '<1h left'],
])('formats %s hours remaining as %s', async (hours, label) => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'primary', used_percent: 29, duration_minutes: 10080, resets_at: resetIn(hours) },
    ])
  );
  await show();
  expect(screen.getByText(label)).toBeInTheDocument();
});

test.each([
  { id: 'primary', resets_at: resetIn(5), used_percent: 29 },
  { id: 'primary', resets_at: resetIn(5), duration_minutes: 10080 },
  { id: 'primary', used_percent: 29, duration_minutes: 10080 },
])('does not fabricate a reserve when required data is missing: %j', async (window) => {
  vi.mocked(getUsage).mockResolvedValue(report([window]));
  await show();
  expect(screen.getAllByText('N/A')).toHaveLength(window.resets_at ? 1 : 2);
  if (window.resets_at) expect(screen.getByText('5h left')).toBeInTheDocument();
  expect(
    screen.queryByText(/Reserve unavailable|Time unavailable|N\/A left/)
  ).not.toBeInTheDocument();
});

test('expires at the reset boundary without showing a reserve for the next window', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'primary', used_percent: 29, duration_minutes: 300, resets_at: resetIn(1 / 60) }])
  );
  await show();
  expect(screen.getByText('<1h left')).toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000);
  });
  expect(screen.getByText('Awaiting update')).toBeInTheDocument();
  expect(screen.getByText('0h left')).toBeInTheDocument();
  expect(screen.queryByText(/% reserve|% deficit/)).not.toBeInTheDocument();
});

test('recalculates with the clock every minute even when the refresh fails', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'primary', used_percent: 29, duration_minutes: 300, resets_at: resetIn(2.5) }])
  );
  await show();
  expect(screen.getByText('21% reserve')).toBeInTheDocument();
  vi.mocked(getUsage).mockRejectedValue(new Error('offline'));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(59_999);
  });
  expect(getUsage).toHaveBeenCalledTimes(1);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(getUsage).toHaveBeenCalledTimes(2);
  await act(async () => {
    await vi.advanceTimersByTimeAsync(120_000);
  });
  expect(screen.getByText('22% reserve')).toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('Unable to refresh plan usage.');
});

test('shows an empty state after receiving no snapshots', async () => {
  vi.mocked(getUsage).mockResolvedValue({ providers: [] });
  await show();
  expect(screen.getByText('No plan quota reported yet.')).toBeInTheDocument();
});

test('reports failed initial fetch separately from empty quota', async () => {
  vi.mocked(getUsage).mockRejectedValue(new Error('offline'));
  await show();
  expect(screen.getByRole('status')).toHaveTextContent('Unable to refresh plan usage.');
  expect(screen.queryByText('No plan quota reported yet.')).not.toBeInTheDocument();
});

test('collapse toggle persists to localStorage', async () => {
  await show();
  fireEvent.click(screen.getByRole('button'));
  expect(localStorage.getItem('plan-usage-collapsed')).toBe('1');
});

test('renders exhausted label and 1d left for Kimi 7-day window at 100%', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report(
      [{ id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(24) }],
      'moonshot'
    )
  );
  await show();
  expect(screen.getByText('Kimi')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  expect(screen.getByText('1d left')).toBeInTheDocument();
});

test('renders exhausted label and 2h left for 5-hour window at 100%', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'five_hour', duration_minutes: 300, used_percent: 100, resets_at: resetIn(2) }])
  );
  await show();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  expect(screen.getByText('2h left')).toBeInTheDocument();
});

test('used_percent above 100 is treated as exhausted (threshold is inclusive and unrounded)', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      {
        id: 'seven_day',
        duration_minutes: 10080,
        used_percent: 150,
        resets_at: resetIn(48),
      },
    ])
  );
  await show();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  expect(screen.queryByText(/% reserve|% deficit/)).not.toBeInTheDocument();
  expect(screen.getByText('2d left')).toBeInTheDocument();
});

test('renders exhausted label for named Claude weekly window without reported duration', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'seven_day', used_percent: 100, resets_at: resetIn(72) }])
  );
  await show();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  expect(screen.getByText('3d left')).toBeInTheDocument();
});

test('renders exhausted label for eligible Primary window without reported duration', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'primary', used_percent: 100, resets_at: resetIn(5) }])
  );
  await show();
  expect(screen.getByText('Primary')).toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  expect(screen.getByText('5h left')).toBeInTheDocument();
  expect(screen.queryByText('N/A')).not.toBeInTheDocument();
});

test('99% used at halfway through the window keeps existing 49% deficit math (not exhausted)', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([{ id: 'primary', used_percent: 99, duration_minutes: 10080, resets_at: resetIn(84) }])
  );
  await show();
  expect(screen.getByText('49% deficit')).toBeInTheDocument();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
});

test.each([
  { id: 'primary', duration_minutes: 10080, used_percent: 100 },
  { id: 'primary', duration_minutes: 10080, resets_at: resetIn(5) },
])('does not render exhausted label when required data is missing: %j', async (window) => {
  vi.mocked(getUsage).mockResolvedValue(report([window]));
  await show();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
  expect(screen.getAllByText('N/A')).toHaveLength(window.resets_at ? 1 : 2);
  if (window.resets_at) expect(screen.getByText('5h left')).toBeInTheDocument();
});

test('100% with reset at or before now shows Awaiting update, not exhausted', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      {
        id: 'primary',
        used_percent: 100,
        duration_minutes: 10080,
        resets_at: now / 1000,
      },
    ])
  );
  await show();
  expect(screen.getByText('Awaiting update')).toBeInTheDocument();
  expect(screen.getByText('0h left')).toBeInTheDocument();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
});

function reportProviders(
  providers: Array<{ provider: string; windows: UsageWindow[] }>
): UsageSnapshotResponse {
  return {
    providers: providers.map((p) => ({
      provider: p.provider,
      updated_at: '2026-09-18T11:00:00Z',
      plan_type: 'prolite',
      limit_id: 'codex',
      status: 'allowed',
      credits: { has_credits: true, unlimited: false, balance: '1447.9074337500' },
      windows: p.windows,
    })),
  };
}

test('omits the five-hour row when an exhausted seven-day window is present', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test('keeps the seven-day row when the exhausted window is the five-hour one', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 100, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 50, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
});

test('omits the five-hour row when both five-hour and seven-day windows are exhausted', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 100, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test('omits five-hour and seven-day rows when a longer 30-day window is exhausted', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 50, resets_at: resetIn(48) },
      { id: 'monthly', duration_minutes: 43200, used_percent: 100, resets_at: resetIn(240) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.queryByText('7-day')).not.toBeInTheDocument();
  expect(screen.getByText('30-day')).toBeInTheDocument();
});

test('keeps both seven-day rows and a longer row when an exhausted seven-day hides only the five-hour', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(48) },
      { id: 'seven_day_opus', used_percent: 50, resets_at: resetIn(48) },
      { id: 'monthly', duration_minutes: 43200, used_percent: 50, resets_at: resetIn(240) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('7-day Opus')).toBeInTheDocument();
  expect(screen.getByText('30-day')).toBeInTheDocument();
});

test('eligible unknown-duration exhausted window does not suppress known-duration rows', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'primary', used_percent: 100, resets_at: resetIn(5) },
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 50, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.getByText('Primary')).toBeInTheDocument();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test('exhausted seven-day hides five-hour but the unknown-duration Primary row stays visible', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'primary', used_percent: 100, resets_at: resetIn(5) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.getByText('Primary')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test.each([
  [{ id: 'seven_day', duration_minutes: 10080, used_percent: 100 }], // missing reset
  [{ id: 'seven_day', duration_minutes: 10080, resets_at: now / 1000, used_percent: 100 }], // expired reset
  [{ id: 'seven_day', duration_minutes: 10080, used_percent: 50, resets_at: resetIn(48) }], // < 100%
])('does not suppress shorter rows when the longer window is not exhausted: %j', async (long) => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      long,
    ])
  );
  await show();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
});

test('an ineligible exhausted bucket does not suppress displayable shorter rows', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
      { id: 'seven_day_overage_included', used_percent: 100, resets_at: resetIn(48) },
    ])
  );
  await show();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.queryByText('7-day overage')).not.toBeInTheDocument();
});

test("shorter window under a different provider is not suppressed by another provider's exhaustion", async () => {
  vi.mocked(getUsage).mockResolvedValue(
    reportProviders([
      {
        provider: 'moonshot',
        windows: [
          { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(48) },
        ],
      },
      {
        provider: 'anthropic',
        windows: [
          { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
        ],
      },
    ])
  );
  await show();
  expect(screen.getByText('Kimi')).toBeInTheDocument();
  expect(screen.getByText('7-day')).toBeInTheDocument();
  expect(screen.getByText('Claude')).toBeInTheDocument();
  expect(screen.getByText('5-hour')).toBeInTheDocument();
});

test('restores suppressed shorter rows when the exhausted window reaches its reset time', async () => {
  vi.mocked(getUsage).mockResolvedValue(
    report([
      { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(5) },
      { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(1 / 60) },
    ])
  );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  expect(screen.getByText('Limit reached')).toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000);
  });
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('Awaiting update')).toBeInTheDocument();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
});

test('restores shorter rows on reset even when the refresh request fails', async () => {
  vi.mocked(getUsage)
    .mockReset()
    .mockResolvedValueOnce(
      report([
        { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(5) },
        { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(1 / 60) },
      ])
    )
    .mockRejectedValue(new Error('offline'));
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_000);
  });
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.getByText('Awaiting update')).toBeInTheDocument();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
  expect(screen.getByRole('status')).toHaveTextContent('Unable to refresh plan usage.');
});

test('a subsequent report below 100% restores shorter rows before the old reset time', async () => {
  vi.mocked(getUsage)
    .mockReset()
    .mockResolvedValueOnce(
      report([
        { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
        { id: 'seven_day', duration_minutes: 10080, used_percent: 100, resets_at: resetIn(24) },
      ])
    )
    .mockResolvedValue(
      report([
        { id: 'five_hour', duration_minutes: 300, used_percent: 50, resets_at: resetIn(2) },
        { id: 'seven_day', duration_minutes: 10080, used_percent: 50, resets_at: resetIn(24) },
      ])
    );
  await show();
  expect(screen.queryByText('5-hour')).not.toBeInTheDocument();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60_001);
  });
  expect(screen.getByText('5-hour')).toBeInTheDocument();
  expect(screen.queryByText('Limit reached')).not.toBeInTheDocument();
});
