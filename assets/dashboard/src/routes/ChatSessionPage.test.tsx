import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import ChatSessionPage from './ChatSessionPage';
import { useSessions } from '../contexts/SessionsContext';
import { useChatSocket } from '../hooks/useChatSocket';
import type { Conversation } from '../lib/chat/types';
import { saveChatDraft } from '../lib/chat-draft';
import { saveChatAnswer, loadChatAnswers } from '../lib/chat-answers';
import { saveChatFocus } from '../lib/chat-focus';

const mockAnalyzeFence = vi.fn();
const mockOpenWorkspaceFile = vi.fn();
const mockSetPendingNavigation = vi.fn();

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
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return {
    ...actual,
    analyzeFence: (...args: unknown[]) => mockAnalyzeFence(...args),
    openWorkspaceFile: (...args: unknown[]) => mockOpenWorkspaceFile(...args),
  };
});
let mockConfig = {
  tmux_socket_name: 'schmux',
  system_capabilities: {},
  fence_analyze: { enabled: false, target: '' },
};
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: mockConfig }),
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

const emptyActivity = {
  operations: {},
  toolIndex: {},
  order: [],
  checklist: {},
  checklistOrder: [],
  pendingInput: [],
  attentionOutcomes: [],
  live: true,
};

const userConversation: Conversation = {
  items: [{ kind: 'user', id: 'u1', text: 'hello from the user', images: [], queued: false }],
  phase: 'idle',
  activity: emptyActivity,
};

function chatSocketReturn(overrides: Partial<ReturnType<typeof useChatSocket>> = {}) {
  return {
    conversation: userConversation,
    status: 'connected' as const,
    error: null,
    historyLoaded: true,
    send: vi.fn(),
    interrupt: vi.fn(),
    answerPermission: vi.fn(),
    answerQuestion: vi.fn(),
    abort: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useChatSocket>;
}

const questionConversation: Conversation = {
  items: [
    {
      kind: 'assistant',
      segments: [
        {
          kind: 'pending',
          requestId: 'r1',
          toolUseId: 't1',
          toolName: 'AskUserQuestion',
          input: {},
          questions: [
            { id: 'Pick?', question: 'Pick?', options: [{ label: 'A' }], multiSelect: false },
          ],
        },
      ],
      end: null,
      interrupted: false,
      thinking: false,
    },
  ],
  phase: 'running',
  activity: emptyActivity,
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/sessions/chat-1']}>
      <Routes>
        <Route path="/sessions/:sessionId" element={<ChatSessionPage />} />
        <Route path="*" element={<CurrentRoute />} />
      </Routes>
    </MemoryRouter>
  );
}

function CurrentRoute() {
  const location = useLocation();
  return <div data-testid="current-route">{location.pathname + location.search}</div>;
}

describe('ChatSessionPage', () => {
  beforeEach(() => {
    sessionStorage.clear();
    mockAnalyzeFence.mockReset().mockResolvedValue({});
    mockOpenWorkspaceFile.mockReset();
    mockSetPendingNavigation.mockReset();
    mockConfig = {
      tmux_socket_name: 'schmux',
      system_capabilities: {},
      fence_analyze: { enabled: false, target: '' },
    };
    useChatSocketMock.mockClear();
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
      workspaces: [{ id: 'ws-1', path: '/Users/dev/ws-1', sessions: [] }],
      setPendingNavigation: mockSetPendingNavigation,
    } as unknown as ReturnType<typeof useSessions>);
    useChatSocketMock.mockReturnValue(chatSocketReturn());
  });

  it('renders the conversation, header, and tabs', () => {
    renderPage();
    expect(screen.getByText('hello from the user')).toBeInTheDocument();
    expect(screen.getByTestId('workspace-header')).toBeInTheDocument();
    expect(screen.getByTestId('session-tabs')).toBeInTheDocument();
  });

  it('opens a chat file link through pending React tab navigation', async () => {
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({
        conversation: {
          items: [
            {
              kind: 'assistant',
              end: { state: 'done' },
              interrupted: false,
              thinking: false,
              segments: [
                {
                  kind: 'prose',
                  text: '[Readme](/Users/dev/ws-1/docs/readme.md)',
                  streaming: false,
                },
              ],
            },
          ],
          phase: 'idle',
          activity: emptyActivity,
        },
      })
    );
    mockOpenWorkspaceFile.mockResolvedValue({
      id: 'tab-1',
      navigation: 'tab',
      route: '/diff/ws-1/md/docs%2Freadme.md',
      status: 'ok',
    });

    renderPage();
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.click(screen.getByRole('link', { name: 'Readme' }));

    expect(mockOpenWorkspaceFile).toHaveBeenCalledWith('ws-1', 'docs/readme.md');
    expect(mockSetPendingNavigation).toHaveBeenCalledWith({
      type: 'tab',
      workspaceId: 'ws-1',
      tabRoute: '/diff/ws-1/md/docs%2Freadme.md',
    });
  });

  it('navigates directly in React when the file view does not create a tab', async () => {
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({
        conversation: {
          items: [
            {
              kind: 'assistant',
              end: { state: 'done' },
              interrupted: false,
              thinking: false,
              segments: [
                {
                  kind: 'prose',
                  text: '[Source](/Users/dev/ws-1/src/main.go)',
                  streaming: false,
                },
              ],
            },
          ],
          phase: 'idle',
          activity: emptyActivity,
        },
      })
    );
    mockOpenWorkspaceFile.mockResolvedValue({
      navigation: 'direct',
      route: '/diff/ws-1?file=src%2Fmain.go',
      status: 'ok',
    });

    renderPage();
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.click(screen.getByRole('link', { name: 'Source' }));

    expect(screen.getByTestId('current-route')).toHaveTextContent('/diff/ws-1?file=src%2Fmain.go');
    expect(mockSetPendingNavigation).not.toHaveBeenCalled();
  });

  it('offers fence analysis for a fenced session when the feature is enabled', async () => {
    mockConfig.fence_analyze.enabled = true;
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
          fence: true,
        },
      },
      workspaces: [{ id: 'ws-1', sessions: [] }],
    } as unknown as ReturnType<typeof useSessions>);

    renderPage();
    const { default: userEvent } = await import('@testing-library/user-event');
    await userEvent.click(screen.getByTestId('analyze-fence'));

    expect(mockAnalyzeFence).toHaveBeenCalledWith('chat-1');
  });

  it('puts focus in the composer on mount', () => {
    renderPage();
    expect(document.activeElement).toBe(screen.getByTestId('chat-input'));
  });

  it('puts focus in the composer once the socket connects and history loads', () => {
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({ status: 'connecting', historyLoaded: false })
    );
    const view = renderPage();
    expect(document.activeElement).not.toBe(screen.getByTestId('chat-input'));
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({ status: 'connected', historyLoaded: true })
    );
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

  it('places Stop in the activity footer and interrupts the running turn', () => {
    const interrupt = vi.fn();
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({
        interrupt,
        conversation: {
          items: [
            { kind: 'user', id: 'u1', text: 'go', images: [], queued: false },
            { kind: 'assistant', end: null, interrupted: false, thinking: false, segments: [] },
          ],
          phase: 'running',
          activity: emptyActivity,
        },
      })
    );
    renderPage();
    const stop = screen.getByTestId('chat-stop');
    expect(stop.closest('[data-testid="chat-activity"]')).toBe(screen.getByTestId('chat-activity'));
    fireEvent.click(stop);
    expect(interrupt).toHaveBeenCalledTimes(1);
  });

  it('hides Stop when idle', () => {
    renderPage();
    expect(screen.queryByTestId('chat-stop')).not.toBeInTheDocument();
  });

  it('does not focus anything before history loads', () => {
    useChatSocketMock.mockReturnValue(
      chatSocketReturn({ status: 'connected', historyLoaded: false })
    );
    renderPage();
    expect(document.activeElement).not.toBe(screen.getByTestId('chat-input'));
  });

  it('restores the composer caret from the focus record', () => {
    saveChatDraft('chat-1', { text: 'hello world', images: [] });
    saveChatFocus('chat-1', { target: 'composer', position: 5 });
    renderPage();
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    expect(document.activeElement).toBe(ta);
    expect(ta.selectionStart).toBe(5);
  });

  it('restores focus to a pending question Other input at the saved caret', () => {
    saveChatFocus('chat-1', {
      target: 'other-input',
      requestId: 'r1',
      questionId: 'Pick?',
      position: 2,
    });
    // Seed an answer so the input has characters at position 2; otherwise
    // the restore clamps the caret to 0.
    saveChatAnswer('chat-1', 'r1', 'Pick?', { selected: [], other: 'abcd' });
    useChatSocketMock.mockReturnValue(chatSocketReturn({ conversation: questionConversation }));
    renderPage();
    const input = screen.getByLabelText('Other ( Pick? )') as HTMLInputElement;
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(2);
  });

  it('falls back to the composer at end when the recorded question field is gone', () => {
    saveChatDraft('chat-1', { text: 'draft', images: [] });
    saveChatFocus('chat-1', {
      target: 'other-input',
      requestId: 'gone',
      questionId: 'Pick?',
      position: 1,
    });
    renderPage(); // default conversation has no pending question
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    expect(document.activeElement).toBe(ta);
    expect(ta.selectionStart).toBe(5);
  });

  it('clears the answers draft and moves focus to the composer when the request resolves', () => {
    saveChatAnswer('chat-1', 'r1', 'Pick?', { selected: ['A'], other: '' });
    saveChatFocus('chat-1', { target: 'option', requestId: 'r1', questionId: 'Pick?', label: 'A' });
    useChatSocketMock.mockReturnValue(chatSocketReturn({ conversation: questionConversation }));
    renderPage();
    const onRequestResolved = useChatSocketMock.mock.calls[0][2] as (id: string) => void;
    act(() => onRequestResolved('r1'));
    expect(loadChatAnswers('chat-1')).toEqual({});
    expect(document.activeElement).toBe(screen.getByTestId('chat-input'));
  });
});
