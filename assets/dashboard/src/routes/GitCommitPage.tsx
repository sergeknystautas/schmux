import { useEffect, useState } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import { getCommitDetail, getErrorMessage } from '../lib/api';
import useTheme from '../hooks/useTheme';
import { useSessions } from '../contexts/SessionsContext';
import useSidebarLayout from '../hooks/useSidebarLayout';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import { formatRelativeTime, splitPath } from '../lib/utils';
import type { GitCommitDetailResponse } from '../lib/types';

const COMMIT_SIDEBAR_WIDTH_KEY = 'schmux-commit-sidebar-width';
const COMMIT_KEYBOARD_FOCUS_KEY = 'schmux-commit-keyboard-focus';
const MAX_MESSAGE_LINES = 3;

export default function GitCommitPage() {
  const { workspaceId, commitHash } = useParams();
  const navigate = useNavigate();
  const { theme } = useTheme();
  const { workspaces } = useSessions();
  const [commitData, setCommitData] = useState<GitCommitDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);
  const [messageExpanded, setMessageExpanded] = useState(false);
  const {
    sidebarWidth,
    isResizing,
    keyboardFocus,
    containerRef,
    contentRef,
    handleMouseDown,
    handleSidebarFocus,
    handleContentFocus,
  } = useSidebarLayout({
    widthKey: COMMIT_SIDEBAR_WIDTH_KEY,
    focusKey: COMMIT_KEYBOARD_FOCUS_KEY,
    fileCount: commitData?.files?.length || 0,
    selectedFileIndex,
    onSelectFile: setSelectedFileIndex,
    vimKeys: true,
  });

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);

  // Navigate home if workspace was disposed
  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  // Load commit data
  useEffect(() => {
    const loadCommit = async () => {
      if (!workspaceId || !commitHash) return;
      setLoading(true);
      setError('');
      try {
        const data = await getCommitDetail(workspaceId, commitHash);
        setCommitData(data);
        setSelectedFileIndex(0);
      } catch (err) {
        setError(getErrorMessage(err, 'Failed to load commit'));
      } finally {
        setLoading(false);
      }
    };
    loadCommit();
  }, [workspaceId, commitHash]);

  const selectedFile = commitData?.files?.[selectedFileIndex];

  // Count message lines
  const messageLines = commitData?.message?.split('\n') || [];
  const needsTruncation = messageLines.length > MAX_MESSAGE_LINES;
  const displayMessage = messageExpanded
    ? commitData?.message
    : messageLines.slice(0, MAX_MESSAGE_LINES).join('\n');

  // Loading state with workspace header
  if (loading && !workspace) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading commit...</span>
      </div>
    );
  }

  if (error) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeGitTab />
          </>
        )}
        <div className="empty-state">
          <div className="empty-state__icon">!!!</div>
          <h3 className="empty-state__title">Failed to load commit</h3>
          <p className="empty-state__description">{error}</p>
          <Link to={`/git/${workspaceId}`} className="btn btn--primary">
            Back to Graph
          </Link>
        </div>
      </>
    );
  }

  if (loading) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeGitTab />
          </>
        )}
        <div className="diff-page">
          <div className="loading-state">
            <div className="spinner"></div>
            <span>Loading commit...</span>
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
          <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeGitTab />
        </>
      )}

      <div className="diff-page">
        {/* Commit header */}
        <div className="commit-header">
          <div className="commit-header__nav">
            <Link to={`/git/${workspaceId}`} className="commit-header__back">
              Graph
            </Link>
            <span className="commit-header__hash">{commitData?.short_hash}</span>
            {commitData?.is_merge && (
              <span className="badge badge--neutral commit-header__merge-badge">
                Merge commit (diff vs first parent)
              </span>
            )}
          </div>
          <div className="commit-header__meta">
            <span className="commit-header__author">{commitData?.author_name}</span>
            <span className="commit-header__sep">*</span>
            <span className="commit-header__date">
              {formatRelativeTime(commitData?.timestamp || '')}
            </span>
          </div>
          <div className="commit-header__message">
            <pre className="commit-header__message-text">{displayMessage}</pre>
            {needsTruncation && (
              <button
                className="commit-header__expand-btn"
                onClick={() => setMessageExpanded(!messageExpanded)}
              >
                {messageExpanded ? 'Show less' : `Show all ${messageLines.length} lines`}
              </button>
            )}
          </div>
        </div>

        {/* Diff layout */}
        {!commitData?.files || commitData.files.length === 0 ? (
          <div className="empty-state" style={{ flex: 1 }}>
            <h3 className="empty-state__title">No files changed</h3>
            <p className="empty-state__description">This commit has no file changes</p>
          </div>
        ) : (
          <div className="diff-layout" ref={containerRef}>
            <div
              className={`diff-sidebar${keyboardFocus === 'left' ? ' diff-sidebar--focused' : ''}`}
              style={{ width: `${sidebarWidth}px`, flexShrink: 0 }}
              onClick={handleSidebarFocus}
            >
              <h3 className="diff-sidebar__title">
                Changed Files ({commitData?.files?.length || 0})
              </h3>
              <div className="diff-file-list" data-testid="diff-file-list">
                {commitData?.files?.map((file, index) => {
                  const path = file.new_path || file.old_path || '';
                  const { filename, directory } = splitPath(path);
                  const status = file.status || 'modified';
                  const statusIndicator =
                    status === 'added'
                      ? 'A'
                      : status === 'deleted'
                        ? 'D'
                        : status === 'renamed'
                          ? 'R'
                          : status === 'copied'
                            ? 'C'
                            : 'M';
                  const statusClass =
                    status === 'added'
                      ? 'diff-file-item__status--added'
                      : status === 'deleted'
                        ? 'diff-file-item__status--deleted'
                        : 'diff-file-item__status--modified';
                  return (
                    <button
                      key={path || index}
                      className={`diff-file-item${selectedFileIndex === index ? ' diff-file-item--active' : ''}`}
                      onClick={() => setSelectedFileIndex(index)}
                      data-testid={`diff-file-${index}`}
                    >
                      <div className="diff-file-item__info">
                        <span className={`diff-file-item__status ${statusClass}`}>
                          {statusIndicator}
                        </span>
                        <span className="diff-file-item__name">{filename}</span>
                        {directory && <span className="diff-file-item__dir">{directory}</span>}
                      </div>
                      <span className="diff-file-item__stats">
                        {file.lines_added > 0 && (
                          <span style={{ color: 'var(--color-success)' }}>+{file.lines_added}</span>
                        )}
                        {file.lines_removed > 0 && (
                          <span
                            style={{
                              color: 'var(--color-error)',
                              marginLeft: file.lines_added > 0 ? '4px' : '0',
                            }}
                          >
                            -{file.lines_removed}
                          </span>
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
              <div className="diff-sidebar__help">j/k navigate . switch</div>
            </div>

            <div
              className={`diff-resizer${isResizing ? ' diff-resizer--active' : ''}`}
              onMouseDown={handleMouseDown}
            />

            <div
              className={`diff-content${keyboardFocus === 'right' ? ' diff-content--focused' : ''}`}
              data-testid="diff-viewer"
              onClick={handleContentFocus}
            >
              {selectedFile && (
                <>
                  <div className="diff-content__header">
                    <h2 className="diff-content__title">
                      {selectedFile.new_path || selectedFile.old_path}
                    </h2>
                    <span
                      className={`badge badge--${selectedFile.status === 'added' ? 'success' : selectedFile.status === 'deleted' ? 'danger' : 'neutral'}`}
                    >
                      {selectedFile.status}
                    </span>
                  </div>
                  <div className="diff-viewer-wrapper" ref={contentRef}>
                    {selectedFile.is_binary ? (
                      <div className="diff-binary-notice">Binary file not shown</div>
                    ) : (
                      <ReactDiffViewer
                        oldValue={selectedFile.old_content || ''}
                        newValue={selectedFile.new_content || ''}
                        splitView={false}
                        useDarkTheme={theme === 'dark'}
                        hideLineNumbers={false}
                        showDiffOnly={true}
                        compareMethod={DiffMethod.DIFF_TRIMMED_LINES}
                        disableWordDiff={true}
                        extraLinesSurroundingDiff={3}
                      />
                    )}
                  </div>
                </>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  );
}
