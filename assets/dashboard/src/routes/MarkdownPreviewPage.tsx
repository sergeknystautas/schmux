import { useEffect, useRef, useState } from 'react';
import { useParams, Link, useNavigate, useLocation } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getFileContent, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

export default function MarkdownPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const { workspaces } = useSessions();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);
  const decodedFilepath = filepath || '';

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
          <div className="diff-viewer-wrapper">
            <div className="markdown-preview-content">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
