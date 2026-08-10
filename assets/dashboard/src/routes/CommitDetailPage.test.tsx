import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import CommitDetailPage from './CommitDetailPage';

vi.mock('../lib/api', () => ({
  getCommitDetail: vi.fn(),
  getDiffFile: vi.fn(),
  getErrorMessage: vi.fn((_err: unknown, fallback: string) => fallback),
}));

vi.mock('react-diff-viewer-continued', () => ({
  default: () => <div data-testid="diff-viewer-stub" />,
  DiffMethod: { WORDS: 'words' },
}));

vi.mock('../hooks/useTheme', () => ({
  default: () => ({ theme: 'dark', toggleTheme: vi.fn() }),
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

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));

vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));

import { getCommitDetail, getDiffFile } from '../lib/api';
const mockGetCommitDetail = vi.mocked(getCommitDetail);
const mockGetDiffFile = vi.mocked(getDiffFile);

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/commit/:workspaceId/:commitHash" element={<CommitDetailPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  // jsdom doesn't implement scrollIntoView; the sidebar auto-scroll effect calls it.
  Element.prototype.scrollIntoView = vi.fn();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('CommitDetailPage content fetch', () => {
  it('fetches file content with the commit hash', async () => {
    mockGetCommitDetail.mockResolvedValue({
      hash: 'abc1234def',
      short_hash: 'abc1234',
      author_name: 'Dev',
      author_email: 'dev@example.com',
      timestamp: '2026-08-09T00:00:00Z',
      message: 'change stuff',
      parents: ['p1'],
      is_merge: false,
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
      old_content: 'before\n',
      new_content: 'after\n',
    });

    renderAt('/commit/ws-001/abc1234def');

    await waitFor(() => {
      expect(mockGetDiffFile).toHaveBeenCalledWith('ws-001', 'a.txt', undefined, 'abc1234def');
    });
  });

  it('scopes a fetch error to its file', async () => {
    mockGetCommitDetail.mockResolvedValue({
      hash: 'abc1234def',
      short_hash: 'abc1234',
      author_name: 'Dev',
      author_email: 'dev@example.com',
      timestamp: '2026-08-09T00:00:00Z',
      message: 'change stuff',
      parents: ['p1'],
      is_merge: false,
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

    renderAt('/commit/ws-001/abc1234def');

    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());

    // b.txt fails.
    fireEvent.click(screen.getByTestId('diff-file-1'));
    await screen.findByText('Failed to load file diff');

    // a.txt is unaffected by b.txt's error (served from cache, no error bleed).
    fireEvent.click(screen.getByTestId('diff-file-0'));
    await waitFor(() => expect(screen.getByTestId('diff-viewer-stub')).toBeInTheDocument());
    expect(screen.queryByText('Failed to load file diff')).not.toBeInTheDocument();
  });

  it('does not fetch content for binary files', async () => {
    mockGetCommitDetail.mockResolvedValue({
      hash: 'abc1234def',
      short_hash: 'abc1234',
      author_name: 'Dev',
      author_email: 'dev@example.com',
      timestamp: '2026-08-09T00:00:00Z',
      message: 'add image',
      parents: ['p1'],
      is_merge: false,
      files: [
        {
          new_path: 'logo.png',
          status: 'added',
          lines_added: 0,
          lines_removed: 0,
          is_binary: true,
        },
      ],
    });

    renderAt('/commit/ws-001/abc1234def');

    await waitFor(() => expect(screen.getByText('Binary file not shown')).toBeInTheDocument());
    expect(mockGetDiffFile).not.toHaveBeenCalled();
  });
});
