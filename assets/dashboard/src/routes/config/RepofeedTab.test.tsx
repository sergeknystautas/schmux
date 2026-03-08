import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import RepofeedTab from './RepofeedTab';
import type { ConfigFormAction, ConfigFormState } from './useConfigForm';
import type { RepoResponse } from '../../lib/types';

function renderTab(
  overrides: Partial<{
    repofeedEnabled: boolean;
    repofeedPublishInterval: number;
    repofeedFetchInterval: number;
    repofeedCompletedRetention: number;
    repofeedRepos: Record<string, boolean>;
    repos: RepoResponse[];
  }> = {},
  dispatch?: React.Dispatch<ConfigFormAction>
) {
  const mockDispatch = dispatch || vi.fn();
  const props = {
    repofeedEnabled: false,
    repofeedPublishInterval: 30,
    repofeedFetchInterval: 60,
    repofeedCompletedRetention: 48,
    repofeedRepos: {},
    repos: [],
    dispatch: mockDispatch,
    ...overrides,
  };
  render(<RepofeedTab {...props} />);
  return mockDispatch;
}

describe('RepofeedTab', () => {
  it('shows enable checkbox', () => {
    renderTab();
    expect(screen.getByLabelText(/Enable repofeed/)).toBeInTheDocument();
  });

  it('hides timing section when disabled', () => {
    renderTab({ repofeedEnabled: false });
    expect(screen.queryByText('Timing')).not.toBeInTheDocument();
  });

  it('shows timing section when enabled', () => {
    renderTab({ repofeedEnabled: true });
    expect(screen.getByText('Timing')).toBeInTheDocument();
    expect(screen.getByText('Publish interval')).toBeInTheDocument();
    expect(screen.getByText('Fetch interval')).toBeInTheDocument();
    expect(screen.getByText('Completed retention')).toBeInTheDocument();
  });

  it('dispatches SET_FIELD when enable checkbox toggled', async () => {
    const dispatch = vi.fn();
    renderTab({ repofeedEnabled: false }, dispatch);
    await userEvent.click(screen.getByLabelText(/Enable repofeed/));
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'repofeedEnabled',
      value: true,
    });
  });

  it('shows repo checkboxes when enabled and repos exist', () => {
    const repos: RepoResponse[] = [
      { name: 'Frontend App', url: 'https://example.com/frontend' },
      { name: 'Backend API', url: 'https://example.com/backend' },
    ];
    renderTab({ repofeedEnabled: true, repos });
    expect(screen.getByText('Repos')).toBeInTheDocument();
    expect(screen.getByLabelText('Frontend App')).toBeInTheDocument();
    expect(screen.getByLabelText('Backend API')).toBeInTheDocument();
  });

  it('hides repo section when no repos', () => {
    renderTab({ repofeedEnabled: true, repos: [] });
    expect(screen.queryByText('Repos')).not.toBeInTheDocument();
  });

  it('toggles repo via dispatch', async () => {
    const repos: RepoResponse[] = [{ name: 'my-repo', url: 'https://example.com/repo' }];
    const dispatch = vi.fn();
    renderTab({ repofeedEnabled: true, repos, repofeedRepos: {} }, dispatch);

    await userEvent.click(screen.getByLabelText('my-repo'));
    expect(dispatch).toHaveBeenCalledWith({
      type: 'SET_FIELD',
      field: 'repofeedRepos',
      value: { 'my-repo': true },
    });
  });

  it('repos default to enabled when not in repofeedRepos map', () => {
    const repos: RepoResponse[] = [{ name: 'my-repo', url: 'https://example.com/repo' }];
    renderTab({ repofeedEnabled: true, repos, repofeedRepos: {} });
    const checkbox = screen.getByLabelText('my-repo') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });

  it('shows repo as unchecked when explicitly disabled', () => {
    const repos: RepoResponse[] = [{ name: 'my-repo', url: 'https://example.com/repo' }];
    renderTab({
      repofeedEnabled: true,
      repos,
      repofeedRepos: { 'my-repo': false },
    });
    const checkbox = screen.getByLabelText('my-repo') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });
});
