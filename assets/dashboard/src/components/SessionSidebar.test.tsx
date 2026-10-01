import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import SessionSidebar from './SessionSidebar';
import type { SessionResponse, SessionWithWorkspace } from '../lib/types';

const toastSuccessMock = vi.fn();
const toastErrorMock = vi.fn();
vi.mock('./ToastProvider', () => ({
  useToast: () => ({ success: toastSuccessMock, error: toastErrorMock }),
}));

const originalClipboard = navigator.clipboard;
const writeTextMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  writeTextMock.mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextMock },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: originalClipboard,
    writable: true,
    configurable: true,
  });
});

const session: SessionResponse = {
  id: 'sess-1',
  target: 'claude',
  branch: 'main',
  created_at: new Date().toISOString(),
  running: true,
  attach_cmd: 'tmux attach -t sess-1',
  nickname: 'worker',
};

function renderSidebar(
  showAttach: boolean,
  onDispose = vi.fn(),
  overrides: Partial<SessionResponse & Pick<SessionWithWorkspace, 'model'>> = {}
) {
  render(
    <SessionSidebar
      session={{ ...session, ...overrides }}
      config={{
        tmux_socket_name: 'schmux',
        system_capabilities: { iterm2_available: true, fence_available: false },
      }}
      showAttach={showAttach}
      onEditNickname={vi.fn()}
      onDispose={onDispose}
    />
  );
  return onDispose;
}

describe('SessionSidebar', () => {
  it('shows metadata and the attach command for terminal sessions', () => {
    renderSidebar(true);
    expect(screen.getByText('sess-1')).toBeInTheDocument();
    expect(screen.getByText('claude')).toBeInTheDocument();
    expect(screen.getByText('worker')).toBeInTheDocument();
    expect(screen.getByText('Attach Command')).toBeInTheDocument();
    expect(screen.getByText('tmux attach -t sess-1')).toBeInTheDocument();
    expect(screen.getByText('Open in iTerm2')).toBeInTheDocument();
  });

  it('hides the attach command and iTerm2 link when showAttach is false', () => {
    renderSidebar(false);
    expect(screen.queryByText('Attach Command')).not.toBeInTheDocument();
    expect(screen.queryByText('Open in iTerm2')).not.toBeInTheDocument();
    expect(screen.getByTestId('dispose-session')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copy attach command' })).toBeNull();
  });

  it('copies the attach command from its field', async () => {
    renderSidebar(true);

    const button = screen.getByRole('button', { name: 'Copy attach command' });
    expect(button).toHaveClass('copy-field__btn');
    expect(button.closest('.copy-field')).not.toBeNull();
    fireEvent.click(button);

    await waitFor(() => expect(toastSuccessMock).toHaveBeenCalledWith('Copied attach command'));
    expect(writeTextMock).toHaveBeenCalledWith('tmux attach -t sess-1');
  });

  it('dispose button calls onDispose', async () => {
    const onDispose = renderSidebar(false);
    await userEvent.click(screen.getByTestId('dispose-session'));
    expect(onDispose).toHaveBeenCalled();
  });

  it('prefixes the context window with live usage when reported', () => {
    renderSidebar(false, vi.fn(), {
      model: { context_window: 1000000 },
      context_tokens: 272000,
    });
    expect(screen.getByTestId('session-context-window')).toHaveTextContent(
      /^272K \/ 1000K tokens$/
    );
  });

  it('shows only the maximum before any usage is reported', () => {
    renderSidebar(false, vi.fn(), { model: { context_window: 1000000 } });
    expect(screen.getByTestId('session-context-window')).toHaveTextContent(/^1000K tokens$/);
  });

  it('hides the row when the model has no known context window', () => {
    renderSidebar(false, vi.fn(), { context_tokens: 157088 });
    expect(screen.queryByText('Context Window')).not.toBeInTheDocument();
  });
});
