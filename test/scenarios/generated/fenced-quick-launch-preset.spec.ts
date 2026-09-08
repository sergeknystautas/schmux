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

test.describe.serial('Fenced quick launch preset fails loudly when fence is unavailable', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fenced-ql');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
          promptable: true,
        },
      ],
      quickLaunch: [
        // The scenarios Docker image does not ship the fence binary (only
        // Dockerfile.e2e installs test/e2e/fence-stub.sh). The honest
        // assertion is that the preset's fence reaches the gate, and the
        // gate hard-fails with the "fence not available" message.
        { name: 'fenced-build', command: 'echo fenced', fence: true },
      ],
    });

    // Spawn a session so we have a workspace with the tab bar visible.
    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('preset surfaces the fence-not-available error from the + dropdown', async ({ page }) => {
    // Sanity: confirm our seeded session is visible to the API.
    const sessions = await getSessions();
    const ws = sessions.find((w) => w.sessions.some((s) => s.id === sessionId));
    expect(ws, 'workspace with seeded session should exist').toBeDefined();

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    await page.waitForSelector('[data-tour="session-tabs"]', { timeout: 15000 });

    // Open the action dropdown.
    await page.locator('[data-tour="session-tab-add"]').click();
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible({ timeout: 5000 });

    // Verify the preset is listed.
    const presetItem = menu.getByRole('menuitem', { name: 'fenced-build' });
    await expect(presetItem).toBeVisible();

    // Clicking surfaces the fence-not-available toast. The scenarios image
    // does not install fence (see Dockerfile.e2e for the e2e stub).
    await presetItem.click();
    const toast = page.getByText(/fence not available/);
    await expect(toast).toBeVisible({ timeout: 10000 });
  });
});
