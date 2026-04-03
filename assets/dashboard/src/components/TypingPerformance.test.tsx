import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import TypingPerformance from './TypingPerformance';
import { inputLatency } from '../lib/inputLatency';

describe('TypingPerformance', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }));
    inputLatency.reset();
    localStorage.removeItem('typing-perf-collapsed');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders typing performance header', () => {
    render(<TypingPerformance />);
    expect(screen.getByText('Typing Performance')).toBeInTheDocument();
  });

  it('shows empty state when no samples collected', () => {
    render(<TypingPerformance />);
    expect(screen.getByText('Type in a terminal to collect samples')).toBeInTheDocument();
  });

  it('shows empty state when fewer than 3 samples', () => {
    // Component requires >= 3 samples to show histogram
    inputLatency.samples = [5, 10];
    render(<TypingPerformance />);
    expect(screen.getByText('Type in a terminal to collect samples')).toBeInTheDocument();
  });

  it('renders histogram SVG when enough samples are present', () => {
    // Provide enough samples for stats + distribution
    inputLatency.samples = [5, 10, 15, 20, 25];
    const { container } = render(<TypingPerformance />);
    // Should not show the empty state
    expect(screen.queryByText('Type in a terminal to collect samples')).not.toBeInTheDocument();
    // Should render an SVG for the histogram
    const svg = container.querySelector('svg');
    expect(svg).not.toBeNull();
  });

  it('hides content when collapsed via toggle', () => {
    inputLatency.samples = [5, 10, 15, 20, 25];
    const { container } = render(<TypingPerformance />);

    // Initially expanded — SVG should be visible
    expect(container.querySelector('svg')).not.toBeNull();

    // Click the toggle to collapse
    fireEvent.click(screen.getByText('Typing Performance'));

    // After collapsing, no SVG or empty state should be visible
    expect(container.querySelector('svg')).toBeNull();
    expect(screen.queryByText('Type in a terminal to collect samples')).not.toBeInTheDocument();
  });

  it('persists collapsed state to localStorage', () => {
    render(<TypingPerformance />);

    // Click to collapse
    fireEvent.click(screen.getByText('Typing Performance'));
    expect(localStorage.getItem('typing-perf-collapsed')).toBe('1');

    // Click to expand
    fireEvent.click(screen.getByText('Typing Performance'));
    expect(localStorage.getItem('typing-perf-collapsed')).toBe('0');
  });

  it('shows reset button only when expanded with samples', () => {
    inputLatency.samples = [5, 10, 15, 20, 25];
    render(<TypingPerformance />);
    expect(screen.getByText('Reset')).toBeInTheDocument();

    // Collapse — Reset button should disappear
    fireEvent.click(screen.getByText('Typing Performance'));
    expect(screen.queryByText('Reset')).not.toBeInTheDocument();
  });

  it('renders breakdown bars when paired server segments are available', () => {
    // Need 20+ samples so the IQR cohort has >= 5 members for 'typical' breakdown
    inputLatency.samples = Array.from({ length: 20 }, (_, i) => 20 + i);
    inputLatency.renderSamples = Array.from({ length: 20 }, () => 1);
    const tuple = { dispatch: 0.5, sendKeys: 2.0, echo: 3.0, frameSend: 0.1, total: 5.6 };
    for (let i = 0; i < 20; i++) inputLatency.serverSegmentSamples.push({ ...tuple });

    render(<TypingPerformance />);
    expect(screen.getByTestId('latency-breakdown')).toBeInTheDocument();
    // Typical/Outlier labels appear in the breakdown bars
    // (P50/P99 still appear in the histogram SVG, but breakdown uses Typical/Outlier)
    expect(screen.getByText('Typical')).toBeInTheDocument();
  });

  it('shows tooltip with segment values on hover', () => {
    // Need 20+ samples so the IQR cohort has >= 5 members for 'typical' breakdown
    inputLatency.samples = Array.from({ length: 20 }, (_, i) => 20 + i);
    inputLatency.renderSamples = Array.from({ length: 20 }, () => 1);
    const tuple = { dispatch: 0.5, sendKeys: 2.0, echo: 3.0, frameSend: 0.1, total: 5.6 };
    for (let i = 0; i < 20; i++) inputLatency.serverSegmentSamples.push({ ...tuple });

    render(<TypingPerformance />);
    // Tooltip should not be visible initially
    expect(screen.queryByTestId('breakdown-tooltip')).not.toBeInTheDocument();

    // Hover over the Typical bar row
    const typicalLabel = screen
      .getAllByText('Typical')
      .find((el) => el.classList.contains('typing-perf__bar-label'));
    expect(typicalLabel).toBeDefined();
    const barRow = typicalLabel!.closest('.typing-perf__bar-row')!;
    fireEvent.mouseEnter(barRow);

    // Tooltip should appear with segment values
    expect(screen.getByTestId('breakdown-tooltip')).toBeInTheDocument();
    expect(screen.getByText('tmux cmd')).toBeInTheDocument();

    // Mouse leave hides tooltip
    fireEvent.mouseLeave(barRow);
    expect(screen.queryByTestId('breakdown-tooltip')).not.toBeInTheDocument();
  });

  it('does not render legend or context counters', () => {
    // Need 20+ samples so the IQR cohort has >= 5 members
    inputLatency.samples = Array.from({ length: 20 }, (_, i) => 20 + i);
    inputLatency.renderSamples = Array.from({ length: 20 }, () => 1);
    const tuple = { dispatch: 0.5, sendKeys: 2.0, echo: 3.0, frameSend: 0.1, total: 5.6 };
    for (let i = 0; i < 20; i++) inputLatency.serverSegmentSamples.push({ ...tuple });

    render(<TypingPerformance />);
    // Legend should not exist
    expect(screen.queryByText('evtLoop')).not.toBeInTheDocument();
    expect(screen.queryByText('execNet')).not.toBeInTheDocument();
    // Context counters should not exist
    expect(screen.queryByTestId('latency-context')).not.toBeInTheDocument();
    expect(screen.queryByText(/chDepth P99:/)).not.toBeInTheDocument();
  });

  it('does not render breakdown bars without paired segment samples', () => {
    inputLatency.samples = [10, 20, 30, 40, 50];
    render(<TypingPerformance />);
    expect(screen.queryByTestId('latency-breakdown')).not.toBeInTheDocument();
  });
});
