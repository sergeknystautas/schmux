import type { FlightSummaryResponse } from './types.generated';

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

export type FlightAction = 'takeoff' | 'execution' | 'completion' | 'done' | 'cancel' | 'terminate';

type FlightActionInput = Pick<
  FlightSummaryResponse,
  'workflow_state' | 'cleanup_state' | 'sessions'
>;

/**
 * The workflow actions the backend accepts for a flight in its current state,
 * in display order. A failed attempt is retried through the same action, so a
 * planning flight whose planner never started, or whose planner session is
 * gone, offers takeoff again, and a partially terminated flight offers
 * terminate again.
 */
export function flightActions(flight: FlightActionInput): FlightAction[] {
  if (flight.cleanup_state === 'terminating') return ['terminate'];
  switch (flight.workflow_state) {
    case 'backlog':
      return ['takeoff'];
    case 'planning': {
      // A planner that spawned but whose session is gone (the workspace was
      // disposed by hand) cannot reach execution; takeoff is offered again.
      const planner = flight.sessions?.find((s) => s.role === 'planner');
      const spawned = planner?.outcome === 'succeeded';
      const actions: FlightAction[] = [];
      if (!spawned || !planner.available) actions.push('takeoff');
      if (spawned) actions.push('execution');
      return [...actions, 'cancel', 'terminate'];
    }
    case 'executing':
      return ['completion', 'cancel', 'terminate'];
    case 'completing':
      return ['done', 'cancel', 'terminate'];
    default:
      // done and cancelled still own their workspace until terminated.
      return ['terminate'];
  }
}

/** Human label for an action button on a flight in its current state. */
export function flightActionLabel(action: FlightAction, flight: FlightActionInput): string {
  switch (action) {
    case 'takeoff':
      return flight.workflow_state === 'planning' ? 'Retry takeoff' : 'Take off';
    case 'execution':
      return 'Enter execution';
    case 'completion':
      return 'Enter completion';
    case 'done':
      return 'Mark done';
    case 'cancel':
      return 'Cancel flight';
    case 'terminate':
      return flight.cleanup_state === 'terminating' ? 'Retry terminate' : 'Terminate';
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
