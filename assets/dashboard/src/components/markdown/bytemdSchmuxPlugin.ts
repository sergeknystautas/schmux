import type { BytemdPlugin } from 'bytemd';
import type { Editor as CodeMirrorEditor, EditorChange } from 'codemirror';
import { getWorkspaceFileUrl } from '../../lib/api';

// The one ByteMD plugin schmux owns. It does three things and nothing else:
// rewrite relative image URLs (after ByteMD's sanitizer has run), keep the
// cursor in place when the value prop replaces the text, and give ByteMD's
// div-based toolbar icons keyboard and screen-reader semantics.

interface HastNode {
  type: string;
  tagName?: string;
  properties?: Record<string, unknown>;
  children?: HastNode[];
}
export type HastRoot = HastNode;

// Scheme-prefixed and protocol-relative URLs are left alone. A single leading
// slash is workspace-root, the same convention the read-only viewer's
// resolveRelativePath uses.
const ABSOLUTE = /^([a-z][a-z0-9+.-]*:|\/\/)/i;

// resolveWorkspaceRelative mirrors path.join's segments but refuses to escape
// the workspace (more `..` than the reference depth) by returning null. The
// shared `resolveRelativePath` does not enforce that.
function resolveWorkspaceRelative(src: string, filePath: string): string | null {
  if (ABSOLUTE.test(src)) return null;
  const base = filePath.split('/').slice(0, -1);
  const parts = src.startsWith('/') ? src.slice(1).split('/') : [...base, ...src.split('/')];
  const stack: string[] = [];
  for (const p of parts) {
    if (p === '..') {
      if (stack.length === 0) return null;
      stack.pop();
    } else if (p !== '.' && p !== '') {
      stack.push(p);
    }
  }
  return stack.join('/');
}

export function rewriteRelativeImages(tree: HastRoot, workspaceId: string, filePath: string): void {
  const visit = (node: HastNode) => {
    if (node.type === 'element' && node.tagName === 'img' && node.properties) {
      const src = node.properties.src;
      if (typeof src === 'string') {
        const resolved = resolveWorkspaceRelative(src, filePath);
        if (resolved !== null) node.properties.src = getWorkspaceFileUrl(workspaceId, resolved);
      }
    }
    node.children?.forEach(visit);
  };
  visit(tree);
}

// mapCursorOffset moves a character offset from oldText to newText: unchanged
// prefix keeps it, unchanged suffix shifts it by the length delta, anything
// inside the changed region clamps to the start of that region.
export function mapCursorOffset(oldText: string, newText: string, offset: number): number {
  let prefix = 0;
  const max = Math.min(oldText.length, newText.length);
  while (prefix < max && oldText[prefix] === newText[prefix]) prefix++;
  if (offset <= prefix) return Math.min(offset, newText.length);
  let suffix = 0;
  while (
    suffix < max - prefix &&
    oldText[oldText.length - 1 - suffix] === newText[newText.length - 1 - suffix]
  ) {
    suffix++;
  }
  if (offset >= oldText.length - suffix) return offset + (newText.length - oldText.length);
  return prefix;
}

function labelFor(icon: HTMLElement): string {
  const tippy = (icon as HTMLElement & { _tippy?: { props?: { content?: unknown } } })._tippy;
  const content = tippy?.props?.content;
  if (typeof content === 'string' && content.trim()) return content.trim();
  return icon.getAttribute('aria-label') || 'Toolbar action';
}

function normalizeToolbar(root: HTMLElement): () => void {
  const apply = () => {
    root.querySelectorAll<HTMLElement>('.bytemd-toolbar-icon').forEach((icon) => {
      icon.setAttribute('role', 'button');
      icon.setAttribute('tabindex', '0');
      icon.setAttribute('aria-label', labelFor(icon));
    });
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const target = e.target as HTMLElement | null;
    if (!target?.classList.contains('bytemd-toolbar-icon')) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      target.click();
    }
  };
  apply();
  const observer = new MutationObserver(apply);
  observer.observe(root, { childList: true, subtree: true });
  root.addEventListener('keydown', onKeyDown);
  return () => {
    observer.disconnect();
    root.removeEventListener('keydown', onKeyDown);
  };
}

export function schmuxPlugin(workspaceId: string, filePath: string): BytemdPlugin {
  return {
    rehype: (processor) =>
      processor.use(() => (tree: unknown) => {
        rewriteRelativeImages(tree as HastRoot, workspaceId, filePath);
      }),
    editorEffect({ editor, root }) {
      const cm = editor as CodeMirrorEditor;
      let saved: { offset: number; scroll: { left: number; top: number }; text: string } | null =
        null;
      const onBefore = (_: CodeMirrorEditor, change: EditorChange) => {
        if (change.origin !== 'setValue') return;
        const info = cm.getScrollInfo();
        saved = {
          offset: cm.indexFromPos(cm.getCursor()),
          scroll: { left: info.left, top: info.top },
          text: cm.getValue(),
        };
      };
      const onChange = (_: CodeMirrorEditor, change: EditorChange) => {
        if (change.origin !== 'setValue' || !saved) return;
        const next = mapCursorOffset(saved.text, cm.getValue(), saved.offset);
        cm.setCursor(cm.posFromIndex(next));
        cm.scrollTo(saved.scroll.left, saved.scroll.top);
        saved = null;
      };
      cm.on('beforeChange', onBefore);
      cm.on('change', onChange);
      const stopToolbar = normalizeToolbar(root);
      return () => {
        cm.off('beforeChange', onBefore);
        cm.off('change', onChange);
        stopToolbar();
      };
    },
  };
}
