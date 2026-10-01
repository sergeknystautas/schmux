import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import ImagePreviewPage from './ImagePreviewPage';

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
  getWorkspaceFileUrl: (workspaceId: string, filePath: string) =>
    `/api/file/${workspaceId}/${encodeURIComponent(filePath)}`,
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

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/diff/:workspaceId/img/:filepath" element={<ImagePreviewPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('ImagePreviewPage', () => {
  it('renders image with correct src', () => {
    renderAt('/diff/ws-001/img/logo.png');
    const img = screen.getByRole('img');
    expect(img).toHaveAttribute('src', '/api/file/ws-001/logo.png');
  });

  it('shows error state for non-image files', () => {
    renderAt('/diff/ws-001/img/readme.txt');
    expect(screen.getByText('Invalid image')).toBeInTheDocument();
  });

  it('renders Open link that opens image in new tab', () => {
    renderAt('/diff/ws-001/img/logo.png');
    const link = screen.getByTestId('open-new-tab');
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/api/file/ws-001/logo.png');
    expect(link).toHaveAttribute('target', '_blank');
  });

  it('renders Download link pointing to the file API', () => {
    renderAt(`/diff/ws-001/img/${encodeURIComponent('assets/logo.png')}`);
    const link = screen.getByTestId('download-image');
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/api/file/ws-001/assets%2Flogo.png');
    expect(link).toHaveAttribute('download', 'logo.png');
  });

  it('copies the decoded file path', async () => {
    renderAt(`/diff/ws-001/img/${encodeURIComponent('assets/my logo.png')}`);

    fireEvent.click(screen.getByRole('button', { name: 'Copy path' }));

    await waitFor(() => expect(toastSuccessMock).toHaveBeenCalledWith('Copied path'));
    expect(writeTextMock).toHaveBeenCalledWith('assets/my logo.png');
  });

  it('has no Back link and labels Open and Download as icon links', () => {
    renderAt('/diff/ws-001/img/logo.png');

    expect(screen.queryByRole('link', { name: 'Back' })).toBeNull();
    expect(screen.getByRole('link', { name: 'Open image in new tab' })).toHaveAttribute(
      'data-testid',
      'open-new-tab'
    );
    expect(screen.getByRole('link', { name: 'Download image' })).toHaveAttribute(
      'data-testid',
      'download-image'
    );
  });
});
