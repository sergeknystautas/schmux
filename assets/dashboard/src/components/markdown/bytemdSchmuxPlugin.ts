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

type MarkdownTab = 'write' | 'preview';

// Scheme-prefixed and protocol-relative URLs are left alone. A single leading
// slash is workspace-root, the same convention the read-only viewer's
// resolveRelativePath uses.
const ABSOLUTE = /^([a-z][a-z0-9+.-]*:|\/\/)/i;

function stateKey(kind: 'activeTab' | 'cursorPosition', workspaceId: string, filePath: string) {
  return `schmux-markdown-${kind}-${workspaceId}-${filePath}`;
}

function readActiveTab(workspaceId: string, filePath: string): MarkdownTab {
  return localStorage.getItem(stateKey('activeTab', workspaceId, filePath)) === 'write'
    ? 'write'
    : 'preview';
}

function writeActiveTab(tab: MarkdownTab, workspaceId: string, filePath: string) {
  localStorage.setItem(stateKey('activeTab', workspaceId, filePath), tab);
}

function readCursorOffset(workspaceId: string, filePath: string) {
  const value = Number(localStorage.getItem(stateKey('cursorPosition', workspaceId, filePath)));
  return Number.isFinite(value) && value >= 0 ? value : 0;
}

function writeCursorOffset(offset: number, workspaceId: string, filePath: string) {
  localStorage.setItem(stateKey('cursorPosition', workspaceId, filePath), String(offset));
}

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
  if (oldText.length === 0) return Math.min(offset, newText.length);
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
  const toolbar = root.querySelector<HTMLElement>('.bytemd-toolbar');
  const apply = () => {
    root.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab').forEach((tab) => {
      const label = tab.textContent?.trim();
      if (!label) return;
      tab.setAttribute('role', 'tab');
      tab.setAttribute('tabindex', '0');
      tab.setAttribute('aria-label', label);
      tab.setAttribute(
        'aria-selected',
        tab.classList.contains('bytemd-toolbar-tab-active').toString()
      );
    });
    root.querySelectorAll<HTMLElement>('.bytemd-toolbar-icon').forEach((icon) => {
      icon.setAttribute('role', 'button');
      icon.setAttribute('tabindex', '0');
      icon.setAttribute('aria-label', labelFor(icon));
    });
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const target = e.target as HTMLElement | null;
    if (
      !target?.classList.contains('bytemd-toolbar-icon') &&
      !target?.classList.contains('bytemd-toolbar-tab')
    ) {
      return;
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      target.click();
    }
  };
  apply();
  const observer = new MutationObserver(apply);
  const observerOptions: MutationObserverInit = {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ['class'],
  };
  observer.observe(root, observerOptions);
  if (toolbar && toolbar !== root) observer.observe(toolbar, observerOptions);
  root.addEventListener('keydown', onKeyDown);
  toolbar?.addEventListener('keydown', onKeyDown);
  const syncAfterClick = () => window.setTimeout(apply, 0);
  toolbar?.addEventListener('click', syncAfterClick, true);
  return () => {
    observer.disconnect();
    root.removeEventListener('keydown', onKeyDown);
    toolbar?.removeEventListener('keydown', onKeyDown);
    toolbar?.removeEventListener('click', syncAfterClick, true);
  };
}

function toolbarFor(root: HTMLElement) {
  return (
    root.querySelector<HTMLElement>('.bytemd-toolbar') ??
    root.closest('.diff-content')?.querySelector<HTMLElement>('.bytemd-toolbar') ??
    null
  );
}

function tabFor(root: HTMLElement, tab: MarkdownTab) {
  return Array.from(
    toolbarFor(root)?.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab') ?? []
  ).find((element) => element.textContent?.trim().toLowerCase() === tab);
}

function activeTabFor(root: HTMLElement): MarkdownTab | null {
  const tabs = Array.from(
    toolbarFor(root)?.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab') ?? []
  );
  const active = tabs.find((element) => element.classList.contains('bytemd-toolbar-tab-active'));
  const label = active?.textContent?.trim().toLowerCase();
  return label === 'write' || label === 'preview' ? label : null;
}

function setTabAria(root: HTMLElement, selected: MarkdownTab) {
  Array.from(toolbarFor(root)?.querySelectorAll<HTMLElement>('.bytemd-toolbar-tab') ?? []).forEach(
    (element) => {
      const label = element.textContent?.trim().toLowerCase();
      if (label === 'write' || label === 'preview') {
        element.setAttribute('aria-selected', (label === selected).toString());
      }
    }
  );
}

function selectTab(root: HTMLElement, tab: MarkdownTab) {
  tabFor(root, tab)?.click();
  setTabAria(root, tab);
}

function restorePreviewScroll(root: HTMLElement, workspaceId: string, filePath: string) {
  const key = `schmux-markdown-scroll-position-${workspaceId}-${filePath}`;
  const saved = Number(localStorage.getItem(key));
  if (!Number.isFinite(saved) || saved < 0) return;

  window.requestAnimationFrame(() => {
    const preview = root.querySelector<HTMLElement>('.bytemd-preview');
    if (preview && getComputedStyle(preview).display !== 'none') preview.scrollTop = saved;
  });
}

function scheduleSelectionSync(
  root: HTMLElement,
  tab: MarkdownTab,
  workspaceId: string,
  filePath: string
) {
  window.setTimeout(() => {
    setTabAria(root, tab);
    if (tab === 'preview') restorePreviewScroll(root, workspaceId, filePath);
  }, 0);
}

function moveToolbarIntoFileHeader(root: HTMLElement) {
  const toolbar = root.querySelector<HTMLElement>('.bytemd-toolbar');
  const header = root.closest('.diff-content')?.querySelector<HTMLElement>('.diff-content__header');
  if (!toolbar || !header || toolbar.parentElement === header) return;

  header.appendChild(toolbar);
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
        const text = cm.getValue();
        saved = {
          offset:
            text.length === 0
              ? readCursorOffset(workspaceId, filePath)
              : cm.indexFromPos(cm.getCursor()),
          scroll: { left: info.left, top: info.top },
          text,
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
      const savedCursor = Math.min(readCursorOffset(workspaceId, filePath), cm.getValue().length);
      cm.setCursor(cm.posFromIndex(savedCursor));
      const onCursorActivity = () => {
        writeCursorOffset(cm.indexFromPos(cm.getCursor()), workspaceId, filePath);
      };
      cm.on('cursorActivity', onCursorActivity);
      const stopToolbar = normalizeToolbar(root);
      const initialTab = readActiveTab(workspaceId, filePath);
      selectTab(root, initialTab);
      writeActiveTab(initialTab, workspaceId, filePath);
      window.requestAnimationFrame(() => {
        if (activeTabFor(root) !== initialTab) selectTab(root, initialTab);
        setTabAria(root, initialTab);
      });
      moveToolbarIntoFileHeader(root);
      const toolbar = toolbarFor(root);
      const onToolbarClick = (event: Event) => {
        const tab = (event.target as Element | null)?.closest('.bytemd-toolbar-tab');
        const label = tab?.textContent?.trim().toLowerCase();
        if (label !== 'write' && label !== 'preview') return;

        writeActiveTab(label, workspaceId, filePath);
        scheduleSelectionSync(root, label, workspaceId, filePath);
      };
      toolbar?.addEventListener('click', onToolbarClick, true);
      return () => {
        cm.off('beforeChange', onBefore);
        cm.off('change', onChange);
        cm.off('cursorActivity', onCursorActivity);
        writeCursorOffset(cm.indexFromPos(cm.getCursor()), workspaceId, filePath);
        toolbar?.removeEventListener('click', onToolbarClick, true);
        stopToolbar();
      };
    },
    viewerEffect({ markdownBody }) {
      const preview = markdownBody.closest<HTMLElement>('.bytemd-preview');
      const root = markdownBody.closest<HTMLElement>('.bytemd');
      if (!preview || !root) return;

      const key = `schmux-markdown-scroll-position-${workspaceId}-${filePath}`;
      const saveScroll = () => {
        if (getComputedStyle(preview).display !== 'none') {
          localStorage.setItem(key, String(preview.scrollTop));
        }
      };
      preview.addEventListener('scroll', saveScroll, { passive: true });
      window.requestAnimationFrame(() => {
        const saved = Number(localStorage.getItem(key));
        if (Number.isFinite(saved) && saved >= 0 && getComputedStyle(preview).display !== 'none') {
          preview.scrollTop = saved;
        }
      });
      return () => preview.removeEventListener('scroll', saveScroll);
    },
  };
}
