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

export type FlightAction = 'takeoff' | 'execution' | 'review' | 'completion' | 'dispose';

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
      return ['review', 'dispose'];
    case 'reviewing':
      return ['completion', 'dispose'];
    default:
      // Completing is the last phase: the commit is ready, and the flight
      // owns its workspace until disposed. Rebase, push, and merge are the
      // user's.
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
    case 'review':
      return 'Enter review';
    case 'completion':
      return 'Enter completion';
    case 'dispose':
      return flight.cleanup_state === 'disposing' ? 'Retry dispose' : 'Dispose';
  }
}

/** Status-pill modifier for a workflow state: active work reads as running. */
export function workflowPillClass(state: string): string {
  switch (state) {
    case 'planning':
    case 'executing':
    case 'reviewing':
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
  reviewer: 'Reviewer',
};

export function roleLabel(role: string): string {
  return ROLE_LABEL[role] || role;
}

type ReviewStatusFields = {
  state?: string;
  tests?: string;
  test_rules_review?: string;
  changed_work?: string;
  artifacts_named?: string;
};

// The review reading in words, from the daemon's own classification of the
// reviewer's last turn. Each classifier has its own words, since choice IDs
// such as not_run repeat across classifiers. Each part is optional: the daemon
// records only the questions it asked. Keys are in display order.
const REVIEW_WORDS: Record<keyof ReviewStatusFields, Record<string, string>> = {
  state: { finished: 'finished', fixing: 'still fixing', blocked: 'blocked' },
  tests: {
    run_passed: 'tests run and passed',
    run_failed: 'tests run and failed',
    not_run: 'tests not run',
  },
  test_rules_review: {
    compliant: 'test-rules-review compliant',
    violations: 'test-rules-review found violations',
    not_run: 'test-rules-review not run',
  },
  changed_work: { changed: 'changed the work', unchanged: 'changed nothing' },
  artifacts_named: { named: 'artifacts named', incomplete: 'artifacts incomplete' },
};

/**
 * The latest reviewer classification in words, or null when the daemon has
 * not classified a reviewer turn yet.
 */
export function reviewReading(status: ReviewStatusFields | undefined): string | null {
  if (!status) return null;
  const parts: string[] = [];
  for (const [field, words] of Object.entries(REVIEW_WORDS)) {
    const value = status[field as keyof ReviewStatusFields];
    const word = value ? words[value] : undefined;
    if (word) parts.push(word);
  }
  if (parts.length === 0) return null;
  return `Reviewer reports: ${parts.join(', ')}`;
}

/**
 * The pilot's latest delivered or queued guidance in words, or null when the
 * pilot has sent none. Monitor history keeps the full record.
 */
export function pilotGuidanceText(
  guidance: FlightSummaryResponse['pilot_guidance']
): string | null {
  if (!guidance) return null;
  const to = roleLabel(guidance.recipient_role) || 'session';
  const queued = guidance.status === 'queued' ? ' (queued)' : '';
  return `Pilot to ${to}${queued}: ${guidance.text}`;
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

// The role each phase supervises, most recent first. The activity line breaks
// a tie with it, so the line names the session the current phase runs.
const PHASE_ROLE_PRIORITY: Record<string, string[]> = {
  planning: ['planner'],
  executing: ['executor'],
  reviewing: ['reviewer'],
  completing: ['commit', 'reviewer'],
};

/** The priority of a role in a phase: lower is more relevant. */
function phaseRoleRank(workflowState: string, role: string): number {
  const order = PHASE_ROLE_PRIORITY[workflowState] ?? [];
  const index = order.indexOf(role);
  return index === -1 ? order.length : index;
}

export function flightActivity(
  flight: Pick<FlightSummaryResponse, 'sessions' | 'workflow_state'>,
  sessionsById: Record<string, SessionWithWorkspace>,
  isInProgress: SessionInProgressPredicate
): FlightActivity | null {
  const assignments = (flight.sessions ?? []).filter((assignment) => assignment.role !== 'pilot');
  if (assignments.length === 0) return null;

  // A session waiting on the user wins the line. Among sessions on the same
  // side of that, the greatest adjusted timestamp wins, and a tie goes to the
  // role the current phase supervises. A resolved session without a timestamp
  // ranks below every timed one; among those, array order decides.
  let best: FlightActivity | null = null;
  let bestNeedsInput = false;
  let bestRank = Number.MAX_SAFE_INTEGER;
  for (const a of assignments) {
    const session = a.session_id ? sessionsById[a.session_id] : undefined;
    if (!session) continue;
    const time = sessionActivityTime(session, isInProgress);
    const needsInput = session.nudge_state === 'Needs Input';
    const rank = phaseRoleRank(flight.workflow_state, a.role);
    let better: boolean;
    if (!best) better = true;
    else if (needsInput !== bestNeedsInput) better = needsInput;
    else if (time !== best.time) better = time !== null && (best.time === null || time > best.time);
    else better = rank < bestRank;
    if (better) {
      bestNeedsInput = needsInput;
      bestRank = rank;
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
