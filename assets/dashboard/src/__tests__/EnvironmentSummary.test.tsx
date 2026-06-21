import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { EnvironmentSummary } from '../components/EnvironmentSummary';
import type { DependenciesResponse } from '../lib/types.generated';

const mockGetDependencies = vi.fn<() => Promise<DependenciesResponse>>();

vi.mock('../lib/api', () => ({
  getDependencies: (...args: unknown[]) => mockGetDependencies(...(args as [])),
}));

function makeResponse(overrides: Partial<DependenciesResponse> = {}): DependenciesResponse {
  return {
    os: 'macos',
    groups: [
      {
        id: 'agents',
        display_name: 'AI agents',
        description: '',
        dependencies: [
          { id: 'claude', display_name: 'Claude Code', description: '', detected: true },
          { id: 'codex', display_name: 'codex', description: '', detected: true },
        ],
      },
      {
        id: 'vcs',
        display_name: 'Version control',
        description: '',
        dependencies: [{ id: 'git', display_name: 'Git', description: '', detected: true }],
      },
      {
        id: 'terminal',
        display_name: 'Terminal',
        description: '',
        dependencies: [{ id: 'tmux', display_name: 'tmux', description: '', detected: true }],
      },
    ],
    ...overrides,
  };
}

describe('EnvironmentSummary', () => {
  beforeEach(() => mockGetDependencies.mockReset());

  it('shows "Detecting tools..." before data loads', () => {
    // Hold the resolver so data stays null (loading), then resolve before exit
    // so no dangling promise keeps the test waiting.
    let resolveReady!: (value: DependenciesResponse) => void;
    mockGetDependencies.mockReturnValue(
      new Promise<DependenciesResponse>((r) => {
        resolveReady = r;
      })
    );

    render(<EnvironmentSummary />);
    expect(screen.getByTestId('env-summary-loading')).toHaveTextContent('Detecting tools...');

    resolveReady(makeResponse({ groups: [] }));
  });

  it('shows agent, VCS, and tmux badges when tools are detected', async () => {
    mockGetDependencies.mockResolvedValue(makeResponse());
    render(<EnvironmentSummary />);

    await waitFor(() => expect(screen.getByTestId('env-summary-found')).toBeInTheDocument());

    const agentBadges = screen.getAllByTestId('env-badge-agent');
    expect(agentBadges).toHaveLength(2);
    expect(agentBadges[0]).toHaveTextContent('Claude Code');

    expect(screen.getByTestId('env-badge-git')).toHaveTextContent('Git');
    expect(screen.getByTestId('env-badge-tmux')).toHaveTextContent('tmux');

    expect(screen.queryByTestId('env-summary-warnings')).not.toBeInTheDocument();
  });

  it('shows warnings when tools are missing', async () => {
    mockGetDependencies.mockResolvedValue(
      makeResponse({
        groups: [
          { id: 'agents', display_name: 'AI agents', description: '', dependencies: [] },
          { id: 'vcs', display_name: 'Version control', description: '', dependencies: [] },
          { id: 'terminal', display_name: 'Terminal', description: '', dependencies: [] },
        ],
      })
    );
    render(<EnvironmentSummary />);

    await waitFor(() => expect(screen.getByTestId('env-summary-warnings')).toBeInTheDocument());

    const warnings = screen.getAllByTestId('env-warning');
    expect(warnings).toHaveLength(3);
    expect(warnings[0]).toHaveTextContent('No agents detected');
    expect(warnings[1]).toHaveTextContent('No version control found');
    expect(warnings[2]).toHaveTextContent('tmux not found');
  });
});
