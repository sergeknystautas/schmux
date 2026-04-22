import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import MarkdownPreviewPage, { resolveMarkdownRelativePath } from './MarkdownPreviewPage';

vi.mock('../lib/api', () => ({
  getFileContent: vi.fn(),
  getWorkspaceFileUrl: (workspaceId: string, filePath: string) =>
    `/api/file/${workspaceId}/${encodeURIComponent(filePath)}`,
  getErrorMessage: vi.fn((_err: unknown, fallback: string) => fallback),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [
      {
        id: 'ws-001',
        files_changed: 0,
        lines_added: 0,
        lines_removed: 0,
        sessions: [],
      },
    ],
  }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));

vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));

import { getFileContent } from '../lib/api';
const mockGetFileContent = vi.mocked(getFileContent);

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/diff/:workspaceId/md/:filepath" element={<MarkdownPreviewPage />} />
      </Routes>
    </MemoryRouter>
  );
}

// The scroll container is `.diff-viewer-wrapper` — the outer div — not the inner
// `.markdown-preview-content`. Both have `overflow: auto`, but `.markdown-preview-content`
// has `flex: 1` with no `display: flex` on its parent, so its flex sizing is inert; the
// outer wrapper is what actually scrolls. Mirrors DiffPage.tsx:487.
async function findScrollContainer(): Promise<HTMLDivElement> {
  const content = await screen.findByText(/hello markdown/i);
  const container = content.closest('.diff-viewer-wrapper') as HTMLDivElement | null;
  if (!container) throw new Error('scrollable container not found');
  return container;
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockGetFileContent.mockResolvedValue('# Hello markdown\n\nbody');
});

afterEach(() => {
  localStorage.clear();
});

describe('resolveMarkdownRelativePath', () => {
  it('resolves sibling path against the markdown file directory', () => {
    expect(resolveMarkdownRelativePath('watercolor.png', 'docs/mood/README.md')).toBe(
      'docs/mood/watercolor.png'
    );
  });

  it('resolves parent-relative paths', () => {
    expect(resolveMarkdownRelativePath('../a.png', 'docs/sub/guide.md')).toBe('docs/a.png');
  });

  it('treats leading slash as workspace-root-absolute', () => {
    expect(resolveMarkdownRelativePath('/a.png', 'docs/README.md')).toBe('a.png');
  });

  it('collapses ./ segments', () => {
    expect(resolveMarkdownRelativePath('./a.png', 'docs/README.md')).toBe('docs/a.png');
  });

  it('returns null for http URLs', () => {
    expect(resolveMarkdownRelativePath('https://example.com/a.png', 'docs/README.md')).toBeNull();
  });

  it('returns null for data URLs', () => {
    expect(resolveMarkdownRelativePath('data:image/png;base64,AAA', 'docs/README.md')).toBeNull();
  });

  it('returns null for protocol-relative URLs', () => {
    expect(resolveMarkdownRelativePath('//cdn.example.com/a.png', 'docs/README.md')).toBeNull();
  });

  it('resolves when markdown file is at the workspace root', () => {
    expect(resolveMarkdownRelativePath('a.png', 'README.md')).toBe('a.png');
  });
});

describe('MarkdownPreviewPage image rewriting', () => {
  it('rewrites relative image src to the workspace file API', async () => {
    mockGetFileContent.mockResolvedValue('![diagram](watercolor-forest-arbor-child.png)');

    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/mood/README.md')}`);

    const img = await screen.findByRole('img', { name: 'diagram' });
    expect(img).toHaveAttribute(
      'src',
      '/api/file/ws-001/' + encodeURIComponent('docs/mood/watercolor-forest-arbor-child.png')
    );
  });

  it('leaves external URLs unchanged', async () => {
    mockGetFileContent.mockResolvedValue('![x](https://example.com/a.png)');

    renderAt('/diff/ws-001/md/README.md');

    const img = await screen.findByRole('img', { name: 'x' });
    expect(img).toHaveAttribute('src', 'https://example.com/a.png');
  });

  it('rewrites workspace-absolute paths (leading slash)', async () => {
    mockGetFileContent.mockResolvedValue('![x](/a.png)');

    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/README.md')}`);

    const img = await screen.findByRole('img', { name: 'x' });
    expect(img).toHaveAttribute('src', '/api/file/ws-001/' + encodeURIComponent('a.png'));
  });
});

describe('MarkdownPreviewPage scroll memory', () => {
  it('writes scrollTop to localStorage when the content scrolls', async () => {
    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    Object.defineProperty(container, 'scrollTop', { value: 250, writable: true });
    fireEvent.scroll(container);

    expect(localStorage.getItem('schmux-markdown-scroll-position-ws-001-README.md')).toBe('250');
  });

  it('restores scrollTop from localStorage on mount', async () => {
    localStorage.setItem('schmux-markdown-scroll-position-ws-001-README.md', '420');

    const rafSpy = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => {
      cb(0);
      return 0;
    });

    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    await waitFor(() => {
      expect(container.scrollTop).toBe(420);
    });

    rafSpy.mockRestore();
  });

  it('keeps scroll positions separate per filepath', async () => {
    localStorage.setItem('schmux-markdown-scroll-position-ws-001-OTHER.md', '999');

    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    Object.defineProperty(container, 'scrollTop', { value: 100, writable: true });
    fireEvent.scroll(container);

    expect(localStorage.getItem('schmux-markdown-scroll-position-ws-001-README.md')).toBe('100');
    expect(localStorage.getItem('schmux-markdown-scroll-position-ws-001-OTHER.md')).toBe('999');
  });
});
