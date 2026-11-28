import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import WorkspacesTab from './WorkspacesTab';
import type { ConfigFormAction } from './useConfigForm';
import type { WorkspaceResponse } from '../../lib/types';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const defaultProps = {
  workspacePath: '/home/user/workspaces',
  repos: [] as { name: string; url: string; default_branch?: string }[],
  workspaces: [] as WorkspaceResponse[],
  overlays: [],
  newRepoName: '',
  newRepoUrl: '',
  newRepoVcs: '',
  dispatch,
  onEditWorkspacePath: vi.fn(),
  onRemoveRepo: vi.fn(),
  onAddRepo: vi.fn(),
};

function renderTab(overrides = {}) {
  return render(
    <MemoryRouter>
      <WorkspacesTab {...defaultProps} {...overrides} />
    </MemoryRouter>
  );
}

describe('WorkspacesTab', () => {
  it('renders workspace path', () => {
    renderTab();
    expect(screen.getByDisplayValue('/home/user/workspaces')).toBeInTheDocument();
  });

  it('shows empty state when no repos', () => {
    renderTab();
    expect(screen.getByText(/No repositories configured/)).toBeInTheDocument();
  });

  it('renders repos', () => {
    renderTab({
      repos: [{ name: 'myrepo', url: 'git@github.com:u/r.git' }],
    });
    expect(screen.getByText('myrepo')).toBeInTheDocument();
    expect(screen.getByText('git@github.com:u/r.git')).toBeInTheDocument();
  });

  it('calls onEditWorkspacePath when Edit button is clicked', async () => {
    const onEditWorkspacePath = vi.fn();
    renderTab({ onEditWorkspacePath });
    await userEvent.click(screen.getByText('Edit'));
    expect(onEditWorkspacePath).toHaveBeenCalled();
  });

  it('calls onRemoveRepo when Remove is clicked', async () => {
    const onRemoveRepo = vi.fn();
    renderTab({
      repos: [{ name: 'myrepo', url: 'u' }],
      onRemoveRepo,
    });
    await userEvent.click(screen.getByText('Remove'));
    expect(onRemoveRepo).toHaveBeenCalledWith('myrepo');
  });

  it('disables Remove and shows usage when workspaces use the repo', () => {
    renderTab({
      repos: [{ name: 'r', url: 'u' }],
      workspaces: [
        { id: 'r-001', repo: 'u' },
        { id: 'r-002', repo: 'u' },
        { id: 'o-001', repo: 'other' },
      ] as WorkspaceResponse[],
    });
    expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled();
    expect(screen.getByText('In use by 2 workspaces')).toBeInTheDocument();
  });

  it('says "1 workspace" for a single user', () => {
    renderTab({
      repos: [{ name: 'r', url: 'u' }],
      workspaces: [{ id: 'r-001', repo: 'u' }] as WorkspaceResponse[],
    });
    expect(screen.getByText('In use by 1 workspace')).toBeInTheDocument();
  });

  it('enables Remove with no usage line when no workspace uses the repo', () => {
    renderTab({
      repos: [{ name: 'r', url: 'u' }],
      workspaces: [{ id: 'o-001', repo: 'other' }] as WorkspaceResponse[],
    });
    expect(screen.getByRole('button', { name: 'Remove' })).toBeEnabled();
    expect(screen.queryByText(/In use by/)).toBeNull();
  });

  it('calls onAddRepo when Add is clicked', async () => {
    const onAddRepo = vi.fn();
    renderTab({ onAddRepo });
    await userEvent.click(screen.getByTestId('add-repo'));
    expect(onAddRepo).toHaveBeenCalled();
  });

  it('dispatches SET_FIELD on repo name input change', async () => {
    dispatch.mockClear();
    renderTab();
    const nameInput = screen.getByPlaceholderText('Name');
    await userEvent.type(nameInput, 'a');
    expect(dispatch).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'SET_FIELD', field: 'newRepoName' })
    );
  });

  it('does not render the workspace disk limit on the default tab', () => {
    renderTab();
    expect(screen.queryByLabelText('Minimum free disk (MiB)')).not.toBeInTheDocument();
  });
});
