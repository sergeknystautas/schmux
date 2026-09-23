import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import UserMessageBubble from './UserMessageBubble';
import type { UserMessage } from '../../lib/chat/types';

const toastSuccessMock = vi.fn();
const toastErrorMock = vi.fn();

vi.mock('../ToastProvider', () => ({
  useToast: () => ({ success: toastSuccessMock, error: toastErrorMock }),
}));

const originalClipboard = navigator.clipboard;
const writeTextMock = vi.fn();

const baseMessage: UserMessage = {
  kind: 'user',
  id: 'user-1',
  text: 'message text',
  images: [],
  queued: false,
};

function renderMessage(overrides: Partial<UserMessage> = {}) {
  return render(<UserMessageBubble message={{ ...baseMessage, ...overrides }} />);
}

// userEvent.setup() reinitializes navigator.clipboard, so the stub is
// applied per test after setup rather than in beforeEach.
function stubClipboard() {
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextMock },
    writable: true,
    configurable: true,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  writeTextMock.mockResolvedValue(undefined);
  stubClipboard();
});

afterEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: originalClipboard,
    writable: true,
    configurable: true,
  });
});

describe('UserMessageBubble', () => {
  it('copies exact message text and reports success', async () => {
    const user = userEvent.setup();
    stubClipboard();
    const text = '  first line\nsecond line  ';
    renderMessage({
      text,
      queued: true,
      images: [{ media_type: 'image/png', data: 'aW1hZ2U=' }],
    });

    const button = screen.getByRole('button', { name: 'Copy message' });
    await user.hover(button);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Copy message');
    await user.click(button);

    await waitFor(() => expect(toastSuccessMock).toHaveBeenCalledWith('Copied message'));
    expect(writeTextMock).toHaveBeenCalledWith(text);
    expect(writeTextMock).toHaveBeenCalledTimes(1);
    expect(toastErrorMock).not.toHaveBeenCalled();
  });

  it('reports a clipboard failure without reporting success', async () => {
    const user = userEvent.setup();
    stubClipboard();
    writeTextMock.mockRejectedValue(new Error('clipboard denied'));
    renderMessage();

    await user.click(screen.getByRole('button', { name: 'Copy message' }));

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledWith('Failed to copy'));
    expect(toastSuccessMock).not.toHaveBeenCalled();
  });

  it('does not offer copy for an image-only message', () => {
    renderMessage({
      text: '',
      images: [{ media_type: 'image/png', data: 'aW1hZ2U=' }],
    });

    expect(screen.queryByRole('button', { name: 'Copy message' })).not.toBeInTheDocument();
  });

  it('keeps whitespace-only text copyable and the action keyboard-accessible', async () => {
    const user = userEvent.setup();
    stubClipboard();
    renderMessage({ text: '  \n' });

    const button = screen.getByRole('button', { name: 'Copy message' });
    expect(button).toHaveAttribute('type', 'button');
    await user.tab();
    expect(button).toHaveFocus();
    await user.keyboard('{Enter}');

    expect(writeTextMock).toHaveBeenCalledWith('  \n');
  });
});
