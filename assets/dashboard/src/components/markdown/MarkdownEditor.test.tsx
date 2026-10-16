import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import MarkdownEditor from './MarkdownEditor';

// jsdom v28 does not implement Range.getBoundingClientRect / getClientRects
// well enough for CodeMirror 5. Patch whichever methods are missing.
const ensureRange = () => {
  if (typeof document.createRange !== 'function') return;
  const orig = document.createRange.bind(document);
  document.createRange = () => {
    const range = orig();
    if (typeof range.getBoundingClientRect !== 'function') {
      range.getBoundingClientRect = () => ({
        top: 0,
        left: 0,
        right: 0,
        bottom: 0,
        width: 0,
        height: 0,
        x: 0,
        y: 0,
        toJSON: () => ({}),
      });
    }
    if (typeof range.getClientRects !== 'function') {
      range.getClientRects = () =>
        ({
          length: 0,
          item: () => null,
          [Symbol.iterator]: function* () {},
        }) as unknown as DOMRectList;
    }
    return range;
  };
};
ensureRange();

// ByteMD renders a real CodeMirror 5 instance into jsdom.
beforeEach(() => {
  localStorage.clear();
});

describe('MarkdownEditor', () => {
  it('opens in Preview and exposes the Write tab', async () => {
    render(<MarkdownEditor value="# hi" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    const preview = await screen.findByRole('tab', { name: 'Preview' });
    const write = screen.getByRole('tab', { name: 'Write' });

    await waitFor(() => expect(preview).toHaveAttribute('aria-selected', 'true'));
    expect(write).toHaveAttribute('aria-selected', 'false');
    expect(document.querySelector('.bytemd-editor')).toHaveStyle({ display: 'none' });

    fireEvent.click(write);

    await waitFor(() => expect(write).toHaveAttribute('aria-selected', 'true'));
    expect(document.querySelector('.bytemd-editor')).not.toHaveStyle({ display: 'none' });
  });

  it('toggles and remembers the tab with Cmd-backslash', async () => {
    const { unmount } = render(
      <MarkdownEditor value="# hi" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />
    );
    const preview = await screen.findByRole('tab', { name: 'Preview' });
    const write = screen.getByRole('tab', { name: 'Write' });
    await waitFor(() => expect(preview).toHaveAttribute('aria-selected', 'true'));

    fireEvent.keyDown(window, { key: '\\', metaKey: true });

    await waitFor(() => expect(write).toHaveAttribute('aria-selected', 'true'));
    expect(localStorage.getItem('schmux-markdown-active-tab-ws-1-a.md')).toBe('write');

    fireEvent.keyDown(window, { key: '\\', metaKey: true });

    await waitFor(() => expect(preview).toHaveAttribute('aria-selected', 'true'));
    expect(localStorage.getItem('schmux-markdown-active-tab-ws-1-a.md')).toBe('preview');
    unmount();

    render(<MarkdownEditor value="# hi" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    const restoredPreview = await screen.findByRole('tab', { name: 'Preview' });
    await waitFor(() => expect(restoredPreview).toHaveAttribute('aria-selected', 'true'));
  });

  it('restores a remembered Write tab and cursor', async () => {
    localStorage.setItem('schmux-markdown-active-tab-ws-1-cursor.md', 'write');
    localStorage.setItem('schmux-markdown-cursor-position-ws-1-cursor.md', '9');
    const { unmount } = render(
      <MarkdownEditor
        value="hello world"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="cursor.md"
      />
    );
    const write = await screen.findByRole('tab', { name: 'Write' });
    await waitFor(() => expect(write).toHaveAttribute('aria-selected', 'true'));
    const cm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: { getCursor(): { line: number; ch: number } };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    expect(cm.getCursor()).toMatchObject({ line: 0, ch: 9 });
    unmount();

    render(
      <MarkdownEditor
        value="hello world"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="cursor.md"
      />
    );
    const restoredWrite = await screen.findByRole('tab', { name: 'Write' });
    const restoredCm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: { getCursor(): { line: number; ch: number } };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    await waitFor(() => expect(restoredWrite).toHaveAttribute('aria-selected', 'true'));
    expect(restoredCm.getCursor()).toMatchObject({ line: 0, ch: 9 });
  });

  it('remembers and restores Preview scroll position', async () => {
    const { unmount } = render(
      <MarkdownEditor
        value="# one\n\n# two"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="scroll.md"
      />
    );
    await screen.findByRole('tab', { name: 'Preview' });
    const preview = document.querySelector('.bytemd-preview') as HTMLElement;
    preview.scrollTop = 123;
    fireEvent.scroll(preview);
    await waitFor(() =>
      expect(localStorage.getItem('schmux-markdown-scroll-position-ws-1-scroll.md')).toBe('123')
    );
    unmount();

    render(
      <MarkdownEditor
        value="# one\n\n# two"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="scroll.md"
      />
    );
    const restoredPreview = await waitFor(() => {
      const el = document.querySelector('.bytemd-preview') as HTMLElement | null;
      if (!el) throw new Error('preview not mounted yet');
      return el;
    });
    await waitFor(() => expect(restoredPreview.scrollTop).toBe(123));
  });

  it('moves the ByteMD toolbar into the file-header row', async () => {
    render(
      <div className="diff-content">
        <div className="diff-content__header">
          <span data-testid="markdown-status">Saved</span>
        </div>
        <MarkdownEditor value="# hi" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />
      </div>
    );

    const header = await waitFor(() => {
      const el = document.querySelector('.diff-content__header') as HTMLElement | null;
      if (!el?.querySelector('.bytemd-toolbar')) throw new Error('toolbar not integrated yet');
      return el;
    });
    const toolbar = header.querySelector('.bytemd-toolbar') as HTMLElement;
    expect(toolbar.parentElement).toBe(header);
    expect(header.children[1]).toBe(toolbar);
  });

  it('renders the value and reports typing through onChange', async () => {
    const onChange = vi.fn();
    render(<MarkdownEditor value="# hi" onChange={onChange} workspaceId="ws-1" filePath="a.md" />);
    const cm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getValue(): string;
          setValue(v: string): void;
          replaceRange(t: string, p: { line: number; ch: number }): void;
        };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    expect(cm.getValue()).toBe('# hi');
    cm.replaceRange('!', { line: 0, ch: 4 });
    expect(onChange).toHaveBeenLastCalledWith('# hi!');
  });

  it('gives every toolbar icon a button role, tab stop, and accessible name', async () => {
    render(<MarkdownEditor value="x" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    await waitFor(() => {
      const icons = document.querySelectorAll('.bytemd-toolbar-icon');
      if (icons.length === 0) throw new Error('toolbar not mounted yet');
    });
    for (const icon of Array.from(document.querySelectorAll('.bytemd-toolbar-icon'))) {
      expect(icon.getAttribute('role')).toBe('button');
      expect(icon.getAttribute('tabindex')).toBe('0');
      expect(icon.getAttribute('aria-label')).toBeTruthy();
    }
  });

  it('activates a toolbar icon with Enter', async () => {
    render(<MarkdownEditor value="x" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />);
    const icon = await waitFor(() => {
      const el = document.querySelector('.bytemd-toolbar-icon') as HTMLElement | null;
      if (!el) throw new Error('toolbar not mounted yet');
      return el;
    });
    const click = vi.spyOn(icon, 'click');
    fireEvent.keyDown(icon, { key: 'Enter' });
    expect(click).toHaveBeenCalled();
  });

  // A focused CodeMirror runs a cursor-blink interval (cursorBlinkRate,
  // default 530 ms). ByteMD never tears CodeMirror down, so an interval left
  // running outlives the component and fires after the test environment is
  // gone ("document is not defined").
  it('stops the cursor blink when unmounted while focused', async () => {
    const setIntervalSpy = vi.spyOn(globalThis, 'setInterval');
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval');
    try {
      const { unmount } = render(
        <MarkdownEditor
          value="x"
          onChange={() => {}}
          workspaceId="ws-1"
          filePath="a.md"
          mode="split"
        />
      );
      const cm = await waitFor(() => {
        const el = document.querySelector('.CodeMirror') as HTMLElement & {
          CodeMirror?: { focus(): void };
        };
        if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
        return el.CodeMirror;
      });
      cm.focus();
      const blinks = setIntervalSpy.mock.calls.flatMap((call, i) =>
        call[1] === 530 ? [setIntervalSpy.mock.results[i].value] : []
      );
      expect(blinks.length).toBeGreaterThan(0);

      unmount();

      expect(clearIntervalSpy).toHaveBeenCalledWith(blinks[blinks.length - 1]);
    } finally {
      setIntervalSpy.mockRestore();
      clearIntervalSpy.mockRestore();
    }
  });

  it('restores the cursor after an external value replacement', async () => {
    const { rerender } = render(
      <MarkdownEditor value="hello world" onChange={() => {}} workspaceId="ws-1" filePath="a.md" />
    );
    const cm = await waitFor(() => {
      const el = document.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          setCursor(p: { line: number; ch: number }): void;
          getCursor(): { line: number; ch: number };
        };
      };
      if (!el?.CodeMirror) throw new Error('CodeMirror not mounted yet');
      return el.CodeMirror;
    });
    cm.setCursor({ line: 0, ch: 9 });
    rerender(
      <MarkdownEditor
        value="hello brave world"
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="a.md"
      />
    );
    await waitFor(() => {
      const cur = cm.getCursor() as { line: number; ch: number };
      expect({ line: cur.line, ch: cur.ch }).toEqual({ line: 0, ch: 15 });
    });
  });

  it('does not render a script from the document', async () => {
    // mode="split" forces the preview pane to render regardless of jsdom's zero width.
    render(
      <MarkdownEditor
        value={'<script>window.__pwned = 1</script>\n\n# ok'}
        onChange={() => {}}
        workspaceId="ws-1"
        filePath="a.md"
        mode="split"
      />
    );
    await screen.findByText('ok');
    expect(document.querySelector('.bytemd-preview script')).toBeNull();
    expect((window as unknown as { __pwned?: number }).__pwned).toBeUndefined();
  });
});
