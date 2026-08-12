import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import MarkdownPreviewPage from './MarkdownPreviewPage';

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

// The scroll listener is attached in a passive effect. RTL's async queries resolve
// as soon as React commits the content to the DOM, and React may flush that effect
// a macrotask later — so firing a single scroll right after `findScrollContainer`
// races the listener and intermittently writes nothing. Retry the scroll until it
// sticks; no fixed number of ticks is a reliable barrier.
async function scrollAndExpectSaved(container: HTMLDivElement, top: number, key: string) {
  Object.defineProperty(container, 'scrollTop', { value: top, writable: true });
  await waitFor(() => {
    fireEvent.scroll(container);
    expect(localStorage.getItem(key)).toBe(String(top));
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockGetFileContent.mockResolvedValue('# Hello markdown\n\nbody');
});

afterEach(() => {
  localStorage.clear();
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

describe('MarkdownPreviewPage download', () => {
  it('renders Download link pointing to the file API', async () => {
    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/README.md')}`);

    const link = await screen.findByTestId('download-markdown');
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/api/file/ws-001/docs%2FREADME.md');
    expect(link).toHaveAttribute('download', 'README.md');
  });
});

describe('MarkdownPreviewPage scroll memory', () => {
  it('writes scrollTop to localStorage when the content scrolls', async () => {
    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    await scrollAndExpectSaved(container, 250, 'schmux-markdown-scroll-position-ws-001-README.md');
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

    await scrollAndExpectSaved(container, 100, 'schmux-markdown-scroll-position-ws-001-README.md');

    expect(localStorage.getItem('schmux-markdown-scroll-position-ws-001-OTHER.md')).toBe('999');
  });
});
