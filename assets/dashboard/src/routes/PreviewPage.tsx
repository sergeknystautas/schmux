import { useEffect, useMemo, useRef } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import { useSessions } from '../contexts/SessionsContext';
import { hidePreviewIframes, showPreviewIframe } from '../lib/previewKeepAlive';

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
  const previewUrl = preview?.url;

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
      <div style={{ flex: 1, minHeight: 0, display: 'flex' }}>
        <div ref={mountRef} style={{ width: '100%', height: '100%' }} />
      </div>
    </>
  );
}
