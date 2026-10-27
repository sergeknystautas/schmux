import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, createRef, type ReactElement } from 'react';
import { render as rtlRender, screen, fireEvent, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ChatView from './ChatView';
import ModalProvider from '../ModalProvider';
import type { TranscriptHandle } from './ChatTranscript';

vi.mock('../ToastProvider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));
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

function fileTransfer(files: File[] = []) {
  return { types: ['Files'], files, dropEffect: 'none' };
}

function render(ui: ReactElement) {
  const view = rtlRender(<ModalProvider>{ui}</ModalProvider>);
  return {
    ...view,
    rerender: (next: ReactElement) => view.rerender(<ModalProvider>{next}</ModalProvider>),
  };
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
  it('renders the user message and assistant prose', async () => {
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
    // The result line renders once the tool block is expanded.
    await userEvent.click(screen.getByTestId('chat-tool-row'));
    expect(screen.getByTestId('chat-tool-result')).toHaveTextContent('file.txt');
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

  it('keeps one pane-wide drop target while crossing transcript content and the composer', () => {
    render(
      <ChatView
        {...baseProps}
        conversation={conversationWith([
          { kind: 'user', id: 'u1', text: 'existing message', images: [], queued: false },
          {
            kind: 'assistant',
            end: { state: 'done' },
            interrupted: false,
            thinking: false,
            segments: [
              {
                kind: 'tool',
                id: 'tool-1',
                name: 'Read',
                input: { file_path: '/workspace/file.txt' },
                inputJson: '{"file_path":"/workspace/file.txt"}',
                result: 'contents',
                state: 'done',
                subtools: [],
              },
            ],
          },
        ])}
      />
    );
    const transfer = fileTransfer();
    const message = screen.getByTestId('chat-user-message');
    const tool = screen.getByTestId('chat-tool-row');
    const composer = screen.getByTestId('chat-composer');

    fireEvent.dragEnter(message, { dataTransfer: transfer });
    expect(screen.getByTestId('chat-file-drop-overlay')).toBeVisible();
    fireEvent.dragEnter(tool, { dataTransfer: transfer });
    fireEvent.dragLeave(message, { dataTransfer: transfer });
    expect(screen.getByTestId('chat-file-drop-overlay')).toBeVisible();
    fireEvent.dragEnter(composer, { dataTransfer: transfer });
    fireEvent.dragLeave(tool, { dataTransfer: transfer });
    expect(screen.getByTestId('chat-file-drop-overlay')).toBeVisible();
    fireEvent.dragLeave(composer, { dataTransfer: transfer });
    expect(screen.queryByTestId('chat-file-drop-overlay')).not.toBeInTheDocument();
  });

  it('drops files over transcript content through the Composer attachment flow', async () => {
    render(<ChatView {...baseProps} conversation={conversationWith([])} />);
    const image = new File([new Uint8Array([1, 2, 3])], 'image.png', { type: 'image/png' });
    const transfer = fileTransfer([image]);
    const transcript = screen.getByTestId('chat-transcript');

    expect(fireEvent.dragOver(transcript, { dataTransfer: transfer })).toBe(false);
    expect(transfer.dropEffect).toBe('copy');
    expect(fireEvent.drop(transcript, { dataTransfer: transfer })).toBe(false);
    expect(await screen.findByTestId('chat-image-chip')).toBeInTheDocument();
  });

  it('updates an active drag when Attach becomes unavailable', () => {
    const { rerender } = render(
      <ChatView {...baseProps} conversation={conversationWith([])} status="connected" />
    );
    const view = screen.getByTestId('chat-view');
    const transfer = fileTransfer([new File(['data'], 'data.csv')]);
    fireEvent.dragEnter(view, { dataTransfer: transfer });
    expect(screen.getByTestId('chat-file-drop-overlay')).toBeVisible();

    rerender(<ChatView {...baseProps} conversation={conversationWith([])} status="disconnected" />);
    expect(screen.queryByTestId('chat-file-drop-overlay')).not.toBeInTheDocument();
    expect(fireEvent.dragOver(view, { dataTransfer: transfer })).toBe(false);
    expect(transfer.dropEffect).toBe('none');
    expect(fireEvent.drop(view, { dataTransfer: transfer })).toBe(false);
    expect(screen.queryByTestId('chat-file-chip')).not.toBeInTheDocument();
  });

  it('leaves text drops native', () => {
    render(<ChatView {...baseProps} conversation={conversationWith([])} />);
    const view = screen.getByTestId('chat-view');
    const transfer = { types: ['text/plain'], files: [], dropEffect: 'none' };
    expect(fireEvent.dragOver(view, { dataTransfer: transfer })).toBe(true);
    expect(fireEvent.drop(view, { dataTransfer: transfer })).toBe(true);
    expect(screen.queryByTestId('chat-file-drop-overlay')).not.toBeInTheDocument();
  });

  it('Escape cancels file-drag feedback without interrupting the running turn', () => {
    const onInterrupt = vi.fn();
    render(
      <ChatView {...baseProps} onInterrupt={onInterrupt} conversation={conversationWith([])} />
    );
    const view = screen.getByTestId('chat-view');
    fireEvent.dragEnter(view, { dataTransfer: fileTransfer() });
    fireEvent.keyDown(view, { key: 'Escape' });
    expect(screen.queryByTestId('chat-file-drop-overlay')).not.toBeInTheDocument();
    expect(onInterrupt).not.toHaveBeenCalled();
    fireEvent.keyDown(view, { key: 'Escape' });
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

  it('question tool row shows the question text and answer bubble, not JSON', () => {
    const conversation = conversationWith([
      { kind: 'user', id: 'u1', text: 'ask', images: [], queued: false },
      {
        kind: 'assistant',
        end: { state: 'done' },
        interrupted: false,
        thinking: false,
        segments: [
          {
            kind: 'tool',
            id: 'tu-2',
            name: 'AskUserQuestion',
            input: {
              questions: [{ question: 'Which one?', header: 'Pick', options: [{ label: 'A' }] }],
            },
            inputJson:
              '{"questions":[{"question":"Which one?","header":"Pick","options":[{"label":"A"}]}]}',
            result: '{"questions":[{"question":"Which one?"}],"answers":{"Which one?":"A"}}',
            state: 'done',
            subtools: [],
          },
          {
            kind: 'answered',
            requestId: 'req-2',
            toolUseId: 'tu-2',
            toolName: 'AskUserQuestion',
            input: {},
            questions: [
              {
                id: 'Which one?',
                question: 'Which one?',
                options: [{ label: 'A' }],
                multiSelect: false,
              },
            ],
            answers: { 'Which one?': 'A' },
          },
        ],
      },
    ]);
    render(<ChatView {...baseProps} conversation={conversation} />);
    expect(screen.getByTestId('chat-tool-summary')).toHaveTextContent('Which one?');
    expect(screen.queryByTestId('chat-tool-result')).not.toBeInTheDocument();
    expect(screen.getByTestId('chat-question-answer')).toHaveTextContent('A');
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

  describe('scroll position restore', () => {
    let resizeCallback: (() => void) | null = null;
    beforeEach(() => {
      resizeCallback = null;
      vi.stubGlobal(
        'ResizeObserver',
        class {
          constructor(cb: () => void) {
            resizeCallback = cb;
          }
          observe() {}
          disconnect() {}
        }
      );
    });
    afterEach(() => vi.unstubAllGlobals());

    const history = conversationWith([
      { kind: 'user', id: 'u1', text: 'one', images: [], queued: false },
      {
        kind: 'assistant',
        end: { state: 'done' },
        interrupted: false,
        thinking: false,
        segments: [],
      },
      { kind: 'user', id: 'u2', text: 'two', images: [], queued: false },
    ]);

    function geometry(el: HTMLElement, scrollHeight: number) {
      Object.defineProperty(el, 'scrollHeight', { value: scrollHeight, configurable: true });
      Object.defineProperty(el, 'clientHeight', { value: 200, configurable: true });
      if (!Object.getOwnPropertyDescriptor(el, 'scrollTop')) {
        Object.defineProperty(el, 'scrollTop', { value: 0, writable: true, configurable: true });
      }
    }

    it('restores a position record when history loads and stays detached', async () => {
      const initialScroll = { mode: 'position' as const, scrollTop: 300 };
      const { rerender } = render(
        <ChatView
          {...baseProps}
          historyLoaded={false}
          conversation={conversationWith([])}
          initialScroll={initialScroll}
        />
      );
      const t = screen.getByTestId('chat-transcript');
      geometry(t, 0);

      // History frame lands: conversation and historyLoaded commit together.
      geometry(t, 1000);
      rerender(
        <ChatView
          {...baseProps}
          historyLoaded={true}
          conversation={history}
          initialScroll={initialScroll}
        />
      );
      expect(t.scrollTop).toBe(300);
      // The browser fires a scroll event for the programmatic scrollTop change.
      fireEvent.scroll(t, { target: { scrollTop: 300 } });
      // The conversation effect's animation-frame pin runs now.
      await new Promise((r) => requestAnimationFrame(r));
      expect(t.scrollTop).toBe(300);
      expect(screen.getByTestId('chat-resume')).toBeInTheDocument();

      // Content resizes after render: the observer fires.
      geometry(t, 1300);
      resizeCallback?.();
      expect(t.scrollTop).toBe(300);

      // New content streams in while detached: still respected.
      rerender(
        <ChatView
          {...baseProps}
          historyLoaded={true}
          conversation={conversationWith([
            ...history.items,
            { kind: 'user', id: 'u3', text: 'three', images: [], queued: false },
          ])}
          initialScroll={initialScroll}
        />
      );
      await new Promise((r) => requestAnimationFrame(r));
      expect(t.scrollTop).toBe(300);
    });

    it('pins to the bottom on history load with a bottom record or no record', async () => {
      for (const initialScroll of [{ mode: 'bottom' as const }, null]) {
        const { rerender, unmount } = render(
          <ChatView
            {...baseProps}
            historyLoaded={false}
            conversation={conversationWith([])}
            initialScroll={initialScroll}
          />
        );
        const t = screen.getByTestId('chat-transcript');
        geometry(t, 1000);
        rerender(
          <ChatView
            {...baseProps}
            historyLoaded={true}
            conversation={history}
            initialScroll={initialScroll}
          />
        );
        await new Promise((r) => requestAnimationFrame(r));
        expect(t.scrollTop).toBe(800);
        expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
        unmount();
      }
    });

    it('does not restore again when history reloads after a reconnect', async () => {
      const initialScroll = { mode: 'position' as const, scrollTop: 300 };
      const { rerender } = render(
        <ChatView
          {...baseProps}
          historyLoaded={true}
          conversation={history}
          initialScroll={initialScroll}
        />
      );
      const t = screen.getByTestId('chat-transcript');
      // The user scrolls somewhere else after the restore.
      geometry(t, 1000);
      fireEvent.scroll(t, { target: { scrollTop: 500 } });
      // Reconnect: historyLoaded flips false, then true with the same history.
      rerender(
        <ChatView
          {...baseProps}
          historyLoaded={false}
          conversation={history}
          initialScroll={initialScroll}
        />
      );
      rerender(
        <ChatView
          {...baseProps}
          historyLoaded={true}
          conversation={history}
          initialScroll={initialScroll}
        />
      );
      await new Promise((r) => requestAnimationFrame(r));
      expect(t.scrollTop).toBe(500);
    });

    it('reports position records while detached and bottom when following', async () => {
      const onScrollChange = vi.fn();
      render(<ChatView {...baseProps} conversation={history} onScrollChange={onScrollChange} />);
      const t = screen.getByTestId('chat-transcript');
      geometry(t, 1000);
      // Wait for the conversation-effect pin to settle so the first scroll is a
      // real user scroll, not a resize-induced scroll handled by the early return.
      await new Promise((r) => requestAnimationFrame(r));

      fireEvent.scroll(t, { target: { scrollTop: 120 } });
      expect(onScrollChange).toHaveBeenLastCalledWith({ mode: 'position', scrollTop: 120 });

      fireEvent.scroll(t, { target: { scrollTop: 800 } });
      expect(onScrollChange).toHaveBeenLastCalledWith({ mode: 'bottom' });
    });
  });
});

describe('message navigation shortcuts', () => {
  const done = {
    kind: 'assistant' as const,
    end: { state: 'done' as const },
    interrupted: false,
    thinking: false,
    segments: [],
  };
  const userTurns = conversationWith([
    { kind: 'user', id: 'u1', text: 'one', images: [], queued: false },
    done,
    { kind: 'user', id: 'u2', text: 'two', images: [], queued: false },
    done,
    { kind: 'user', id: 'u3', text: 'three', images: [], queued: false },
    done,
  ]);

  // The shortcut is handled on window, so the press can start anywhere.
  // fireEvent returns false when the default was prevented.
  const press = (key: string, init: KeyboardEventInit = { ctrlKey: true }) =>
    fireEvent.keyDown(document.activeElement ?? document.body, { key, ...init });

  // jsdom has no layout. Give the transcript a 200px viewport over 1200px of
  // content (scrollTop 0–1000, clamped like a browser) and place each user
  // message row at a content offset, so getBoundingClientRect reports its top
  // relative to the viewport top. Starts at the bottom, following the tail.
  async function renderLaidOut(
    rowTops: Record<string, number> = { u1: 0, u2: 400, u3: 800 },
    conversation: Conversation = userTurns
  ) {
    const onScrollChange = vi.fn();
    const view = render(
      <ChatView {...baseProps} conversation={conversation} onScrollChange={onScrollChange} />
    );
    const t = screen.getByTestId('chat-transcript');
    let scrollTop = 0;
    Object.defineProperty(t, 'scrollHeight', { value: 1200, configurable: true });
    Object.defineProperty(t, 'clientHeight', { value: 200, configurable: true });
    Object.defineProperty(t, 'scrollTop', {
      configurable: true,
      get: () => scrollTop,
      set: (v: number) => {
        scrollTop = Math.min(Math.max(v, 0), 1000);
      },
    });
    t.getBoundingClientRect = () => ({ top: 0 }) as DOMRect;
    for (const [id, top] of Object.entries(rowTops)) {
      const row = view.container.querySelector<HTMLElement>(`[data-user-message-id="${id}"]`)!;
      row.getBoundingClientRect = () => ({ top: top - scrollTop }) as DOMRect;
    }
    await act(() => new Promise((r) => requestAnimationFrame(r)));
    fireEvent.scroll(t);
    expect(t.scrollTop).toBe(1000);
    const scrollTo = (top: number) => fireEvent.scroll(t, { target: { scrollTop: top } });
    return { ...view, t, onScrollChange, scrollTo };
  }

  it('[ from the bottom scrolls to the most recent message and leaves the composer alone', async () => {
    const { t } = await renderLaidOut();
    const input = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'hello' } });
    input.focus();
    input.setSelectionRange(2, 2);
    expect(press('[')).toBe(false);
    expect(t.scrollTop).toBe(800);
    expect(input).toHaveFocus();
    expect(input.selectionStart).toBe(2);
    expect(screen.getByTestId('chat-resume')).toBeInTheDocument();
    expect(press(']')).toBe(false);
    expect(input).toHaveFocus();
    expect(input.selectionStart).toBe(2);
  });

  it('uses the Command key on macOS', async () => {
    const { t } = await renderLaidOut();
    expect(press('[', { metaKey: true })).toBe(false);
    expect(t.scrollTop).toBe(800);
  });

  it('[ steps older one message at a time and stops at the oldest', async () => {
    const { t } = await renderLaidOut();
    press('[');
    expect(t.scrollTop).toBe(800);
    press('[');
    expect(t.scrollTop).toBe(400);
    press('[');
    expect(t.scrollTop).toBe(0);
    expect(press('[')).toBe(false);
    expect(t.scrollTop).toBe(0);
  });

  it('] steps newer, and past the most recent message resumes at the bottom', async () => {
    const { t, scrollTo } = await renderLaidOut();
    scrollTo(0);
    expect(screen.getByTestId('chat-resume')).toBeInTheDocument();
    press(']');
    expect(t.scrollTop).toBe(400);
    press(']');
    expect(t.scrollTop).toBe(800);
    expect(press(']')).toBe(false);
    expect(t.scrollTop).toBe(1000);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });

  it('measures from where the user scrolled, not from the last step', async () => {
    const { t, scrollTo } = await renderLaidOut();
    press('[');
    expect(t.scrollTop).toBe(800);
    scrollTo(500);
    press('[');
    expect(t.scrollTop).toBe(400);
    scrollTo(500);
    press(']');
    expect(t.scrollTop).toBe(800);
  });

  it('[ skips a most recent message that is already in view below the top', async () => {
    const { t } = await renderLaidOut({ u1: 0, u2: 400, u3: 1100 });
    press('[');
    expect(t.scrollTop).toBe(400);
  });

  it('a step that is clamped at the bottom resumes following the tail', async () => {
    const { t, scrollTo } = await renderLaidOut({ u1: 0, u2: 400, u3: 1100 });
    scrollTo(500);
    expect(screen.getByTestId('chat-resume')).toBeInTheDocument();
    press(']');
    expect(t.scrollTop).toBe(1000);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });

  it('] at the bottom is claimed and changes nothing', async () => {
    const { t } = await renderLaidOut();
    expect(press(']')).toBe(false);
    expect(t.scrollTop).toBe(1000);
    expect(screen.queryByTestId('chat-resume')).not.toBeInTheDocument();
  });

  it('with no user messages, [ is claimed and changes nothing', async () => {
    const { t } = await renderLaidOut({}, conversationWith([done]));
    expect(press('[')).toBe(false);
    expect(t.scrollTop).toBe(1000);
  });

  it('a step reports the new scroll position for saving', async () => {
    const { t, onScrollChange } = await renderLaidOut();
    press('[');
    // The browser fires scroll after a scrollTop change; jsdom does not.
    fireEvent.scroll(t);
    expect(onScrollChange).toHaveBeenLastCalledWith({ mode: 'position', scrollTop: 800 });
  });

  it('new output after a step does not snap back to the bottom', async () => {
    const { t, rerender } = await renderLaidOut();
    press('[');
    expect(t.scrollTop).toBe(800);
    rerender(
      <ChatView {...baseProps} conversation={conversationWith([...userTurns.items, done])} />
    );
    await act(() => new Promise((r) => requestAnimationFrame(r)));
    expect(t.scrollTop).toBe(800);
  });

  it('while a modal is open, the key is claimed but nothing scrolls', async () => {
    const modal = document.createElement('div');
    modal.setAttribute('aria-modal', 'true');
    document.body.appendChild(modal);
    try {
      const { t } = await renderLaidOut();
      expect(press('[')).toBe(false);
      expect(t.scrollTop).toBe(1000);
    } finally {
      modal.remove();
    }
  });

  it('ignores Shift and Alt variants and brackets without a modifier', async () => {
    const { t } = await renderLaidOut();
    expect(press('[', { ctrlKey: true, shiftKey: true })).toBe(true);
    expect(press('[', { ctrlKey: true, altKey: true })).toBe(true);
    expect(press('[', {})).toBe(true);
    expect(t.scrollTop).toBe(1000);
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
    // Re-clicking the selected single-select option clears it.
    expect(onAnswerChange).toHaveBeenCalledWith('r1', 'Pick?', { selected: [], other: 'note' });
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

describe('session ended notice', () => {
  it('shows a prominent alert above the composer when the process is gone', () => {
    render(<ChatView {...baseProps} ended={true} conversation={conversationWith([])} />);
    const banner = screen.getByTestId('chat-ended-banner');
    expect(banner).toHaveAttribute('role', 'alert');
    expect(banner).toHaveTextContent('Session ended');
  });

  it('renders no ended banner while the session is alive', () => {
    render(<ChatView {...baseProps} ended={false} conversation={conversationWith([])} />);
    expect(screen.queryByTestId('chat-ended-banner')).not.toBeInTheDocument();
  });
});
