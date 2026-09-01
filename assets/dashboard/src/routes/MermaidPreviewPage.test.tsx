import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import MermaidPreviewPage from './MermaidPreviewPage';

vi.mock('mermaid', () => ({
  default: {
    initialize: vi.fn(),
    render: vi.fn(),
  },
}));

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

import mermaid from 'mermaid';
import { getFileContent } from '../lib/api';

const mockGetFileContent = vi.mocked(getFileContent);
const mockRender = vi.mocked(mermaid.render);

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/diff/:workspaceId/mmd/:filepath" element={<MermaidPreviewPage />} />
      </Routes>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  document.documentElement.setAttribute('data-theme', 'light');
  mockGetFileContent.mockResolvedValue('graph TD; A-->B');
  mockRender.mockResolvedValue({
    svg: '<svg aria-label="diagram"><text>A</text></svg>',
    diagramType: 'flowchart-v2',
  });
});

describe('MermaidPreviewPage', () => {
  it('renders the Mermaid source as an SVG', async () => {
    renderAt(`/diff/ws-001/mmd/${encodeURIComponent('docs/architecture.mmd')}`);

    const diagram = await screen.findByTestId('mermaid-diagram');
    expect(diagram.querySelector('svg')).not.toBeNull();
    expect(mockGetFileContent).toHaveBeenCalledWith('ws-001', 'docs/architecture.mmd');
    expect(mockRender).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-/), 'graph TD; A-->B');
    expect(mermaid.initialize).toHaveBeenCalledWith({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'default',
    });
  });

  it('keeps light custom node fills readable while raising edge contrast in dark mode', async () => {
    document.documentElement.setAttribute('data-theme', 'dark');
    renderAt('/diff/ws-001/mmd/architecture.mmd');

    await screen.findByTestId('mermaid-diagram');
    expect(mermaid.initialize).toHaveBeenCalledWith({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'default',
      themeVariables: { lineColor: 'var(--color-text-muted)' },
    });
  });

  it('rerenders when the dashboard mode changes', async () => {
    renderAt('/diff/ws-001/mmd/architecture.mmd');
    await screen.findByTestId('mermaid-diagram');
    expect(mockRender).toHaveBeenCalledTimes(1);

    document.documentElement.setAttribute('data-theme', 'dark');

    await waitFor(() => expect(mockRender).toHaveBeenCalledTimes(2));
    expect(mermaid.initialize).toHaveBeenLastCalledWith({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'default',
      themeVariables: { lineColor: 'var(--color-text-muted)' },
    });
  });

  it('zooms with the toolbar and resets to fit', async () => {
    renderAt('/diff/ws-001/mmd/architecture.mmd');
    const diagram = await screen.findByTestId('mermaid-diagram');

    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    expect(diagram).toHaveStyle({ width: '125%' });
    expect(screen.getByText('125%')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Fit' }));
    expect(diagram).toHaveStyle({ width: '100%' });
  });

  it('zooms around the pointer with Ctrl/Cmd + scroll', async () => {
    renderAt('/diff/ws-001/mmd/architecture.mmd');
    const diagram = await screen.findByTestId('mermaid-diagram');
    const viewport = screen.getByTestId('mermaid-viewport');

    fireEvent.wheel(viewport, { ctrlKey: true, deltaY: -100, clientX: 20, clientY: 20 });

    expect(diagram).toHaveStyle({ width: '125%' });
  });

  it('fits the diagram on double-click', async () => {
    renderAt('/diff/ws-001/mmd/architecture.mmd');
    const diagram = await screen.findByTestId('mermaid-diagram');
    const viewport = screen.getByTestId('mermaid-viewport');

    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    fireEvent.doubleClick(viewport);

    expect(diagram).toHaveStyle({ width: '100%' });
  });

  it('restores zoom and scroll position for the workspace file', async () => {
    localStorage.setItem(
      'schmux-mermaid-view-state-ws-001-docs/architecture.mmd',
      JSON.stringify({ zoom: 1.75, scrollLeft: 320, scrollTop: 140 })
    );

    renderAt(`/diff/ws-001/mmd/${encodeURIComponent('docs/architecture.mmd')}`);
    const diagram = await screen.findByTestId('mermaid-diagram');
    const viewport = screen.getByTestId('mermaid-viewport');

    expect(diagram).toHaveStyle({ width: '175%' });
    await waitFor(() => {
      expect(viewport.scrollLeft).toBe(320);
      expect(viewport.scrollTop).toBe(140);
    });
  });

  it('saves zoom and scroll position for the workspace file', async () => {
    renderAt(`/diff/ws-001/mmd/${encodeURIComponent('docs/architecture.mmd')}`);
    await screen.findByTestId('mermaid-diagram');
    const viewport = screen.getByTestId('mermaid-viewport');

    fireEvent.click(screen.getByRole('button', { name: 'Zoom in' }));
    viewport.scrollLeft = 180;
    viewport.scrollTop = 90;
    fireEvent.scroll(viewport);

    await waitFor(() => {
      expect(
        JSON.parse(
          localStorage.getItem('schmux-mermaid-view-state-ws-001-docs/architecture.mmd') || ''
        )
      ).toEqual({ zoom: 1.25, scrollLeft: 180, scrollTop: 90 });
    });
  });

  it('shows a render error for invalid Mermaid source', async () => {
    mockRender.mockRejectedValue(new Error('parse error'));
    renderAt('/diff/ws-001/mmd/broken.mmd');

    expect(await screen.findByText('Failed to render diagram')).toBeInTheDocument();
    expect(screen.getByText('Failed to render Mermaid diagram')).toBeInTheDocument();
  });

  it('downloads the original .mmd file', async () => {
    renderAt(`/diff/ws-001/mmd/${encodeURIComponent('docs/architecture.mmd')}`);

    const link = await screen.findByTestId('download-mermaid');
    await waitFor(() => expect(mockRender).toHaveBeenCalled());
    expect(link).toHaveAttribute('href', '/api/file/ws-001/docs%2Farchitecture.mmd');
    expect(link).toHaveAttribute('download', 'architecture.mmd');
  });
});
