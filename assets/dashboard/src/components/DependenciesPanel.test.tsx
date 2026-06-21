import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../lib/api', () => ({ getDependencies: vi.fn() }));
import { getDependencies } from '../lib/api';
import { DependenciesPanel } from './DependenciesPanel';

const mockGet = vi.mocked(getDependencies);

const sample = {
  os: 'macos',
  groups: [
    {
      id: 'agents',
      display_name: 'AI agents',
      description: 'x',
      dependencies: [
        {
          id: 'claude',
          display_name: 'Claude Code',
          description: 'c',
          detected: true,
          install: [],
        },
        {
          id: 'codex',
          display_name: 'codex',
          description: 'c2',
          detected: false,
          install: [
            { os: 'any', label: 'npm', command: 'npm i -g @openai/codex', requires: 'npm' },
          ],
        },
      ],
    },
  ],
};

describe('DependenciesPanel', () => {
  beforeEach(() => mockGet.mockReset());

  it('renders a group table with a row per tool', async () => {
    mockGet.mockResolvedValue(sample as never);
    render(<DependenciesPanel />);
    await waitFor(() => expect(screen.getByTestId('dep-group-agents')).toBeInTheDocument());
    expect(screen.getByTestId('dep-row-claude')).toBeInTheDocument();
    expect(screen.getByTestId('dep-row-codex')).toBeInTheDocument();
  });

  it('shows "installed" for a detected tool and the install command for a missing one', async () => {
    mockGet.mockResolvedValue(sample as never);
    render(<DependenciesPanel />);
    await waitFor(() => expect(screen.getByTestId('dep-row-claude')).toBeInTheDocument());
    expect(screen.getByTestId('dep-row-claude')).toHaveTextContent('installed');
    expect(screen.getByTestId('dep-row-codex')).toHaveTextContent('npm i -g @openai/codex');
  });
});
