import { useEffect, useState, useRef, useCallback } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import { getCommitDetail, getErrorMessage } from '../lib/api';
import useTheme from '../hooks/useTheme';
import { useSessions } from '../contexts/SessionsContext';
import useLocalStorage from '../hooks/useLocalStorage';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import type { GitCommitDetailResponse } from '../lib/types';

const COMMIT_SIDEBAR_WIDTH_KEY = 'schmux-commit-sidebar-width';
const COMMIT_KEYBOARD_FOCUS_KEY = 'schmux-commit-keyboard-focus';
const DEFAULT_SIDEBAR_WIDTH = 300;
const MIN_SIDEBAR_WIDTH = 150;
const MAX_SIDEBAR_WIDTH = 600;
const MAX_MESSAGE_LINES = 3;

// Format relative time (e.g., "2d ago", "3h ago")
function formatRelativeTime(timestamp: string): string {
  const date = new Date(timestamp);
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffSecs = Math.floor(diffMs / 1000);
  const diffMins = Math.floor(diffSecs / 60);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffDays > 0) return `${diffDays}d ago`;
  if (diffHours > 0) return `${diffHours}h ago`;
  if (diffMins > 0) return `${diffMins}m ago`;
  return 'just now';
}

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
  const [sidebarWidth, setSidebarWidth] = useLocalStorage<number>(
    COMMIT_SIDEBAR_WIDTH_KEY,
    DEFAULT_SIDEBAR_WIDTH
  );
  const [isResizing, setIsResizing] = useState(false);
  const [keyboardFocus, setKeyboardFocus] = useLocalStorage<'left' | 'right' | null>(
    COMMIT_KEYBOARD_FOCUS_KEY,
    'left'
  );
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);

  // Navigate home if workspace was disposed
  useEffect(() => {
    if (!loading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, workspaceId, workspaceExists, navigate]);

  // Resize handlers
  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsResizing(true);
  }, []);

  const handleMouseMove = useCallback(
    (e: MouseEvent) => {
      if (!isResizing || !containerRef.current) return;
      const containerRect = containerRef.current.getBoundingClientRect();
      const newWidth = e.clientX - containerRect.left;
      const clampedWidth = Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, newWidth));
      setSidebarWidth(clampedWidth);
    },
    [isResizing, setSidebarWidth]
  );

  const handleMouseUp = useCallback(() => {
    setIsResizing(false);
  }, []);

  useEffect(() => {
    if (isResizing) {
      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
    }
    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };
  }, [isResizing, handleMouseMove, handleMouseUp]);

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const files = commitData?.files || [];

      switch (e.key) {
        case 'ArrowLeft':
          e.preventDefault();
          setKeyboardFocus('left');
          break;
        case 'ArrowRight':
          e.preventDefault();
          setKeyboardFocus('right');
          break;
        case 'ArrowUp':
        case 'k':
          if (keyboardFocus === 'left' && selectedFileIndex > 0) {
            e.preventDefault();
            setSelectedFileIndex(selectedFileIndex - 1);
          } else if (keyboardFocus === 'right' && contentRef.current) {
            e.preventDefault();
            contentRef.current.scrollBy({ top: -100, behavior: 'smooth' });
          }
          break;
        case 'ArrowDown':
        case 'j':
          if (keyboardFocus === 'left' && selectedFileIndex < files.length - 1) {
            e.preventDefault();
            setSelectedFileIndex(selectedFileIndex + 1);
          } else if (keyboardFocus === 'right' && contentRef.current) {
            e.preventDefault();
            contentRef.current.scrollBy({ top: 100, behavior: 'smooth' });
          }
          break;
      }
    };

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [keyboardFocus, selectedFileIndex, commitData, setKeyboardFocus]);

  // Auto-scroll sidebar to keep selected file visible
  useEffect(() => {
    if (keyboardFocus === 'left') {
      const fileListEl = document.querySelector('.diff-file-list');
      const activeFileEl = document.querySelector('.diff-file-item--active');
      if (fileListEl && activeFileEl) {
        activeFileEl.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
      }
    }
  }, [selectedFileIndex, keyboardFocus]);

  const handleSidebarFocus = () => setKeyboardFocus('left');
  const handleContentFocus = () => setKeyboardFocus('right');

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

  // Helper to split path into filename and directory
  const splitPath = (fullPath: string) => {
    const lastSlash = fullPath.lastIndexOf('/');
    if (lastSlash === -1) {
      return { filename: fullPath, directory: '' };
    }
    return {
      filename: fullPath.substring(lastSlash + 1),
      directory: fullPath.substring(0, lastSlash + 1),
    };
  };

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
          <div className="loading-state" style={{ flex: 1 }}>
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
                  return (
                    <button
                      key={index}
                      className={`diff-file-item${selectedFileIndex === index ? ' diff-file-item--active' : ''}`}
                      onClick={() => setSelectedFileIndex(index)}
                      data-testid={`diff-file-${index}`}
                    >
                      <div className="diff-file-item__info">
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path>
                          <polyline points="13 2 13 9 20 9"></polyline>
                        </svg>
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
