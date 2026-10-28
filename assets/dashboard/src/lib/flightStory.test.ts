import { describe, expect, it } from 'vitest';
import { actorNames, buildFlightStory, type StoryFlight } from './flightStory';
import type {
  FlightMessage,
  FlightMonitoredTurn,
  FlightOperation,
  FlightSessionAssignment,
} from './types.generated';

const T = (minute: number) => `2026-10-27T19:${String(minute).padStart(2, '0')}:00-07:00`;

function assignment(
  role: string,
  sessionId: string,
  prior: string[] = []
): FlightSessionAssignment {
  return {
    id: `sa-${role}`,
    role: role as FlightSessionAssignment['role'],
    target: 'claude',
    session_id: sessionId,
    prior_session_ids: prior,
    outcome: 'succeeded',
    available: true,
    created_at: T(0),
    updated_at: T(0),
  };
}

function operation(
  id: string,
  action: string,
  status: string,
  at: string,
  extra: Partial<FlightOperation> = {}
): FlightOperation {
  return {
    id,
    action,
    correlation_id: 'c',
    status,
    created_at: at,
    updated_at: at,
    ...extra,
  };
}

function turn(
  id: string,
  sessionId: string,
  at: string,
  extra: Partial<FlightMonitoredTurn> = {}
): FlightMonitoredTurn {
  return {
    id,
    session_id: sessionId,
    start_seq: 1,
    end_seq: 2,
    text: 'Response text.',
    outcome: 'completed',
    status: 'classified',
    created_at: at,
    updated_at: at,
    ...extra,
  };
}

function message(
  id: string,
  kind: string,
  recipient: string,
  text: string,
  at: string,
  extra: Partial<FlightMessage> = {}
): FlightMessage {
  return {
    id,
    kind,
    recipient_session_id: recipient,
    text,
    status: 'accepted',
    created_at: at,
    updated_at: at,
    ...extra,
  };
}

const sessions = [
  assignment('pilot', 'pilot-1'),
  assignment('planner', 'planner-1'),
  assignment('executor', 'executor-1'),
  assignment('reviewer', 'reviewer-3', ['reviewer-1', 'reviewer-2']),
];

describe('actorNames', () => {
  it('names a role plainly and numbers a role that has been restarted or replaced', () => {
    const names = actorNames(sessions);
    expect(names.get('planner-1')).toBe('Planner');
    expect(names.get('reviewer-1')).toBe('Reviewer 1');
    expect(names.get('reviewer-2')).toBe('Reviewer 2');
    expect(names.get('reviewer-3')).toBe('Reviewer 3');
    expect(names.get('pilot-1')).toBe('Pilot');
  });
});

describe('buildFlightStory', () => {
  it('tells the flight in plain words, newest first, with raw identifiers behind each entry', () => {
    const flight: StoryFlight = {
      sessions,
      operations: [
        operation('op-1', 'takeoff', 'succeeded', T(1)),
        operation('op-2', 'execution', 'succeeded', T(10)),
        operation('op-3', 'review', 'succeeded', T(20)),
        operation('op-4', 'next_review', 'succeeded', T(30)),
      ],
      monitor: {
        turns: [
          turn('turn-reviewer-1-2', 'reviewer-1', T(25), {
            review: { skill: 'test-rules-review skill file read at record 37' },
            attempts: [
              {
                created_at: T(25),
                answers: [
                  { id: 'review_state', choice: 'finished', confidence: 0.9 },
                  { id: 'tests', choice: 'run_passed', confidence: 0.8 },
                  { id: 'changed_work', choice: 'changed', confidence: 0.95 },
                ],
              },
            ],
          }),
        ],
        messages: [
          message('msg-n', 'notification', 'pilot-1', 'Flight …; reviewer reviewer-1 …', T(26)),
          message(
            'msg-nr',
            'next_reviewer',
            'reviewer-2',
            'The previous reviewer changed the parser; re-check it.',
            T(30),
            { sender_session_id: 'pilot-1' }
          ),
          message('msg-rn', 'review_notice', 'pilot-1', 'A reviewer has now been launched', T(30)),
        ],
      },
    };

    const story = buildFlightStory(flight);

    expect(story.map((e) => e.headline)).toEqual([
      'Pilot asked for another reviewer (Reviewer 2): The previous reviewer changed the parser; re-check it.',
      'Reviewer 1: finished, tests run and passed, changed the work',
      'Entered review: reviewer started',
      'Entered execution: executor started',
      'Took off: workspace allocated, pilot and planner started',
    ]);
    const reviewerTurn = story[1];
    expect(reviewerTurn.tag).toEqual({ text: 'test-rules-review read', tone: 'ok' });
    expect(reviewerTurn.text).toBe('Response text.');
    expect(reviewerTurn.fields).toEqual(
      expect.arrayContaining([
        { label: 'Turn', value: 'turn-reviewer-1-2', mono: true },
        { label: 'Session', value: 'reviewer-1', mono: true },
        { label: 'Records', value: '1–2' },
        { label: 'Skill check', value: 'test-rules-review skill file read at record 37' },
      ])
    );
    expect(reviewerTurn.answers?.map((a) => a.choice)).toEqual([
      'finished',
      'run_passed',
      'changed',
    ]);
    // Notifications, launch notices, and a successful next_review operation
    // are the mechanism, not the story.
    expect(story.some((e) => e.id === 'msg-n' || e.id === 'msg-rn' || e.id === 'op-4')).toBe(false);
  });

  it('summarizes planner, executor, and committer turns from the daemon checks and classifiers', () => {
    const flight: StoryFlight = {
      sessions: [...sessions, assignment('commit', 'commit-1')],
      operations: [],
      monitor: {
        turns: [
          turn('t-p', 'planner-1', T(1), {
            artifacts: { spec_path: '/w/spec.md', plan_path: '/w/plan.md' },
            attempts: [
              {
                created_at: T(1),
                answers: [
                  { id: 'attention', choice: 'needs_attention', confidence: 0.9 },
                  { id: 'planning_artifacts', choice: 'both', confidence: 0.9 },
                ],
              },
            ],
          }),
          turn('t-e', 'executor-1', T(2), {
            execution: {
              skill: 'executing-plans not found',
              report_path: '/w/plan-execution.md',
              report_verified: false,
              report_error: 'not written yet',
            },
            attempts: [
              {
                created_at: T(2),
                answers: [
                  { id: 'attention', choice: 'routine', confidence: 0.9 },
                  { id: 'execution_state', choice: 'stopped', confidence: 0.7 },
                ],
              },
            ],
          }),
          turn('t-c', 'commit-1', T(3), {
            attempts: [
              {
                created_at: T(3),
                answers: [{ id: 'completion_state', choice: 'gate_failed', confidence: 0.9 }],
              },
            ],
          }),
        ],
      },
    };

    const story = buildFlightStory(flight);

    expect(story.map((e) => [e.headline, e.tag])).toEqual([
      ['Commit: a commit gate failed', undefined],
      [
        'Executor: stopped on a question or blocker, report not written yet',
        { text: 'executing-plans not found', tone: 'error' },
      ],
      [
        'Planner: needs attention, claims the spec and plan written, spec and plan verified',
        undefined,
      ],
    ]);
  });

  it('shows failures as failures with a retry handle, and skips empty turns', () => {
    const flight: StoryFlight = {
      sessions,
      operations: [
        operation('op-f', 'takeoff', 'failed', T(1), { error: 'spawn planner: boom' }),
        operation('op-nr', 'next_review', 'failed', T(5), { error: 'spawn reviewer: no capacity' }),
      ],
      monitor: {
        turns: [
          turn('t-fail', 'executor-1', T(2), {
            outcome: 'failed',
            error: 'the harness stopped the turn',
            status: 'skipped',
          }),
          turn('t-class', 'executor-1', T(3), {
            status: 'failed',
            attempts: [{ created_at: T(3), error: 'TypeSafe API key is not configured' }],
          }),
          turn('t-empty', 'executor-1', T(4), { text: '', status: 'skipped' }),
        ],
        messages: [
          message('msg-g', 'pilot_message', 'executor-1', 'Fix the test.', T(6), {
            sender_session_id: 'pilot-1',
            status: 'failed',
            error: 'executor session is not running',
          }),
          message('msg-dn', 'notification', 'pilot-1', 'Flight …', T(7), {
            status: 'failed',
            error: 'pilot session is not running',
          }),
        ],
      },
    };

    const story = buildFlightStory(flight);

    expect(story.map((e) => [e.headline, e.tone, e.retryId])).toEqual([
      ['Notifying the Pilot failed: pilot session is not running', 'error', 'msg-dn'],
      [
        'Pilot → Executor: Fix the test. (not delivered: executor session is not running)',
        'error',
        'msg-g',
      ],
      ['Starting another reviewer failed: spawn reviewer: no capacity', 'error', undefined],
      [
        'Executor responded; classification failed: TypeSafe API key is not configured',
        'error',
        't-class',
      ],
      ["Executor's turn failed: the harness stopped the turn", 'error', undefined],
      ['Takeoff failed: spawn planner: boom', 'error', undefined],
    ]);
  });

  it("shows the pilot's prose and failed decisions, not its JSON decisions", () => {
    const flight: StoryFlight = {
      sessions,
      operations: [],
      monitor: {
        turns: [
          turn('t-json', 'pilot-1', T(1), { text: '{"guidance":null}', status: 'skipped' }),
          turn('t-prose', 'pilot-1', T(2), {
            text: 'No. The second reviewer’s notification omitted its notification ID.',
            status: 'skipped',
          }),
          turn('t-rej', 'pilot-1', T(3), {
            text: '{"notification_id":"x","next_reviewer":"go again"}',
            status: 'failed',
            attempts: [
              {
                created_at: T(3),
                error:
                  'flight state conflict: pilot decision must reference a delivered notification',
              },
            ],
          }),
        ],
        messages: [
          message('msg-rej', 'rejection', 'pilot-1', 'Schmux rejected your decision…', T(3), {
            source_turn_id: 't-rej',
          }),
          message(
            'msg-stop',
            'pilot_stop',
            'pilot-1',
            'the reviewer chain is not converging',
            T(4),
            {
              sender_session_id: 'pilot-1',
            }
          ),
        ],
      },
    };

    const story = buildFlightStory(flight);

    expect(story.map((e) => [e.headline, e.tone])).toEqual([
      ['Pilot stopped the flight: the reviewer chain is not converging', 'error'],
      [
        "Pilot's decision could not be applied: flight state conflict: pilot decision must reference a delivered notification",
        'error',
      ],
      ['Pilot: No. The second reviewer’s notification omitted its notification ID.', 'neutral'],
    ]);
    expect(story[1].retryId).toBe('t-rej');
    expect(story[1].text).toBe('{"notification_id":"x","next_reviewer":"go again"}');
  });
});
