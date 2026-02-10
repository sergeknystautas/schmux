import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import GitHistoryDAG from '../components/GitHistoryDAG';

export default function GitGraphPage() {
  const { workspaceId } = useParams();
  const navigate = useNavigate();
  const { workspaces, loading } = useSessions();

  const workspace = workspaces.find((ws) => ws.id === workspaceId);

  useEffect(() => {
    if (!loading && !workspace) navigate('/');
  }, [loading, workspace, navigate]);

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
      <SessionTabs sessions={workspace.sessions || []} workspace={workspace} activeGitTab />

      <div style={{ marginTop: 'var(--spacing-sm)', flex: 1, minHeight: 0 }}>
        <GitHistoryDAG workspaceId={workspaceId} />
      </div>
    </>
  );
}
