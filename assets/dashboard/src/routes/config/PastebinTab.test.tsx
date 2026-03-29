import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import PastebinTab from './PastebinTab';
import type { ConfigFormAction } from './useConfigForm';

const dispatch = vi.fn<(action: ConfigFormAction) => void>();

const defaultProps = {
  pastebin: [] as string[],
  dispatch,
  onOpenPastebinEditModal: vi.fn<(index: number, content: string) => void>(),
  onOpenAddPastebinModal: vi.fn<() => void>(),
};

describe('PastebinTab', () => {
  it('renders empty state when no clips', () => {
    render(<PastebinTab {...defaultProps} pastebin={[]} />);
    expect(screen.getByText('No clips yet.')).toBeInTheDocument();
  });

  it('renders clip list with truncated content', () => {
    const shortText = 'short text';
    render(<PastebinTab {...defaultProps} pastebin={[shortText]} />);
    expect(screen.getByText(shortText)).toBeInTheDocument();

    const longText = 'a'.repeat(101);
    render(<PastebinTab {...defaultProps} pastebin={[longText]} />);
    const truncated = 'a'.repeat(100) + '...';
    expect(screen.getByText(truncated)).toBeInTheDocument();
  });

  it('dispatches REMOVE_PASTEBIN when Remove is clicked', async () => {
    dispatch.mockClear();
    render(<PastebinTab {...defaultProps} pastebin={['alpha', 'beta']} />);
    const removeButtons = screen.getAllByText('Remove');
    await userEvent.click(removeButtons[0]);
    expect(dispatch).toHaveBeenCalledWith({ type: 'REMOVE_PASTEBIN', index: 0 });
  });

  it('calls onOpenAddPastebinModal when Add clip is clicked', async () => {
    const onOpenAddPastebinModal = vi.fn();
    render(<PastebinTab {...defaultProps} onOpenAddPastebinModal={onOpenAddPastebinModal} />);
    await userEvent.click(screen.getByRole('button', { name: 'Add clip' }));
    expect(onOpenAddPastebinModal).toHaveBeenCalled();
  });

  it('calls onOpenPastebinEditModal when Edit is clicked', async () => {
    const onOpenPastebinEditModal = vi.fn<(index: number, content: string) => void>();
    render(
      <PastebinTab
        {...defaultProps}
        pastebin={['clip content']}
        onOpenPastebinEditModal={onOpenPastebinEditModal}
      />
    );
    await userEvent.click(screen.getByText('Edit'));
    expect(onOpenPastebinEditModal).toHaveBeenCalledWith(0, 'clip content');
  });
});
