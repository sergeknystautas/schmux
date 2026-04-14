import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
  apiPost,
} from './helpers';

test.describe.serial('Share workspace intent via repofeed', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-share-intent');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo working; sleep 600'",
        },
      ],
    });

    // Enable repofeed
    await apiPost('/api/config', {
      repofeed: { enabled: true },
    });

    // Spawn a session so we have a workspace
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      targets: { 'echo-agent': 1 },
    });
    workspaceId = results[0].workspace_id;
    await waitForSessionRunning(results[0].session_id);
  });

  test('repofeed page shows outgoing and incoming sections', async ({ page }) => {
    await page.goto('/repofeed');
    await waitForDashboardLive(page);

    // Verify Outgoing section exists
    await expect(page.locator('h3', { hasText: 'Outgoing' })).toBeVisible();

    // Verify Incoming section exists
    await expect(page.locator('h3', { hasText: 'Incoming' })).toBeVisible();

    // Verify workspace appears in outgoing with Share button
    await expect(page.locator('text=Share').first()).toBeVisible();
  });

  test('toggle share intent via API', async () => {
    // Share the workspace
    await apiPost(`/api/workspaces/${workspaceId}/share-intent`, { share: true });

    // Verify workspace is now shared via raw API response
    const res = await fetch(
      `${process.env.SCHMUX_BASE_URL || 'http://localhost:7337'}/api/sessions`
    );
    const workspaces = (await res.json()) as Array<Record<string, unknown>>;
    const ws = workspaces.find((w) => w.id === workspaceId);
    expect(ws).toBeDefined();
    expect(ws!.intent_shared).toBe(true);

    // Unshare the workspace
    await apiPost(`/api/workspaces/${workspaceId}/share-intent`, { share: false });

    // Verify workspace is no longer shared
    const res2 = await fetch(
      `${process.env.SCHMUX_BASE_URL || 'http://localhost:7337'}/api/sessions`
    );
    const workspaces2 = (await res2.json()) as Array<Record<string, unknown>>;
    const ws2 = workspaces2.find((w) => w.id === workspaceId);
    expect(ws2).toBeDefined();
    // intent_shared should be false or absent (omitempty)
    expect(ws2!.intent_shared).toBeFalsy();
  });

  test('share intent toggle works from repofeed page UI', async ({ page }) => {
    await page.goto('/repofeed');
    await waitForDashboardLive(page);

    // Click Share button for the workspace
    const shareButton = page.locator('button', { hasText: 'Share' }).first();
    await expect(shareButton).toBeVisible();
    await shareButton.click();

    // After clicking, button should change to Unshare
    await expect(page.locator('button', { hasText: 'Unshare' }).first()).toBeVisible({
      timeout: 5000,
    });
  });
});
