import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
  disposeAllSessions,
} from './helpers';

test.describe.serial('Verify multi-session workspace after API spawn', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    // Dispose leftover sessions from prior test files to prevent
    // accumulated sessions from slowing WebSocket broadcasts
    await disposeAllSessions();
    repoPath = await createTestRepo('test-repo-multi');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'agent-alpha',
          command: "sh -c 'echo hello from alpha; sleep 600'",
        },
        {
          name: 'agent-beta',
          command: "sh -c 'echo hello from beta; sleep 600'",
        },
      ],
    });

    // Spawn both agents via API
    await spawnSession({
      repo: repoPath,
      branch: 'test-multi',
      targets: { 'agent-alpha': 1, 'agent-beta': 1 },
    });

    await waitForSessionRunning();
  });

  test.afterAll(async () => {
    // Dispose all sessions to prevent accumulation across repeated test runs
    await disposeAllSessions();
  });

  test('spawn multiple agents via the UI', async ({ page }) => {
    // Wait for sessions to be running
    await waitForSessionRunning();
    const workspaces = await getSessions();
    const targetWs = workspaces.find((ws) =>
      ws.sessions.some((s) => s.target === 'agent-alpha' || s.target === 'agent-beta')
    );
    expect(targetWs).toBeDefined();
    expect(targetWs!.sessions.length).toBeGreaterThanOrEqual(1);

    // Navigate to a session detail page
    await page.goto(`/sessions/${targetWs!.sessions[0].id}`);
    await waitForDashboardLive(page);

    // Wait for the session detail page to fully render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify: terminal viewport is visible
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();
  });

  test('both sessions are navigable', async ({ page }) => {
    // Wait for sessions to be fully available
    await waitForSessionRunning();

    // Get the workspaces via API to find the session IDs
    const workspaces = await getSessions();

    // Each agent gets its own workspace, so find both
    const alphaWorkspace = workspaces.find((ws) =>
      ws.sessions.some((s) => s.target === 'agent-alpha')
    );
    const betaWorkspace = workspaces.find((ws) =>
      ws.sessions.some((s) => s.target === 'agent-beta')
    );
    expect(alphaWorkspace).toBeDefined();
    expect(betaWorkspace).toBeDefined();

    const alphaSessionId = alphaWorkspace!.sessions[0].id;
    const betaSessionId = betaWorkspace!.sessions[0].id;

    // Navigate to the alpha session
    await page.goto(`/sessions/${alphaSessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify the alpha session page loaded
    expect(page.url()).toContain(`/sessions/${alphaSessionId}`);
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();

    // Navigate to the beta session
    await page.goto(`/sessions/${betaSessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify the beta session page loaded (different URL)
    expect(page.url()).toContain(`/sessions/${betaSessionId}`);
    expect(page.url()).not.toContain(alphaSessionId);
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();
  });

  test('API confirms two sessions created with different targets', async () => {
    const workspaces = await getSessions();

    // Find sessions for our agents across all workspaces
    const allSessions = workspaces.flatMap((ws) => ws.sessions);
    const alphaSession = allSessions.find((s) => s.target === 'agent-alpha');
    const betaSession = allSessions.find((s) => s.target === 'agent-beta');

    expect(alphaSession).toBeDefined();
    expect(betaSession).toBeDefined();

    // Verify each session has a different target
    expect(alphaSession!.target).toBe('agent-alpha');
    expect(betaSession!.target).toBe('agent-beta');
    expect(alphaSession!.id).not.toBe(betaSession!.id);
  });
});
