import React from 'react';
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import WorkspaceStatusBadge from './WorkspaceStatusBadge';
import type { WorkspaceResponse } from '../lib/types';

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'git@github.com:test/repo.git',
    repo_name: 'test-repo',
    branch: 'feature/x',
    path: '/tmp/ws',
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

describe('WorkspaceStatusBadge', () => {
  it('renders the working spinner when locked, ignoring line counts', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({ lines_added: 12, lines_removed: 3 })}
        locked
      />
    );
    expect(container.querySelector('.working-spinner')).not.toBeNull();
    expect(screen.queryByText('+12')).toBeNull();
  });

  it('renders added and removed line counts', () => {
    render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({ lines_added: 12, lines_removed: 3 })}
        locked={false}
      />
    );
    expect(screen.getByText('+12')).toBeInTheDocument();
    expect(screen.getByText('-3')).toBeInTheDocument();
  });

  it('omits the removed count when nothing was removed', () => {
    render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({ lines_added: 12, lines_removed: 0 })}
        locked={false}
      />
    );
    expect(screen.getByText('+12')).toBeInTheDocument();
    expect(screen.queryByText('-0')).toBeNull();
  });

  it('renders nothing for a non-git workspace even when it has line changes', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({ vcs: 'sapling', lines_added: 12, lines_removed: 3 })}
        locked={false}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing for a clean git workspace', () => {
    const { container } = render(
      <WorkspaceStatusBadge workspace={makeWorkspace()} locked={false} />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders the ahead/behind pair when the remote branch has diverged', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          remote_branch_exists: true,
          remote_unique_commits: 2,
          local_unique_commits: 1,
        })}
        locked={false}
      />
    );
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(container.querySelectorAll('.app-header__git-pair')).toHaveLength(2);
  });

  it('renders nothing when the remote branch exists and is in sync', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          remote_branch_exists: true,
          remote_unique_commits: 0,
          local_unique_commits: 0,
        })}
        locked={false}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders the CI chip alone when in sync but CI has reported', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          remote_branch_exists: true,
          remote_unique_commits: 0,
          local_unique_commits: 0,
          ci_status: 'success',
          ci_url: 'https://run',
        })}
        locked={false}
      />
    );
    expect(screen.getByLabelText('CI: passing')).toBeInTheDocument();
    expect(container.querySelector('.app-header__git-pair')).toBeNull();
  });

  it('renders the pair and the CI chip together', () => {
    render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          remote_branch_exists: true,
          remote_unique_commits: 2,
          local_unique_commits: 1,
          ci_status: 'failure',
          ci_url: 'https://run',
        })}
        locked={false}
      />
    );
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByLabelText('CI: failing')).toBeInTheDocument();
  });

  it('hides the sync group entirely when the workspace has line changes', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          lines_added: 12,
          lines_removed: 3,
          remote_branch_exists: true,
          remote_unique_commits: 2,
          local_unique_commits: 1,
          ci_status: 'success',
        })}
        locked={false}
      />
    );
    expect(screen.getByText('+12')).toBeInTheDocument();
    expect(container.querySelector('.app-header__git-pair')).toBeNull();
    expect(screen.queryByLabelText('CI: passing')).toBeNull();
  });

  it('renders nothing when there is no remote branch and no CI status', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({ remote_branch_exists: false })}
        locked={false}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders nothing for a non-git workspace that has diverged from its remote', () => {
    const { container } = render(
      <WorkspaceStatusBadge
        workspace={makeWorkspace({
          vcs: 'sapling',
          remote_branch_exists: true,
          remote_unique_commits: 2,
          local_unique_commits: 1,
        })}
        locked={false}
      />
    );
    expect(container).toBeEmptyDOMElement();
  });
});
