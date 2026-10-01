import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import CopyButton from './CopyButton';

const toastSuccessMock = vi.fn();
const toastErrorMock = vi.fn();

vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: toastSuccessMock, error: toastErrorMock }),
}));

const originalClipboard = navigator.clipboard;
const writeTextMock = vi.fn();

// userEvent.setup() reinitializes navigator.clipboard, so each test that
// uses userEvent re-stubs after setup.
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

describe('CopyButton', () => {
  it('labels the button and tooltip with the copy target', async () => {
    const user = userEvent.setup();
    stubClipboard();
    render(<CopyButton text="x" label="prompt" />);

    const button = screen.getByRole('button', { name: 'Copy prompt' });
    expect(button).toHaveAttribute('type', 'button');
    await user.hover(button);
    expect(await screen.findByRole('tooltip')).toHaveTextContent('Copy prompt');
  });

  it('copies the exact text and reports success', async () => {
    const user = userEvent.setup();
    stubClipboard();
    const text = '  line one\nline two  \n';
    render(<CopyButton text={text} label="prompt" />);

    await user.click(screen.getByRole('button', { name: 'Copy prompt' }));

    await waitFor(() => expect(toastSuccessMock).toHaveBeenCalledWith('Copied prompt'));
    expect(writeTextMock).toHaveBeenCalledWith(text);
    expect(writeTextMock).toHaveBeenCalledTimes(1);
    expect(toastErrorMock).not.toHaveBeenCalled();
  });

  it('reports a clipboard failure without reporting success', async () => {
    const user = userEvent.setup();
    stubClipboard();
    writeTextMock.mockRejectedValue(new Error('clipboard denied'));
    render(<CopyButton text="x" label="path" />);

    await user.click(screen.getByRole('button', { name: 'Copy path' }));

    await waitFor(() => expect(toastErrorMock).toHaveBeenCalledWith('Failed to copy'));
    expect(toastSuccessMock).not.toHaveBeenCalled();
  });

  it('is reachable by keyboard and activates with Enter', async () => {
    const user = userEvent.setup();
    stubClipboard();
    render(<CopyButton text="keyboard" label="message" />);

    const button = screen.getByRole('button', { name: 'Copy message' });
    await user.tab();
    expect(button).toHaveFocus();
    await user.keyboard('{Enter}');

    await waitFor(() => expect(writeTextMock).toHaveBeenCalledWith('keyboard'));
  });

  it('defaults to icon-btn and accepts a class and test id', () => {
    const { rerender } = render(<CopyButton text="x" label="path" />);
    expect(screen.getByRole('button', { name: 'Copy path' })).toHaveClass('icon-btn');

    rerender(
      <CopyButton text="x" label="path" className="copy-field__btn" testId="copy-path-btn" />
    );
    const button = screen.getByTestId('copy-path-btn');
    expect(button).toHaveClass('copy-field__btn');
    expect(button).not.toHaveClass('icon-btn');
  });
});
