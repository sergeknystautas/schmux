import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import HtmlPreviewPage from './HtmlPreviewPage';

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
        <Route path="/diff/:workspaceId/html/:filepath" element={<HtmlPreviewPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockGetFileContent.mockResolvedValue('<html><body><h1>Hello</h1></body></html>');
});

describe('HtmlPreviewPage', () => {
  it('shows loading state initially', () => {
    mockGetFileContent.mockReturnValue(new Promise(() => {}));
    renderAt('/diff/ws-001/html/index.html');
    expect(screen.getByText('Loading preview...')).toBeInTheDocument();
  });

  it('renders an iframe with sandbox attribute after loading', async () => {
    renderAt('/diff/ws-001/html/index.html');

    await waitFor(() => {
      const iframe = document.querySelector('iframe');
      expect(iframe).toBeTruthy();
    });

    const iframe = document.querySelector('iframe') as HTMLIFrameElement;
    expect(iframe.getAttribute('sandbox')).toBe('allow-same-origin');
  });

  it('sets srcdoc with rewritten HTML content', async () => {
    mockGetFileContent.mockResolvedValue('<html><body><img src="logo.png"></body></html>');

    renderAt('/diff/ws-001/html/docs%2Findex.html');

    await waitFor(() => {
      const iframe = document.querySelector('iframe');
      expect(iframe).toBeTruthy();
    });

    const iframe = document.querySelector('iframe') as HTMLIFrameElement;
    const srcdoc = iframe.getAttribute('srcdoc') || '';
    expect(srcdoc).toContain('/api/file/ws-001/');
  });

  it('shows error state when file fetch fails', async () => {
    mockGetFileContent.mockRejectedValue(new Error('Not found'));

    renderAt('/diff/ws-001/html/missing.html');

    await waitFor(() => {
      expect(screen.getByText('Failed to load preview')).toBeInTheDocument();
    });
  });

  it('displays the filepath in the header', async () => {
    renderAt('/diff/ws-001/html/index.html');

    await waitFor(() => {
      expect(screen.getByText('index.html')).toBeInTheDocument();
    });
  });

  it('renders workspace header and session tabs', async () => {
    renderAt('/diff/ws-001/html/index.html');

    await waitFor(() => {
      expect(screen.getByTestId('workspace-header')).toBeInTheDocument();
      expect(screen.getByTestId('session-tabs')).toBeInTheDocument();
    });
  });

  it('shows script warning when HTML contains script tags', async () => {
    mockGetFileContent.mockResolvedValue(
      '<html><body><h1>Hi</h1><script>alert(1)</script></body></html>'
    );
    renderAt('/diff/ws-001/html/index.html');

    await waitFor(() => {
      expect(screen.getByTestId('script-warning')).toBeInTheDocument();
    });
    expect(screen.getByTestId('script-warning').textContent).toContain('JavaScript is disabled');
  });

  it('does not show script warning when HTML has no scripts', async () => {
    mockGetFileContent.mockResolvedValue('<html><body><h1>Hi</h1></body></html>');
    renderAt('/diff/ws-001/html/index.html');

    await waitFor(() => {
      const iframe = document.querySelector('iframe');
      expect(iframe).toBeTruthy();
    });
    expect(screen.queryByTestId('script-warning')).toBeNull();
  });

  it('renders Open button that opens rewritten HTML in new window', async () => {
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
    const createObjectURLSpy = vi
      .spyOn(URL, 'createObjectURL')
      .mockReturnValue('blob:http://localhost/fake');

    renderAt('/diff/ws-001/html/index.html');

    const btn = await screen.findByTestId('open-new-window');
    expect(btn).toBeInTheDocument();
    btn.click();

    expect(createObjectURLSpy).toHaveBeenCalledWith(expect.any(Blob));
    expect(openSpy).toHaveBeenCalledWith('blob:http://localhost/fake', '_blank');

    openSpy.mockRestore();
    createObjectURLSpy.mockRestore();
  });

  it('renders Download link pointing to the file API', async () => {
    renderAt('/diff/ws-001/html/docs%2Freport.html');

    const link = await screen.findByTestId('download-html');
    expect(link).toBeInTheDocument();
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('href', '/api/file/ws-001/docs%2Freport.html');
    expect(link).toHaveAttribute('download', 'report.html');
  });
});
