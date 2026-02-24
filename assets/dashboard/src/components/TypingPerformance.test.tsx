import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import TypingPerformance from './TypingPerformance';

// Mock fetch to control tmux leak API responses
const mockFetch = vi.fn();

describe('TypingPerformance', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
    // Ensure document is visible for the initial load
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      writable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders typing performance header', () => {
    mockFetch.mockResolvedValue({
      ok: false,
    });

    render(<TypingPerformance />);
    expect(screen.getByText('Typing Performance')).toBeInTheDocument();
  });

  it('shows empty state when no samples collected', () => {
    mockFetch.mockResolvedValue({
      ok: false,
    });

    render(<TypingPerformance />);
    expect(screen.getByText('Type in a terminal to collect samples')).toBeInTheDocument();
  });

  it('renders tmux counts when API returns data', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: { count: 3 },
          os_processes: {
            attach_session_process_count: 2,
            tmux_process_count: 5,
          },
        }),
    });

    render(<TypingPerformance />);

    await waitFor(() => {
      expect(screen.getByText(/Tmux:/)).toBeInTheDocument();
    });

    // Should show "Tmux: 3 / 2 / 5"
    const tmuxDisplay = screen.getByText(/Tmux:/);
    expect(tmuxDisplay).toHaveTextContent('3 / 2 / 5');
  });

  it('shows question marks when counts are missing', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: {},
          os_processes: {},
        }),
    });

    render(<TypingPerformance />);

    await waitFor(() => {
      expect(screen.getByText(/Tmux:/)).toBeInTheDocument();
    });

    // Should show "Tmux: ? / ? / ?"
    const tmuxDisplay = screen.getByText(/Tmux:/);
    expect(tmuxDisplay).toHaveTextContent('? / ? / ?');
  });

  it('does not render tmux counts when fetch fails', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'));

    render(<TypingPerformance />);

    // Wait a bit to ensure fetch was attempted
    await waitFor(
      () => {
        expect(mockFetch).toHaveBeenCalled();
      },
      { timeout: 2000 }
    );

    // Tmux display should not appear
    expect(screen.queryByText(/Tmux:/)).not.toBeInTheDocument();
  });

  it('does not render tmux counts when API returns non-OK status', async () => {
    mockFetch.mockResolvedValue({
      ok: false,
      status: 404,
    });

    render(<TypingPerformance />);

    // Wait a bit to ensure fetch was attempted
    await waitFor(
      () => {
        expect(mockFetch).toHaveBeenCalled();
      },
      { timeout: 2000 }
    );

    // Tmux display should not appear
    expect(screen.queryByText(/Tmux:/)).not.toBeInTheDocument();
  });

  it('calls correct API endpoint with credentials', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
    });

    render(<TypingPerformance />);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalled();
    });

    const [url, options] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/debug/tmux-leak');
    expect(options.credentials).toBe('same-origin');
  });

  it('has correct title attribute for tmux counts', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: { count: 1 },
          os_processes: { attach_session_process_count: 1, tmux_process_count: 1 },
        }),
    });

    render(<TypingPerformance />);

    await waitFor(() => {
      expect(screen.getByText(/Tmux:/)).toBeInTheDocument();
    });

    const tmuxDisplay = screen.getByText(/Tmux:/);
    expect(tmuxDisplay).toHaveAttribute('title', 'sessions / attach-session procs / tmux procs');
  });

  it('includes existing typing performance functionality', () => {
    mockFetch.mockResolvedValue({
      ok: false,
    });

    render(<TypingPerformance />);

    // The original "Type in a terminal..." message should still be there
    expect(screen.getByText('Type in a terminal to collect samples')).toBeInTheDocument();
    // The header should be present
    expect(screen.getByText('Typing Performance')).toBeInTheDocument();
  });
});
