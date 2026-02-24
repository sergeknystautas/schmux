import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import TypingPerformance from './TypingPerformance';

describe('TypingPerformance', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false }));
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
});
