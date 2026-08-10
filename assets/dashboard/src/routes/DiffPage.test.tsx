import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import DiffPage from './DiffPage';

vi.mock('../lib/api', () => ({
  getDiff: vi.fn(),
  getDiffFile: vi.fn(),
  diffExternal: vi.fn(),
  getWorkspaceFileUrl: (workspaceId: string, filePath: string) =>
    `/api/file/${workspaceId}/${encodeURIComponent(filePath)}`,
  createTab: vi.fn(),
  getErrorMessage: vi.fn((_err: unknown, fallback: string) => fallback),
}));

vi.mock('react-diff-viewer-continued', () => ({
  default: () => <div data-testid="diff-viewer-stub" />,
  DiffMethod: { WORDS: 'words' },
}));

vi.mock('../hooks/useTheme', () => ({
  default: () => ({ theme: 'dark', toggleTheme: vi.fn() }),
}));

vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({ config: {} }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: [
      {
        id: 'ws-001',
        files_changed: 1,
        lines_added: 3,
        lines_removed: 1,
        sessions: [],
      },
    ],
    loading: false,
  }),
}));

vi.mock('../contexts/RemoteAccessContext', () => ({
  useRemoteAccess: () => ({ simulateRemote: false }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({ alert: vi.fn(), confirm: vi.fn() }),
}));

const toastSuccessMock = vi.fn();
const toastErrorMock = vi.fn();
vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ success: toastSuccessMock, error: toastErrorMock }),
}));

vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({ setPendingNavigation: vi.fn() }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));

vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));

import { getDiff, getDiffFile } from '../lib/api';
const mockGetDiff = vi.mocked(getDiff);
const mockGetDiffFile = vi.mocked(getDiffFile);

const writeTextMock = vi.fn();

const DIFF_DATA = {
  workspace_id: 'ws-001',
  repo: 'repo',
  branch: 'main',
  files: [
    {
      old_path: 'docs/guide.md',
      new_path: 'docs/guide.md',
      status: 'modified',
      lines_added: 3,
      lines_removed: 1,
      is_binary: false,
    },
  ],
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/diff/:workspaceId" element={<DiffPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  // jsdom doesn't implement scrollIntoView; the sidebar auto-scroll effect calls it.
  Element.prototype.scrollIntoView = vi.fn();
  mockGetDiff.mockResolvedValue(DIFF_DATA);
  mockGetDiffFile.mockResolvedValue({
    workspace_id: 'ws-001',
    path: 'docs/guide.md',
    old_content: 'old\n',
    new_content: 'new\n',
  });
  writeTextMock.mockResolvedValue(undefined);
  Object.defineProperty(navigator, 'clipboard', {
    value: { writeText: writeTextMock },
    configurable: true,
  });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('DiffPage copy path', () => {
  it('copies the selected file path to the clipboard', async () => {
    renderAt('/diff/ws-001');

    const copyBtn = await screen.findByTestId('copy-path-btn');
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('docs/guide.md');
    });
    await waitFor(() => {
      expect(toastSuccessMock).toHaveBeenCalledWith('Copied path');
    });
    expect(toastErrorMock).not.toHaveBeenCalled();
  });

  it('copies old_path for deleted files', async () => {
    mockGetDiff.mockResolvedValue({
      ...DIFF_DATA,
      files: [
        {
          ...DIFF_DATA.files[0],
          old_path: 'docs/removed.md',
          new_path: undefined,
          status: 'deleted',
        },
      ],
    });
    renderAt('/diff/ws-001');

    const copyBtn = await screen.findByTestId('copy-path-btn');
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(writeTextMock).toHaveBeenCalledWith('docs/removed.md');
    });
  });

  it('shows an error toast when the clipboard write fails', async () => {
    writeTextMock.mockRejectedValue(new Error('denied'));
    renderAt('/diff/ws-001');

    const copyBtn = await screen.findByTestId('copy-path-btn');
    fireEvent.click(copyBtn);

    await waitFor(() => {
      expect(toastErrorMock).toHaveBeenCalledWith('Failed to copy');
    });
    expect(toastSuccessMock).not.toHaveBeenCalled();
  });
});

describe('DiffPage content fetch', () => {
  it('fetches content for the selected file and renders the diff', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'a.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 1,
          is_binary: false,
        },
      ],
    });
    mockGetDiffFile.mockResolvedValue({
      workspace_id: 'ws-001',
      path: 'a.txt',
      old_content: 'old line\n',
      new_content: 'new line\n',
    });

    renderAt('/diff/ws-001');

    await waitFor(() => {
      expect(mockGetDiffFile).toHaveBeenCalledWith('ws-001', 'a.txt', undefined);
    });
  });

  it('serves reselected files from the cache without refetching', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'a.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
        {
          new_path: 'b.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
      ],
    });
    mockGetDiffFile.mockImplementation((_workspaceId: string, path: string) =>
      Promise.resolve({
        workspace_id: 'ws-001',
        path,
        old_content: '',
        new_content: `${path} content\n`,
      })
    );

    renderAt('/diff/ws-001');

    await waitFor(() => expect(mockGetDiffFile).toHaveBeenCalledTimes(1));
    fireEvent.click(await screen.findByTestId('diff-file-1'));
    await waitFor(() => expect(mockGetDiffFile).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByTestId('diff-file-0'));
    // Cached — no third fetch.
    await waitFor(() => expect(mockGetDiffFile).toHaveBeenCalledTimes(2));
  });

  it('passes old_path for renamed files', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          old_path: 'old.txt',
          new_path: 'new.txt',
          status: 'renamed',
          lines_added: 0,
          lines_removed: 0,
          is_binary: false,
        },
      ],
    });
    mockGetDiffFile.mockResolvedValue({
      workspace_id: 'ws-001',
      path: 'new.txt',
      old_content: '',
      new_content: '',
    });

    renderAt('/diff/ws-001');

    await waitFor(() => {
      expect(mockGetDiffFile).toHaveBeenCalledWith('ws-001', 'new.txt', 'old.txt');
    });
  });

  it('does not fetch content for binary files', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'blob.bin',
          status: 'modified',
          lines_added: 0,
          lines_removed: 0,
          is_binary: true,
        },
      ],
    });

    renderAt('/diff/ws-001');

    await waitFor(() => expect(screen.getByText('Binary file not shown')).toBeInTheDocument());
    expect(mockGetDiffFile).not.toHaveBeenCalled();
  });

  it('keeps cached content visible when switching back while another file loads', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'a.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
        {
          new_path: 'b.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
      ],
    });
    let resolveB!: (v: {
      workspace_id: string;
      path: string;
      old_content: string;
      new_content: string;
    }) => void;
    mockGetDiffFile.mockImplementation((_workspaceId: string, path: string) => {
      if (path === 'b.txt') {
        return new Promise((res) => {
          resolveB = res;
        });
      }
      return Promise.resolve({ workspace_id: 'ws-001', path, old_content: '', new_content: 'a\n' });
    });

    renderAt('/diff/ws-001');

    // a.txt loads and renders.
    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());

    // Switch to b.txt — its fetch hangs; the pending state names the file.
    fireEvent.click(screen.getByTestId('diff-file-1'));
    await screen.findByText(/Loading b\.txt/);

    // Switch back to cached a.txt mid-fetch — content shows, no spinner.
    fireEvent.click(screen.getByTestId('diff-file-0'));
    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());
    expect(screen.queryByText(/Loading b\.txt/)).not.toBeInTheDocument();

    // b.txt's late response still lands in the cache — no refetch on reselect.
    resolveB({ workspace_id: 'ws-001', path: 'b.txt', old_content: '', new_content: 'b\n' });
    await waitFor(() => expect(mockGetDiffFile).toHaveBeenCalledTimes(2));
    fireEvent.click(screen.getByTestId('diff-file-1'));
    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());
    expect(mockGetDiffFile).toHaveBeenCalledTimes(2);
  });

  it('scopes a fetch error to its file', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'a.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
        {
          new_path: 'b.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
      ],
    });
    mockGetDiffFile.mockImplementation((_workspaceId: string, path: string) => {
      if (path === 'b.txt') return Promise.reject(new Error('boom'));
      return Promise.resolve({ workspace_id: 'ws-001', path, old_content: '', new_content: 'a\n' });
    });

    renderAt('/diff/ws-001');

    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());

    // b.txt fails.
    fireEvent.click(screen.getByTestId('diff-file-1'));
    await screen.findByText('Failed to load file diff');

    // a.txt is unaffected by b.txt's error.
    fireEvent.click(screen.getByTestId('diff-file-0'));
    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());
    expect(screen.queryByText('Failed to load file diff')).not.toBeInTheDocument();
  });

  it('shows an inline error when the content fetch fails', async () => {
    mockGetDiff.mockResolvedValue({
      workspace_id: 'ws-001',
      repo: 'repo',
      branch: 'main',
      files: [
        {
          new_path: 'a.txt',
          status: 'modified',
          lines_added: 1,
          lines_removed: 0,
          is_binary: false,
        },
      ],
    });
    mockGetDiffFile.mockRejectedValue(new Error('boom'));

    renderAt('/diff/ws-001');

    // The mocked getErrorMessage returns its fallback string regardless of the error.
    await waitFor(() => expect(screen.getByText('Failed to load file diff')).toBeInTheDocument());
    // The file list is still rendered — only the content pane errors.
    expect(screen.getByTestId('diff-file-list')).toBeInTheDocument();
  });
});
