import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import PastebinDropdown from './PastebinDropdown';

// Track navigations
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

function renderDropdown(
  entries: string[] = [],
  opts: { onPaste?: (content: string) => void; onClose?: () => void; disabled?: boolean } = {}
) {
  const onPaste = opts.onPaste ?? vi.fn();
  const onClose = opts.onClose ?? vi.fn();
  act(() => {
    render(
      <MemoryRouter>
        <PastebinDropdown
          entries={entries}
          onPaste={onPaste}
          onClose={onClose}
          disabled={opts.disabled}
        />
      </MemoryRouter>
    );
  });
  return { onPaste, onClose };
}

describe('PastebinDropdown', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders entries with first line truncated to 40 chars', () => {
    renderDropdown(['short text']);

    expect(screen.getByText('short text')).toBeInTheDocument();

    const longLine = 'a'.repeat(50);
    renderDropdown([longLine]);

    const expected = 'a'.repeat(40) + '...';
    expect(screen.getByText(expected)).toBeInTheDocument();
  });

  it('renders "No pastes yet" when entries is empty', () => {
    renderDropdown([]);

    expect(screen.getByText('No pastes yet')).toBeInTheDocument();
  });

  it('renders "No active session" when disabled', () => {
    renderDropdown([], { disabled: true });

    expect(screen.getByText('No active session')).toBeInTheDocument();
  });

  it('calls onPaste and onClose when entry is clicked', async () => {
    const user = userEvent.setup();
    const onPaste = vi.fn();
    const onClose = vi.fn();

    renderDropdown(['hello world'], { onPaste, onClose });
    await user.click(screen.getByText('hello world'));

    expect(onPaste).toHaveBeenCalledWith('hello world');
    expect(onClose).toHaveBeenCalled();
  });

  it('calls onClose and navigates when manage is clicked', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();

    renderDropdown([], { onClose });
    await user.click(screen.getByText('manage'));

    expect(onClose).toHaveBeenCalled();
    expect(mockNavigate).toHaveBeenCalledWith('/config?tab=pastebin');
  });

  it('shows only first line of multiline content', () => {
    renderDropdown(['line1\nline2\nline3']);

    expect(screen.getByText('line1')).toBeInTheDocument();
    expect(screen.queryByText('line2')).not.toBeInTheDocument();
    expect(screen.queryByText('line3')).not.toBeInTheDocument();
  });
});
