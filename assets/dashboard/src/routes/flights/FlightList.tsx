import { Link } from 'react-router';
import { flightLabel, workflowPillClass } from '../../lib/flights';
import type { FlightSummaryResponse } from '../../lib/types.generated';
import { ArrowDownIcon, ArrowUpIcon } from '../../components/Icons';
import FlightActions from './FlightActions';
import { FlightSessionLink, FlightWorkspaceLink, roleLabel } from './FlightAssignmentLinks';
import layout from './FlightLayout.module.css';

export function FlightStatePills({
  flight,
}: {
  flight: Pick<FlightSummaryResponse, 'id' | 'workflow_state' | 'cleanup_state'>;
}) {
  return (
    <span className="inline-flex gap-xs">
      <span
        className={`status-pill status-pill--sm ${workflowPillClass(flight.workflow_state)}`}
        data-testid={`flight-state-${flight.id}`}
      >
        <span className="status-pill__dot" />
        {flight.workflow_state}
      </span>
      {flight.cleanup_state === 'terminating' && (
        <span
          className="status-pill status-pill--sm status-pill--error"
          data-testid={`flight-cleanup-${flight.id}`}
        >
          <span className="status-pill__dot" />
          terminating
        </span>
      )}
    </span>
  );
}

function FlightError({ flight }: { flight: FlightSummaryResponse }) {
  if (!flight.last_error) return null;
  return (
    <p className="form-group__error m-0 w-full" data-testid={`flight-error-${flight.id}`}>
      {flight.last_error}
    </p>
  );
}

type FlightListProps = {
  flights: FlightSummaryResponse[];
  repoNames: Record<string, string>;
  onMove: (id: string, delta: -1 | 1) => void;
  onEdit: (flight: FlightSummaryResponse) => void;
  onDelete: (flight: FlightSummaryResponse) => void;
};

/**
 * The ordered backlog and every started flight, including done and cancelled
 * flights that still own a workspace.
 */
export default function FlightList({
  flights,
  repoNames,
  onMove,
  onEdit,
  onDelete,
}: FlightListProps) {
  const backlog = flights
    .filter((f) => f.workflow_state === 'backlog')
    .sort((a, b) => a.order - b.order);
  // Active flights are being worked (planning, executing, completing). Finished
  // flights (done, cancelled) are listed because they still hold their
  // workspace until terminated.
  const active = flights.filter((f) =>
    ['planning', 'executing', 'completing'].includes(f.workflow_state)
  );
  const finished = flights.filter((f) => ['done', 'cancelled'].includes(f.workflow_state));

  return (
    <>
      <section className="card" data-testid="flight-backlog">
        <div className="card__header">
          <h2 className="settings-section__title">Backlog</h2>
        </div>
        {backlog.length === 0 ? (
          <p className="text-muted p-lg m-0">No flights in the backlog.</p>
        ) : (
          <div className={layout.tableScroll}>
            <table
              className={`session-table ${layout.table} ${layout.listTable}`}
              aria-label="Backlog flights"
            >
              <thead>
                <tr>
                  <th scope="col">Flight</th>
                  <th scope="col">Repository</th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {backlog.map((f, i) => (
                  <tr key={f.id} data-testid={`flight-row-${f.id}`}>
                    <td>
                      <div className={layout.identity}>
                        <span className="inline-flex">
                          <button
                            type="button"
                            className="icon-btn"
                            aria-label={`Move ${flightLabel(f)} up`}
                            disabled={i === 0}
                            onClick={() => onMove(f.id, -1)}
                          >
                            {ArrowUpIcon}
                          </button>
                          <button
                            type="button"
                            className="icon-btn"
                            aria-label={`Move ${flightLabel(f)} down`}
                            disabled={i === backlog.length - 1}
                            onClick={() => onMove(f.id, 1)}
                          >
                            {ArrowDownIcon}
                          </button>
                        </span>
                        <div className="flex-col gap-xs">
                          <Link to={`/flights/${encodeURIComponent(f.id)}`} className="text-accent">
                            {flightLabel(f)}
                          </Link>
                          <span className="text-muted">
                            {f.planner_model} → {f.executor_model}
                          </span>
                          <FlightError flight={f} />
                        </div>
                      </div>
                    </td>
                    <td>{repoNames[f.repo] ?? f.repo}</td>
                    <td className={layout.actionsCell}>
                      <div className={layout.actions}>
                        <button
                          type="button"
                          className="btn btn--sm btn--ghost btn--bordered"
                          onClick={() => onEdit(f)}
                          data-testid={`flight-edit-${f.id}`}
                        >
                          Edit
                        </button>
                        <button
                          type="button"
                          className="btn btn--sm btn--ghost btn--danger"
                          onClick={() => onDelete(f)}
                          data-testid={`flight-delete-${f.id}`}
                        >
                          Delete
                        </button>
                        <FlightActions flight={f} compact />
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <StartedSection
        testId="flight-active"
        title="Active"
        flights={active}
        empty="No flights are in progress."
        repoNames={repoNames}
        onEdit={onEdit}
        onDelete={onDelete}
      />

      <StartedSection
        testId="flight-finished"
        title="Finished"
        flights={finished}
        empty="No finished flights are holding a workspace."
        repoNames={repoNames}
        onEdit={onEdit}
        onDelete={onDelete}
      />
    </>
  );
}

type StartedSectionProps = {
  testId: string;
  title: string;
  flights: FlightSummaryResponse[];
  empty: string;
  repoNames: Record<string, string>;
  onEdit: (flight: FlightSummaryResponse) => void;
  onDelete: (flight: FlightSummaryResponse) => void;
};

/**
 * One table of flights that have taken off: identity and resource links,
 * workflow/cleanup status, and actions.
 */
function StartedSection({
  testId,
  title,
  flights,
  empty,
  repoNames,
  onEdit,
  onDelete,
}: StartedSectionProps) {
  return (
    <section className="card" data-testid={testId}>
      <div className="card__header">
        <h2 className="settings-section__title">{title}</h2>
      </div>
      {flights.length === 0 ? (
        <p className="text-muted p-lg m-0">{empty}</p>
      ) : (
        <div className={layout.tableScroll}>
          <table
            className={`session-table ${layout.table} ${layout.listTable}`}
            aria-label={`${title} flights`}
          >
            <thead>
              <tr>
                <th scope="col">Flight</th>
                <th scope="col">Status</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {flights.map((f) => (
                <tr key={f.id} data-testid={`flight-row-${f.id}`}>
                  <td>
                    <div className="flex-col gap-xs">
                      <Link to={`/flights/${encodeURIComponent(f.id)}`} className="text-accent">
                        {flightLabel(f)}
                      </Link>
                      <span className="text-muted">{repoNames[f.repo] ?? f.repo}</span>
                      {/* One resource per line, label column aligned. */}
                      <dl
                        className={`${layout.metadata} ${layout.resources}`}
                        data-testid={`flight-resources-${f.id}`}
                      >
                        {f.workspace && (
                          <div className={layout.metadataRow}>
                            <dt>Workspace</dt>
                            <dd>
                              <FlightWorkspaceLink workspace={f.workspace} />
                            </dd>
                          </div>
                        )}
                        {(f.sessions ?? []).map((s) => (
                          <div key={s.id} className={layout.metadataRow}>
                            <dt>{roleLabel(s.role)}</dt>
                            <dd>
                              <FlightSessionLink session={s} />
                            </dd>
                          </div>
                        ))}
                      </dl>
                      <FlightError flight={f} />
                    </div>
                  </td>
                  <td>
                    <FlightStatePills flight={f} />
                  </td>
                  <td className={layout.actionsCell}>
                    <div className={layout.actions}>
                      <button
                        type="button"
                        className="btn btn--sm btn--ghost btn--bordered"
                        onClick={() => onEdit(f)}
                        data-testid={`flight-edit-${f.id}`}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        className="btn btn--sm btn--ghost btn--danger"
                        onClick={() => onDelete(f)}
                        data-testid={`flight-delete-${f.id}`}
                      >
                        Delete
                      </button>
                      <FlightActions flight={f} compact />
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
