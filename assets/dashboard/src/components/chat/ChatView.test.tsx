import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ChatView from './ChatView';
import type { Conversation } from '../../lib/chat/types';

const baseProps = {
  status: 'connected' as const,
  ended: false,
  onSend: vi.fn(),
  onInterrupt: vi.fn(),
  onPermission: vi.fn(),
  onAnswer: vi.fn(),
};

function conversationWith(items: Conversation['items']): Conversation {
  return { items, phase: 'running' };
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
    expect(resume).toHaveClass('log-viewer__new-content');
    expect(resume).toHaveTextContent('Resume');
    await userEvent.click(resume);
    expect(t.scrollTop).toBe(1200 - 200);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });
});
