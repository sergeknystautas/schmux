import { useEffect, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { getDiff, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import type { DiffResponse } from '../lib/types';

export default function MarkdownPreviewPage() {
  const { workspaceId, filepath } = useParams();
  const navigate = useNavigate();
  const { workspaces } = useSessions();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);

  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  useEffect(() => {
    const loadDiff = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getDiff(workspaceId || '');
        setDiffData(data);
      } catch (err) {
        setError(getErrorMessage(err, 'Failed to load diff'));
      } finally {
        setLoading(false);
      }
    };
    loadDiff();
  }, [workspaceId]);

  // Find the file in the diff data
  const decodedFilepath = filepath ? decodeURIComponent(filepath) : '';
  const selectedFile = diffData?.files?.find((f) => (f.new_path || f.old_path) === decodedFilepath);

  // Navigate back if file not found or not markdown
  useEffect(() => {
    if (!loading && diffData && (!selectedFile || !decodedFilepath.match(/\.(md|mdx)$/i))) {
      navigate(`/diff/${workspaceId}`);
    }
  }, [loading, diffData, selectedFile, workspaceId, navigate, decodedFilepath]);

  if (loading || !selectedFile || !decodedFilepath.match(/\.(md|mdx)$/i)) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
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
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
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
          <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
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
              {selectedFile.new_path || selectedFile.old_path}
              <Link to={`/diff/${workspaceId}`} className="diff-content__preview-btn">
                Back
              </Link>
            </h2>
          </div>
          <div className="diff-viewer-wrapper">
            <div className="markdown-preview-content">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {selectedFile.new_content || ''}
              </ReactMarkdown>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
