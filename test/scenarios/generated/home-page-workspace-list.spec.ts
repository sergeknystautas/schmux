import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

test.describe.serial('View active workspaces on the home page', () => {
  let repoPathA: string;
  let repoPathB: string;
  let workspaceIdA: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    // Two repos so the workspace list spans multiple groups
    repoPathA = await createTestRepo('test-repo-home-a');
    repoPathB = await createTestRepo('test-repo-home-b');
    await seedConfig({
      repos: [repoPathA, repoPathB],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
        },
      ],
    });

    // Spawn sessions: two workspaces on repo A (same-repo adjacency)
    const resultsA = await spawnSession({
      repo: repoPathA,
      branch: 'branch-a',
      targets: { 'echo-agent': 1 },
    });
    workspaceIdA = resultsA[0].workspace_id;

    await spawnSession({
      repo: repoPathA,
      branch: 'branch-b',
      targets: { 'echo-agent': 1 },
    });

    // And one workspace on repo B (forces a separator above it)
    await spawnSession({
      repo: repoPathB,
      branch: 'main',
      targets: { 'echo-agent': 1 },
    });

    // Wait for sessions to be fully running
    await waitForSessionRunning();
  });

  test('home page shows workspace list', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Verify workspace-list is visible
    const workspaceList = page.locator('[data-testid="workspace-list"]');
    await expect(workspaceList).toBeVisible({ timeout: 15000 });

    // Verify at least 3 workspace rows exist (other tests may have created more).
    // Scope to buttons inside workspace-list to avoid matching the list container itself.
    const workspaceRows = workspaceList.locator('button[data-testid^="workspace-"]');
    const count = await workspaceRows.count();
    expect(count).toBeGreaterThanOrEqual(3);

    // Verify each row shows git stats
    for (let i = 0; i < count; i++) {
      const row = workspaceRows.nth(i);
      const gitStats = row.locator('[data-testid="git-stats"]');
      await expect(gitStats).toBeVisible();
    }
  });

  test('repo separator appears between workspaces from different repos', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Sidebar nav list (separate from the home page workspace table)
    const sidebarList = page.locator('.nav-workspaces');
    await expect(sidebarList).toBeVisible({ timeout: 15000 });

    // With two same-repo workspaces (repo A) plus one cross-repo workspace
    // (repo B) sorted together, the sidebar must render at least 1 separator
    // (between the A group and the B entry).
    const separatorCount = await sidebarList.locator('.nav-workspaces__repo-separator').count();
    expect(separatorCount).toBeGreaterThanOrEqual(1);
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
