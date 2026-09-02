import {
  useEffect,
  useId,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
  type WheelEvent as ReactWheelEvent,
} from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router';
import mermaid from 'mermaid';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import { useSessions } from '../contexts/SessionsContext';
import { getErrorMessage, getFileContent, getWorkspaceFileUrl } from '../lib/api';

type PreviewTheme = 'light' | 'dark';

const MIN_ZOOM = 0.5;
const MAX_ZOOM = 4;
const ZOOM_STEP = 0.25;

interface MermaidViewState {
  zoom: number;
  scrollLeft: number;
  scrollTop: number;
}

const defaultViewState: MermaidViewState = { zoom: 1, scrollLeft: 0, scrollTop: 0 };

function getMermaidViewStateKey(workspaceId: string | undefined, filepath: string) {
  return `schmux-mermaid-view-state-${workspaceId || ''}-${filepath}`;
}

function loadMermaidViewState(key: string): MermaidViewState {
  try {
    const saved = JSON.parse(localStorage.getItem(key) || '') as Partial<MermaidViewState>;
    return {
      zoom:
        typeof saved.zoom === 'number' && Number.isFinite(saved.zoom)
          ? Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, saved.zoom))
          : defaultViewState.zoom,
      scrollLeft:
        typeof saved.scrollLeft === 'number' && Number.isFinite(saved.scrollLeft)
          ? Math.max(0, saved.scrollLeft)
          : defaultViewState.scrollLeft,
      scrollTop:
        typeof saved.scrollTop === 'number' && Number.isFinite(saved.scrollTop)
          ? Math.max(0, saved.scrollTop)
          : defaultViewState.scrollTop,
    };
  } catch {
    return defaultViewState;
  }
}

function getPreviewTheme(): PreviewTheme {
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

export function makeStandaloneSvg(svg: string): string {
  const wrapper = document.createElement('div');
  wrapper.innerHTML = svg;
  const svgElement = wrapper.querySelector('svg');
  const normalizedSvg = svgElement ? new XMLSerializer().serializeToString(svgElement) : svg;
  const lineColor = getComputedStyle(document.documentElement)
    .getPropertyValue('--color-text-muted')
    .trim();
  return lineColor ? normalizedSvg.split('var(--color-text-muted)').join(lineColor) : normalizedSvg;
}

export default function MermaidPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const renderId = `mermaid-${useId().replace(/:/g, '')}`;
  const { workspaces } = useSessions();
  const decodedFilepath = filepath || '';
  const viewStateKey = getMermaidViewStateKey(workspaceId, decodedFilepath);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [renderedSvg, setRenderedSvg] = useState('');
  const [renderVersion, setRenderVersion] = useState(0);
  const [renderError, setRenderError] = useState('');
  const [openSvgUrl, setOpenSvgUrl] = useState('');
  const [theme, setTheme] = useState<PreviewTheme>(getPreviewTheme);
  const [zoom, setZoom] = useState(() => loadMermaidViewState(viewStateKey).zoom);
  const [isPanning, setIsPanning] = useState(false);
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);
  const viewportRef = useRef<HTMLDivElement>(null);
  const panRef = useRef<{
    pointerId: number;
    clientX: number;
    clientY: number;
    scrollLeft: number;
    scrollTop: number;
  } | null>(null);
  const pendingRestoreRef = useRef<{ key: string; state: MermaidViewState } | null>({
    key: viewStateKey,
    state: loadMermaidViewState(viewStateKey),
  });
  const restoringViewRef = useRef(true);
  const renderedViewStateKeyRef = useRef('');

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);

  const loadFile = async () => {
    if (!workspaceId || !decodedFilepath) return;
    setLoading(true);
    setError('');
    try {
      setContent(await getFileContent(workspaceId, decodedFilepath));
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load file'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  useEffect(() => {
    loadFile();
  }, [workspaceId, decodedFilepath, location.key]);

  useEffect(() => {
    if (!workspace) return;
    const currentStats = {
      files: workspace.files_changed,
      added: workspace.lines_added,
      removed: workspace.lines_removed,
    };
    const prevStats = prevGitStatsRef.current;
    if (
      prevStats !== null &&
      (prevStats.files !== currentStats.files ||
        prevStats.added !== currentStats.added ||
        prevStats.removed !== currentStats.removed)
    ) {
      loadFile();
    }
    prevGitStatsRef.current = currentStats;
  }, [workspace, workspaceId]);

  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(getPreviewTheme()));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (loading || error) return;
    let cancelled = false;
    setRenderedSvg('');
    setRenderError('');
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'default',
      ...(theme === 'dark' ? { themeVariables: { lineColor: 'var(--color-text-muted)' } } : {}),
    });
    mermaid
      .render(renderId, content)
      .then(({ svg }) => {
        if (!cancelled) {
          renderedViewStateKeyRef.current = viewStateKey;
          setRenderedSvg(svg);
          setRenderVersion((version) => version + 1);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setRenderError(getErrorMessage(err, 'Failed to render Mermaid diagram'));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [content, error, loading, renderId, theme]);

  useEffect(() => {
    if (!renderedSvg) {
      setOpenSvgUrl('');
      return;
    }

    const url = URL.createObjectURL(
      new Blob([makeStandaloneSvg(renderedSvg)], { type: 'image/svg+xml' })
    );
    setOpenSvgUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [renderedSvg]);

  useEffect(() => {
    const saved = loadMermaidViewState(viewStateKey);
    restoringViewRef.current = true;
    pendingRestoreRef.current = { key: viewStateKey, state: saved };
    setZoom(saved.zoom);
  }, [viewStateKey]);

  useEffect(() => {
    const pending = pendingRestoreRef.current;
    if (
      !renderedSvg ||
      !pending ||
      pending.key !== viewStateKey ||
      renderedViewStateKeyRef.current !== viewStateKey ||
      pending.state.zoom !== zoom
    ) {
      return;
    }
    const viewport = viewportRef.current;
    if (!viewport) return;

    requestAnimationFrame(() => {
      viewport.scrollLeft = pending.state.scrollLeft;
      viewport.scrollTop = pending.state.scrollTop;
      pendingRestoreRef.current = null;
      restoringViewRef.current = false;
    });
  }, [renderVersion, renderedSvg, viewStateKey, zoom]);

  useEffect(() => {
    if (restoringViewRef.current) return;
    const viewport = viewportRef.current;
    localStorage.setItem(
      viewStateKey,
      JSON.stringify({
        zoom,
        scrollLeft: viewport?.scrollLeft || 0,
        scrollTop: viewport?.scrollTop || 0,
      } satisfies MermaidViewState)
    );
  }, [viewStateKey, zoom]);

  const adjustDiagramZoom = (delta: number, anchor?: { x: number; y: number }) => {
    pendingRestoreRef.current = null;
    restoringViewRef.current = false;
    const viewport = viewportRef.current;
    setZoom((currentZoom) => {
      const nextZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, currentZoom + delta));
      if (nextZoom === currentZoom) return currentZoom;

      if (viewport) {
        const anchorX = anchor?.x ?? viewport.clientWidth / 2;
        const anchorY = anchor?.y ?? viewport.clientHeight / 2;
        const contentX = viewport.scrollLeft + anchorX;
        const contentY = viewport.scrollTop + anchorY;
        const ratio = nextZoom / currentZoom;
        requestAnimationFrame(() => {
          viewport.scrollLeft = contentX * ratio - anchorX;
          viewport.scrollTop = contentY * ratio - anchorY;
        });
      }
      return nextZoom;
    });
  };

  const fitDiagram = () => {
    pendingRestoreRef.current = null;
    restoringViewRef.current = false;
    setZoom(1);
    const viewport = viewportRef.current;
    if (viewport) {
      requestAnimationFrame(() => {
        viewport.scrollLeft = 0;
        viewport.scrollTop = 0;
      });
    }
  };

  const handleWheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    if (!event.ctrlKey && !event.metaKey) return;
    event.preventDefault();
    const rect = event.currentTarget.getBoundingClientRect();
    adjustDiagramZoom(event.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP, {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
    });
  };

  const handlePointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (zoom <= 1 || event.button !== 0) return;
    panRef.current = {
      pointerId: event.pointerId,
      clientX: event.clientX,
      clientY: event.clientY,
      scrollLeft: event.currentTarget.scrollLeft,
      scrollTop: event.currentTarget.scrollTop,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
    setIsPanning(true);
    event.preventDefault();
  };

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== event.pointerId) return;
    event.currentTarget.scrollLeft = pan.scrollLeft - (event.clientX - pan.clientX);
    event.currentTarget.scrollTop = pan.scrollTop - (event.clientY - pan.clientY);
  };

  const stopPanning = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (panRef.current?.pointerId !== event.pointerId) return;
    panRef.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    setIsPanning(false);
  };

  const handleScroll = () => {
    pendingRestoreRef.current = null;
    restoringViewRef.current = false;
    const viewport = viewportRef.current;
    if (!viewport) return;
    localStorage.setItem(
      viewStateKey,
      JSON.stringify({
        zoom,
        scrollLeft: viewport.scrollLeft,
        scrollTop: viewport.scrollTop,
      } satisfies MermaidViewState)
    );
  };

  if (loading) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
          </>
        )}
        <div className="diff-page">
          <div className="loading-state flex-1">
            <div className="spinner" />
            <span>Loading preview...</span>
          </div>
        </div>
      </>
    );
  }

  if (error) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
          </>
        )}
        <div className="diff-page">
          <div className="empty-state flex-1">
            <div className="empty-state__icon">!</div>
            <h3 className="empty-state__title">Failed to load preview</h3>
            <p className="empty-state__description">{error}</p>
            <Link to={`/diff/${workspaceId}`} className="btn btn--primary">
              Back to Diff
            </Link>
          </div>
        </div>
      </>
    );
  }

  return (
    <>
      {workspace && (
        <>
          <WorkspaceHeader workspace={workspace} />
          <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
        </>
      )}
      <div className="diff-page">
        <div className="diff-content diff-content--standalone">
          <div className="diff-content__header">
            <h2 className="diff-content__title">{decodedFilepath}</h2>
            <div className="button-group diff-mermaid-controls" aria-label="Diagram zoom controls">
              <button
                className="btn btn--sm btn--secondary"
                type="button"
                aria-label="Zoom out"
                title="Zoom out"
                disabled={zoom <= MIN_ZOOM}
                onClick={() => adjustDiagramZoom(-ZOOM_STEP)}
              >
                −
              </button>
              <output className="diff-mermaid-zoom" aria-live="polite">
                {Math.round(zoom * 100)}%
              </output>
              <button
                className="btn btn--sm btn--secondary"
                type="button"
                aria-label="Zoom in"
                title="Zoom in"
                disabled={zoom >= MAX_ZOOM}
                onClick={() => adjustDiagramZoom(ZOOM_STEP)}
              >
                +
              </button>
              <button
                className="btn btn--sm btn--secondary"
                type="button"
                title="Fit diagram"
                onClick={fitDiagram}
              >
                Fit
              </button>
              {openSvgUrl && (
                <a
                  className="btn btn--sm btn--secondary"
                  data-testid="open-mermaid-svg"
                  title="Open rendered SVG in new tab"
                  href={openSvgUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Open
                </a>
              )}
              <a
                className="btn btn--sm btn--secondary"
                data-testid="download-mermaid"
                title="Download Mermaid file"
                href={workspaceId ? getWorkspaceFileUrl(workspaceId, decodedFilepath) : '#'}
                download={decodedFilepath.split('/').pop() || 'diagram.mmd'}
              >
                Download
              </a>
            </div>
          </div>
          <div
            ref={viewportRef}
            className={`diff-viewer-wrapper diff-mermaid-viewport${zoom > 1 ? ' diff-mermaid-viewport--pannable' : ''}${isPanning ? ' diff-mermaid-viewport--panning' : ''}`}
            data-testid="mermaid-viewport"
            title="Ctrl/Cmd + scroll to zoom; drag to pan; double-click to fit"
            onWheel={handleWheel}
            onDoubleClick={fitDiagram}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerUp={stopPanning}
            onPointerCancel={stopPanning}
            onScroll={handleScroll}
          >
            {renderError ? (
              <div className="empty-state">
                <div className="empty-state__icon">!</div>
                <h3 className="empty-state__title">Failed to render diagram</h3>
                <p className="empty-state__description">{renderError}</p>
              </div>
            ) : renderedSvg ? (
              <div
                className="diff-mermaid-frame"
                data-testid="mermaid-diagram"
                style={{ width: `${zoom * 100}%` }}
                dangerouslySetInnerHTML={{ __html: renderedSvg }}
              />
            ) : (
              <div className="loading-state flex-1">
                <div className="spinner" />
                <span>Rendering diagram...</span>
              </div>
            )}
          </div>
        </div>
      </div>
    </>
  );
}
