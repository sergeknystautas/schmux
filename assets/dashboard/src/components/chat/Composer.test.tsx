import { describe, it, expect, vi } from 'vitest';
import { act, createRef } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import Composer from './Composer';
import type { ComposerHandle } from './Composer';
import type { ChatImage } from '../../lib/chat/types';

interface MockProps {
  disabled: boolean;
  disabledReason?: string;
  ended: boolean;
  onSend: ReturnType<typeof vi.fn<(text: string, images: ChatImage[]) => void>>;
  initialDraft?: { text: string; images: ChatImage[] };
  onDraftChange?: (draft: { text: string; images: ChatImage[] }) => void;
}

function renderComposer(overrides: Partial<MockProps> = {}): MockProps {
  const props: MockProps = {
    disabled: false,
    ended: false,
    onSend: vi.fn<(text: string, images: ChatImage[]) => void>(),
    ...overrides,
  };
  render(<Composer {...props} />);
  return props;
}

describe('Composer', () => {
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
