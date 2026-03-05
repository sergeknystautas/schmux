import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import TmuxDiagnostic from './TmuxDiagnostic';

const mockFetch = vi.fn();

describe('TmuxDiagnostic', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
    Object.defineProperty(document, 'visibilityState', {
      value: 'visible',
      writable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders labeled rows when API returns data', async () => {
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

    render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(screen.getByText('Sessions')).toBeInTheDocument();
    });

    expect(screen.getByText('Attach procs')).toBeInTheDocument();
    expect(screen.getByText('Tmux procs')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('5')).toBeInTheDocument();
  });

  it('renders nothing when fetch fails', async () => {
    mockFetch.mockRejectedValue(new Error('Network error'));

    const { container } = render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalled();
    });

    expect(container.innerHTML).toBe('');
  });

  it('renders nothing when API returns non-OK status', async () => {
    mockFetch.mockResolvedValue({ ok: false, status: 404 });

    const { container } = render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalled();
    });

    expect(container.innerHTML).toBe('');
  });

  it('calls correct API endpoint with credentials', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ tmux_sessions: { count: 0 }, os_processes: {} }),
    });

    render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalled();
    });

    const [url, options] = mockFetch.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/debug/tmux-leak');
    expect(options.credentials).toBe('same-origin');
  });

  it('defaults missing counts to zero', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: {},
          os_processes: {},
        }),
    });

    render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(screen.getByText('Sessions')).toBeInTheDocument();
    });

    // All three values should be 0
    const zeros = screen.getAllByText('0');
    expect(zeros).toHaveLength(3);
  });

  it('shows Tmux header', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: { count: 1 },
          os_processes: { attach_session_process_count: 1, tmux_process_count: 2 },
        }),
    });

    render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(screen.getByText('Tmux')).toBeInTheDocument();
    });
  });

  it('has description tooltips on rows', async () => {
    mockFetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          tmux_sessions: { count: 1 },
          os_processes: { attach_session_process_count: 1, tmux_process_count: 2 },
        }),
    });

    render(<TmuxDiagnostic />);

    await waitFor(() => {
      expect(screen.getByText('Sessions')).toBeInTheDocument();
    });

    const sessionsRow = screen.getByText('Sessions').closest('[data-testid="tmux-diag-row"]');
    expect(sessionsRow).toHaveAttribute('title', 'Active tmux sessions on this machine');

    const attachRow = screen.getByText('Attach procs').closest('[data-testid="tmux-diag-row"]');
    expect(attachRow).toHaveAttribute(
      'title',
      'Control-mode processes watching sessions (expect ≤ sessions)'
    );

    const tmuxRow = screen.getByText('Tmux procs').closest('[data-testid="tmux-diag-row"]');
    expect(tmuxRow).toHaveAttribute('title', 'Total OS processes with "tmux" in command line');
  });
});
