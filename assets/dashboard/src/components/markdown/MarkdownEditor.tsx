import { useEffect, useMemo, useRef } from 'react';
import { Editor } from '@bytemd/react';
import gfm from '@bytemd/plugin-gfm';
import 'bytemd/dist/index.css';
import { schmuxPlugin } from './bytemdSchmuxPlugin';
import styles from '../../styles/markdownEditor.module.css';

export interface MarkdownEditorProps {
  value: string;
  onChange: (text: string) => void;
  workspaceId: string;
  filePath: string;
  // Tests can force split/auto; production always starts in tab mode.
  mode?: 'auto' | 'split' | 'tab';
}

// MarkdownEditor is the whole ByteMD surface. `value` is the reducer's draft:
// ByteMD only calls CodeMirror setValue when the prop differs from the
// editor's text, so a typed change is a no-op here and an incoming document
// is a replacement. Tab mode gives the page one predictable writing surface and
// lets the plugin select Preview on mount without fighting auto-split layout.
export default function MarkdownEditor({
  value,
  onChange,
  workspaceId,
  filePath,
  mode = 'tab',
}: MarkdownEditorProps) {
  const editorRef = useRef<HTMLDivElement>(null);
  const plugins = useMemo(
    () => [gfm(), schmuxPlugin(workspaceId, filePath)],
    [workspaceId, filePath]
  );
  useEffect(() => {
    if (mode !== 'tab') return;

    const frame = window.requestAnimationFrame(() => {
      const editor = editorRef.current;
      const scope = editor?.closest('.diff-content') ?? editor;
      const key = `schmux-markdown-active-tab-${workspaceId}-${filePath}`;
      const desired = localStorage.getItem(key) === 'write' ? 'write' : 'preview';
      const tabs = Array.from(scope?.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab') ?? []);
      const target = tabs.find((element) => element.textContent?.trim().toLowerCase() === desired);
      target?.click();
      tabs.forEach((element) => {
        const label = element.textContent?.trim().toLowerCase();
        if (label === 'write' || label === 'preview') {
          element.setAttribute('aria-selected', (label === desired).toString());
        }
      });
      const cmEl = editor?.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getValue(): string;
          indexFromPos(position: { line: number; ch: number }): number;
          posFromIndex(index: number): { line: number; ch: number };
          setCursor(position: { line: number; ch: number }): void;
        };
      };
      const cursorKey = `schmux-markdown-cursor-position-${workspaceId}-${filePath}`;
      const cursorOffset = Number(localStorage.getItem(cursorKey));
      if (cmEl?.CodeMirror && Number.isFinite(cursorOffset) && cursorOffset >= 0) {
        cmEl.CodeMirror.setCursor(
          cmEl.CodeMirror.posFromIndex(Math.min(cursorOffset, cmEl.CodeMirror.getValue().length))
        );
      }
    });
    return () => window.cancelAnimationFrame(frame);
  }, [mode, workspaceId, filePath]);

  useEffect(() => {
    const saveCursorPosition = () => {
      const cmEl = editorRef.current?.querySelector('.CodeMirror') as HTMLElement & {
        CodeMirror?: {
          getCursor(): { line: number; ch: number };
          indexFromPos(position: { line: number; ch: number }): number;
        };
      };
      if (!cmEl?.CodeMirror) return;

      const key = `schmux-markdown-cursor-position-${workspaceId}-${filePath}`;
      localStorage.setItem(key, String(cmEl.CodeMirror.indexFromPos(cmEl.CodeMirror.getCursor())));
    };

    window.addEventListener('keyup', saveCursorPosition, true);
    window.addEventListener('mouseup', saveCursorPosition, true);
    return () => {
      window.removeEventListener('keyup', saveCursorPosition, true);
      window.removeEventListener('mouseup', saveCursorPosition, true);
    };
  }, [workspaceId, filePath]);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
      if (event.key !== '\\') return;

      event.preventDefault();
      const key = `schmux-markdown-active-tab-${workspaceId}-${filePath}`;
      const next = localStorage.getItem(key) === 'write' ? 'preview' : 'write';
      const editor = editorRef.current;
      const scope = editor?.closest('.diff-content') ?? editor;
      const tabs = Array.from(scope?.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab') ?? []);
      const target = tabs.find((element) => element.textContent?.trim().toLowerCase() === next);
      target?.click();
      localStorage.setItem(key, next);
      tabs.forEach((element) => {
        const label = element.textContent?.trim().toLowerCase();
        if (label === 'write' || label === 'preview') {
          element.setAttribute('aria-selected', (label === next).toString());
        }
      });
    };

    window.addEventListener('keydown', onKeyDown, true);
    return () => window.removeEventListener('keydown', onKeyDown, true);
  }, [workspaceId, filePath]);
  return (
    <div className={styles.editor} data-testid="markdown-editor" ref={editorRef}>
      <Editor value={value} plugins={plugins} mode={mode} onChange={onChange} />
    </div>
  );
}
