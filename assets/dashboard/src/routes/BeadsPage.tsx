import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { getSessions, spawnSessions, getErrorMessage } from '../lib/api';
import { useToast } from '../components/ToastProvider';
import { useSessions } from '../contexts/SessionsContext';
import type { WorkspaceResponse } from '../lib/types';

export interface BeadTask {
  id: string;
  title: string;
  description?: string;
  priority: number;
  status: string;
  assignee?: string;
}

export interface BeadsTasksResponse {
  workspace_id: string;
  tasks: BeadTask[];
}

export default function BeadsPage() {
  const navigate = useNavigate();
  const { error: toastError, success: toastSuccess } = useToast();
  const { workspaces, refresh } = useSessions();

  const [selectedWorkspaceId, setSelectedWorkspaceId] = useState<string>('');
  const [beadsTasks, setBeadsTasks] = useState<BeadTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [spawning, setSpawning] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bdNotInstalled, setBdNotInstalled] = useState(false);

  const workspacesWithBeads = workspaces.filter((ws: WorkspaceResponse) => (ws as any).has_beads === true);
  const selectedWorkspace = workspaces.find((ws: WorkspaceResponse) => ws.id === selectedWorkspaceId);

  useEffect(() => {
    if (selectedWorkspaceId) {
      fetchBeadsTasks();
    }
  }, [selectedWorkspaceId]);

  const fetchBeadsTasks = async () => {
    if (!selectedWorkspaceId) return;

    setLoading(true);
    setError(null);
    setBdNotInstalled(false);

    try {
      const response = await fetch(`/api/beads-tasks?workspace_id=${encodeURIComponent(selectedWorkspaceId)}`);

      if (response.status === 404) {
        setBdNotInstalled(true);
        setBeadsTasks([]);
        return;
      }

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({ error: 'Failed to fetch beads tasks' }));
        throw new Error(errorData.error || 'Failed to fetch beads tasks');
      }

      const data: BeadsTasksResponse = await response.json();
      setBeadsTasks(data.tasks || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to fetch beads tasks'));
    } finally {
      setLoading(false);
    }
  };

  const handleSpawn = async (taskId: string, title: string) => {
    if (!selectedWorkspace) return;

    setSpawning(true);
    try {
      const results = await spawnSessions({
        repo: selectedWorkspace.repo,
        branch: selectedWorkspace.branch,
        prompt: `Working on bead ${taskId}: ${title}\n\nPlease run 'bd show ${taskId}' to see full details.`,
        nickname: `${taskId}`,
        targets: { claude: 1 },
        workspace_id: selectedWorkspace.id,
      });

      const failed = results.filter(r => r.error);
      if (failed.length > 0) {
        toastError(`Failed to spawn ${failed.length} session(s)`);
      } else {
        toastSuccess(`Session spawned for bead ${taskId}`);
      }

      // Refresh sessions and navigate to the new session
      await refresh();

      const spawned = results.find(r => r.session_id);
      if (spawned) {
        navigate(`/sessions/${spawned.session_id}`);
      }
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to spawn session'));
    } finally {
      setSpawning(false);
    }
  };

  const getPriorityLabel = (priority: number): string => {
    if (priority === 0) return 'P0';
    if (priority === 1) return 'P1';
    if (priority === 2) return 'P2';
    return `P${priority}`;
  };

  const getPriorityBadgeClass = (priority: number): string => {
    if (priority === 0) return 'badge--danger';
    if (priority === 1) return 'badge--warning';
    return 'badge--neutral';
  };

  return (
    <div className="page-header">
      <div className="page-header__title">Beads Tasks</div>
      <div className="page-header__actions">
        <button className="btn btn--ghost" onClick={() => navigate('/spawn')}>
          Back to Spawn
        </button>
      </div>

      {/* Workspace Selector */}
      <div className="card" style={{ marginTop: '24px' }}>
        <div className="card__header">
          <h3>Select Workspace</h3>
        </div>
        <div className="card__body">
          {workspacesWithBeads.length === 0 ? (
            <div className="empty-state">
              <div className="empty-state__title">No Beads Workspaces</div>
              <div className="empty-state__description">
                No workspaces with beads initialized found. Run <code>bd init</code> in a workspace directory to get started.
              </div>
            </div>
          ) : (
            <div className="form-group">
              <label className="form-group__label">Workspace</label>
              <select
                className="select"
                value={selectedWorkspaceId}
                onChange={(e) => setSelectedWorkspaceId(e.target.value)}
              >
                <option value="">-- Select a workspace --</option>
                {workspacesWithBeads.map((ws) => (
                  <option key={ws.id} value={ws.id}>
                    {ws.repo} ({ws.branch})
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
      </div>

      {/* Tasks List */}
      {selectedWorkspace && (
        <div className="card" style={{ marginTop: '16px' }}>
          <div className="card__header">
            <h3>Ready Tasks - {selectedWorkspace.repo}</h3>
          </div>
          <div className="card__body">
            {loading ? (
              <div className="loading-state">
                <div className="spinner"></div>
                <span>Loading tasks...</span>
              </div>
            ) : bdNotInstalled ? (
              <div className="banner banner--warning">
                The <code>bd</code> command is not installed. Please install beads to use this feature.
                <br />
                <a href="https://github.com/steveyegne/beads" target="_blank" rel="noopener noreferrer">
                  View beads on GitHub
                </a>
              </div>
            ) : error ? (
              <div className="banner banner--error">
                {error}
              </div>
            ) : beadsTasks.length === 0 ? (
              <div className="empty-state">
                <div className="empty-state__title">No Ready Tasks</div>
                <div className="empty-state__description">
                  There are no unblocked tasks in this workspace. All tasks may have dependencies or blockers.
                </div>
              </div>
            ) : (
              <table className="session-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>Title</th>
                    <th>Priority</th>
                    <th>Status</th>
                    <th>Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {beadsTasks.map((task) => (
                    <tr key={task.id}>
                      <td className="mono">{task.id}</td>
                      <td>
                        <div>
                          <div style={{ fontWeight: 500 }}>{task.title}</div>
                          {task.description && (
                            <div className="text-muted" style={{ fontSize: '0.875rem', marginTop: '4px' }}>
                              {task.description}
                            </div>
                          )}
                        </div>
                      </td>
                      <td>
                        <span className={`badge ${getPriorityBadgeClass(task.priority)}`}>
                          {getPriorityLabel(task.priority)}
                        </span>
                      </td>
                      <td>
                        <span className="badge badge--neutral">{task.status}</span>
                      </td>
                      <td>
                        <button
                          className="btn btn--primary btn--sm"
                          onClick={() => handleSpawn(task.id, task.title)}
                          disabled={spawning}
                        >
                          Spawn
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
