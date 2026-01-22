import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import ReactDiffViewer from 'react-diff-viewer-continued';
import { getDiff, getErrorMessage } from '../lib/api';
import useTheme from '../hooks/useTheme';
import WorkspacesList from '../components/WorkspacesList';
import type { DiffResponse } from '../lib/types';

export default function DiffPage() {
  const { workspaceId } = useParams();
  const { theme } = useTheme();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);

  useEffect(() => {
    const loadDiff = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getDiff(workspaceId || '');
        setDiffData(data);
        if (data.files?.length > 0) {
          setSelectedFileIndex(0);
        }
      } catch (err) {
        setError(getErrorMessage(err, 'Failed to load diff'));
      } finally {
        setLoading(false);
      }
    };
    loadDiff();
  }, [workspaceId]);

  const selectedFile = diffData?.files?.[selectedFileIndex];

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
      <>
        <WorkspacesList
          workspaceId={workspaceId}
          showControls={false}
        />
        <div className="empty-state">
          <div className="empty-state__icon">⚠️</div>
          <h3 className="empty-state__title">Failed to load diff</h3>
          <p className="empty-state__description">{error}</p>
          <a href="/sessions" className="btn btn--primary">Back to Sessions</a>
        </div>
      </>
    );
  }

  if (!error && (!diffData.files || diffData.files.length === 0)) {
    return (
      <>
        <WorkspacesList
          workspaceId={workspaceId}
          showControls={false}
        />
        <div className="empty-state">
          <h3 className="empty-state__title">No changes in workspace</h3>
          <p className="empty-state__description">This workspace has no uncommitted changes</p>
          <a href="/sessions" className="btn btn--primary">Back to Sessions</a>
        </div>
      </>
    );
  }

  return (
    <>
      <WorkspacesList
        workspaceId={workspaceId}
        showControls={false}
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
