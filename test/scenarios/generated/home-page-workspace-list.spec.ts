import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
} from './helpers';

test.describe.serial('View active workspaces on the home page', () => {
  let repoPath: string;
  let workspaceIdA: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-home');
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

    // Spawn two sessions on different branches to create two workspaces
    const resultsA = await spawnSession({
      repo: repoPath,
      branch: 'branch-a',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });
    workspaceIdA = resultsA[0].workspace_id;

    await spawnSession({
      repo: repoPath,
      branch: 'branch-b',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });

    // Wait for sessions to be fully running
    await sleep(2000);
  });

  test('home page shows workspace list', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Verify workspace-list is visible
    const workspaceList = page.locator('[data-testid="workspace-list"]');
    await expect(workspaceList).toBeVisible({ timeout: 15000 });

    // Verify at least 2 workspace rows exist (other tests may have created more).
    // Scope to buttons inside workspace-list to avoid matching the list container itself.
    const workspaceRows = workspaceList.locator('button[data-testid^="workspace-"]');
    const count = await workspaceRows.count();
    expect(count).toBeGreaterThanOrEqual(2);

    // Verify each row shows a session count (CSS module class names are hashed,
    // so match with attribute substring selector)
    for (let i = 0; i < count; i++) {
      const row = workspaceRows.nth(i);
      const sessionCount = row.locator('[class*="sessionCount"]');
      await expect(sessionCount).toBeVisible();
    }
  });

  test('clicking workspace navigates to session', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Wait for the workspace list to be visible
    const workspaceList = page.locator('[data-testid="workspace-list"]');
    await expect(workspaceList).toBeVisible({ timeout: 15000 });

    // Click a workspace that we know has a running session
    const targetRow = workspaceList.locator(`button[data-testid="workspace-${workspaceIdA}"]`);
    await expect(targetRow).toBeVisible();
    await targetRow.click();

    // Verify URL changes to /sessions/
    await page.waitForURL(/\/sessions\//, { timeout: 15000 });
    expect(page.url()).toMatch(/\/sessions\//);
  });
});
