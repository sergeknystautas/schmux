import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useParams, Link, useNavigate, useLocation } from 'react-router';
import { getFileContent, getWorkspaceFileUrl, getErrorMessage } from '../lib/api';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import Tooltip from '../components/Tooltip';
import CopyButton from '../components/CopyButton';
import { DownloadIcon } from '../components/Icons';
import MarkdownViewer from '../components/markdown/MarkdownViewer';
import MarkdownEditor from '../components/markdown/MarkdownEditor';
import useMarkdownDocument from '../hooks/useMarkdownDocument';
import styles from '../styles/markdownEditor.module.css';

const VIEWER_FALLBACK_REASONS = new Set(['too_large', 'not_utf8']);

// Controller for /diff/:workspaceId/md/:filepath. Local workspaces get the
// editor over its WebSocket; remote workspaces, and files the editor refuses,
// get the read-only viewer over GET /api/file.
export default function MarkdownPreviewPage() {
  const { workspaceId = '', filepath = '' } = useParams();
  const navigate = useNavigate();
  const { workspaces, loading } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const isRemote = Boolean(workspace?.remote_host_id);

  // Same behavior as before the editor: once the workspace list has loaded,
  // a workspace that no longer exists sends the user home. `workspaces` starts
  // as [] before the first dashboard snapshot, so `loading` is the gate.
  useEffect(() => {
    if (!loading && workspaceId && !workspace) navigate('/');
  }, [loading, workspaceId, workspace, navigate]);

  if (!workspaceId || !filepath) return null;
  if (isRemote) {
    return <ViewerPage workspaceId={workspaceId} filePath={filepath} />;
  }
  return <EditorPage workspaceId={workspaceId} filePath={filepath} />;
}

function Frame({
  workspaceId,
  filePath,
  status,
  children,
}: {
  workspaceId: string;
  filePath: string;
  status?: { text: string; error: boolean };
  children: ReactNode;
}) {
  const { workspaces } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  return (
    <>
      {workspace && (
        <>
          <WorkspaceHeader workspace={workspace} />
          <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
        </>
      )}
      <div className="diff-page">
        <div className="diff-content diff-content--standalone">
          <div className="diff-content__header">
            <h2 className="diff-content__title">
              {filePath}
              <CopyButton
                text={filePath}
                label="path"
                className="copy-field__btn"
                testId="copy-path-btn"
              />
              <Tooltip content="Download Markdown file">
                <a
                  className="copy-field__btn"
                  data-testid="download-markdown"
                  aria-label="Download Markdown file"
                  href={getWorkspaceFileUrl(workspaceId, filePath)}
                  download={filePath.split('/').pop() || 'file.md'}
                >
                  {DownloadIcon}
                </a>
              </Tooltip>
              {status && (
                <span
                  className={
                    status.error ? `${styles.status} ${styles.statusError}` : styles.status
                  }
                  data-testid="markdown-status"
                  role="status"
                >
                  {status.text}
                </span>
              )}
            </h2>
          </div>
          {children}
        </div>
      </div>
    </>
  );
}

function EditorPage({ workspaceId, filePath }: { workspaceId: string; filePath: string }) {
  const { draft, status, reason, onEdit } = useMarkdownDocument(workspaceId, filePath);
  if (status === 'error' && reason && VIEWER_FALLBACK_REASONS.has(reason)) {
    return <ViewerPage workspaceId={workspaceId} filePath={filePath} notice={reason} />;
  }
  const statusText =
    status === 'saving'
      ? 'Saving…'
      : status === 'saved'
        ? 'Saved'
        : status === 'error'
          ? reason || 'Disconnected'
          : 'Connecting…';
  return (
    <Frame
      workspaceId={workspaceId}
      filePath={filePath}
      status={{ text: statusText, error: status === 'error' }}
    >
      <MarkdownEditor
        value={draft}
        onChange={onEdit}
        workspaceId={workspaceId}
        filePath={filePath}
      />
    </Frame>
  );
}

const getMarkdownScrollPositionKey = (workspaceId: string, filepath: string) =>
  `schmux-markdown-scroll-position-${workspaceId}-${filepath}`;

function ViewerPage({
  workspaceId,
  filePath,
  notice,
}: {
  workspaceId: string;
  filePath: string;
  notice?: string;
}) {
  const location = useLocation();
  const { workspaces } = useSessions();
  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const prevGitStatsRef = useRef<{ files: number; added: number; removed: number } | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  const loadFile = async () => {
    setLoading(true);
    setError('');
    try {
      setContent(await getFileContent(workspaceId, filePath));
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load file'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadFile();
  }, [workspaceId, filePath, location.key]);

  // The viewer keeps the VCS-counter refetch; the editor path does not need it.
  useEffect(() => {
    if (!workspace) return;
    const current = {
      files: workspace.files_changed,
      added: workspace.lines_added,
      removed: workspace.lines_removed,
    };
    const prev = prevGitStatsRef.current;
    if (
      prev !== null &&
      (prev.files !== current.files ||
        prev.added !== current.added ||
        prev.removed !== current.removed)
    ) {
      loadFile();
    }
    prevGitStatsRef.current = current;
  }, [workspace, workspaceId]);

  useEffect(() => {
    if (!contentRef.current || !content) return;
    const scrollEl = contentRef.current;
    const key = getMarkdownScrollPositionKey(workspaceId, filePath);
    const handleScroll = () => localStorage.setItem(key, scrollEl.scrollTop.toString());
    scrollEl.addEventListener('scroll', handleScroll);
    // Expose listener-attached state for tests so they can await this
    // transition once instead of polling the listener side-effect.
    scrollEl.dataset.scrollListenerReady = 'true';
    const saved = localStorage.getItem(key);
    if (saved)
      requestAnimationFrame(() => {
        scrollEl.scrollTop = parseInt(saved, 10);
      });
    return () => {
      scrollEl.removeEventListener('scroll', handleScroll);
      delete scrollEl.dataset.scrollListenerReady;
    };
  }, [workspaceId, filePath, content]);

  if (loading) {
    return (
      <Frame workspaceId={workspaceId} filePath={filePath}>
        <div className="loading-state flex-1">
          <div className="spinner"></div>
          <span>Loading preview...</span>
        </div>
      </Frame>
    );
  }
  if (error) {
    return (
      <Frame workspaceId={workspaceId} filePath={filePath}>
        <div className="empty-state flex-1">
          <div className="empty-state__icon">!</div>
          <h3 className="empty-state__title">Failed to load preview</h3>
          <p className="empty-state__description">{error}</p>
          <Link to={`/diff/${workspaceId}`} className="btn btn--primary">
            Back to Diff
          </Link>
        </div>
      </Frame>
    );
  }
  return (
    <Frame
      workspaceId={workspaceId}
      filePath={filePath}
      status={notice ? { text: `Read-only: ${notice}`, error: true } : undefined}
    >
      <div className="diff-viewer-wrapper" ref={contentRef}>
        <MarkdownViewer workspaceId={workspaceId} filePath={filePath} content={content} />
      </div>
    </Frame>
  );
}
