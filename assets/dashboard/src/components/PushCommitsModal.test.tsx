import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import PushCommitsModal from './PushCommitsModal';

const handlePushCommits = vi.fn();
vi.mock('../hooks/useSync', () => ({
  useSync: () => ({ handlePushCommits: (...args: unknown[]) => handlePushCommits(...args) }),
}));

function renderModal(overrides: Partial<Parameters<typeof PushCommitsModal>[0]> = {}) {
  const props = {
    workspaceId: 'ws-1',
    commitHash: 'a'.repeat(40),
    commitShortHash: 'aaaaaaa',
    commitMessage: 'fix the thing',
    defaultBranch: 'main',
    branchName: 'feature',
    onDefaultBranch: false,
    behind: false,
    defaultBranchOrphaned: false,
    dirty: false,
    branchTargetAvailable: true,
    branchAlreadyPushed: false,
    countToMain: 3,
    countToBranch: 2,
    headCommit: false,
    workspacePath: '/tmp/ws',
    onClose: vi.fn(),
    onPushed: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
  render(<PushCommitsModal {...props} />);
  return props;
}

beforeEach(() => {
  handlePushCommits.mockReset();
});

describe('PushCommitsModal', () => {
  it('shows the commit and both targets with per-commit counts', () => {
    renderModal();
    expect(screen.getByTestId('push-modal')).toBeInTheDocument();
    expect(screen.getByText(/aaaaaaa/)).toBeInTheDocument();
    expect(screen.getByText(/fix the thing/)).toBeInTheDocument();
    expect(screen.getByTestId('push-modal-target-default')).not.toBeDisabled();
    expect(screen.getByTestId('push-modal-target-branch')).not.toBeDisabled();
  });

  it('disables the main target when behind', () => {
    renderModal({ behind: true });
    expect(screen.getByTestId('push-modal-target-default')).toBeDisabled();
  });

  it('disables the main target when the default branch is orphaned', () => {
    renderModal({ defaultBranchOrphaned: true });
    expect(screen.getByTestId('push-modal-target-default')).toBeDisabled();
  });

  it('disables both targets when dirty', () => {
    renderModal({ dirty: true });
    expect(screen.getByTestId('push-modal-target-default')).toBeDisabled();
    expect(screen.getByTestId('push-modal-target-branch')).toBeDisabled();
  });

  it('omits the target choice on the default branch', () => {
    renderModal({ onDefaultBranch: true });
    expect(screen.queryByTestId('push-modal-target-branch')).not.toBeInTheDocument();
  });

  it('hides the branch target for fork remotes', () => {
    renderModal({ branchTargetAvailable: false });
    expect(screen.queryByTestId('push-modal-target-branch')).not.toBeInTheDocument();
  });

  it('submits bulk push to main and closes on success', async () => {
    handlePushCommits.mockResolvedValue(true);
    const props = renderModal();
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() =>
      expect(handlePushCommits).toHaveBeenCalledWith('ws-1', {
        hash: 'a'.repeat(40),
        target: 'default',
        perCommit: false,
        targetBranchName: 'main',
        headCommit: false,
        workspacePath: '/tmp/ws',
      })
    );
    await waitFor(() => expect(props.onPushed).toHaveBeenCalled());
    expect(props.onClose).toHaveBeenCalled();
  });

  it('passes headCommit through so a full push to main can offer cleanup', async () => {
    handlePushCommits.mockResolvedValue(true);
    renderModal({ headCommit: true });
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() =>
      expect(handlePushCommits).toHaveBeenCalledWith(
        'ws-1',
        expect.objectContaining({ headCommit: true, workspacePath: '/tmp/ws' })
      )
    );
  });

  it('submits per-commit push to branch', async () => {
    handlePushCommits.mockResolvedValue(true);
    renderModal();
    fireEvent.click(screen.getByTestId('push-modal-target-branch'));
    fireEvent.click(screen.getByTestId('push-modal-mode-percommit'));
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() =>
      expect(handlePushCommits).toHaveBeenCalledWith('ws-1', {
        hash: 'a'.repeat(40),
        target: 'branch',
        perCommit: true,
        targetBranchName: 'feature',
        headCommit: false,
        workspacePath: '/tmp/ws',
      })
    );
  });

  it('stays open when the push does not land', async () => {
    handlePushCommits.mockResolvedValue(false);
    const props = renderModal();
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() => expect(handlePushCommits).toHaveBeenCalled());
    expect(props.onClose).not.toHaveBeenCalled();
  });

  it('labels the per-commit count as an estimate when countToBranch is null', () => {
    renderModal({ countToBranch: null });
    fireEvent.click(screen.getByTestId('push-modal-target-branch'));
    expect(screen.getByTestId('push-modal-mode-percommit-label').textContent).toMatch(/up to/);
  });

  it('omits the mode choice when only one commit would be pushed', () => {
    renderModal({ countToMain: 1, countToBranch: 1 });
    expect(screen.queryByTestId('push-modal-mode-bulk')).not.toBeInTheDocument();
    expect(screen.queryByTestId('push-modal-mode-percommit')).not.toBeInTheDocument();
    expect(screen.getByTestId('push-modal-submit').textContent).toMatch(/Push 1 commit \(1 push\)/);
  });

  it('submits per_commit=false when the mode choice is hidden', async () => {
    handlePushCommits.mockResolvedValue(true);
    renderModal({ countToMain: 1, countToBranch: 1 });
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() =>
      expect(handlePushCommits).toHaveBeenCalledWith('ws-1', {
        hash: 'a'.repeat(40),
        target: 'default',
        perCommit: false,
        targetBranchName: 'main',
        headCommit: false,
        workspacePath: '/tmp/ws',
      })
    );
  });

  it('restores the mode choice when switching to a target with more commits', () => {
    renderModal({ countToMain: 1, countToBranch: 3 });
    // Default target: 1 commit → no mode choice.
    expect(screen.queryByTestId('push-modal-mode-percommit')).not.toBeInTheDocument();
    // Branch target: 3 commits → mode choice appears.
    fireEvent.click(screen.getByTestId('push-modal-target-branch'));
    expect(screen.getByTestId('push-modal-mode-percommit')).toBeInTheDocument();
  });

  it('closes on Escape when idle', () => {
    const props = renderModal();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(props.onClose).toHaveBeenCalled();
  });

  it('ignores Escape while a push is in flight', async () => {
    // Never-resolving push keeps the modal in the submitting state.
    handlePushCommits.mockReturnValue(new Promise(() => {}));
    const props = renderModal();
    fireEvent.click(screen.getByTestId('push-modal-submit'));
    await waitFor(() => expect(handlePushCommits).toHaveBeenCalled());

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(props.onClose).not.toHaveBeenCalled();
  });
});
