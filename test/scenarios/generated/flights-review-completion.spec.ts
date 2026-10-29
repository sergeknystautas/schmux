import { test, expect } from './coverage-fixture';
import type { Route, WebSocketRoute } from '@playwright/test';
import { seedConfig, waitForDashboardLive, waitForHealthy } from './helpers';

// The scenario image has no chat harness, so the daemon cannot start real
// reviewer or committer sessions, and it cannot run a pilot that decides to
// advance a phase. The flight API and the /ws/dashboard snapshot are
// delivered under test control while the real detail page, action buttons,
// API client, and snapshot reducer run. The daemon's own review and
// completion transitions, and the pilot's advance decisions, are covered by
// the Go manager and pilot tests.

const FLIGHT_ID = 'fl-scenario';
const WORKSPACE_ID = 'flight-ws';
const T0 = '2026-10-26T10:00:00Z';
const DECIDING_TURN = 'turn-flight-ws-pilot-7';

type Assignment = { role: string; session: string; at: string };

type Operation = {
  id: string;
  action: string;
  status: string;
  source_turn_id?: string;
  error?: string;
};

function flightIn(
  state: string,
  assignments: Assignment[],
  options: { operations?: Operation[]; pilotStop?: string; lastError?: string } = {}
) {
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
    operations: (options.operations ?? []).map((op) => ({
      id: op.id,
      action: op.action,
      correlation_id: `corr-${op.id}`,
      status: op.status,
      ...(op.source_turn_id ? { source_turn_id: op.source_turn_id } : {}),
      ...(op.error ? { error: op.error } : {}),
      created_at: T0,
      updated_at: T0,
    })),
    ...(options.pilotStop
      ? {
          pilot_stop: {
            workflow_state: state,
            reason: options.pilotStop,
            source_turn_id: DECIDING_TURN,
            created_at: T0,
          },
        }
      : {}),
    ...(options.lastError ? { last_error: options.lastError } : {}),
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

const PLANNING: Assignment[] = [
  { role: 'pilot', session: 'flight-ws-pilot', at: '2026-10-26T10:01:00Z' },
  { role: 'planner', session: 'flight-ws-planner', at: '2026-10-26T10:02:00Z' },
];
const EXECUTING: Assignment[] = [
  ...PLANNING,
  { role: 'executor', session: 'flight-ws-executor', at: '2026-10-26T10:03:00Z' },
];
const REVIEWING: Assignment[] = [
  ...EXECUTING,
  { role: 'reviewer', session: 'flight-ws-reviewer', at: '2026-10-26T10:04:00Z' },
];
const COMPLETING: Assignment[] = [
  ...REVIEWING,
  { role: 'commit', session: 'flight-ws-commit', at: '2026-10-26T10:05:00Z' },
];

test.describe('Flights: review and completion', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    await seedConfig();
  });

  test('the pilot advances the flight and the page follows without a phase click', async ({
    page,
  }) => {
    let current = flightIn('planning', PLANNING);
    let dashboard: WebSocketRoute | undefined;
    const transitions: string[] = [];

    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      dashboard = ws;
      ws.send(snapshot(current, PLANNING));
    });
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));
    await page.route(`**/api/flights/${FLIGHT_ID}/**`, (route: Route) => {
      transitions.push(route.request().url());
      return route.fulfill({ status: 409, json: { error: 'unexpected transition request' } });
    });

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    const state = page.getByTestId(`flight-state-${FLIGHT_ID}`);
    const activity = page.getByTestId(`flight-activity-${FLIGHT_ID}`);

    // Planning offers Enter execution and not Review and commit.
    await expect(state).toHaveText('planning');
    await expect(page.getByTestId(`flight-action-execution-${FLIGHT_ID}`)).toHaveText(
      'Enter execution'
    );
    await expect(page.getByTestId(`flight-action-completion-${FLIGHT_ID}`)).toHaveCount(0);

    // The pilot advances the flight; the daemon broadcasts the new phase and
    // the page follows it. Nothing was clicked.
    current = flightIn('executing', EXECUTING, {
      operations: [
        {
          id: 'op-auto-exec',
          action: 'execution',
          status: 'succeeded',
          source_turn_id: DECIDING_TURN,
        },
      ],
    });
    dashboard!.send(snapshot(current, EXECUTING));
    await expect(state).toHaveText('executing');
    await expect(activity).toContainText('Executing · Executor');
    await expect(activity.getByRole('link')).toHaveAttribute(
      'href',
      '/sessions/flight-ws-executor'
    );

    // And again into review.
    current = flightIn('reviewing', REVIEWING, {
      operations: [
        {
          id: 'op-auto-exec',
          action: 'execution',
          status: 'succeeded',
          source_turn_id: DECIDING_TURN,
        },
        {
          id: 'op-auto-review',
          action: 'review',
          status: 'succeeded',
          source_turn_id: DECIDING_TURN,
        },
      ],
    });
    dashboard!.send(snapshot(current, REVIEWING));
    await expect(state).toHaveText('reviewing');
    await expect(activity).toContainText('Reviewing · Reviewer');
    await expect(activity.getByRole('link')).toHaveAttribute(
      'href',
      '/sessions/flight-ws-reviewer'
    );

    // No phase button was pressed, so no transition was requested. The
    // absence is proven over the two semantic boundaries above — the page
    // followed both snapshots — not over a timed window.
    expect(transitions).toEqual([]);
  });

  test('the story names the pilot turn that decided an automatic transition', async ({ page }) => {
    const current = flightIn('executing', EXECUTING, {
      operations: [
        {
          id: 'op-auto-exec',
          action: 'execution',
          status: 'succeeded',
          source_turn_id: DECIDING_TURN,
        },
      ],
    });
    await page.routeWebSocket('**/ws/dashboard', (ws) => ws.send(snapshot(current, EXECUTING)));
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    const entry = page.locator('[data-entry-id="op-auto-exec"]');
    await expect(entry).toBeVisible();
    await expect(entry.getByTestId('flight-story-headline')).toContainText(
      "at the pilot's request"
    );
    await entry.getByTestId('flight-story-toggle').click();
    await expect(entry.getByTestId('flight-story-detail')).toContainText('From turn');
    await expect(entry.getByTestId('flight-story-detail')).toContainText(DECIDING_TURN);
  });

  test('Review and commit is the human checkpoint: nothing asks for completion before the click', async ({
    page,
  }) => {
    let current = flightIn('reviewing', REVIEWING);
    let dashboard: WebSocketRoute | undefined;
    const transitions: string[] = [];

    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      dashboard = ws;
      ws.send(snapshot(current, REVIEWING));
    });
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));
    const transition = async (route: Route) => {
      transitions.push(`${route.request().method()} completion`);
      current = flightIn('completing', COMPLETING);
      await route.fulfill({ json: { flight: current } });
      dashboard!.send(snapshot(current, COMPLETING));
    };
    await page.route(`**/api/flights/${FLIGHT_ID}/completion`, transition);

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    const state = page.getByTestId(`flight-state-${FLIGHT_ID}`);
    const activity = page.getByTestId(`flight-activity-${FLIGHT_ID}`);
    const completion = page.getByTestId(`flight-action-completion-${FLIGHT_ID}`);

    // Reviewing offers Review and commit, and the action explains itself.
    await expect(state).toHaveText('reviewing');
    await expect(completion).toHaveText('Review and commit');
    await expect(
      page.getByText('Review the work, artifacts, and running changes, then start the commit.')
    ).toBeVisible();

    // Nothing requested completion until the click.
    expect(transitions).toEqual([]);

    await completion.click();
    await expect(state).toHaveText('completing');
    await expect(activity).toContainText('Completing · Commit');
    await expect(activity.getByRole('link')).toHaveAttribute('href', '/sessions/flight-ws-commit');
    // Completing is the last phase: no further transition, only Dispose.
    await expect(page.getByTestId(`flight-action-completion-${FLIGHT_ID}`)).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Dispose' })).toBeVisible();

    expect(transitions).toEqual(['POST completion']);
  });

  test('a pilot stop keeps the manual actions available', async ({ page }) => {
    const current = flightIn('reviewing', REVIEWING, {
      pilotStop: 'the reviewer keeps changing the same file',
    });
    await page.routeWebSocket('**/ws/dashboard', (ws) => ws.send(snapshot(current, REVIEWING)));
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));
    await page.route(`**/api/flights/${FLIGHT_ID}/completion`, (route: Route) =>
      route.fulfill({ json: { flight: current } })
    );

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    await expect(page.getByTestId('flight-detail-pilot-stop')).toContainText(
      'the reviewer keeps changing the same file'
    );
    await expect(page.getByTestId(`flight-action-completion-${FLIGHT_ID}`)).toBeEnabled();
  });

  test('an unresolved operation is reported in words, naming the operation', async ({ page }) => {
    const current = flightIn('executing', EXECUTING);
    await page.routeWebSocket('**/ws/dashboard', (ws) => ws.send(snapshot(current, EXECUTING)));
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: current }));
    await page.route(`**/api/flights/${FLIGHT_ID}/review`, (route: Route) =>
      route.fulfill({
        status: 409,
        json: {
          error:
            'flight state conflict: flight fl-scenario has an unresolved execution operation op-42',
        },
      })
    );

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    await page.getByTestId(`flight-action-review-${FLIGHT_ID}`).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(
      'flight fl-scenario has an unresolved execution operation op-42'
    );
  });

  test('a reconnect with an incomplete commit claims nothing about a merge', async ({ page }) => {
    const started = flightIn('completing', COMPLETING);
    const broken = flightIn('completing', COMPLETING, {
      operations: [
        {
          id: 'op-commit',
          action: 'completion',
          status: 'failed',
          error: 'spawn commit agent: boom',
        },
      ],
      lastError: 'spawn commit agent: boom',
    });
    let dashboard: WebSocketRoute | undefined;
    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      dashboard = ws;
      ws.send(snapshot(started, COMPLETING));
    });
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: started }));

    await page.goto(`/flights/${FLIGHT_ID}`);
    await waitForDashboardLive(page);

    // The daemon reconnects with a completing flight whose commit failed.
    await page.route(`**/api/flights/${FLIGHT_ID}`, (route) => route.fulfill({ json: broken }));
    dashboard!.send(snapshot(broken, COMPLETING));

    await expect(page.getByTestId('flight-detail-last-error')).toContainText(
      'spawn commit agent: boom'
    );
    await expect(page.getByText(/ready to merge/i)).toHaveCount(0);
  });
});
