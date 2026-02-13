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

  useEffect(() => {
    if (!preview || !mountRef.current) return;
    const { id, url } = preview;
    const mount = mountRef.current;
    const updateViewport = () => {
      const rect = mount.getBoundingClientRect();
      showPreviewIframe(id, url, {
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
  }, [preview]);

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
      <div style={{ height: 'calc(100vh - 176px)', minHeight: 320 }}>
        <div ref={mountRef} style={{ width: '100%', height: '100%' }} />
      </div>
    </>
  );
}
