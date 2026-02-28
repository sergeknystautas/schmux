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
});
