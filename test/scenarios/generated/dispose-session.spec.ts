import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
} from './helpers';

test.describe.serial('Dispose a session', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-dispose');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
          promptable: true,
        },
      ],
    });

    // Spawn a session via API (faster than going through UI)
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;

    // Wait for the session to be fully running
    await sleep(2000);
  });

  test('dispose session via the UI', async ({ page }) => {
    // Navigate to the session detail page
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // Wait for the session detail page to fully render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });
    await expect(page.locator('[data-testid="session-sidebar"]')).toBeVisible();

    // Verify the "Dispose Session" button is visible in the sidebar
    const disposeButton = page.locator('[data-testid="dispose-session"]');
    await expect(disposeButton).toBeVisible();

    // Click the "Dispose Session" button
    await disposeButton.click();

    // Wait for the confirmation dialog to appear
    const confirmButton = page.getByRole('button', { name: 'Confirm' });
    await expect(confirmButton).toBeVisible();

    // Confirm the dispose action
    await confirmButton.click();

    // After disposing the only session, the page stays on the same URL
    // but shows a "Session unavailable" message since the workspace persists
    // with no sessions remaining.
    await expect(page.getByText('Session unavailable')).toBeVisible({ timeout: 15000 });

    // The terminal viewport should no longer be visible
    await expect(page.locator('[data-testid="terminal-viewport"]')).not.toBeVisible();

    // The dispose button should no longer be visible (sidebar is gone)
    await expect(page.locator('[data-testid="dispose-session"]')).not.toBeVisible();
  });

  test('API confirms session was disposed', async () => {
    const workspaces = await getSessions();

    // The disposed session should not appear in any workspace
    for (const ws of workspaces) {
      const session = ws.sessions.find((s) => s.id === sessionId);
      expect(session).toBeUndefined();
    }
  });
});
