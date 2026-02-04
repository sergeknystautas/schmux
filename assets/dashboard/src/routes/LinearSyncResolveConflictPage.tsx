import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import LinearSyncResolveConflictProgress from '../components/LinearSyncResolveConflictProgress';

export default function LinearSyncResolveConflictPage() {
  const { workspaceId } = useParams();
  const navigate = useNavigate();
  const { workspaces, linearSyncResolveConflictStates } = useSessions();
  const [waitingForState, setWaitingForState] = useState(true);

  const workspace = workspaces?.find(ws => ws.id === workspaceId);
  const crState = workspaceId ? linearSyncResolveConflictStates[workspaceId] : undefined;

  useEffect(() => {
    setWaitingForState(true);
  }, [workspaceId]);

  // Give the WS broadcast time to deliver the state after navigation
  useEffect(() => {
    if (crState) {
      setWaitingForState(false);
      return;
    }
    const timer = setTimeout(() => setWaitingForState(false), 15000);
    return () => clearTimeout(timer);
  }, [crState]);

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
        ) : waitingForState ? (
          <div className="loading-state">
            <div className="spinner"></div>
            <span>Starting conflict resolution...</span>
          </div>
        ) : (
          <div className="empty-state">
            <h3 className="empty-state__title">No active conflict resolution</h3>
            <p className="empty-state__description">
              Start a conflict resolution from the git status menu.
            </p>
          </div>
        )}
      </div>
    </>
  );
}
