import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import WorkspaceInfoTooltip from './WorkspaceInfoTooltip';
import type { WorkspaceResponse } from '../lib/types';

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'https://github.com/acme/repo.git',
    branch: 'feature/foo',
    path: '/Users/me/code/workspaces/ws-1',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  };
}

describe('WorkspaceInfoTooltip', () => {
  it('renders branch, name, repo, and commit-pair rows for a minimal git workspace', () => {
    const { container } = render(<WorkspaceInfoTooltip workspace={makeWorkspace()} />);
    expect(screen.getByText('feature/foo')).toBeInTheDocument();
    expect(screen.getByText('ws-1')).toBeInTheDocument();
    expect(screen.getByText('https://github.com/acme/repo.git')).toBeInTheDocument();
    const pairs = container.querySelectorAll('.app-header__git-pair');
    expect(pairs).toHaveLength(2);
    expect(pairs[0].textContent).toBe('0');
    expect(pairs[1].textContent).toBe('0');
  });

  it('does not use a "Main" text label on the commits row', () => {
    render(<WorkspaceInfoTooltip workspace={makeWorkspace()} />);
    expect(screen.queryByText(/Main/)).not.toBeInTheDocument();
  });

  it('renders behind then ahead, each in its own git-pair span with an arrow svg', () => {
    const { container } = render(
      <WorkspaceInfoTooltip workspace={makeWorkspace({ ahead: 2, behind: 1 })} />
    );
    const pairs = container.querySelectorAll('.app-header__git-pair');
    expect(pairs).toHaveLength(2);
    expect(pairs[0].textContent).toBe('1');
    expect(pairs[0].querySelector('svg')).not.toBeNull();
    expect(pairs[1].textContent).toBe('2');
    expect(pairs[1].querySelector('svg')).not.toBeNull();
  });

  it('does not render path or Remote rows', () => {
    render(
      <WorkspaceInfoTooltip
        workspace={makeWorkspace({
          remote_branch_exists: true,
          commits_synced_with_remote: false,
        })}
      />
    );
    expect(screen.queryByText('/Users/me/code/workspaces/ws-1')).not.toBeInTheDocument();
    expect(screen.queryByText(/^Remote:/)).not.toBeInTheDocument();
  });

  it('omits the commits row for sapling workspaces', () => {
    const { container } = render(
      <WorkspaceInfoTooltip workspace={makeWorkspace({ vcs: 'sapling', ahead: 3 })} />
    );
    expect(container.querySelectorAll('.app-header__git-pair')).toHaveLength(0);
  });

  it('applies smaller font inline to the repo row', () => {
    render(<WorkspaceInfoTooltip workspace={makeWorkspace()} />);
    const repoRow = screen.getByText('https://github.com/acme/repo.git');
    expect(repoRow.style.fontSize).toBe('0.85em');
  });
});
