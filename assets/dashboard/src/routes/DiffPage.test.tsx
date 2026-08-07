import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import DiffPage from './DiffPage';

vi.mock('../lib/api', () => ({
  getDiff: vi.fn(),
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

import { getDiff } from '../lib/api';
const mockGetDiff = vi.mocked(getDiff);

const writeTextMock = vi.fn();

const DIFF_DATA = {
  workspace_id: 'ws-001',
  repo: 'repo',
  branch: 'main',
  files: [
    {
      old_path: 'docs/guide.md',
      new_path: 'docs/guide.md',
      old_content: 'old\n',
      new_content: 'new\n',
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
  mockGetDiff.mockResolvedValue(DIFF_DATA);
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
