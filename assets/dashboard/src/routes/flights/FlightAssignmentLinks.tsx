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

/** The flight's workspace and role sessions, with retry history. */
export default function FlightAssignmentLinks({ workspace, sessions }: FlightAssignmentLinksProps) {
  if (!workspace) {
    return (
      <p className="text-muted p-lg m-0">Takeoff has not allocated a workspace for this flight.</p>
    );
  }
  return (
    <div className={layout.tableScroll}>
      <table
        className={`session-table ${layout.table}`}
        data-testid="flight-assignment-links"
        aria-label="Flight resources"
      >
        <thead>
          <tr>
            <th scope="col">Role</th>
            <th scope="col">Resource</th>
            <th scope="col">Agent</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>Workspace</td>
            <td>
              <FlightWorkspaceLink workspace={workspace} />
              {workspace.branch && <div className="mono text-muted mt-sm">{workspace.branch}</div>}
            </td>
            <td>—</td>
          </tr>
          {sessions.map((s) => (
            <tr key={s.id} data-testid={`flight-session-${s.role}`}>
              <td>{roleLabel(s.role)}</td>
              <td>
                <FlightSessionLink session={s} />
                {s.outcome === 'failed' && s.error && (
                  <div className="text-error" data-testid={`flight-session-error-${s.role}`}>
                    {s.error}
                  </div>
                )}
                {s.prior_session_ids && s.prior_session_ids.length > 0 && (
                  <div className="text-faint">
                    Earlier attempts: <span className="mono">{s.prior_session_ids.join(', ')}</span>
                  </div>
                )}
              </td>
              <td>{s.target}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
