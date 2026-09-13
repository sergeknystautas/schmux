import { describe, it, expect, vi } from 'vitest';
import { act, createRef } from 'react';
import { render, screen, fireEvent, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ChatView from './ChatView';
import type { TranscriptHandle } from './ChatTranscript';
import { capturedActivity } from '../../lib/chat/__fixtures__/activity';
import { reduceRecords } from '../../lib/chat/reducer';
import type { Conversation, ConversationRecord, HarnessLine } from '../../lib/chat/types';

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

const baseProps = {
  status: 'connected' as const,
  ended: false,
  historyLoaded: true,
  socketError: null,
  onSend: vi.fn(),
  onInterrupt: vi.fn(),
  onPermission: vi.fn(),
  onAnswer: vi.fn(),
  onAbort: vi.fn(),
  signedOut: false,
  signedOutProtocol: '',
  onReauth: vi.fn(),
};

function conversationWith(items: Conversation['items']): Conversation {
  return { items, phase: 'running', activity: emptyActivity };
}

describe('ChatView', () => {
  it("renders prose through the dashboard's Markdown stylesheet, not a chat-only one", () => {
    render(
      <ChatView
        {...baseProps}
        conversation={conversationWith([
          { kind: 'user', id: 'u1', text: 'hi', images: [], queued: false },
          {
            kind: 'assistant',
            end: { state: 'done' },
            interrupted: false,
            thinking: false,
            segments: [{ kind: 'prose', text: '- one\n- two', streaming: false }],
          },
        ])}
      />
    );
    const prose = screen.getByTestId('chat-prose');
    expect(prose).toHaveClass('markdown-preview-content', 'markdown-preview-content--inline');
    expect(prose.querySelectorAll('li')).toHaveLength(2);
  });

  it.each(['background', 'agent'] as const)(
    'jumps from the captured %s operation to its distinct launch tool',
    (name) => {
      const records = capturedActivity(name);
      const index = records.findIndex(
        (r) => r.type === 'harness' && r.line.subtype === 'task_notification'
      );
      const notification = records[index];
      if (notification.type !== 'harness') throw new Error('missing notification');
      const toolId = String(notification.line.tool_use_id);
      expect(toolId).not.toBe(notification.line.task_id);
      const clock = vi.spyOn(Date, 'now').mockReturnValue(Date.parse(notification.ts));
      const scrollIntoView = vi.fn();
      const { container } = render(
        <ChatView
          {...baseProps}
          conversation={reduceRecords(
            'claude-stream-json',
            records.slice(
              0,
              records.findIndex((r) => r.type === 'harness' && r.line.subtype === 'task_started') +
                1
            )
          )}
        />
      );
      const transcript = screen.getByTestId('chat-transcript');
      const target = transcript.querySelector<HTMLElement>(`[data-tool-id="${toolId}"]`)!;
      target.scrollIntoView = scrollIntoView;
      const row = container.querySelector<HTMLElement>(
        `[data-activity-key$=":${String(notification.line.task_id)}"]`
      )!;
      expect(target).not.toHaveFocus();
      fireEvent.click(within(row).getByRole('button', { name: 'Jump to transcript' }));
      expect(target).toHaveFocus();
      expect(scrollIntoView).toHaveBeenCalledWith({ block: 'center', behavior: 'instant' });
      clock.mockRestore();
    }
  );
  it('renders the user message and assistant prose', () => {
    const conversation = conversationWith([
      { kind: 'user', id: 'u1', text: 'hello there', images: [], queued: false },
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          { kind: 'prose', text: 'Hi **friend**', streaming: false },
          {
            kind: 'tool',
            id: 't1',
            name: 'Bash',
            input: { command: 'ls' },
            inputJson: '{"command":"ls"}',
            result: 'file.txt',
            state: 'done',
            subtools: [],
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} conversation={conversation} />);
    expect(screen.getByText('hello there')).toBeInTheDocument();
    expect(screen.getByText('friend')).toBeInTheDocument(); // markdown bold
    expect(screen.getByText('Bash')).toBeInTheDocument();
    expect(screen.getByText('file.txt')).toBeInTheDocument();
  });

  it('Escape interrupts only while running', async () => {
    const onInterrupt = vi.fn();
    const { rerender } = render(
      <ChatView
        {...baseProps}
        onInterrupt={onInterrupt}
        conversation={conversationWith([
          {
            kind: 'assistant',
            end: null,
            interrupted: false,
            thinking: false,
            segments: [],
          },
        ])}
      />
    );
    fireEvent.keyDown(screen.getByTestId('chat-view'), { key: 'Escape' });
    expect(onInterrupt).toHaveBeenCalledTimes(1);

    rerender(
      <ChatView
        {...baseProps}
        onInterrupt={onInterrupt}
        conversation={{
          items: [
            {
              kind: 'assistant',
              end: { state: 'done' },
              interrupted: false,
              thinking: false,
              segments: [],
            },
          ],
          phase: 'idle',
          activity: emptyActivity,
        }}
      />
    );
    fireEvent.keyDown(screen.getByTestId('chat-view'), { key: 'Escape' });
    expect(onInterrupt).toHaveBeenCalledTimes(1);
  });

  it('Escape outside the chat view does not interrupt', async () => {
    const onInterrupt = vi.fn();
    render(
      <div>
        <ChatView
          {...baseProps}
          onInterrupt={onInterrupt}
          conversation={conversationWith([
            {
              kind: 'assistant',
              end: null,
              interrupted: false,
              thinking: false,
              segments: [],
            },
          ])}
        />
        <div data-testid="outside">outside</div>
      </div>
    );
    fireEvent.keyDown(screen.getByTestId('outside'), { key: 'Escape' });
    expect(onInterrupt).not.toHaveBeenCalled();
  });

  it('permission card answers call onPermission', async () => {
    const onPermission = vi.fn();
    const conversation = conversationWith([
      { kind: 'user', id: 'u1', text: 'run it', images: [], queued: false },
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'pending',
            requestId: 'req-1',
            toolUseId: 'tu-1',
            toolName: 'Bash',
            input: { command: 'rm -rf /' },
            questions: null,
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} onPermission={onPermission} conversation={conversation} />);
    await userEvent.click(screen.getByRole('button', { name: 'Allow' }));
    expect(onPermission).toHaveBeenCalledWith('req-1', true, { command: 'rm -rf /' });
    await userEvent.click(screen.getByRole('button', { name: 'Deny' }));
    expect(onPermission).toHaveBeenCalledWith(
      'req-1',
      false,
      undefined,
      'Denied from the schmux chat'
    );
  });

  it('permission card shows the command, not raw JSON', () => {
    const conversation = conversationWith([
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'pending',
            requestId: 'req-1',
            toolUseId: 'tu-1',
            toolName: 'Bash',
            input: { command: 'ls -la' },
            questions: null,
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} conversation={conversation} />);
    expect(screen.getByText('ls -la')).toBeInTheDocument();
    expect(screen.queryByText(/^\{.*command.*\}$/)).not.toBeInTheDocument();
  });

  it('permission card shows the reason when present (Codex escalation)', () => {
    const conversation = conversationWith([
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'pending',
            requestId: 'req-1',
            toolUseId: 'tu-1',
            toolName: 'command',
            input: { command: 'sleep 6', reason: 'May I run this outside the sandbox?' },
            questions: null,
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} conversation={conversation} />);
    expect(screen.getByTestId('chat-permission-reason')).toHaveTextContent(
      'May I run this outside the sandbox?'
    );
  });

  it('abort-only cards offer Deny alone and call onAbort', async () => {
    const onAbort = vi.fn();
    const onPermission = vi.fn();
    const conversation = conversationWith([
      { kind: 'user', id: 'u1', text: 'go', images: [], queued: false },
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'pending',
            requestId: '4',
            toolUseId: 'p1',
            toolName: 'item/permissions/requestApproval',
            input: { permissions: {} },
            questions: null,
            abortOnly: true,
          },
        ],
      },
    ]);
    render(
      <ChatView
        {...baseProps}
        onAbort={onAbort}
        onPermission={onPermission}
        conversation={conversation}
      />
    );
    expect(screen.queryByRole('button', { name: 'Allow' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Deny' }));
    expect(onAbort).toHaveBeenCalledWith('4');
    expect(onPermission).not.toHaveBeenCalled();
  });

  it('question card submits selected answers', async () => {
    const onAnswer = vi.fn();
    const conversation = conversationWith([
      { kind: 'user', id: 'u1', text: 'ask me', images: [], queued: false },
      {
        kind: 'assistant',
        end: null,
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'pending',
            requestId: 'req-2',
            toolUseId: 'tu-2',
            toolName: 'AskUserQuestion',
            input: {
              questions: [
                {
                  id: 'Which one?',
                  question: 'Which one?',
                  header: 'Pick',
                  options: [{ label: 'Alpha' }, { label: 'Beta' }],
                },
              ],
            },
            questions: [
              {
                id: 'Which one?',
                question: 'Which one?',
                header: 'Pick',
                options: [{ label: 'Alpha' }, { label: 'Beta' }],
              },
            ],
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} onAnswer={onAnswer} conversation={conversation} />);
    await userEvent.click(screen.getByRole('radio', { name: 'Beta' }));
    await userEvent.click(screen.getByRole('button', { name: 'Submit' }));
    expect(onAnswer).toHaveBeenCalledWith(
      'req-2',
      { 'Which one?': ['Beta'] },
      {
        questions: [
          {
            id: 'Which one?',
            question: 'Which one?',
            header: 'Pick',
            options: [{ label: 'Alpha' }, { label: 'Beta' }],
          },
        ],
      }
    );
  });

  it('follows the tail at the bottom and detaches on scroll up', async () => {
    const one = conversationWith([
      { kind: 'user', id: 'u1', text: 'one', images: [], queued: false },
      {
        kind: 'assistant',
        end: { state: 'done' },
        interrupted: false,
        thinking: false,
        segments: [],
      },
    ]);
    const { rerender } = render(<ChatView {...baseProps} conversation={one} />);
    const t = screen.getByTestId('chat-transcript');
    Object.defineProperty(t, 'scrollHeight', { value: 1000, configurable: true });
    Object.defineProperty(t, 'clientHeight', { value: 200, configurable: true });
    Object.defineProperty(t, 'scrollTop', { value: 0, writable: true, configurable: true });

    // At the bottom: growth keeps the view at the bottom.
    fireEvent.scroll(t, { target: { scrollTop: 800 } });
    await new Promise((r) => requestAnimationFrame(r));
    rerender(
      <ChatView
        {...baseProps}
        conversation={conversationWith([
          ...one.items,
          { kind: 'user', id: 'u2', text: 'two', images: [], queued: false },
        ])}
      />
    );
    Object.defineProperty(t, 'scrollHeight', { value: 1200, configurable: true });
    rerender(
      <ChatView
        {...baseProps}
        conversation={conversationWith([
          ...one.items,
          { kind: 'user', id: 'u2', text: 'two', images: [], queued: false },
          { kind: 'user', id: 'u3', text: 'three', images: [], queued: false },
        ])}
      />
    );
    await new Promise((r) => requestAnimationFrame(r));
    expect(t.scrollTop).toBe(1200 - 200);

    // Scroll up: view stays, the terminal's Resume control appears, clicking resumes following.
    fireEvent.scroll(t, { target: { scrollTop: 100 } });
    const resume = await screen.findByTestId('chat-resume');
    expect(resume).toHaveClass('btn', 'btn--primary');
    expect(resume).toHaveTextContent('Resume');
    await userEvent.click(resume);
    expect(t.scrollTop).toBe(1200 - 200);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });

  it('anchors the Resume control to the transcript frame, above the activity panel', async () => {
    render(
      <ChatView
        {...baseProps}
        conversation={conversationWith([
          { kind: 'user', id: 'u1', text: 'one', images: [], queued: false },
          {
            kind: 'assistant',
            end: { state: 'done' },
            interrupted: false,
            thinking: false,
            segments: [],
          },
        ])}
      />
    );
    const t = screen.getByTestId('chat-transcript');
    Object.defineProperty(t, 'scrollHeight', { value: 1000, configurable: true });
    Object.defineProperty(t, 'clientHeight', { value: 200, configurable: true });
    Object.defineProperty(t, 'scrollTop', { value: 0, writable: true, configurable: true });
    // Prime the size sync at the bottom first; the first scroll event after
    // a metrics change is treated as a resize, not a user scroll.
    fireEvent.scroll(t, { target: { scrollTop: 800 } });
    fireEvent.scroll(t, { target: { scrollTop: 100 } });
    const resume = await screen.findByTestId('chat-resume');
    // The control floats over the transcript — same frame as the transcript,
    // ahead of the activity section — never in its own row below it.
    expect(resume.parentElement).toBe(t.parentElement);
    const activity = screen.getByTestId('chat-activity');
    expect(
      activity.compareDocumentPosition(resume) & Node.DOCUMENT_POSITION_PRECEDING
    ).toBeTruthy();
  });
});

describe('ChatView persistence', () => {
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

  it('restores question answers through the tree and reports changes up', () => {
    const onAnswerChange = vi.fn();
    render(
      <ChatView
        {...baseProps}
        conversation={questionConversation}
        initialAnswers={{ r1: { 'Pick?': { selected: ['A'], other: 'note' } } }}
        onAnswerChange={onAnswerChange}
      />
    );
    expect(screen.getByRole('radio', { name: 'A' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByLabelText('Other ( Pick? )')).toHaveValue('note');
    fireEvent.click(screen.getByRole('radio', { name: 'A' }));
    expect(onAnswerChange).toHaveBeenCalledWith('r1', 'Pick?', { selected: ['A'], other: 'note' });
  });

  it('focusQuestionTarget focuses the Other input with caret, or an option button', () => {
    const transcriptRef = createRef<TranscriptHandle>();
    render(
      <ChatView
        {...baseProps}
        conversation={questionConversation}
        transcriptRef={transcriptRef}
        initialAnswers={{ r1: { 'Pick?': { selected: [], other: 'abcd' } } }}
      />
    );
    let ok: boolean | undefined;
    act(() => {
      ok = transcriptRef.current?.focusQuestionTarget('r1', 'Pick?', 'other-input', undefined, 2);
    });
    expect(ok).toBe(true);
    const input = screen.getByLabelText('Other ( Pick? )') as HTMLInputElement;
    expect(document.activeElement).toBe(input);
    expect(input.selectionStart).toBe(2);

    act(() => {
      ok = transcriptRef.current?.focusQuestionTarget('r1', 'Pick?', 'option', 'A');
    });
    expect(ok).toBe(true);
    expect(document.activeElement).toBe(screen.getByRole('radio', { name: 'A' }));

    act(() => {
      ok = transcriptRef.current?.focusQuestionTarget('gone', 'Pick?', 'option', 'A');
    });
    expect(ok).toBe(false);
  });
});

describe('signed-out recovery', () => {
  function renderChatView(
    overrides: Partial<{
      signedOut: boolean;
      signedOutProtocol: string;
      onReauth: () => void;
    }> = {}
  ) {
    const onReauth = overrides.onReauth ?? vi.fn();
    return render(
      <ChatView
        {...baseProps}
        conversation={conversationWith([])}
        signedOut={overrides.signedOut ?? false}
        signedOutProtocol={overrides.signedOutProtocol ?? ''}
        onReauth={onReauth}
      />
    );
  }

  it('shows the claude banner copy and sign-in button when signed out', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json' });
    expect(screen.getByTestId('signed-out-banner')).toHaveTextContent(
      "Claude is signed out — messages won't reach it until you sign in again."
    );
    expect(screen.getByRole('button', { name: /sign in/i })).toBeInTheDocument();
  });

  it('shows the codex banner copy (restart instruction)', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'codex-app-server' });
    expect(screen.getByTestId('signed-out-banner')).toHaveTextContent(
      'Codex is signed out. After signing in, restart this session.'
    );
  });

  it('disables the composer with a reason placeholder while signed out', () => {
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json' });
    const textarea = screen.getByTestId('chat-input');
    expect(textarea).toBeDisabled();
    expect(textarea).toHaveAttribute('placeholder', 'Signed out — sign in to continue');
  });

  it('renders no banner and leaves the composer alone when clear', () => {
    renderChatView({ signedOut: false, signedOutProtocol: '' });
    expect(screen.queryByTestId('signed-out-banner')).not.toBeInTheDocument();
  });

  it('navigates via onReauth when the sign-in button is clicked', async () => {
    const onReauth = vi.fn();
    renderChatView({ signedOut: true, signedOutProtocol: 'claude-stream-json', onReauth });
    await userEvent.click(screen.getByRole('button', { name: /sign in/i }));
    expect(onReauth).toHaveBeenCalledTimes(1);
  });
});
