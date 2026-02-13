import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForTerminalOutput,
  sleep,
} from './helpers';

test.describe.serial('View live terminal output', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-terminal');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'tick-agent',
          command: "sh -c 'for i in $(seq 1 10); do echo tick-$i; sleep 0.5; done; sleep 600'",
          promptable: true,
        },
      ],
    });

    // Spawn a session via API
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      prompt: 'test',
      targets: { 'tick-agent': 1 },
    });
    sessionId = results[0].session_id;

    // Wait for the session to start producing output
    await sleep(2000);
  });

  test('terminal viewport is visible on session page', async ({ page }) => {
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // Wait for the terminal viewport to render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify: terminal viewport is visible
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();

    // Verify: session status shows Running
    const statusPill = page.locator('[data-testid="session-status"]');
    await expect(statusPill).toBeVisible();
    await expect(statusPill).toHaveText(/Running/);
  });

  test('terminal receives output via WebSocket', async () => {
    // Use the WebSocket helper to verify the backend streams terminal output.
    // This connects directly to ws://localhost:7337/ws/terminal/{sessionId}
    // and waits for a message containing the substring.
    const buffer = await waitForTerminalOutput(sessionId, 'tick-', 15_000);
    expect(buffer).toContain('tick-');
  });
});
