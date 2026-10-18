import { Link } from 'react-router';
import { useMemo } from 'react';
import { useSessions } from '../../contexts/SessionsContext';
import {
  flightActivity,
  flightLabel,
  sortStartedFlights,
  workflowPillClass,
  type FlightActivity,
} from '../../lib/flights';
import { isSessionInProgress, type SessionInProgressPredicate } from '../../lib/workspaceSort';
import { useConfig } from '../../contexts/ConfigContext';
import type { FlightSummaryResponse } from '../../lib/types.generated';
import { ArrowDownIcon, ArrowUpIcon } from '../../components/Icons';
import FlightActivityLine from './FlightActivityLine';
import FlightDisposeButton from './FlightDisposeButton';
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
      {flight.cleanup_state === 'disposing' && (
        <span
          className="status-pill status-pill--sm status-pill--error"
          data-testid={`flight-cleanup-${flight.id}`}
        >
          <span className="status-pill__dot" />
          disposing
        </span>
      )}
    </span>
  );
}

function FlightError({ flightId, message }: { flightId: string; message?: string }) {
  if (!message) return null;
  return (
    <p className="form-group__error m-0 w-full" data-testid={`flight-error-${flightId}`}>
      {message}
    </p>
  );
}

type FlightListProps = {
  flights: FlightSummaryResponse[];
  repoNames: Record<string, string>;
  onMove: (id: string, delta: -1 | 1) => void;
  onEdit: (flight: FlightSummaryResponse) => void;
};

/** Backlog rows keep the manual order controls beside the flight name. */
function BacklogRow({
  flight,
  index,
  total,
  repoNames,
  onMove,
  onEdit,
}: {
  flight: FlightSummaryResponse;
  index: number;
  total: number;
  repoNames: Record<string, string>;
  onMove: (id: string, delta: -1 | 1) => void;
  onEdit: (flight: FlightSummaryResponse) => void;
}) {
  return (
    <tr key={flight.id} data-testid={`flight-row-${flight.id}`}>
      <td>
        <div className={layout.identity}>
          <span className="inline-flex">
            <button
              type="button"
              className="icon-btn"
              aria-label={`Move ${flightLabel(flight)} up`}
              disabled={index === 0}
              onClick={() => onMove(flight.id, -1)}
            >
              {ArrowUpIcon}
            </button>
            <button
              type="button"
              className="icon-btn"
              aria-label={`Move ${flightLabel(flight)} down`}
              disabled={index === total - 1}
              onClick={() => onMove(flight.id, 1)}
            >
              {ArrowDownIcon}
            </button>
          </span>
          <div className="flex-col gap-xs">
            <Link to={`/flights/${encodeURIComponent(flight.id)}`} className="text-accent">
              {flightLabel(flight)}
            </Link>
            <span className="text-muted">
              {flight.planner_model} → {flight.executor_model}
            </span>
            <FlightError flightId={flight.id} message={flight.last_error} />
          </div>
        </div>
      </td>
      <td>{repoNames[flight.repo] ?? flight.repo}</td>
      <td className={layout.actionsCell}>
        <div className={layout.actions}>
          <button
            type="button"
            className="btn btn--sm btn--ghost btn--bordered"
            onClick={() => onEdit(flight)}
            data-testid={`flight-edit-${flight.id}`}
          >
            Edit
          </button>
          <FlightDisposeButton flight={flight} size="sm" />
        </div>
      </td>
    </tr>
  );
}

/** Started rows have one status line; resource inventories live on the detail page. */
function AirRow({
  flight,
  activity,
  repoNames,
  onEdit,
}: {
  flight: FlightSummaryResponse;
  activity: FlightActivity | null;
  repoNames: Record<string, string>;
  onEdit: (flight: FlightSummaryResponse) => void;
}) {
  return (
    <tr key={flight.id} data-testid={`flight-row-${flight.id}`}>
      <td>
        <div className="flex-col gap-xs">
          <Link to={`/flights/${encodeURIComponent(flight.id)}`} className="text-accent">
            {flightLabel(flight)}
          </Link>
          <span className="text-muted">{repoNames[flight.repo] ?? flight.repo}</span>
          <FlightError flightId={flight.id} message={flight.last_error} />
        </div>
      </td>
      <td>
        <FlightActivityLine flight={flight} activity={activity} />
      </td>
      <td className={layout.actionsCell}>
        <div className={layout.actions}>
          <button
            type="button"
            className="btn btn--sm btn--ghost btn--bordered"
            onClick={() => onEdit(flight)}
            data-testid={`flight-edit-${flight.id}`}
          >
            Edit
          </button>
          <FlightDisposeButton flight={flight} size="sm" />
        </div>
      </td>
    </tr>
  );
}

/**
 * The two-section list: Backlog (manual order, arrows) and In the air
 * (ordered by session activity). Rows show only Edit and Dispose — workflow
 * transitions live on the detail page.
 */
export default function FlightList({ flights, repoNames, onMove, onEdit }: FlightListProps) {
  const { sessionsById } = useSessions();
  const { config } = useConfig();

  // The same in-progress predicate AppShell uses for the sidebar workspace
  // sort. We don't render the workspace row here, but the rule must match
  // so the activity line and the sidebar agree on what's busy.
  const isInProgress = useMemo<SessionInProgressPredicate>(() => {
    const runTargets = config?.run_targets?.map((t: { name: string }) => t.name) ?? [];
    const nudgenikEnabled = (config?.nudgenik?.targets?.length ?? 0) > 0;
    return (session) =>
      isSessionInProgress(session, {
        nudgenikEnabled,
        isPromptable: !runTargets.includes(session.target),
      });
  }, [config]);

  const backlog = flights
    .filter((f) => f.workflow_state === 'backlog')
    .sort((a, b) => a.order - b.order);
  const started = flights.filter((f) => f.workflow_state !== 'backlog');
  const activityById = new Map(
    started.map((f) => [f.id, flightActivity(f, sessionsById, isInProgress)])
  );
  const activityOf = (f: FlightSummaryResponse) => activityById.get(f.id) ?? null;
  const inAir = sortStartedFlights(started, activityOf);

  return (
    <>
      <section className="card" data-testid="flight-backlog">
        <div className={`card__header ${layout.sectionHeader}`}>
          <h2 className="settings-section__title">Backlog</h2>
          <p className="text-muted m-0">Manual order · use the arrows to reorder.</p>
        </div>
        {backlog.length === 0 ? (
          <p className="text-muted p-lg m-0" data-testid="flight-backlog-empty">
            No flights in the backlog.
          </p>
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
                  <BacklogRow
                    key={f.id}
                    flight={f}
                    index={i}
                    total={backlog.length}
                    repoNames={repoNames}
                    onMove={onMove}
                    onEdit={onEdit}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="card" data-testid="flight-air">
        <div className={`card__header ${layout.sectionHeader}`}>
          <h2 className="settings-section__title">In the air</h2>
          <p className="text-muted m-0">
            Latest activity first · working sessions rank one hour older. No activity goes last,
            newest flight first.
          </p>
        </div>
        {inAir.length === 0 ? (
          <p className="text-muted p-lg m-0" data-testid="flight-air-empty">
            No flights are in the air.
          </p>
        ) : (
          <div className={layout.tableScroll}>
            <table
              className={`session-table ${layout.table} ${layout.listTable}`}
              aria-label="Flights in the air"
            >
              <thead>
                <tr>
                  <th scope="col">Flight</th>
                  <th scope="col">Status</th>
                  <th scope="col">Actions</th>
                </tr>
              </thead>
              <tbody>
                {inAir.map((f) => (
                  <AirRow
                    key={f.id}
                    flight={f}
                    activity={activityOf(f)}
                    repoNames={repoNames}
                    onEdit={onEdit}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  );
}
