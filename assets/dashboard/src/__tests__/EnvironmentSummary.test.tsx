import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import { EnvironmentSummary } from '../components/EnvironmentSummary';
import type { DetectionSummaryResponse } from '../lib/types.generated';

// --- Mocks ---
const mockGetDetectionSummary = vi.fn<() => Promise<DetectionSummaryResponse>>();

vi.mock('../lib/api', () => ({
  getDetectionSummary: (...args: unknown[]) => mockGetDetectionSummary(...(args as [])),
}));

function makeReadyResponse(
  overrides: Partial<DetectionSummaryResponse> = {}
): DetectionSummaryResponse {
  return {
    status: 'ready',
    agents: [
      { name: 'Claude Code', command: 'claude', source: 'path' },
      { name: 'Codex', command: 'codex', source: 'path' },
    ],
    vcs: [{ name: 'git', path: '/usr/bin/git' }],
    tmux: { available: true, path: '/usr/local/bin/tmux' },
    ...overrides,
  };
}

describe('EnvironmentSummary', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockGetDetectionSummary.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('shows "Detecting tools..." while pending', async () => {
    // Never resolves during this test — stays in loading
    mockGetDetectionSummary.mockReturnValue(new Promise(() => {}));

    render(<EnvironmentSummary />);

    expect(screen.getByTestId('env-summary-loading')).toHaveTextContent('Detecting tools...');
  });

  it('shows agent names and VCS when all tools are found', async () => {
    mockGetDetectionSummary.mockResolvedValue(makeReadyResponse());

    render(<EnvironmentSummary />);

    await waitFor(() => {
      expect(screen.getByTestId('env-summary-found')).toBeInTheDocument();
    });

    // Agent badges
    const agentBadges = screen.getAllByTestId('env-badge-agent');
    expect(agentBadges).toHaveLength(2);
    expect(agentBadges[0]).toHaveTextContent('Claude Code');
    expect(agentBadges[1]).toHaveTextContent('Codex');

    // VCS badge (name only, no version)
    const vcsBadges = screen.getAllByTestId('env-badge-vcs');
    expect(vcsBadges).toHaveLength(1);
    expect(vcsBadges[0]).toHaveTextContent('git');

    // tmux badge (name only)
    expect(screen.getByTestId('env-badge-tmux')).toHaveTextContent('tmux');

    // No warnings
    expect(screen.queryByTestId('env-summary-warnings')).not.toBeInTheDocument();
  });

  it('shows warning messages when tools are missing', async () => {
    mockGetDetectionSummary.mockResolvedValue(
      makeReadyResponse({
        agents: [],
        vcs: [],
        tmux: { available: false },
      })
    );

    render(<EnvironmentSummary />);

    await waitFor(() => {
      expect(screen.getByTestId('env-summary-warnings')).toBeInTheDocument();
    });

    const warnings = screen.getAllByTestId('env-warning');
    expect(warnings).toHaveLength(3);
    expect(warnings[0]).toHaveTextContent(
      'No agents detected — install one or configure in Settings'
    );
    expect(warnings[1]).toHaveTextContent('No version control found — install git to get started');
    expect(warnings[2]).toHaveTextContent(
      'tmux not found — required to spawn sessions. Install: brew install tmux'
    );
  });

  it('shows timeout message after max retries', async () => {
    // Always return pending
    mockGetDetectionSummary.mockResolvedValue({
      status: 'pending',
      agents: [],
      vcs: [],
      tmux: { available: false },
    });

    render(<EnvironmentSummary />);

    // Initial call shows loading
    expect(screen.getByTestId('env-summary-loading')).toBeInTheDocument();

    // Advance through all retries (each waits 1s)
    for (let i = 0; i < MAX_RETRIES; i++) {
      await act(async () => {
        vi.advanceTimersByTime(1000);
      });
    }

    await waitFor(() => {
      expect(screen.getByTestId('env-summary-timeout')).toHaveTextContent(
        'Detection timed out — some tools may not be shown. Refresh the page to retry.'
      );
    });
  });

  it('retries on pending status and succeeds when ready', async () => {
    // First call: pending, second call: ready
    mockGetDetectionSummary
      .mockResolvedValueOnce({
        status: 'pending',
        agents: [],
        vcs: [],
        tmux: { available: false },
      })
      .mockResolvedValueOnce(makeReadyResponse());

    render(<EnvironmentSummary />);

    // Wait for first call to resolve and schedule retry timer
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1100);
    });

    await waitFor(() => {
      expect(screen.getByTestId('env-summary-found')).toBeInTheDocument();
    });

    expect(screen.getByText('Claude Code')).toBeInTheDocument();
  });
});

const MAX_RETRIES = 10;
