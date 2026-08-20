import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ForcePushModal from './ForcePushModal';
import type { BranchDivergenceResponse } from '../lib/types.generated';

const pushToBranch = vi.fn();
vi.mock('../lib/api', () => ({
  pushToBranch: (...args: unknown[]) => pushToBranch(...args),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

const toastSuccess = vi.fn();
vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: toastSuccess, error: vi.fn() }),
}));

vi.mock('../hooks/useFocusTrap', () => ({ default: vi.fn() }));

const divergence: BranchDivergenceResponse = {
  branch: 'feature/foo',
  local_head: 'a'.repeat(40),
  remote_head: 'b'.repeat(40),
  local_commits: [
    {
      hash: 'a'.repeat(40),
      short_hash: 'aaaaaaa',
      author: 'Alice',
      timestamp: '2026-08-19T12:00:00Z',
      subject: 'local work',
    },
  ],
  remote_commits: [
    {
      hash: 'b'.repeat(40),
      short_hash: 'bbbbbbb',
      author: 'Bob',
      timestamp: '2026-08-19T11:00:00Z',
      subject: 'remote work',
    },
  ],
  local_total: 1,
  remote_total: 1,
};

describe('ForcePushModal', () => {
  beforeEach(() => {
    pushToBranch.mockReset();
    toastSuccess.mockClear();
  });

  it('renders both directions with hash, author, and subject', () => {
    render(<ForcePushModal workspaceId="ws-1" divergence={divergence} onClose={() => {}} />);
    expect(screen.getByTestId('force-push-modal')).toBeInTheDocument();
    expect(screen.getByText(/overwritten/i)).toBeInTheDocument();
    expect(screen.getByText('bbbbbbb')).toBeInTheDocument();
    expect(screen.getByText('Bob')).toBeInTheDocument();
    expect(screen.getByText('remote work')).toBeInTheDocument();
    expect(screen.getByText('aaaaaaa')).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('local work')).toBeInTheDocument();
  });

  it('shows "and N more" when totals exceed the listed commits', () => {
    render(
      <ForcePushModal
        workspaceId="ws-1"
        divergence={{ ...divergence, remote_total: 14 }}
        onClose={() => {}}
      />
    );
    expect(screen.getByText(/and 13 more/)).toBeInTheDocument();
  });

  it('cancel closes without pushing', () => {
    const onClose = vi.fn();
    render(<ForcePushModal workspaceId="ws-1" divergence={divergence} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('force-push-modal-cancel'));
    expect(onClose).toHaveBeenCalled();
    expect(pushToBranch).not.toHaveBeenCalled();
  });

  it('force push sends confirm and the reviewed SHAs; success toasts and closes', async () => {
    pushToBranch.mockResolvedValue({ success: true });
    const onClose = vi.fn();
    render(<ForcePushModal workspaceId="ws-1" divergence={divergence} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('force-push-modal-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(pushToBranch).toHaveBeenCalledWith('ws-1', {
      confirm: true,
      expected_local: divergence.local_head,
      expected_remote: divergence.remote_head,
    });
    expect(toastSuccess).toHaveBeenCalledWith('Pushed to origin/feature/foo');
  });

  it('failure keeps the modal open and shows the message', async () => {
    pushToBranch.mockResolvedValue({
      success: false,
      message: 'local branch changed since review - reopen the push options to see the new state',
    });
    const onClose = vi.fn();
    render(<ForcePushModal workspaceId="ws-1" divergence={divergence} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('force-push-modal-submit'));
    await screen.findByText(/changed since review/);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('request error keeps the modal open and shows the error', async () => {
    pushToBranch.mockRejectedValue(new Error('push rejected: stale info'));
    const onClose = vi.fn();
    render(<ForcePushModal workspaceId="ws-1" divergence={divergence} onClose={onClose} />);
    fireEvent.click(screen.getByTestId('force-push-modal-submit'));
    await screen.findByText(/stale info/);
    expect(onClose).not.toHaveBeenCalled();
  });
});
