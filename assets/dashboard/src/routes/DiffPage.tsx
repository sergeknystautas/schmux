import { useEffect, useState, useRef } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import { getDiff, diffExternal, getErrorMessage, getWorkspaceFileUrl } from '../lib/api';
import useTheme from '../hooks/useTheme';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useModal } from '../components/ModalProvider';
import useSidebarLayout from '../hooks/useSidebarLayout';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import type { DiffResponse } from '../lib/types';

type ExternalDiffCommand = {
  name: string;
  command: string;
};

// Built-in diff commands (always available)
const BUILTIN_DIFF_COMMANDS: ExternalDiffCommand[] = [
  { name: 'VS Code', command: 'code --diff "$LOCAL" "$REMOTE"' },
];

const DIFF_SIDEBAR_WIDTH_KEY = 'schmux-diff-sidebar-width';
const DIFF_KEYBOARD_FOCUS_KEY = 'schmux-diff-keyboard-focus';

// Helper to get localStorage key for selected file (stores file path, not index)
const getSelectedFileKey = (workspaceId: string | undefined) =>
  `schmux-diff-selected-file-${workspaceId || ''}`;

// Helper to get localStorage key for scroll position
const getScrollPositionKey = (workspaceId: string | undefined) =>
  `schmux-diff-scroll-position-${workspaceId || ''}`;

export default function DiffPage() {
  const { workspaceId } = useParams();
  const navigate = useNavigate();
  const { theme } = useTheme();
  const { config } = useConfig();
  const { workspaces, loading: sessionsLoading, simulateRemote } = useSessions();
  const { alert } = useModal();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);
  const [executingDiff, setExecutingDiff] = useState<string | null>(null);
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);

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
    widthKey: DIFF_SIDEBAR_WIDTH_KEY,
    focusKey: DIFF_KEYBOARD_FOCUS_KEY,
    fileCount: diffData?.files?.length || 0,
    selectedFileIndex,
    onSelectFile: setSelectedFileIndex,
  });

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);
  const isRemoteClient =
    window.location.hostname !== 'localhost' && window.location.hostname !== '127.0.0.1';
  const isRemoteAccess = isRemoteClient || simulateRemote;
  const externalDiffCommands = config?.external_diff_commands || [];

  // Navigate home if workspace was disposed
  useEffect(() => {
    if (!loading && !sessionsLoading && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [loading, sessionsLoading, workspaceId, workspaceExists, navigate]);

  const handleExternalDiff = async (cmd: ExternalDiffCommand) => {
    if (!workspaceId) return;
    setExecutingDiff(cmd.name);
    try {
      const response = await diffExternal(workspaceId, cmd.command);
      const title = response.success ? 'Diff tool opened' : 'Failed to open diff tool';
      await alert(title, response.message);
    } catch (err) {
      await alert('Failed to open diff tool', getErrorMessage(err, 'Failed to open diff tool'));
    } finally {
      setExecutingDiff(null);
    }
  };

  useEffect(() => {
    const loadDiff = async () => {
      setLoading(true);
      setError('');
      try {
        const data = await getDiff(workspaceId || '');
        setDiffData(data);

        // Restore selected file from localStorage by file path (not index)
        const savedFilePath = localStorage.getItem(getSelectedFileKey(workspaceId));

        if (savedFilePath && data.files?.length > 0) {
          // Find the file by path (check new_path first, then old_path for deleted files)
          const foundIndex = data.files.findIndex(
            (f) => (f.new_path || f.old_path) === savedFilePath
          );
          if (foundIndex >= 0) {
            setSelectedFileIndex(foundIndex);
          } else {
            setSelectedFileIndex(0);
          }
        } else if (data.files?.length > 0) {
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

  // Reload diff data when workspace git stats change (file system changes)
  useEffect(() => {
    if (!workspace) return;

    const currentStats = {
      files: workspace.git_files_changed,
      added: workspace.git_lines_added,
      removed: workspace.git_lines_removed,
    };

    const prevStats = prevGitStatsRef.current;

    // Check if any git stat has changed
    const statsChanged =
      !prevStats ||
      prevStats.files !== currentStats.files ||
      prevStats.added !== currentStats.added ||
      prevStats.removed !== currentStats.removed;

    if (statsChanged && prevStats !== null) {
      // Git stats changed, reload diff data
      const reloadDiff = async () => {
        setLoading(true);
        setError('');
        try {
          const data = await getDiff(workspaceId || '');
          setDiffData(data);

          // Try to restore the same file by path if it still exists
          const currentFilePath =
            diffData?.files?.[selectedFileIndex]?.new_path ||
            diffData?.files?.[selectedFileIndex]?.old_path;

          if (currentFilePath && data.files?.length > 0) {
            const foundIndex = data.files.findIndex(
              (f) => (f.new_path || f.old_path) === currentFilePath
            );
            if (foundIndex >= 0) {
              setSelectedFileIndex(foundIndex);
            } else {
              setSelectedFileIndex(0);
            }
          } else {
            setSelectedFileIndex(0);
          }
        } catch (err) {
          setError(getErrorMessage(err, 'Failed to load diff'));
        } finally {
          setLoading(false);
        }
      };
      reloadDiff();
    }

    prevGitStatsRef.current = currentStats;
  }, [workspace, workspaceId, selectedFileIndex, diffData]);

  const selectedFile = diffData?.files?.[selectedFileIndex];

  // Save/restore scroll position - attach to diff-viewer-wrapper directly
  useEffect(() => {
    if (!contentRef.current || !selectedFile) return;

    const scrollEl = contentRef.current;

    // Save on scroll
    const handleScroll = () => {
      localStorage.setItem(getScrollPositionKey(workspaceId), scrollEl.scrollTop.toString());
    };
    scrollEl.addEventListener('scroll', handleScroll);

    // Restore saved position
    const saved = localStorage.getItem(getScrollPositionKey(workspaceId));
    if (saved) {
      requestAnimationFrame(() => {
        scrollEl.scrollTop = parseInt(saved, 10);
      });
    }

    return () => scrollEl.removeEventListener('scroll', handleScroll);
  }, [selectedFile, workspaceId]);

  // Save selected file path to localStorage when it changes
  useEffect(() => {
    const filePath =
      diffData?.files?.[selectedFileIndex]?.new_path ||
      diffData?.files?.[selectedFileIndex]?.old_path;
    if (filePath) {
      localStorage.setItem(getSelectedFileKey(workspaceId), filePath);
    }
  }, [selectedFileIndex, workspaceId, diffData]);

  // Only show loading spinner if we don't have workspace data yet
  // This prevents flash when navigating from session page (which has cached data)
  if (loading && !workspace) {
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
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
          </>
        )}
        <div className="empty-state">
          <div className="empty-state__icon">⚠️</div>
          <h3 className="empty-state__title">Failed to load diff</h3>
          <p className="empty-state__description">{error}</p>
          <Link to="/" className="btn btn--primary">
            Back to Home
          </Link>
        </div>
      </>
    );
  }

  // Only show "no changes" after loading completes
  if (!loading && !error && (!diffData?.files || diffData.files.length === 0)) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
          </>
        )}
        <div className="empty-state diff-tab-empty">
          <h3 className="empty-state__title">No changes in workspace</h3>
          <p className="empty-state__description">This workspace has no uncommitted changes</p>
          <Link to="/" className="btn btn--primary">
            Back to Home
          </Link>
        </div>
      </>
    );
  }

  const hasUserCommands = externalDiffCommands && externalDiffCommands.length > 0;

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

  // Show loading state inside the page structure (keeps header stable)
  if (loading) {
    return (
      <>
        {workspace && (
          <>
            <WorkspaceHeader workspace={workspace} />
            <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeDiffTab />
          </>
        )}
        <div className="diff-page">
          <div className="loading-state" style={{ flex: 1 }}>
            <div className="spinner"></div>
            <span>Loading diff...</span>
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
        {!isRemoteAccess && (
          <div className="diff-actions">
            <span className="diff-actions__label">Diff in:</span>
            {BUILTIN_DIFF_COMMANDS.map((cmd) => (
              <button
                key={`builtin-${cmd.name}`}
                className="btn btn--sm btn--ghost btn--bordered"
                onClick={() => handleExternalDiff(cmd)}
                disabled={executingDiff !== null}
              >
                {executingDiff === cmd.name ? (
                  <div className="spinner spinner--small"></div>
                ) : (
                  cmd.name
                )}
              </button>
            ))}
            {hasUserCommands &&
              externalDiffCommands.map((cmd) => (
                <button
                  key={cmd.name}
                  className="btn btn--sm btn--ghost btn--bordered"
                  onClick={() => handleExternalDiff(cmd)}
                  disabled={executingDiff !== null}
                >
                  {executingDiff === cmd.name ? (
                    <div className="spinner spinner--small"></div>
                  ) : (
                    cmd.name
                  )}
                </button>
              ))}
          </div>
        )}

        <div className="diff-layout" ref={containerRef}>
          <div
            className={`diff-sidebar${keyboardFocus === 'left' ? ' diff-sidebar--focused' : ''}`}
            style={{ width: `${sidebarWidth}px`, flexShrink: 0 }}
            onClick={handleSidebarFocus}
          >
            <h3 className="diff-sidebar__title">Changed Files ({diffData?.files?.length || 0})</h3>
            <div className="diff-file-list" data-testid="diff-file-list">
              {diffData?.files?.map((file, index) => {
                const { filename, directory } = splitPath(file.new_path || file.old_path || '');
                const status = file.status || 'modified';
                const statusIndicator =
                  status === 'added'
                    ? 'A'
                    : status === 'deleted'
                      ? 'D'
                      : status === 'untracked'
                        ? '?'
                        : 'M';
                const statusClass =
                  status === 'added' || status === 'untracked'
                    ? 'diff-file-item__status--added'
                    : status === 'deleted'
                      ? 'diff-file-item__status--deleted'
                      : 'diff-file-item__status--modified';
                return (
                  <button
                    key={index}
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
                    {/* Markdown preview: only for non-deleted files */}
                    {selectedFile.status !== 'deleted' &&
                      (selectedFile.new_path?.match(/\.(md|mdx)$/i) ||
                        selectedFile.old_path?.match(/\.(md|mdx)$/i)) && (
                        <Link
                          className="diff-content__preview-btn"
                          to={`/diff/${workspaceId}/md/${encodeURIComponent(selectedFile.new_path || '')}`}
                          title="Preview markdown"
                        >
                          Preview
                        </Link>
                      )}
                    {/* Image preview: only for non-deleted image files */}
                    {selectedFile.status !== 'deleted' &&
                      (selectedFile.new_path?.match(/\.(png|jpg|jpeg|webp|gif)$/i) ||
                        selectedFile.old_path?.match(/\.(png|jpg|jpeg|webp|gif)$/i)) && (
                        <Link
                          className="diff-content__preview-btn"
                          to={`/diff/${workspaceId}/img/${encodeURIComponent(selectedFile.new_path || '')}`}
                          title="Preview image"
                        >
                          Preview
                        </Link>
                      )}
                  </h2>
                  <span
                    className={`badge badge--${selectedFile.status === 'added' ? 'success' : selectedFile.status === 'deleted' ? 'danger' : 'neutral'}`}
                  >
                    {selectedFile.status}
                  </span>
                </div>
                <div className="diff-viewer-wrapper" ref={contentRef}>
                  {/* Show image thumbnail for image files that are not deleted */}
                  {selectedFile.status !== 'deleted' &&
                  (selectedFile.new_path?.match(/\.(png|jpg|jpeg|webp|gif)$/i) ||
                    selectedFile.old_path?.match(/\.(png|jpg|jpeg|webp|gif)$/i)) ? (
                    <div style={{ padding: '20px', textAlign: 'center' }}>
                      <img
                        src={getWorkspaceFileUrl(workspaceId || '', selectedFile.new_path || '')}
                        alt={selectedFile.new_path || ''}
                        style={{ maxWidth: '300px', maxHeight: '300px', objectFit: 'contain' }}
                      />
                    </div>
                  ) : selectedFile.is_binary ? (
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
      </div>
    </>
  );
}
