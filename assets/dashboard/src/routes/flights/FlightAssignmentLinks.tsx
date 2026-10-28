import { Link } from 'react-router';
import { useSessions } from '../../contexts/SessionsContext';
import { roleLabel } from '../../lib/flights';
import { workspaceRoute } from '../../lib/navigation';
import type { FlightSessionAssignment, FlightWorkspaceAssignment } from '../../lib/types.generated';
import layout from './FlightLayout.module.css';

/**
 * Links the flight's workspace to the same route the sidebar would open. A
 * real link, styled like the session links, so it reads as clickable. A
 * workspace that no longer exists is shown as retained history.
 */
function FlightWorkspaceLink({ workspace }: { workspace: FlightWorkspaceAssignment }) {
  const { workspaces } = useSessions();
  if (!workspace.available) {
    return (
      <span className="text-muted" data-testid="flight-workspace-unavailable">
        <span className="mono">{workspace.workspace_id}</span> (unavailable)
      </span>
    );
  }
  return (
    <Link
      to={workspaceRoute(workspaces, workspace.workspace_id)}
      className="mono"
      data-testid="flight-workspace-link"
    >
      {workspace.workspace_id}
    </Link>
  );
}

/** Links a role's current session to its existing session page. */
function FlightSessionLink({ session }: { session: FlightSessionAssignment }) {
  if (!session.session_id) {
    return <span className="text-muted">not started</span>;
  }
  if (!session.available) {
    return (
      <span className="text-muted" data-testid={`flight-session-unavailable-${session.role}`}>
        <span className="mono">{session.session_id}</span> (unavailable)
      </span>
    );
  }
  return (
    <Link
      to={`/sessions/${encodeURIComponent(session.session_id)}`}
      className="mono"
      data-testid={`flight-session-link-${session.role}`}
    >
      {session.session_id}
    </Link>
  );
}

type FlightAssignmentLinksProps = {
  workspace?: FlightWorkspaceAssignment;
  sessions: FlightSessionAssignment[];
};

/**
 * The flight's workspace and role sessions. The short columns (role, agent)
 * come first and the session column last, so each agent sits beside its role.
 * Earlier sessions of a role fold under a count.
 */
export default function FlightAssignmentLinks({ workspace, sessions }: FlightAssignmentLinksProps) {
  if (!workspace) {
    return (
      <p className="text-muted p-lg m-0">Takeoff has not allocated a workspace for this flight.</p>
    );
  }
  return (
    <div className={layout.tableScroll}>
      <table
        className={`session-table ${layout.table} ${layout.resources}`}
        data-testid="flight-assignment-links"
        aria-label="Flight resources"
      >
        <thead>
          <tr>
            <th scope="col">Role</th>
            <th scope="col">Agent</th>
            <th scope="col">Session</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Workspace</td>
            <td className="text-muted">—</td>
            <td>
              <FlightWorkspaceLink workspace={workspace} />
              {workspace.branch && <span className="mono text-muted"> · {workspace.branch}</span>}
            </td>
          </tr>
          {sessions.map((s) => {
            const earlier = s.prior_session_ids ?? [];
            return (
              <tr key={s.id} data-testid={`flight-session-${s.role}`}>
                <td>{roleLabel(s.role)}</td>
                <td>{s.target}</td>
                <td>
                  <FlightSessionLink session={s} />
                  {s.outcome === 'failed' && s.error && (
                    <div className="text-error" data-testid={`flight-session-error-${s.role}`}>
                      {s.error}
                    </div>
                  )}
                  {earlier.length > 0 && (
                    <details className={`${layout.earlierAttempts} text-muted`}>
                      <summary className="cursor-pointer">
                        {earlier.length} earlier {earlier.length === 1 ? 'session' : 'sessions'}
                      </summary>
                      <ul>
                        {earlier.map((id) => (
                          <li key={id} className="mono">
                            {id}
                          </li>
                        ))}
                      </ul>
                    </details>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
