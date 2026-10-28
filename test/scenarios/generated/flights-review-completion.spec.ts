import { test, expect } from './coverage-fixture';
import type { Route, WebSocketRoute } from '@playwright/test';
import { seedConfig, waitForDashboardLive, waitForHealthy } from './helpers';

// The scenario image has no chat harness, so the daemon cannot start real
// reviewer or committer sessions. The flight API and the /ws/dashboard
// snapshot are delivered under test control while the real detail page,
// action buttons, API client, and snapshot reducer run. The daemon's own
// review and completion transitions are covered by the Go handler and
// manager tests.

const FLIGHT_ID = 'fl-scenario';
const WORKSPACE_ID = 'flight-ws';
const T0 = '2026-10-26T10:00:00Z';

type Assignment = { role: string; session: string; at: string };

function flightIn(state: string, assignments: Assignment[]) {
  return {
    id: FLIGHT_ID,
    repo: 'https://example.com/flight.git',
    short_name: 'Review scenario flight',
    prompt: 'Make the change.',
    planner_model: 'claude',
    executor_model: 'codex',
    workflow_state: state,
    cleanup_state: 'idle',
    order: 0,
    workspace: {
      id: 'wa-scenario',
      requested_branch: `schmux/flight/${FLIGHT_ID}`,
      workspace_id: WORKSPACE_ID,
      path: '/tmp/flight-ws',
      branch: `schmux/flight/${FLIGHT_ID}`,
      outcome: 'succeeded',
      available: true,
      created_at: T0,
      updated_at: T0,
    },
    sessions: assignments.map((a) => ({
      id: `sa-${a.role}`,
      role: a.role,
      target: a.role === 'executor' || a.role === 'commit' ? 'codex' : 'claude',
      session_id: a.session,
      outcome: 'succeeded',
      available: true,
      created_at: a.at,
      updated_at: a.at,
    })),
    operations: [],
    created_at: T0,
    updated_at: assignments[assignments.length - 1].at,
  };
}

/** The dashboard snapshot the daemon broadcasts: the workspace's live sessions and the flight summary. */
function snapshot(flight: ReturnType<typeof flightIn>, assignments: Assignment[]) {
  const { prompt: _prompt, operations: _operations, ...summary } = flight;
  return JSON.stringify({
    type: 'sessions',
    workspaces: [
      {
        id: WORKSPACE_ID,
        repo: flight.repo,
        branch: flight.workspace.branch,
        path: flight.workspace.path,
        sessions: assignments.map((a) => ({
          id: a.session,
          target: a.role === 'executor' || a.role === 'commit' ? 'codex' : 'claude',
          kind: 'chat',
          running: true,
          branch: flight.workspace.branch,
          created_at: a.at,
          last_output_at: a.at,
          nudge_state: '',
          attach_cmd: '',
        })),
      },
    ],
    flights: [summary],
  });
}

test.describe('Flights: review and completion', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    await seedConfig();
  });

  test('an executing flight enters review, then completion, with each agent arriving by snapshot', async ({
    page,
  }) => {
    const executing: Assignment[] = [
      { role: 'pilot', session: 'flight-ws-pilot', at: '2026-10-26T10:01:00Z' },
      { role: 'planner', session: 'flight-ws-planner', at: '2026-10-26T10:02:00Z' },
      { role: 'executor', session: 'flight-ws-executor', at: '2026-10-26T10:03:00Z' },
    ];
    const reviewing = [
      ...executing,
      { role: 'reviewer', session: 'flight-ws-reviewer', at: '2026-10-26T10:04:00Z' },
    ];
    const completing = [
      ...reviewing,
      { role: 'commit', session: 'flight-ws-commit', at: '2026-10-26T10:05:00Z' },
    ];

    let current = flightIn('executing', executing);
    let dashboard: WebSocketRoute | undefined;
    const transitions: string[] = [];

    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      dashboard = ws;
      ws.send(snapshot(current, executing));
    });
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));
    // A transition answers with the moved flight, then the daemon broadcasts
    // the snapshot that carries the new session.
    const transition =
      (action: string, state: string, assignments: Assignment[]) => async (route: Route) => {
        transitions.push(`${route.request().method()} ${action}`);
        current = flightIn(state, assignments);
        await route.fulfill({ json: { flight: current } });
        dashboard!.send(snapshot(current, assignments));
      };
    await page.route(
      `**/api/flights/${FLIGHT_ID}/review`,
      transition('review', 'reviewing', reviewing)
    );
    await page.route(
      `**/api/flights/${FLIGHT_ID}/completion`,
      transition('completion', 'completing', completing)
    );

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    const state = page.getByTestId(`flight-state-${FLIGHT_ID}`);
    const activity = page.getByTestId(`flight-activity-${FLIGHT_ID}`);
    const review = page.getByTestId(`flight-action-review-${FLIGHT_ID}`);
    const completion = page.getByTestId(`flight-action-completion-${FLIGHT_ID}`);

    // Executing offers Enter review and not Enter completion.
    await expect(state).toHaveText('executing');
    await expect(review).toHaveText('Enter review');
    await expect(completion).toHaveCount(0);

    // Enter review: the phase moves, and the reviewer the snapshot carries
    // leads the activity line with a link to its session.
    await review.click();
    await expect(state).toHaveText('reviewing');
    await expect(activity).toContainText('Reviewing · Reviewer');
    await expect(activity.getByRole('link')).toHaveAttribute(
      'href',
      '/sessions/flight-ws-reviewer'
    );
    await expect(page.getByTestId('flight-session-link-reviewer')).toHaveAttribute(
      'href',
      '/sessions/flight-ws-reviewer'
    );

    // Reviewing offers Enter completion with no reviewer report at all.
    await expect(page.getByTestId('flight-detail-review-reading')).toHaveCount(0);
    await expect(completion).toHaveText('Enter completion');
    await expect(review).toHaveCount(0);

    // Enter completion: the committer arrives the same way.
    await completion.click();
    await expect(state).toHaveText('completing');
    await expect(activity).toContainText('Completing · Commit');
    await expect(activity.getByRole('link')).toHaveAttribute('href', '/sessions/flight-ws-commit');
    // Completing is the last phase: no further transition, only Dispose.
    await expect(page.getByTestId(`flight-action-done-${FLIGHT_ID}`)).toHaveCount(0);
    await expect(completion).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Dispose' })).toBeVisible();

    expect(transitions).toEqual(['POST review', 'POST completion']);
  });
});
