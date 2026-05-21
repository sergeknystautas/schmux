import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams, Link, useNavigate, useLocation } from 'react-router-dom';
import { getFileContent, getWorkspaceFileUrl, getHtmlOpenUrl, getErrorMessage } from '../lib/api';
import { rewriteHtmlRelativePaths } from '../lib/pathUtils';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

const getHtmlScrollPositionKey = (workspaceId: string | undefined, filepath: string | undefined) =>
  `schmux-html-scroll-position-${workspaceId || ''}-${filepath || ''}`;

export default function HtmlPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const { workspaces } = useSessions();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);
  const iframeRef = useRef<HTMLIFrameElement>(null);

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);
  const decodedFilepath = filepath || '';

  const rewrittenHtml = useMemo(() => {
    if (!content || !workspaceId) return '';
    return rewriteHtmlRelativePaths(content, workspaceId, decodedFilepath);
  }, [content, workspaceId, decodedFilepath]);

  const hasScripts = useMemo(() => /<script[\s>]/i.test(content), [content]);

  const loadFile = async () => {
    if (!workspaceId || !decodedFilepath) return;
    setLoading(true);
    setError('');
    try {
      const text = await getFileContent(workspaceId, decodedFilepath);
      setContent(text);
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

  const handleIframeLoad = useCallback(() => {
    const iframe = iframeRef.current;
    if (!iframe) return;
    try {
      const iframeWindow = iframe.contentWindow;
      if (!iframeWindow) return;
      const key = getHtmlScrollPositionKey(workspaceId, decodedFilepath);
      iframeWindow.addEventListener('scroll', () => {
        localStorage.setItem(key, iframeWindow.scrollY.toString());
      });
      const saved = localStorage.getItem(key);
      if (saved) {
        requestAnimationFrame(() => {
          iframeWindow.scrollTo(0, parseInt(saved, 10));
        });
      }
    } catch {
      // sandbox cross-origin access blocked
    }
  }, [workspaceId, decodedFilepath]);

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
            <div className="spinner"></div>
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
        <div
          className="diff-content"
          style={{
            flex: 1,
            borderTop: '1px solid var(--color-border)',
            borderLeft: '1px solid var(--color-border)',
            borderRadius: '0 0 var(--radius-lg) 0',
          }}
        >
          <div className="diff-content__header">
            <h2 className="diff-content__title">
              {decodedFilepath}
              <a
                className="diff-content__preview-btn"
                data-testid="open-new-window"
                title="Open in new window"
                href={workspaceId ? getHtmlOpenUrl(workspaceId, decodedFilepath) : '#'}
                target="_blank"
                rel="noopener noreferrer"
              >
                Open
              </a>
              <a
                className="diff-content__preview-btn"
                data-testid="download-html"
                title="Download HTML file"
                href={workspaceId ? getWorkspaceFileUrl(workspaceId, decodedFilepath) : '#'}
                download={decodedFilepath.split('/').pop() || 'file.html'}
              >
                Download
              </a>
            </h2>
            {hasScripts && (
              <span
                data-testid="script-warning"
                style={{
                  color: 'var(--color-warning)',
                  fontSize: '0.8rem',
                }}
              >
                JavaScript is disabled in preview — page may not render as intended
              </span>
            )}
          </div>
          <div className="diff-viewer-wrapper">
            <iframe
              ref={iframeRef}
              srcDoc={rewrittenHtml}
              sandbox="allow-same-origin"
              title={`HTML preview: ${decodedFilepath}`}
              style={{ width: '100%', height: '100%', border: 'none' }}
              onLoad={handleIframeLoad}
            />
          </div>
        </div>
      </div>
    </>
  );
}
