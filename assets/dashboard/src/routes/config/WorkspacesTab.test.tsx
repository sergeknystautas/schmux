import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import WorkspacesTab from './WorkspacesTab';
import type { ConfigFormAction } from './useConfigForm';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const defaultProps = {
  workspacePath: '/home/user/workspaces',
  recycleWorkspaces: false,
  repos: [] as { name: string; url: string; default_branch?: string }[],
  overlays: [],
  newRepoName: '',
  newRepoUrl: '',
  newRepoVcs: '',
  stepErrors: { 1: null, 2: null, 3: null, 4: null, 5: null, 6: null } as Record<
    number,
    string | null
  >,
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

  it('shows step error when present', () => {
    renderTab({ stepErrors: { 1: 'Workspace path is required' } });
    expect(screen.getByText('Workspace path is required')).toBeInTheDocument();
  });
});
