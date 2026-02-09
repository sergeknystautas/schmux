import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import LinearSyncResolveConflictProgress from '../components/LinearSyncResolveConflictProgress';

export default function LinearSyncResolveConflictPage() {
  const { workspaceId } = useParams();
  const navigate = useNavigate();
  const { workspaces, linearSyncResolveConflictStates } = useSessions();

  const workspace = workspaces?.find(ws => ws.id === workspaceId);
  const crState = workspaceId ? linearSyncResolveConflictStates[workspaceId] : undefined;

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
      <SessionTabs
        sessions={workspace.sessions || []}
        workspace={workspace}
        activeLinearSyncResolveConflictTab
      />
      <div className="spawn-content">
        {crState ? (
          <LinearSyncResolveConflictProgress workspaceId={workspaceId} />
        ) : (
          <div className="loading-state">
            <div className="spinner"></div>
            <span>Starting conflict resolution...</span>
          </div>
        )}
      </div>
    </>
  );
}
