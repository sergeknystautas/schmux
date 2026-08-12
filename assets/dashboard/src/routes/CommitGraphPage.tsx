import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router';
import { useSessions } from '../contexts/SessionsContext';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import CommitHistoryDAG from '../components/CommitHistoryDAG';
import GitHubConnectBanner from '../components/GitHubConnectBanner';

export default function CommitGraphPage() {
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
      <SessionTabs sessions={workspace.sessions || []} workspace={workspace} />
      {workspace.repo?.startsWith('local:') && <GitHubConnectBanner workspaceId={workspaceId} />}
      <CommitHistoryDAG workspaceId={workspaceId} />
    </>
  );
}
