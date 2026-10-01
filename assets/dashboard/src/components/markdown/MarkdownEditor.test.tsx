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

describe('MarkdownEditor', () => {
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
