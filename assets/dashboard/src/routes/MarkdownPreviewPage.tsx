import { useEffect, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getFileContent, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

export default function MarkdownPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const { workspaces } = useSessions();
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);
  const decodedFilepath = filepath || '';

  // Redirect home if workspace no longer exists
  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  // Fetch file content directly
  useEffect(() => {
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
    loadFile();
  }, [workspaceId, decodedFilepath]);

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
