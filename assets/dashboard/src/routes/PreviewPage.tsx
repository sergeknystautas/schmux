import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import Tooltip from '../components/Tooltip';
import { useSessions } from '../contexts/SessionsContext';
import {
  goBackPreviewIframe,
  hidePreviewIframes,
  refreshPreviewIframe,
  showPreviewIframe,
} from '../lib/previewKeepAlive';

export default function PreviewPage() {
  const { workspaceId, previewId } = useParams();
  const navigate = useNavigate();
  const { workspaces } = useSessions();
  const mountRef = useRef<HTMLDivElement | null>(null);

  const workspace = useMemo(
    () => workspaces?.find((ws) => ws.id === workspaceId),
    [workspaces, workspaceId]
  );
  const preview = useMemo(
    () => workspace?.previews?.find((p) => p.id === previewId),
    [workspace, previewId]
  );

  useEffect(() => {
    if (!workspaceId || !workspace) {
      navigate('/');
      return;
    }
    if (!previewId || !preview) {
      const firstSession = workspace.sessions?.[0];
      navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
    }
  }, [workspaceId, workspace, previewId, preview, navigate]);

  const previewId_ = preview?.id;
  const previewProxyPort = preview?.proxy_port;
  const previewUrl = useMemo(
    () => (previewProxyPort ? `${window.location.protocol}//${window.location.hostname}:${previewProxyPort}` : null),
    [previewProxyPort]
  );

  useEffect(() => {
    if (!previewId_ || !previewUrl || !mountRef.current) return;
    const mount = mountRef.current;
    const updateViewport = () => {
      const rect = mount.getBoundingClientRect();
      showPreviewIframe(previewId_, previewUrl, {
        left: rect.left,
        top: rect.top,
        width: rect.width,
        height: rect.height,
      });
    };
    updateViewport();

    const observer = new ResizeObserver(() => updateViewport());
    observer.observe(mount);
    window.addEventListener('resize', updateViewport);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', updateViewport);
      hidePreviewIframes();
    };
  }, [previewId_, previewUrl]);

  const handleBack = useCallback(() => {
    if (previewId_) goBackPreviewIframe(previewId_);
  }, [previewId_]);

  const handleRefresh = useCallback(() => {
    if (previewId_) refreshPreviewIframe(previewId_);
  }, [previewId_]);

  if (!workspace) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading preview...</span>
      </div>
    );
  }

  return (
    <>
      <WorkspaceHeader workspace={workspace} />
      <SessionTabs
        sessions={workspace.sessions || []}
        workspace={workspace}
        activePreviewId={preview?.id}
      />
      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          flexDirection: 'column',
          backgroundColor: 'var(--color-surface)',
          border: '1px solid var(--color-border)',
        }}
      >
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 'var(--spacing-sm)',
            padding: 'var(--spacing-xs) var(--spacing-sm)',
            background: 'var(--color-surface-alt)',
            borderBottom: '1px solid var(--color-border-subtle)',
          }}
        >
          <Tooltip content="Go back">
            <button onClick={handleBack} className="btn btn--sm">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="15 18 9 12 15 6"></polyline>
              </svg>
            </button>
          </Tooltip>
          <Tooltip content="Refresh">
            <button onClick={handleRefresh} className="btn btn--sm">
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="23 4 23 10 17 10"></polyline>
                <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"></path>
              </svg>
            </button>
          </Tooltip>
          {previewUrl && (
            <Tooltip content="Open in new tab">
              <a
                href={previewUrl}
                target="_blank"
                rel="noopener noreferrer"
                style={{
                  flex: 1,
                  padding: 'var(--spacing-xs) var(--spacing-sm)',
                  background: 'var(--color-surface)',
                  border: '1px solid var(--color-border-subtle)',
                  borderRadius: 'var(--radius-sm)',
                  fontFamily: 'var(--font-mono)',
                  fontSize: '12px',
                  color: 'var(--color-text-muted)',
                  textDecoration: 'none',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                  whiteSpace: 'nowrap',
                }}
              >
                {previewUrl}
              </a>
            </Tooltip>
          )}
        </div>
        <div ref={mountRef} style={{ flex: 1, minHeight: 0 }} />
      </div>
    </>
  );
}
