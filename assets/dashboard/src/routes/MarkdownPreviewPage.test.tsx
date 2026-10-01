import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import MarkdownPreviewPage from './MarkdownPreviewPage';

const toastSuccessMock = vi.fn();
const toastErrorMock = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: toastSuccessMock, error: toastErrorMock }),
}));

const originalClipboard = navigator.clipboard;
const writeTextMock = vi.fn();

beforeEach(() => {
  writeTextMock.mockReset();
  writeTextMock.mockResolvedValue(undefined);
  toastSuccessMock.mockReset();
  toastErrorMock.mockReset();
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextMock },
    writable: true,
    configurable: true,
  });
});

afterEach(() => {
  Object.defineProperty(navigator, 'clipboard', {
    value: originalClipboard,
    writable: true,
    configurable: true,
  });
});

vi.mock('../lib/api', () => ({
  getFileContent: vi.fn(),
  getWorkspaceFileUrl: (workspaceId: string, filePath: string) =>
    `/api/file/${workspaceId}/${encodeURIComponent(filePath)}`,
  getErrorMessage: vi.fn((_err: unknown, fallback: string) => fallback),
}));

const workspacesRef = {
  current: [
    {
      id: 'ws-001',
      remote_host_id: '',
      files_changed: 0,
      lines_added: 0,
      lines_removed: 0,
      sessions: [],
    },
  ] as Array<Record<string, unknown>>,
};

// Mirrors useSessionsWebSocket: `workspaces` starts as [] and `loading` is the
// only signal that the first dashboard snapshot has arrived.
const loadingRef = { current: false };

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ workspaces: workspacesRef.current, loading: loadingRef.current }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));

vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));

const hookState = {
  draft: '',
  status: 'connecting' as 'connecting' | 'saved' | 'saving' | 'error',
  reason: null as string | null,
  onEdit: vi.fn(),
};
vi.mock('../hooks/useMarkdownDocument', () => ({
  default: () => hookState,
}));

vi.mock('../components/markdown/MarkdownEditor', () => ({
  default: ({ value }: { value: string }) => <div data-testid="markdown-editor">{value}</div>,
}));

import { getFileContent } from '../lib/api';
const mockGetFileContent = vi.mocked(getFileContent);

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/" element={<div data-testid="home" />} />
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

// The scroll listener is attached in a passive useEffect. RTL's async queries
// resolve as soon as React commits the DOM, but the effect may run a macrotask
// later — so a single scroll right after `findScrollContainer` races the
// listener. The page exposes the listener-attached transition as
// `data-scroll-listener-ready="true"`; await that once, then fire scroll once
// and assert once (docs/testing.md rule 5 + 7).
async function scrollAndExpectSaved(container: HTMLDivElement, top: number, key: string) {
  await waitFor(() => {
    if (container.dataset.scrollListenerReady !== 'true') {
      throw new Error('scroll listener not attached yet');
    }
  });
  Object.defineProperty(container, 'scrollTop', { value: top, writable: true });
  fireEvent.scroll(container);
  expect(localStorage.getItem(key)).toBe(String(top));
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  mockGetFileContent.mockResolvedValue('# Hello markdown\n\nbody');
  hookState.draft = '';
  hookState.status = 'connecting';
  hookState.reason = null;
  loadingRef.current = false;
  workspacesRef.current = [
    {
      id: 'ws-001',
      remote_host_id: '',
      files_changed: 0,
      lines_added: 0,
      lines_removed: 0,
      sessions: [],
    },
  ];
});

afterEach(() => {
  localStorage.clear();
});

describe('MarkdownPreviewPage editor/viewer split', () => {
  it('stays on the page while the workspace list is still loading', () => {
    // A direct load or reload: the sessions socket has not delivered its first
    // snapshot, so the list is empty and loading is true. No redirect.
    loadingRef.current = true;
    workspacesRef.current = [];
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.queryByTestId('home')).toBeNull();
    expect(screen.getByTestId('markdown-editor')).toBeInTheDocument();
  });

  it('redirects home once the list has loaded without the workspace', () => {
    loadingRef.current = false;
    workspacesRef.current = [];
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('home')).toBeInTheDocument();
  });

  it('renders the editor for a local workspace with the hook draft and status', () => {
    hookState.draft = '# from socket';
    hookState.status = 'saved';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-editor')).toHaveTextContent('# from socket');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('Saved');
    expect(mockGetFileContent).not.toHaveBeenCalled();
  });

  it('shows Saving… while a save is in flight and the close reason on error', () => {
    hookState.status = 'saving';
    const { unmount } = renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('Saving…');
    unmount();
    hookState.status = 'error';
    hookState.reason = 'write_failed';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('markdown-status')).toHaveTextContent('write_failed');
  });

  it('renders the read-only viewer for a remote workspace', async () => {
    workspacesRef.current = [
      {
        id: 'ws-001',
        remote_host_id: 'host-1',
        files_changed: 0,
        lines_added: 0,
        lines_removed: 0,
        sessions: [],
      },
    ];
    mockGetFileContent.mockResolvedValue('# remote');
    renderAt('/diff/ws-001/md/notes.md');
    expect(await screen.findByTestId('markdown-viewer')).toHaveTextContent('remote');
    expect(screen.queryByTestId('markdown-editor')).toBeNull();
  });

  it('falls back to the viewer when the editor refuses the file', async () => {
    hookState.status = 'error';
    hookState.reason = 'too_large';
    mockGetFileContent.mockResolvedValue('# big');
    renderAt('/diff/ws-001/md/notes.md');
    expect(await screen.findByTestId('markdown-viewer')).toHaveTextContent('big');
    expect(screen.queryByTestId('markdown-editor')).toBeNull();
  });

  it('keeps the Download link', () => {
    hookState.status = 'saved';
    renderAt('/diff/ws-001/md/notes.md');
    expect(screen.getByTestId('download-markdown')).toHaveAttribute(
      'href',
      '/api/file/ws-001/notes.md'
    );
  });
});

// All tests below exercise the viewer path: they set remote_host_id so the
// page uses the read-only renderer instead of the editor.
function setRemoteWorkspace() {
  workspacesRef.current = [
    {
      id: 'ws-001',
      remote_host_id: 'host-1',
      files_changed: 0,
      lines_added: 0,
      lines_removed: 0,
      sessions: [],
    },
  ];
}

describe('MarkdownPreviewPage image rewriting', () => {
  it('rewrites relative image src to the workspace file API', async () => {
    setRemoteWorkspace();
    mockGetFileContent.mockResolvedValue('![diagram](watercolor-forest-arbor-child.png)');

    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/mood/README.md')}`);

    const img = await screen.findByRole('img', { name: 'diagram' });
    expect(img).toHaveAttribute(
      'src',
      '/api/file/ws-001/' + encodeURIComponent('docs/mood/watercolor-forest-arbor-child.png')
    );
  });

  it('leaves external URLs unchanged', async () => {
    setRemoteWorkspace();
    mockGetFileContent.mockResolvedValue('![x](https://example.com/a.png)');

    renderAt('/diff/ws-001/md/README.md');

    const img = await screen.findByRole('img', { name: 'x' });
    expect(img).toHaveAttribute('src', 'https://example.com/a.png');
  });

  it('rewrites workspace-absolute paths (leading slash)', async () => {
    setRemoteWorkspace();
    mockGetFileContent.mockResolvedValue('![x](/a.png)');

    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/README.md')}`);

    const img = await screen.findByRole('img', { name: 'x' });
    expect(img).toHaveAttribute('src', '/api/file/ws-001/' + encodeURIComponent('a.png'));
  });
});

describe('MarkdownPreviewPage download', () => {
  it('renders Download link pointing to the file API', async () => {
    setRemoteWorkspace();
    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/README.md')}`);

    const link = await screen.findByTestId('download-markdown');
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/api/file/ws-001/docs%2FREADME.md');
    expect(link).toHaveAttribute('download', 'README.md');
  });

  it('copies the decoded file path', async () => {
    renderAt(`/diff/ws-001/md/${encodeURIComponent('docs/my notes.md')}`);

    fireEvent.click(await screen.findByRole('button', { name: 'Copy path' }));

    await waitFor(() => expect(toastSuccessMock).toHaveBeenCalledWith('Copied path'));
    expect(writeTextMock).toHaveBeenCalledWith('docs/my notes.md');
  });

  it('labels Download as an icon link', async () => {
    renderAt('/diff/ws-001/md/README.md');

    expect(await screen.findByRole('link', { name: 'Download Markdown file' })).toHaveAttribute(
      'data-testid',
      'download-markdown'
    );
  });
});

describe('MarkdownPreviewPage scroll memory', () => {
  it('writes scrollTop to localStorage when the content scrolls', async () => {
    setRemoteWorkspace();
    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    await scrollAndExpectSaved(container, 250, 'schmux-markdown-scroll-position-ws-001-README.md');
  });

  it('restores scrollTop from localStorage on mount', async () => {
    setRemoteWorkspace();
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
    setRemoteWorkspace();
    localStorage.setItem('schmux-markdown-scroll-position-ws-001-OTHER.md', '999');

    renderAt('/diff/ws-001/md/README.md');
    const container = await findScrollContainer();

    await scrollAndExpectSaved(container, 100, 'schmux-markdown-scroll-position-ws-001-README.md');

    expect(localStorage.getItem('schmux-markdown-scroll-position-ws-001-OTHER.md')).toBe('999');
  });
});
