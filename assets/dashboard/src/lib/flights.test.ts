import { describe, it, expect } from 'vitest';
import {
  indexFlights,
  flightLabel,
  flightActions,
  flightActionLabel,
  flightActivity,
  sortStartedFlights,
} from './flights';
import type { FlightSessionAssignment, FlightSummaryResponse } from './types.generated';
import type { SessionResponse, SessionWithWorkspace } from './types';

function flight(
  id: string,
  opts: {
    workflow_state?: FlightSummaryResponse['workflow_state'];
    sessions?: FlightSessionAssignment[];
    created_at?: string;
  } = {}
): FlightSummaryResponse {
  const ws = opts.workflow_state ?? 'planning';
  return {
    id,
    repo: 'r',
    planner_model: 'm',
    executor_model: 'm',
    workflow_state: ws,
    cleanup_state: 'idle',
    order: 0,
    workspace:
      ws === 'backlog'
        ? undefined
        : {
            id: `wa-${id}`,
            requested_branch: `schmux/flight/${id}`,
            workspace_id: `ws-${id}`,
            path: `/w/${id}`,
            outcome: 'succeeded',
            available: true,
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
          },
    sessions: opts.sessions,
    created_at: opts.created_at ?? '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  };
}

function live(opts: {
  id: string;
  running?: boolean;
  nudge_state?: string | null;
  nudge_summary?: string;
  last_output_at?: string;
  target?: string;
}): SessionWithWorkspace {
  return {
    id: opts.id,
    target: opts.target ?? 'claude',
    branch: 'main',
    created_at: '2026-01-01T00:00:00Z',
    last_output_at: opts.last_output_at ?? '2026-01-01T01:00:00Z',
    running: opts.running ?? true,
    attach_cmd: '',
    nudge_state: opts.nudge_state ?? '',
    nudge_summary: opts.nudge_summary,
    workspace_id: 'ws-1',
    workspace_path: '/w/ws-1',
    repo: 'r',
  };
}

function assign(
  role: FlightSessionAssignment['role'],
  sessionId: string,
  updatedAt?: string
): FlightSessionAssignment {
  return {
    id: `sa-${role}`,
    role,
    target: 'm',
    session_id: sessionId,
    outcome: 'succeeded',
    available: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: updatedAt ?? '2026-01-01T00:00:00Z',
  };
}

describe('indexFlights', () => {
  it('maps every flight by ID and only assigned flights by workspace', () => {
    const { flightsById, flightByWorkspaceId } = indexFlights([
      flight('backlog-1', { workflow_state: 'backlog' }),
      flight('started-1'),
    ]);

    expect(Object.keys(flightsById).sort()).toEqual(['backlog-1', 'started-1']);
    expect(Object.keys(flightByWorkspaceId)).toEqual(['ws-started-1']);
    expect(flightByWorkspaceId['ws-started-1'].id).toBe('started-1');
  });

  it('drops flights that a later snapshot no longer carries', () => {
    const first = indexFlights([flight('a'), flight('b')]);
    const second = indexFlights([flight('b')]);

    expect(first.flightByWorkspaceId['ws-a']).toBeDefined();
    expect(second.flightsById['a']).toBeUndefined();
    expect(second.flightByWorkspaceId['ws-a']).toBeUndefined();
  });
});

describe('flightActions', () => {
  const planner = (outcome: string, available = true) => [
    {
      id: 'sa',
      role: 'planner' as const,
      target: 'm',
      outcome,
      available,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ];

  it.each([
    ['backlog', 'idle', undefined, ['takeoff', 'dispose']],
    ['planning', 'idle', undefined, ['dispose']],
    ['planning', 'idle', planner('failed'), ['dispose']],
    ['planning', 'idle', planner('succeeded'), ['execution', 'dispose']],
    ['planning', 'idle', planner('succeeded', false), ['execution', 'dispose']],
    ['executing', 'idle', undefined, ['completion', 'dispose']],
    ['completing', 'idle', undefined, ['done', 'dispose']],
    ['done', 'idle', undefined, ['dispose']],
    ['executing', 'disposing', undefined, ['dispose']],
  ])('%s with cleanup %s offers the backend-accepted actions', (wf, cleanup, sessions, want) => {
    expect(
      flightActions({ workflow_state: wf, cleanup_state: cleanup, sessions } as never)
    ).toEqual(want);
  });

  it('labels takeoff and partial dispose', () => {
    expect(flightActionLabel('takeoff', { workflow_state: 'backlog', cleanup_state: 'idle' })).toBe(
      'Take off'
    );
    expect(
      flightActionLabel('dispose', { workflow_state: 'done', cleanup_state: 'disposing' })
    ).toBe('Retry dispose');
    expect(
      flightActionLabel('dispose', { workflow_state: 'executing', cleanup_state: 'idle' })
    ).toBe('Dispose');
  });
});

describe('flightLabel', () => {
  it('prefers the short name and falls back to the ID', () => {
    expect(flightLabel({ id: 'fl-1', short_name: 'Login fix' })).toBe('Login fix');
    expect(flightLabel({ id: 'fl-1' })).toBe('fl-1');
  });
});

describe('flightActivity', () => {
  it('picks the session with the greatest adjusted time, so a stopped session beats a working one', () => {
    const plannerSession = live({
      id: 'p',
      running: true,
      nudge_state: 'Working',
      last_output_at: '2026-01-01T01:00:00Z',
    });
    const executorSession = live({
      id: 'x',
      running: false,
      last_output_at: '2026-01-01T00:30:00Z',
    });
    const byId = { p: plannerSession, x: executorSession };
    const f = flight('fl-1', {
      sessions: [assign('planner', 'p'), assign('executor', 'x')],
    });
    const working = (s: SessionResponse) => s.nudge_state === 'Working';
    const a = flightActivity(f, byId, working);
    expect(a?.sessionId).toBe('x');
    expect(a?.role).toBe('executor');
    expect(a?.inProgress).toBe(false);
  });

  it('carries the in-progress predicate result that aged the time', () => {
    const background = live({
      id: 'p',
      nudge_state: 'Background',
      last_output_at: '2026-01-01T01:00:00Z',
    });
    const f = flight('fl-1', { sessions: [assign('planner', 'p')] });
    const a = flightActivity(f, { p: background }, (s) => s.nudge_state === 'Background');
    expect(a?.inProgress).toBe(true);
    expect(a?.time).toBe(Date.parse('2026-01-01T00:00:00Z'));
  });

  it('resolves a session that has no output yet instead of calling it gone', () => {
    const fresh = { ...live({ id: 'p' }), last_output_at: undefined };
    const f = flight('fl-1', { sessions: [assign('planner', 'p')] });
    const a = flightActivity(f, { p: fresh }, () => false);
    expect(a?.sessionId).toBe('p');
    expect(a?.time).toBeNull();
  });

  it('ranks a session with output above a resolved session without output', () => {
    const fresh = { ...live({ id: 'x' }), last_output_at: undefined };
    const talked = live({ id: 'p', last_output_at: '2026-01-01T00:00:00Z' });
    const f = flight('fl-1', { sessions: [assign('executor', 'x'), assign('planner', 'p')] });
    expect(flightActivity(f, { p: talked, x: fresh }, () => false)?.sessionId).toBe('p');
  });

  it('falls back to the latest-updated assignment, unlinked, when no session resolves', () => {
    const f = flight('fl-1', {
      sessions: [
        assign('planner', '', '2026-01-01T00:00:00Z'),
        assign('executor', 'missing', '2026-01-02T00:00:00Z'),
      ],
    });
    const a = flightActivity(f, {}, () => false);
    expect(a).toEqual({
      role: 'executor',
      sessionId: null,
      session: null,
      inProgress: false,
      time: null,
    });
  });

  it('is null for a flight with no assignments', () => {
    const byId = {
      p: live({ id: 'p' }),
      x: live({ id: 'x' }),
    };
    const f = flight('fl-1', { sessions: [] });
    expect(flightActivity(f, byId, () => false)).toBeNull();
  });
});

describe('sortStartedFlights', () => {
  it('orders by activity time, with timeless flights last by newest created_at', () => {
    const a = flight('fl-1', {
      sessions: [assign('planner', 'p')],
      workflow_state: 'planning',
      created_at: '2026-01-01T00:00:00Z',
    });

    const b = flight('fl-2', {
      sessions: [assign('planner', 'q')],
      workflow_state: 'planning',
      created_at: '2026-01-02T00:00:00Z',
    });

    const c = flight('fl-3', {
      sessions: [assign('planner', '', '2026-01-03T00:00:00Z')],
      workflow_state: 'planning',
      created_at: '2026-01-03T00:00:00Z',
    });

    const sessionsById = {
      p: live({ id: 'p', last_output_at: '2026-01-01T01:00:00Z' }),
      q: live({ id: 'q', last_output_at: '2026-01-01T02:00:00Z' }),
    };
    const activityOf = (f: FlightSummaryResponse) => flightActivity(f, sessionsById, () => false);
    const sorted = sortStartedFlights([a, b, c], activityOf);
    // p@01:00 → fl-1, q@02:00 → fl-2; fl-3 has no time and ties on
    // created_at newest-first.
    expect(sorted.map((f) => f.id)).toEqual(['fl-2', 'fl-1', 'fl-3']);
  });
});

it('keeps pilot chatter out of working-session activity', () => {
  const f = flight('f', { sessions: [assign('pilot', 'pilot'), assign('planner', 'planner')] });
  const sessions = {
    pilot: live({ id: 'pilot', last_output_at: '2026-01-01T03:00:00Z' }),
    planner: live({ id: 'planner', last_output_at: '2026-01-01T01:00:00Z' }),
  };
  expect(flightActivity(f, sessions, () => false)?.sessionId).toBe('planner');
});
