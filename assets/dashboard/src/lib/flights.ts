import type { FlightSummaryResponse } from './types.generated';
import type { SessionWithWorkspace } from './types';
import { sessionActivityTime, type SessionInProgressPredicate } from './workspaceSort';

export type FlightIndex = {
  flightsById: Record<string, FlightSummaryResponse>;
  flightByWorkspaceId: Record<string, FlightSummaryResponse>;
};

/**
 * Index a complete flights snapshot. Both maps are rebuilt from scratch on
 * every snapshot, so a removed flight or released workspace disappears with
 * the payload that dropped it.
 */
export function indexFlights(flights: FlightSummaryResponse[]): FlightIndex {
  const flightsById: Record<string, FlightSummaryResponse> = {};
  const flightByWorkspaceId: Record<string, FlightSummaryResponse> = {};
  for (const flight of flights) {
    flightsById[flight.id] = flight;
    if (flight.workspace?.workspace_id) {
      flightByWorkspaceId[flight.workspace.workspace_id] = flight;
    }
  }
  return { flightsById, flightByWorkspaceId };
}

/** The label a flight is identified by in the UI: its short name, else its ID. */
export function flightLabel(flight: { id: string; short_name?: string }): string {
  return flight.short_name || flight.id;
}

export type FlightAction = 'takeoff' | 'execution' | 'completion' | 'done' | 'dispose';

type FlightActionInput = Pick<
  FlightSummaryResponse,
  'workflow_state' | 'cleanup_state' | 'sessions'
>;

/**
 * The workflow actions the backend accepts for a flight in its current state,
 * in display order. Takeoff is available only in backlog. `dispose` is always
 * offered (and is the only action while `cleanup_state === 'disposing'`).
 */
export function flightActions(flight: FlightActionInput): FlightAction[] {
  if (flight.cleanup_state === 'disposing') return ['dispose'];
  switch (flight.workflow_state) {
    case 'backlog':
      return ['takeoff', 'dispose'];
    case 'planning': {
      const planner = flight.sessions?.find((s) => s.role === 'planner');
      const spawned = planner?.outcome === 'succeeded';
      const actions: FlightAction[] = [];
      if (spawned) actions.push('execution');
      return [...actions, 'dispose'];
    }
    case 'executing':
      return ['completion', 'dispose'];
    case 'completing':
      return ['done', 'dispose'];
    default:
      // A finished flight still owns its workspace until disposed.
      return ['dispose'];
  }
}

/** Human label for an action button on a flight in its current state. */
export function flightActionLabel(action: FlightAction, flight: FlightActionInput): string {
  switch (action) {
    case 'takeoff':
      return 'Take off';
    case 'execution':
      return 'Enter execution';
    case 'completion':
      return 'Enter completion';
    case 'done':
      return 'Mark done';
    case 'dispose':
      return flight.cleanup_state === 'disposing' ? 'Retry dispose' : 'Dispose';
  }
}

/** Status-pill modifier for a workflow state: active work reads as running. */
export function workflowPillClass(state: string): string {
  switch (state) {
    case 'planning':
    case 'executing':
    case 'completing':
      return 'status-pill--running';
    default:
      return 'status-pill--stopped';
  }
}

// ROLE_LABEL is the user-facing name for each session role. It also exposes
// `roleLabel` so the rest of the dashboard can keep using the per-route helper.
const ROLE_LABEL: Record<string, string> = {
  planner: 'Planner',
  executor: 'Executor',
  commit: 'Commit',
  pilot: 'Pilot',
};

export function roleLabel(role: string): string {
  return ROLE_LABEL[role] || role;
}

/**
 * The role session on a flight most likely to need the user, resolved through
 * `sessionsById`, so the activity line can render one
 * `Role · state · relative-time` row that links to the session page.
 *
 * `inProgress` is the same predicate result that aged `time`, so the line's
 * "Working" and the list order never disagree. When no session resolves (a
 * planner spawn never produced one, or the session was disposed by hand) the
 * activity carries the role with `session: null`, so the row renders unlinked.
 */
type FlightSessionAssignment = NonNullable<FlightSummaryResponse['sessions']>[number];

export type FlightActivity = {
  role: FlightSessionAssignment['role'];
  sessionId: string | null;
  session: SessionWithWorkspace | null;
  inProgress: boolean;
  time: number | null;
};

export function flightActivity(
  flight: Pick<FlightSummaryResponse, 'sessions'>,
  sessionsById: Record<string, SessionWithWorkspace>,
  isInProgress: SessionInProgressPredicate
): FlightActivity | null {
  const assignments = (flight.sessions ?? []).filter((assignment) => assignment.role !== 'pilot');
  if (assignments.length === 0) return null;

  // Greatest adjusted timestamp wins. A resolved session without a timestamp
  // ranks below every timed one; among those, array order decides.
  let best: FlightActivity | null = null;
  for (const a of assignments) {
    const session = a.session_id ? sessionsById[a.session_id] : undefined;
    if (!session) continue;
    const time = sessionActivityTime(session, isInProgress);
    const better = !best || (time !== null && (best.time === null || time > best.time));
    if (better) {
      best = {
        role: a.role,
        sessionId: session.id,
        session,
        inProgress: isInProgress(session),
        time,
      };
    }
  }
  if (best) return best;

  // No resolved session: name the most recently updated assignment.
  let latest = assignments[0];
  for (const a of assignments) {
    if (a.updated_at > latest.updated_at) latest = a;
  }
  return { role: latest.role, sessionId: null, session: null, inProgress: false, time: null };
}

/**
 * Order started flights so the one most likely to need the user sits at the
 * top. Activity time ranks first; flights with no resolved activity rank
 * last, newest `created_at` first within that group.
 */
export function sortStartedFlights(
  flights: FlightSummaryResponse[],
  activityOf: (f: FlightSummaryResponse) => FlightActivity | null
): FlightSummaryResponse[] {
  const annotated = flights.map((f) => ({
    flight: f,
    activity: activityOf(f),
  }));
  annotated.sort((a, b) => {
    const at = a.activity?.time ?? null;
    const bt = b.activity?.time ?? null;
    if (at !== null && bt === null) return -1;
    if (at === null && bt !== null) return 1;
    if (at !== null && bt !== null && at !== bt) return bt - at;
    // No-time or tied: newest created_at first.
    if (a.flight.created_at !== b.flight.created_at) {
      return a.flight.created_at < b.flight.created_at ? 1 : -1;
    }
    return a.flight.id < b.flight.id ? -1 : 1;
  });
  return annotated.map((x) => x.flight);
}
