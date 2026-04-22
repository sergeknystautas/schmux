import { useEffect, useMemo, useRef, useState } from 'react';
import type { ImgHTMLAttributes } from 'react';
import { useParams, Link, useNavigate, useLocation } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getFileContent, getWorkspaceFileUrl, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

const getMarkdownScrollPositionKey = (
  workspaceId: string | undefined,
  filepath: string | undefined
) => `schmux-markdown-scroll-position-${workspaceId || ''}-${filepath || ''}`;

// Resolve a markdown image/link src against the markdown file's directory.
// Returns null for external URLs (they pass through unchanged).
export function resolveMarkdownRelativePath(src: string, mdFilePath: string): string | null {
  if (/^([a-z][a-z0-9+.-]*:|\/\/)/i.test(src)) return null;
  const parts = src.startsWith('/')
    ? src.slice(1).split('/')
    : [...mdFilePath.split('/').slice(0, -1), ...src.split('/')];
  const stack: string[] = [];
  for (const p of parts) {
    if (p === '..') stack.pop();
    else if (p !== '.' && p !== '') stack.push(p);
  }
  return stack.join('/');
}

export default function MarkdownPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const { workspaces } = useSessions();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);
  const decodedFilepath = filepath || '';

  const markdownComponents = useMemo(
    () => ({
      img: ({ src, alt, ...rest }: ImgHTMLAttributes<HTMLImageElement>) => {
        if (typeof src !== 'string' || !workspaceId) {
          return <img src={src} alt={alt} {...rest} />;
        }
        const resolved = resolveMarkdownRelativePath(src, decodedFilepath);
        const finalSrc = resolved === null ? src : getWorkspaceFileUrl(workspaceId, resolved);
        return <img src={finalSrc} alt={alt} {...rest} />;
      },
    }),
    [workspaceId, decodedFilepath]
  );

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

  // Redirect home if workspace no longer exists
  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  // Fetch file content on mount / route change / tab re-focus
  useEffect(() => {
    loadFile();
  }, [workspaceId, decodedFilepath, location.key]);

  // Re-fetch when workspace git stats change (file edited on disk)
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

  // Persist and restore scroll position per workspace+file
  useEffect(() => {
    if (!contentRef.current || !content) return;

    const scrollEl = contentRef.current;
    const key = getMarkdownScrollPositionKey(workspaceId, decodedFilepath);

    const handleScroll = () => {
      localStorage.setItem(key, scrollEl.scrollTop.toString());
    };
    scrollEl.addEventListener('scroll', handleScroll);

    const saved = localStorage.getItem(key);
    if (saved) {
      requestAnimationFrame(() => {
        scrollEl.scrollTop = parseInt(saved, 10);
      });
    }

    return () => scrollEl.removeEventListener('scroll', handleScroll);
  }, [workspaceId, decodedFilepath, content]);

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
            <h2 className="diff-content__title">{decodedFilepath}</h2>
          </div>
          <div className="diff-viewer-wrapper" ref={contentRef}>
            <div className="markdown-preview-content">
              <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                {content}
              </ReactMarkdown>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
