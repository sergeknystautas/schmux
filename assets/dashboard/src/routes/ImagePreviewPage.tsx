import { useParams, Link } from 'react-router-dom';
import { getWorkspaceFileUrl } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

export default function ImagePreviewPage() {
  const { workspaceId, filepath } = useParams();
  const { workspaces } = useSessions();

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  // filepath is already decoded by React Router - use it directly
  const decodedFilepath = filepath || '';
  const imageUrl =
    workspaceId && decodedFilepath ? getWorkspaceFileUrl(workspaceId, decodedFilepath) : '';

  // Validate image extension
  const isImage = decodedFilepath.match(/\.(png|jpg|jpeg|webp|gif)$/i);

  if (!workspace || !isImage || !imageUrl) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
          </>
        )}
        <div className="diff-page">
          <div className="empty-state" style={{ flex: 1 }}>
            <div className="empty-state__icon">!</div>
            <h3 className="empty-state__title">Invalid image</h3>
            <p className="empty-state__description">This file cannot be previewed</p>
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
      <WorkspaceHeader workspace={workspace} />
      <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />

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
              <Link to={`/diff/${workspaceId}`} className="diff-content__preview-btn">
                Back
              </Link>
            </h2>
          </div>
          <div className="diff-viewer-wrapper" style={{ padding: '20px', overflow: 'auto' }}>
            <img
              src={imageUrl}
              alt={decodedFilepath}
              style={{ maxWidth: '100%', height: 'auto', display: 'block', margin: '0 auto' }}
            />
          </div>
        </div>
      </div>
    </>
  );
}
