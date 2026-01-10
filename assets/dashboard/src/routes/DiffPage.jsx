import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import ReactDiffViewer from 'react-diff-viewer-continued';
import { getDiff, disposeSession } from '../lib/api.js';
import { copyToClipboard } from '../lib/utils.js';
import useTheme from '../hooks/useTheme.js';
import useLocalStorage from '../hooks/useLocalStorage.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import WorkspacesList from '../components/WorkspacesList.jsx';
import Tooltip from '../components/Tooltip.jsx';

export default function DiffPage() {
  const { workspaceId } = useParams();
  const navigate = useNavigate();
  const { theme } = useTheme();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const [diffData, setDiffData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);

  useEffect(() => {
    const loadDiff = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getDiff(workspaceId);
        setDiffData(data);
        if (data.files?.length > 0) {
          setSelectedFileIndex(0);
        }
      } catch (err) {
        setError(err.message || 'Failed to load diff');
      } finally {
        setLoading(false);
      }
    };
    loadDiff();
  }, [workspaceId]);

  const selectedFile = diffData?.files?.[selectedFileIndex];

  const handleCopyAttach = async (command) => {
    const ok = await copyToClipboard(command);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  };

  const handleDispose = async (sessionId) => {
    const accepted = await confirm(`Dispose session ${sessionId}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
      // WorkspacesList will auto-refresh
    } catch (err) {
      toastError(`Failed to dispose: ${err.message}`);
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading diff...</span>
      </div>
    );
  }

  if (error) {
    return (
      <WorkspacesList
        workspaceId={workspaceId}
        showControls={false}
        renderActions={(workspace) => (
          <Tooltip content="Spawn session in this workspace">
            <button
              className="btn btn--sm btn--primary"
              onClick={(event) => {
                event.stopPropagation();
                navigate(`/spawn?workspace_id=${workspace.id}`);
              }}
              aria-label={`Spawn in ${workspace.id}`}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" y1="8" x2="12" y2="16"></line>
                <line x1="8" y1="12" x2="16" y2="12"></line>
              </svg>
              Spawn
            </button>
          </Tooltip>
        )}
        renderSessionActions={(action, sess) => {
          if (action === 'dispose') {
            return () => handleDispose(sess.id);
          }
          return undefined;
        }}
      />
    );
  }

  if (!error && (!diffData.files || diffData.files.length === 0)) {
    return (
      <>
        <WorkspacesList
          workspaceId={workspaceId}
          showControls={false}
          renderActions={(workspace) => (
            <>
              <Tooltip content="Spawn session in this workspace">
                <button
                  className="btn btn--sm btn--primary"
                  onClick={(event) => {
                    event.stopPropagation();
                    navigate(`/spawn?workspace_id=${workspace.id}`);
                  }}
                  aria-label={`Spawn in ${workspace.id}`}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="16"></line>
                    <line x1="8" y1="12" x2="16" y2="12"></line>
                  </svg>
                  Spawn
                </button>
              </Tooltip>
            </>
          )}
          renderSessionActions={(action, sess) => {
            if (action === 'dispose') {
              return () => handleDispose(sess.id);
            }
            return undefined;
          }}
        />
        <div className="empty-state">
          <h3 className="empty-state__title">No changes in workspace</h3>
          <p className="empty-state__description">This workspace has no uncommitted changes</p>
          <a href="/workspaces" className="btn btn--primary">Back to Workspaces</a>
        </div>
      </>
    );
  }

  return (
    <>
      <WorkspacesList
        workspaceId={workspaceId}
        showControls={false}
        renderActions={(workspace) => (
          <Tooltip content="Spawn session in this workspace">
            <button
              className="btn btn--sm btn--primary"
              onClick={(event) => {
                event.stopPropagation();
                navigate(`/spawn?workspace_id=${workspace.id}`);
              }}
              aria-label={`Spawn in ${workspace.id}`}
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" y1="8" x2="12" y2="16"></line>
                <line x1="8" y1="12" x2="16" y2="12"></line>
              </svg>
              Spawn
            </button>
          </Tooltip>
        )}
        renderSessionActions={(action, sess) => {
          if (action === 'dispose') {
            return () => handleDispose(sess.id);
          }
          return undefined;
        }}
      />

      <div className="diff-layout">
        <div className="diff-sidebar">
          <h3 className="diff-sidebar__title">Changed Files ({diffData.files.length})</h3>
          <div className="diff-file-list">
            {diffData.files.map((file, index) => (
              <button
                key={index}
                className={`diff-file-item${selectedFileIndex === index ? ' diff-file-item--active' : ''}`}
                onClick={() => setSelectedFileIndex(index)}
              >
                <div className="diff-file-item__info">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path>
                    <polyline points="13 2 13 9 20 9"></polyline>
                  </svg>
                  <span className="diff-file-item__path">{file.new_path || file.old_path}</span>
                </div>
                <span className={`badge badge--${file.status === 'added' ? 'success' : file.status === 'deleted' ? 'danger' : file.status === 'untracked' ? 'info' : 'neutral'}`}>
                  {file.status}
                </span>
              </button>
            ))}
          </div>
        </div>

        <div className="diff-content">
          {selectedFile && (
            <>
              <div className="diff-content__header">
                <h2 className="diff-content__title">{selectedFile.new_path || selectedFile.old_path}</h2>
                <span className={`badge badge--${selectedFile.status === 'added' ? 'success' : selectedFile.status === 'deleted' ? 'danger' : 'neutral'}`}>
                  {selectedFile.status}
                </span>
              </div>
              <div className="diff-viewer-wrapper">
                <ReactDiffViewer
                  oldValue={selectedFile.old_content || ''}
                  newValue={selectedFile.new_content || ''}
                  splitView={false}
                  useDarkTheme={theme === 'dark'}
                  hideLineNumbers={false}
                  showDiffOnly={true}
                  extraLinesSurroundingDiff={2}
                />
              </div>
            </>
          )}
        </div>
      </div>
    </>
  );
}
