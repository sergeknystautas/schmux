import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import LinearSyncResolveConflictProgress from '../components/LinearSyncResolveConflictProgress';

export default function LinearSyncResolveConflictPage() {
  const { workspaceId, tabId } = useParams();
  const navigate = useNavigate();
  const { workspaces } = useSessions();

  const workspace = workspaces?.find((ws) => ws.id === workspaceId);
  const tab = workspace?.tabs?.find((t) => t.id === tabId);
  const conflictHash = tab?.meta?.hash;
  const displayHash = conflictHash ? conflictHash.slice(0, 7) : '';
  const resolveConflict = workspace?.resolve_conflicts?.find((item) => item.hash === conflictHash);

  // Navigate home if workspace was disposed
  useEffect(() => {
    if (workspaceId && workspaces?.length > 0 && !workspace) {
      navigate('/');
    }
  }, [workspaceId, workspaces, workspace, navigate]);

  if (!workspace || !workspaceId) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading...</span>
      </div>
    );
  }

  return (
    <>
      <WorkspaceHeader workspace={workspace} />
      <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
      <div className="spawn-content">
        {resolveConflict ? (
          <LinearSyncResolveConflictProgress
            workspaceId={workspaceId}
            resolveConflict={resolveConflict}
            displayHash={displayHash}
          />
        ) : (
          <div className="loading-state">
            <span>Conflict record unavailable.</span>
          </div>
        )}
      </div>
    </>
  );
}
