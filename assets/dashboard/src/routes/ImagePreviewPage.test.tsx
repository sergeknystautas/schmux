import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import ImagePreviewPage from './ImagePreviewPage';

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
});
