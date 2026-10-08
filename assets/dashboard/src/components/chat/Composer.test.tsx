import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, createRef } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import Composer from './Composer';
import type { ComposerHandle } from './Composer';
import type { ChatImage } from '../../lib/chat/types';
import { clientPerf } from '../../lib/clientPerf';
import { uploadWorkspaceAttachment } from '../../lib/api';

vi.mock('../../lib/api', () => ({
  uploadWorkspaceAttachment: vi.fn(),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

interface MockProps {
  workspaceId?: string;
  disabled: boolean;
  disabledReason?: string;
  ended: boolean;
  onSend: ReturnType<typeof vi.fn<(text: string, images: ChatImage[]) => void>>;
  initialDraft?: { text: string; images: ChatImage[] };
  onDraftChange?: (draft: { text: string; images: ChatImage[] }) => void;
  onAttachmentAvailabilityChange?: (available: boolean) => void;
  recordingSince?: number | null;
}

function renderComposer(overrides: Partial<MockProps> = {}): MockProps {
  const props: MockProps = {
    workspaceId: 'ws-1',
    disabled: false,
    ended: false,
    onSend: vi.fn<(text: string, images: ChatImage[]) => void>(),
    ...overrides,
  };
  render(<Composer {...props} />);
  return props;
}

describe('Composer', () => {
  beforeEach(() => {
    vi.mocked(uploadWorkspaceAttachment).mockReset();
    vi.mocked(uploadWorkspaceAttachment).mockResolvedValue({
      name: 'data.csv',
      path: '/workspace/.schmux/attachments/upload-1/data.csv',
    });
  });

  it('exposes Attach through its handle and reports availability while processing', async () => {
    let finish!: (file: { name: string; path: string }) => void;
    vi.mocked(uploadWorkspaceAttachment).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        })
    );
    const ref = createRef<ComposerHandle>();
    const onAttachmentAvailabilityChange = vi.fn();
    const file = new File(['data'], 'data.csv', { type: 'text/csv' });
    render(
      <Composer
        ref={ref}
        workspaceId="ws-1"
        disabled={false}
        ended={false}
        onSend={vi.fn()}
        onAttachmentAvailabilityChange={onAttachmentAvailabilityChange}
      />
    );

    expect(onAttachmentAvailabilityChange).toHaveBeenLastCalledWith(true);
    act(() => ref.current?.attachFiles([file]));
    expect(uploadWorkspaceAttachment).toHaveBeenCalledWith('ws-1', file);
    expect(onAttachmentAvailabilityChange).toHaveBeenLastCalledWith(false);

    await act(async () => finish({ name: 'data.csv', path: '/workspace/data.csv' }));
    expect(screen.getByTestId('chat-file-chip')).toHaveTextContent('data.csv');
    expect(onAttachmentAvailabilityChange).toHaveBeenLastCalledWith(true);
  });

  it('rejects attachment requests through its handle while disabled', () => {
    const ref = createRef<ComposerHandle>();
    const onAttachmentAvailabilityChange = vi.fn();
    render(
      <Composer
        ref={ref}
        workspaceId="ws-1"
        disabled
        ended={false}
        onSend={vi.fn()}
        onAttachmentAvailabilityChange={onAttachmentAvailabilityChange}
      />
    );

    expect(onAttachmentAvailabilityChange).toHaveBeenLastCalledWith(false);
    act(() => ref.current?.attachFiles([new File(['data'], 'data.csv')]));
    expect(uploadWorkspaceAttachment).not.toHaveBeenCalled();
  });

  it('attaches a non-image and sends its saved path without image data', async () => {
    const props = renderComposer();
    const file = new File(['a,b\n1,2'], 'data.csv', { type: 'text/csv' });
    await userEvent.upload(screen.getByTestId('chat-file-input'), file);
    expect(await screen.findByText('data.csv')).toBeInTheDocument();
    expect(uploadWorkspaceAttachment).toHaveBeenCalledWith('ws-1', file);
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(props.onSend).toHaveBeenCalledWith(
      'File attachments:\n/workspace/.schmux/attachments/upload-1/data.csv',
      []
    );
    expect(screen.queryByText('data.csv')).not.toBeInTheDocument();
  });

  it('removes a file from the message without sending its path', async () => {
    const props = renderComposer();
    await userEvent.upload(
      screen.getByTestId('chat-file-input'),
      new File(['data'], 'data.csv', { type: 'text/csv' })
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Remove data.csv' }));
    await userEvent.type(screen.getByTestId('chat-input'), 'hello{Enter}');
    expect(props.onSend).toHaveBeenCalledWith('hello', []);
  });

  it('keeps the draft and reports an upload failure', async () => {
    vi.mocked(uploadWorkspaceAttachment).mockRejectedValue(new Error('File exceeds 50 MiB'));
    renderComposer({ initialDraft: { text: 'inspect this', images: [] } });
    await userEvent.upload(
      screen.getByTestId('chat-file-input'),
      new File(['data'], 'data.csv', { type: 'text/csv' })
    );
    expect(await screen.findByRole('alert')).toHaveTextContent('File exceeds 50 MiB');
    expect(screen.getByTestId('chat-input')).toHaveValue('inspect this');
    expect(screen.queryByText('data.csv')).not.toBeInTheDocument();
  });

  it('waits for the upload before allowing the message to be sent', async () => {
    let finish!: (file: { name: string; path: string }) => void;
    vi.mocked(uploadWorkspaceAttachment).mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        })
    );
    const props = renderComposer({ initialDraft: { text: 'inspect', images: [] } });
    await userEvent.upload(
      screen.getByTestId('chat-file-input'),
      new File(['data'], 'data.csv', { type: 'text/csv' })
    );
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled();
    await userEvent.type(screen.getByTestId('chat-input'), '{Enter}');
    expect(props.onSend).not.toHaveBeenCalled();
    await act(async () => finish({ name: 'data.csv', path: '/workspace/data.csv' }));
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled();
  });

  it('does not focus itself when it becomes enabled', () => {
    const onSend = vi.fn<(text: string, images: ChatImage[]) => void>();
    const { rerender } = render(<Composer disabled ended={false} onSend={onSend} />);
    const ta = screen.getByTestId('chat-input');
    rerender(<Composer disabled={false} ended={false} onSend={onSend} />);
    expect(document.activeElement).not.toBe(ta);
  });

  it('reports the caret on focus and select', () => {
    const onCaretChange = vi.fn();
    renderComposer({
      initialDraft: { text: 'hello world', images: [] },
      onCaretChange,
    } as Partial<MockProps>);
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    fireEvent.focus(ta);
    expect(onCaretChange).toHaveBeenLastCalledWith(0);
    ta.selectionStart = ta.selectionEnd = 5;
    fireEvent.select(ta);
    expect(onCaretChange).toHaveBeenLastCalledWith(5);
  });

  it('focus(position) sets the caret, clamped to the text', () => {
    const ref = createRef<ComposerHandle>();
    render(
      <Composer
        ref={ref}
        disabled={false}
        ended={false}
        onSend={vi.fn()}
        initialDraft={{ text: 'abc', images: [] }}
      />
    );
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    act(() => ref.current?.focus(2));
    expect(document.activeElement).toBe(ta);
    expect(ta.selectionStart).toBe(2);
    act(() => ref.current?.focus(99));
    expect(ta.selectionStart).toBe(3);
    act(() => ref.current?.focus());
    expect(ta.selectionStart).toBe(3);
  });

  it('starts from the restored draft and reports every change; sending empties it', async () => {
    const onDraftChange = vi.fn();
    const props = renderComposer({
      initialDraft: { text: 'half typed', images: [{ media_type: 'image/png', data: 'AA==' }] },
      onDraftChange,
    } as Partial<MockProps>);
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    expect(ta.value).toBe('half typed');
    expect(screen.getAllByTestId('chat-image-chip')).toHaveLength(1);
    expect(onDraftChange).not.toHaveBeenCalled(); // restoring is not a change

    await userEvent.type(ta, ' more');
    expect(onDraftChange).toHaveBeenLastCalledWith({
      text: 'half typed more',
      images: [{ media_type: 'image/png', data: 'AA==' }],
    });

    await userEvent.keyboard('{Enter}');
    expect(props.onSend).toHaveBeenCalled();
    expect(onDraftChange).toHaveBeenLastCalledWith({ text: '', images: [] });
  });

  it('grows with its content', async () => {
    renderComposer();
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    // jsdom has no layout; stand in for the browser's measurement.
    let measured = 20;
    Object.defineProperty(ta, 'scrollHeight', { get: () => measured, configurable: true });
    measured = 96;
    await userEvent.type(ta, 'one{Shift>}{Enter}{/Shift}two{Shift>}{Enter}{/Shift}three');
    expect(ta.style.height).toBe('96px');
    expect(ta.rows).toBe(1);
  });

  it('Enter sends, Shift+Enter inserts a newline, focus stays', async () => {
    const props = renderComposer();
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    ta.focus();
    await userEvent.type(ta, 'line one{Shift>}{Enter}{/Shift}line two');
    expect(ta.value).toBe('line one\nline two');
    await userEvent.keyboard('{Enter}');
    expect(props.onSend).toHaveBeenCalledWith('line one\nline two', []);
    expect(ta.value).toBe('');
    expect(document.activeElement).toBe(ta);
  });

  it('does not send an empty message', async () => {
    const props = renderComposer();
    const ta = screen.getByTestId('chat-input');
    await userEvent.type(ta, '   {Enter}');
    expect(props.onSend).not.toHaveBeenCalled();
  });

  it('is disabled with the reason as placeholder', () => {
    renderComposer({ disabled: true, disabledReason: 'Session is not running' });
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    expect(ta).toBeDisabled();
    expect(ta.placeholder).toBe('Session is not running');
  });

  it('placeholder says session ended when ended is true', () => {
    renderComposer({ ended: true });
    const ta = screen.getByTestId('chat-input') as HTMLTextAreaElement;
    expect(ta.placeholder).toContain('Session ended');
  });

  it('pastes an image and sends it with the message', async () => {
    const props = renderComposer();
    const ta = screen.getByTestId('chat-input');
    const file = new File([new Uint8Array([1, 2, 3])], 'pasted.png', { type: 'image/png' });
    fireEvent.paste(ta, { clipboardData: { files: [file] } });
    expect(await screen.findByTestId('chat-image-chip')).toBeInTheDocument();

    await userEvent.type(ta, 'look at this');
    await userEvent.keyboard('{Enter}');
    expect(props.onSend).toHaveBeenCalledTimes(1);
    const [text, images] = props.onSend.mock.calls[0];
    expect(text).toBe('look at this');
    expect(images).toHaveLength(1);
    expect(images[0].media_type).toBe('image/png');
    expect(images[0].data.length).toBeGreaterThan(0);
  });

  it('removes an attached image chip', async () => {
    renderComposer();
    const ta = screen.getByTestId('chat-input');
    const file = new File([new Uint8Array([1])], 'a.png', { type: 'image/png' });
    fireEvent.paste(ta, { clipboardData: { files: [file] } });
    const chip = await screen.findByTestId('chat-image-chip');
    await userEvent.click(chip.querySelector('button')!);
    expect(screen.queryByTestId('chat-image-chip')).not.toBeInTheDocument();
  });
});

describe('recording attach checkbox', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.mocked(uploadWorkspaceAttachment).mockReset();
    vi.mocked(uploadWorkspaceAttachment).mockResolvedValue({
      name: 'client-perf-1.json',
      path: '/ws/.schmux/attachments/u1/client-perf-1.json',
    });
  });

  it('is hidden without recordingSince', () => {
    renderComposer();
    expect(screen.queryByLabelText(/Recording since/)).toBeNull();
  });

  it('uploads the recording, appends its path, sends, and marks sent', async () => {
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const buildFile = vi
      .spyOn(clientPerf, 'buildFile')
      .mockReturnValue({ version: 1, timeline: [] } as never);
    const props = renderComposer({ recordingSince: Date.UTC(2026, 9, 8, 10, 42) });
    expect(screen.getByLabelText(/Recording since \d\d:\d\d · attach/)).toBeChecked();
    await userEvent.type(screen.getByTestId('chat-input'), 'typing lags');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(uploadWorkspaceAttachment).toHaveBeenCalledWith(
      'ws-1',
      expect.objectContaining({ name: expect.stringMatching(/^client-perf-.*\.json$/) })
    );
    expect(props.onSend).toHaveBeenCalledWith(
      'typing lags\n\nFile attachments:\n/ws/.schmux/attachments/u1/client-perf-1.json',
      []
    );
    expect(markSent).toHaveBeenCalled();
    markSent.mockRestore();
    buildFile.mockRestore();
  });

  it('keeps the message and the recording when the upload fails', async () => {
    vi.mocked(uploadWorkspaceAttachment).mockRejectedValue(new Error('upload failed'));
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const props = renderComposer({ recordingSince: 1 });
    await userEvent.type(screen.getByTestId('chat-input'), 'typing lags');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(props.onSend).not.toHaveBeenCalled();
    expect(markSent).not.toHaveBeenCalled();
    expect(screen.getByTestId('chat-input')).toHaveValue('typing lags');
    expect(await screen.findByText('upload failed')).toBeInTheDocument();
    markSent.mockRestore();
  });

  it('keeps the uploaded path as a chip when send throws, and does not upload twice', async () => {
    vi.mocked(uploadWorkspaceAttachment).mockResolvedValue({
      name: 'client-perf-1.json',
      path: '/p/client-perf-1.json',
    });
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const props = renderComposer({ recordingSince: 1 });
    props.onSend.mockImplementationOnce(() => {
      throw new Error('socket closed');
    });
    await userEvent.type(screen.getByTestId('chat-input'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(markSent).not.toHaveBeenCalled();
    expect(screen.getByText('client-perf-1.json')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Send' }));
    expect(uploadWorkspaceAttachment).toHaveBeenCalledTimes(1);
    expect(props.onSend).toHaveBeenLastCalledWith(
      'x\n\nFile attachments:\n/p/client-perf-1.json',
      []
    );
    expect(markSent).toHaveBeenCalledTimes(1);
    markSent.mockRestore();
  });

  it('ignores a second send while the recording upload is in flight', async () => {
    let resolveUpload: (v: { name: string; path: string }) => void = () => {};
    vi.mocked(uploadWorkspaceAttachment).mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveUpload = resolve;
        })
    );
    const markSent = vi.spyOn(clientPerf, 'markSent');
    const props = renderComposer({ recordingSince: 1 });
    await userEvent.type(screen.getByTestId('chat-input'), 'x');
    const send = screen.getByRole('button', { name: 'Send' });
    await userEvent.click(send);
    await userEvent.click(send);
    expect(uploadWorkspaceAttachment).toHaveBeenCalledTimes(1);
    await act(async () => {
      resolveUpload({ name: 'client-perf-1.json', path: '/p/client-perf-1.json' });
    });
    expect(props.onSend).toHaveBeenCalledTimes(1);
    expect(markSent).toHaveBeenCalledTimes(1);
    markSent.mockRestore();
  });
});
