import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import ChatSessionPage from './ChatSessionPage';
import { useSessions } from '../contexts/SessionsContext';
import { useChatSocket } from '../hooks/useChatSocket';
import type { Conversation } from '../lib/chat/types';

vi.mock('../contexts/SessionsContext', () => ({ useSessions: vi.fn() }));
vi.mock('../hooks/useChatSocket', () => ({ useChatSocket: vi.fn() }));
vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/RestartSessionModal', () => ({
  default: () => null,
}));
vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ confirm: vi.fn(), alert: vi.fn(), prompt: vi.fn() }),
}));
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: { tmux_socket_name: 'schmux', system_capabilities: {} } }),
}));
const sessionActions = { editNickname: vi.fn(), dispose: vi.fn(), copyAttach: vi.fn() };
vi.mock('../hooks/useSessionActions', () => ({
  useSessionActions: () => sessionActions,
}));
const keyboard = { registerAction: vi.fn(), unregisterAction: vi.fn() };
vi.mock('../contexts/KeyboardContext', () => ({
  useKeyboardMode: () => keyboard,
}));

const useSessionsMock = vi.mocked(useSessions);
const useChatSocketMock = vi.mocked(useChatSocket);

const userConversation: Conversation = {
  items: [{ kind: 'user', id: 'u1', text: 'hello from the user', images: [], queued: false }],
  phase: 'idle',
};

function chatSocketReturn(overrides: Partial<ReturnType<typeof useChatSocket>> = {}) {
  return {
    conversation: userConversation,
    status: 'connected' as const,
    error: null,
    send: vi.fn(),
    interrupt: vi.fn(),
    answerPermission: vi.fn(),
    answerQuestion: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useChatSocket>;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/sessions/chat-1']}>
      <Routes>
        <Route path="/sessions/:sessionId" element={<ChatSessionPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('ChatSessionPage', () => {
  beforeEach(() => {
    useSessionsMock.mockReturnValue({
      sessionsById: {
        'chat-1': {
          id: 'chat-1',
          kind: 'chat',
          workspace_id: 'ws-1',
          target: 'claude',
          branch: 'main',
          created_at: new Date().toISOString(),
          attach_cmd: 'tmux attach -t chat-1',
          running: false,
          fence: false,
        },
      },
      workspaces: [{ id: 'ws-1', sessions: [] }],
    } as unknown as ReturnType<typeof useSessions>);
    useChatSocketMock.mockReturnValue(chatSocketReturn());
  });

  it('renders the conversation, header, and tabs', () => {
    renderPage();
    expect(screen.getByText('hello from the user')).toBeInTheDocument();
    expect(screen.getByTestId('workspace-header')).toBeInTheDocument();
    expect(screen.getByTestId('session-tabs')).toBeInTheDocument();
  });

  it('puts focus in the composer on mount', () => {
    renderPage();
    expect(document.activeElement).toBe(screen.getByTestId('chat-input'));
  });

  it('puts focus in the composer once the socket connects', () => {
    useChatSocketMock.mockReturnValue(chatSocketReturn({ status: 'connecting' }));
    const view = renderPage();
    expect(document.activeElement).not.toBe(screen.getByTestId('chat-input'));
    useChatSocketMock.mockReturnValue(chatSocketReturn({ status: 'connected' }));
    view.rerender(
      <MemoryRouter initialEntries={['/sessions/chat-1']}>
        <Routes>
          <Route path="/sessions/:sessionId" element={<ChatSessionPage />} />
        </Routes>
      </MemoryRouter>
    );
    expect(document.activeElement).toBe(screen.getByTestId('chat-input'));
  });

  it('renders the shared session sidebar without the attach command, inside the session grid', () => {
    renderPage();
    const sidebar = screen.getByTestId('session-sidebar');
    expect(sidebar).toBeInTheDocument();
    expect(screen.getByText('chat-1')).toBeInTheDocument();
    expect(screen.queryByText('Attach Command')).not.toBeInTheDocument();
    expect(screen.getByTestId('dispose-session')).toBeInTheDocument();
    expect(document.querySelector('.session-detail')).toContainElement(sidebar);
    expect(document.querySelector('.session-detail__main .log-viewer')).toContainElement(
      screen.getByTestId('chat-view')
    );
  });

  it('collapses the sidebar from the status row', async () => {
    renderPage();
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.click(screen.getByTestId('chat-sidebar-toggle'));
    expect(document.querySelector('.session-detail')).toHaveClass(
      'session-detail--sidebar-collapsed'
    );
  });

  it("registers the terminal page's Down-arrow resume action in session scope", () => {
    keyboard.registerAction.mockClear();
    const view = renderPage();
    expect(keyboard.registerAction).toHaveBeenCalledWith(
      expect.objectContaining({
        key: 'ArrowDown',
        description: 'Resume / scroll to bottom',
        scope: { type: 'session', id: 'chat-1' },
      })
    );
    const action = keyboard.registerAction.mock.calls[0][0] as { handler: () => void };
    action.handler();
    expect(document.activeElement).toBe(screen.getByTestId('chat-input'));
    view.unmount();
    expect(keyboard.unregisterAction).toHaveBeenCalledWith('ArrowDown', false, {
      type: 'session',
      id: 'chat-1',
    });
  });

  it('keeps the in-progress message per session across tab switches', async () => {
    sessionStorage.clear();
    useSessionsMock.mockReturnValue({
      sessionsById: {
        'chat-1': {
          id: 'chat-1',
          kind: 'chat',
          workspace_id: 'ws-1',
          target: 'claude',
          branch: 'main',
          created_at: new Date().toISOString(),
          attach_cmd: '',
          running: true,
          fence: false,
        },
        'chat-2': {
          id: 'chat-2',
          kind: 'chat',
          workspace_id: 'ws-1',
          target: 'claude',
          branch: 'main',
          created_at: new Date().toISOString(),
          attach_cmd: '',
          running: true,
          fence: false,
        },
      },
      workspaces: [{ id: 'ws-1', sessions: [] }],
    } as unknown as ReturnType<typeof useSessions>);
    const { default: userEvent } = await import('@testing-library/user-event');
    const at = (id: string) => (
      <MemoryRouter initialEntries={[`/sessions/${id}`]}>
        <Routes>
          <Route path="/sessions/:sessionId" element={<ChatSessionPage />} />
        </Routes>
      </MemoryRouter>
    );
    const view = render(at('chat-1'));
    await userEvent.type(screen.getByTestId('chat-input'), 'draft for one');

    // Switch to another chat session: its composer is empty, not carrying the draft over.
    view.unmount();
    const second = render(at('chat-2'));
    expect((screen.getByTestId('chat-input') as HTMLTextAreaElement).value).toBe('');
    second.unmount();

    // Back to the first: the draft is restored.
    render(at('chat-1'));
    expect((screen.getByTestId('chat-input') as HTMLTextAreaElement).value).toBe('draft for one');
  });

  it('dispose in the sidebar uses the shared session action', async () => {
    renderPage();
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.click(screen.getByTestId('dispose-session'));
    expect(sessionActions.dispose).toHaveBeenCalled();
  });

  it('shows Stop only while a turn is running', () => {
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({
        conversation: {
          items: [
            { kind: 'user', id: 'u1', text: 'go', images: [], queued: false },
            { kind: 'assistant', end: null, interrupted: false, thinking: false, segments: [] },
          ],
          phase: 'running',
        },
      })
    );
    renderPage();
    expect(screen.getByTestId('chat-stop')).toBeInTheDocument();
  });

  it('hides Stop when idle', () => {
    renderPage();
    expect(screen.queryByTestId('chat-stop')).not.toBeInTheDocument();
  });
});
