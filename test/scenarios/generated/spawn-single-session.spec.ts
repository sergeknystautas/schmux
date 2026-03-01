import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

test.describe('Spawn a single session', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
        },
      ],
    });

    // Spawn a session via API
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('spawn a single session via the UI', async ({ page }) => {
    // Navigate to the session detail page
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // Wait for the session detail page to fully render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify: terminal viewport is visible
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();

    // Verify: session sidebar is visible
    await expect(page.locator('[data-testid="session-sidebar"]')).toBeVisible();

    // Verify: session status shows "Running" or "Stopped"
    const statusPill = page.locator('[data-testid="session-status"]');
    await expect(statusPill).toBeVisible();
    await expect(statusPill).toHaveText(/Running|Stopped/);
  });

  test('API confirms session was created', async () => {
    const workspaces = await getSessions();

    // Find the workspace containing our echo-agent session
    const workspace = workspaces.find((ws) => ws.sessions.some((s) => s.target === 'echo-agent'));
    expect(workspace).toBeDefined();
    expect(workspace!.sessions.length).toBeGreaterThanOrEqual(1);

    // The session target matches 'echo-agent'
    const session = workspace!.sessions.find((s) => s.target === 'echo-agent');
    expect(session).toBeDefined();
    expect(session!.target).toBe('echo-agent');
  });
});
