import { describe, it, expect } from 'vitest';
import { indexFlights, flightLabel, flightActions, flightActionLabel } from './flights';
import type { FlightSummaryResponse } from './types.generated';

function flight(id: string, workspaceId?: string): FlightSummaryResponse {
  return {
    id,
    repo: 'r',
    planner_model: 'm',
    executor_model: 'm',
    workflow_state: workspaceId ? 'planning' : 'backlog',
    cleanup_state: 'idle',
    order: 0,
    workspace: workspaceId
      ? {
          id: `wa-${id}`,
          requested_branch: `schmux/flight/${id}`,
          workspace_id: workspaceId,
          path: `/w/${workspaceId}`,
          outcome: 'succeeded',
          available: true,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        }
      : undefined,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

describe('indexFlights', () => {
  it('maps every flight by ID and only assigned flights by workspace', () => {
    const { flightsById, flightByWorkspaceId } = indexFlights([
      flight('backlog-1'),
      flight('started-1', 'ws-1'),
    ]);

    expect(Object.keys(flightsById).sort()).toEqual(['backlog-1', 'started-1']);
    expect(Object.keys(flightByWorkspaceId)).toEqual(['ws-1']);
    expect(flightByWorkspaceId['ws-1'].id).toBe('started-1');
  });

  it('drops flights that a later snapshot no longer carries', () => {
    const first = indexFlights([flight('a', 'ws-1'), flight('b', 'ws-2')]);
    const second = indexFlights([flight('b', 'ws-2')]);

    expect(first.flightByWorkspaceId['ws-1']).toBeDefined();
    expect(second.flightsById['a']).toBeUndefined();
    expect(second.flightByWorkspaceId['ws-1']).toBeUndefined();
  });
});

describe('flightActions', () => {
  const planner = (outcome: string, available = true) => [
    {
      id: 'sa',
      role: 'planner',
      target: 'm',
      outcome,
      available,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ];

  it.each([
    ['backlog', 'idle', undefined, ['takeoff']],
    ['planning', 'idle', undefined, ['takeoff', 'cancel', 'terminate']],
    ['planning', 'idle', planner('failed'), ['takeoff', 'cancel', 'terminate']],
    ['planning', 'idle', planner('succeeded'), ['execution', 'cancel', 'terminate']],
    // Planner spawned but its session is gone (workspace disposed by hand):
    // execution would fail, so takeoff is offered again.
    [
      'planning',
      'idle',
      planner('succeeded', false),
      ['takeoff', 'execution', 'cancel', 'terminate'],
    ],
    ['executing', 'idle', undefined, ['completion', 'cancel', 'terminate']],
    ['completing', 'idle', undefined, ['done', 'cancel', 'terminate']],
    ['done', 'idle', undefined, ['terminate']],
    ['cancelled', 'idle', undefined, ['terminate']],
    ['executing', 'terminating', undefined, ['terminate']],
    ['cancelled', 'terminating', undefined, ['terminate']],
  ])('%s with cleanup %s offers the backend-accepted actions', (wf, cleanup, sessions, want) => {
    expect(
      flightActions({ workflow_state: wf, cleanup_state: cleanup, sessions } as never)
    ).toEqual(want);
  });

  it('labels retries of failed takeoff and partial terminate', () => {
    expect(
      flightActionLabel('takeoff', {
        workflow_state: 'planning',
        cleanup_state: 'idle',
      })
    ).toBe('Retry takeoff');
    expect(flightActionLabel('takeoff', { workflow_state: 'backlog', cleanup_state: 'idle' })).toBe(
      'Take off'
    );
    expect(
      flightActionLabel('terminate', { workflow_state: 'done', cleanup_state: 'terminating' })
    ).toBe('Retry terminate');
  });
});

describe('flightLabel', () => {
  it('prefers the short name and falls back to the ID', () => {
    expect(flightLabel({ id: 'fl-1', short_name: 'Login fix' })).toBe('Login fix');
    expect(flightLabel({ id: 'fl-1' })).toBe('fl-1');
  });
});
