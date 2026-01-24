import { useEffect, useState, useRef, useCallback } from 'react';
import { useParams, Link } from 'react-router-dom';
import ReactDiffViewer from 'react-diff-viewer-continued';
import { getDiff, diffExternal, getErrorMessage } from '../lib/api';
import useTheme from '../hooks/useTheme';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import useLocalStorage from '../hooks/useLocalStorage';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import type { DiffResponse } from '../lib/types';

type ExternalDiffCommand = {
  name: string;
  command: string;
};

// Built-in diff commands (always available)
const BUILTIN_DIFF_COMMANDS: ExternalDiffCommand[] = [
  { name: 'VS Code', command: 'code --diff "$LOCAL" "$REMOTE"' }
];

const DIFF_SIDEBAR_WIDTH_KEY = 'schmux-diff-sidebar-width';
const DEFAULT_SIDEBAR_WIDTH = 300;
const MIN_SIDEBAR_WIDTH = 150;
const MAX_SIDEBAR_WIDTH = 600;

export default function DiffPage() {
  const { workspaceId } = useParams();
  const { theme } = useTheme();
  const { config } = useConfig();
  const { workspaces } = useSessions();
  const [diffData, setDiffData] = useState<DiffResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selectedFileIndex, setSelectedFileIndex] = useState(0);
  const [executingDiff, setExecutingDiff] = useState<string | null>(null);
  const [diffResult, setDiffResult] = useState<{ success: boolean; message: string } | null>(null);
  const [sidebarWidth, setSidebarWidth] = useLocalStorage<number>(DIFF_SIDEBAR_WIDTH_KEY, DEFAULT_SIDEBAR_WIDTH);
  const [isResizing, setIsResizing] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const workspace = workspaces?.find(ws => ws.id === workspaceId);
  const externalDiffCommands = config?.external_diff_commands || [];

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    setIsResizing(true);
  }, []);

  const handleMouseMove = useCallback((e: MouseEvent) => {
    if (!isResizing || !containerRef.current) return;
    const containerRect = containerRef.current.getBoundingClientRect();
    const newWidth = e.clientX - containerRect.left;
    const clampedWidth = Math.max(MIN_SIDEBAR_WIDTH, Math.min(MAX_SIDEBAR_WIDTH, newWidth));
    setSidebarWidth(clampedWidth);
  }, [isResizing, setSidebarWidth]);

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

  const handleExternalDiff = async (cmd: ExternalDiffCommand) => {
    if (!workspaceId) return;
    setExecutingDiff(cmd.name);
    try {
      const response = await diffExternal(workspaceId, cmd.command);
      setDiffResult({ success: response.success, message: response.message });
    } catch (err) {
      setDiffResult({ success: false, message: getErrorMessage(err, 'Failed to open diff tool') });
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
          <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
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
        <div className="empty-state">
          <h3 className="empty-state__title">No changes in workspace</h3>
          <p className="empty-state__description">This workspace has no uncommitted changes</p>
          <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
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
      directory: fullPath.substring(0, lastSlash + 1)
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
        <div className="diff-actions">
          <span className="diff-actions__label">Open in:</span>
          {BUILTIN_DIFF_COMMANDS.map((cmd) => (
            <button
              key={`builtin-${cmd.name}`}
              className="btn btn--sm btn--ghost btn--bordered"
              onClick={() => handleExternalDiff(cmd)}
              disabled={executingDiff !== null}
            >
              {executingDiff === cmd.name ? <div className="spinner--small"></div> : cmd.name}
            </button>
          ))}
          {hasUserCommands && externalDiffCommands.map((cmd) => (
            <button
              key={cmd.name}
              className="btn btn--sm btn--ghost btn--bordered"
              onClick={() => handleExternalDiff(cmd)}
              disabled={executingDiff !== null}
            >
              {executingDiff === cmd.name ? <div className="spinner--small"></div> : cmd.name}
            </button>
          ))}
        </div>

        <div className="diff-layout" ref={containerRef}>
          <div className="diff-sidebar" style={{ width: `${sidebarWidth}px`, flexShrink: 0 }}>
            <h3 className="diff-sidebar__title">Changed Files ({diffData?.files?.length || 0})</h3>
            <div className="diff-file-list">
              {diffData?.files?.map((file, index) => {
                const { filename, directory } = splitPath(file.new_path || file.old_path);
                return (
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
                      <span className="diff-file-item__name">{filename}</span>
                      {directory && <span className="diff-file-item__dir">{directory}</span>}
                    </div>
                    <span className="diff-file-item__stats">
                      {file.lines_added > 0 && <span style={{ color: 'var(--color-success)' }}>+{file.lines_added}</span>}
                      {file.lines_removed > 0 && <span style={{ color: 'var(--color-error)', marginLeft: file.lines_added > 0 ? '4px' : '0' }}>-{file.lines_removed}</span>}
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
      </div>

      {diffResult && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="diff-modal-title">
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="diff-modal-title">
                {diffResult.success ? 'Diff tool opened' : 'Failed to open diff tool'}
              </h2>
            </div>
            <div className="modal__body">
              <p>{diffResult.message}</p>
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={() => setDiffResult(null)}>
                OK
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
