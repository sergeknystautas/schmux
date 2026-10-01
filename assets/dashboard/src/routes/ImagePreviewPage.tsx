import { useParams, Link } from 'react-router';
import { getWorkspaceFileUrl } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import Tooltip from '../components/Tooltip';
import CopyButton from '../components/CopyButton';
import { DownloadIcon, ExternalLinkIcon } from '../components/Icons';

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
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
          </>
        )}
        <div className="diff-page">
          <div className="empty-state flex-1">
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
      <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />

      <div className="diff-page">
        <div className="diff-content diff-content--standalone">
          <div className="diff-content__header">
            <h2 className="diff-content__title">
              {decodedFilepath}
              <CopyButton
                text={decodedFilepath}
                label="path"
                className="copy-field__btn"
                testId="copy-path-btn"
              />
              <Tooltip content="Open image in new tab">
                <a
                  className="copy-field__btn"
                  data-testid="open-new-tab"
                  aria-label="Open image in new tab"
                  href={imageUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  {ExternalLinkIcon}
                </a>
              </Tooltip>
              <Tooltip content="Download image">
                <a
                  className="copy-field__btn"
                  data-testid="download-image"
                  aria-label="Download image"
                  href={imageUrl}
                  download={decodedFilepath.split('/').pop() || 'image'}
                >
                  {DownloadIcon}
                </a>
              </Tooltip>
            </h2>
          </div>
          <div className="diff-viewer-wrapper diff-image-frame">
            <img src={imageUrl} alt={decodedFilepath} />
          </div>
        </div>
      </div>
    </>
  );
}
