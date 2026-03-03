import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  sleep,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

test.describe.serial('Action dropdown shows quick launch and emerged sections', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-actions');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
          promptable: true,
        },
      ],
      quickLaunch: [{ name: 'echo-agent', target: 'echo-agent' }],
    });

    // Spawn a session so we have a workspace with the tab bar visible
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });
    workspaceId = results[0].workspace_id;
    await waitForSessionRunning(results[0].session_id);
  });

  test('action dropdown shows both sections with correct structure', async ({ page }) => {
    // Navigate to the session page so the tab bar is visible
    const workspaces = await getSessions();
    const ws = workspaces.find((w) => w.id === workspaceId);
    const sessionId = ws!.sessions[0].id;
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // Wait for session tabs to render
    await page.waitForSelector('[data-tour="session-tabs"]', { timeout: 15000 });

    // Click the "+" button to open the action dropdown
    const addButton = page.locator('[data-tour="session-tab-add"]');
    await expect(addButton).toBeVisible();
    await addButton.click();

    // Wait for the dropdown menu to appear
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible({ timeout: 5000 });

    // Verify: "Spawn a session..." is at the top
    await expect(menu.getByText('Spawn a session...')).toBeVisible();

    // Verify: Quick Launch section header with manage link
    await expect(menu.getByText('Quick Launch')).toBeVisible();

    // Verify: Emerged section header with manage link
    await expect(menu.getByText('Emerged', { exact: true })).toBeVisible();

    // Verify: two "manage" links
    const manageLinks = menu.getByText('manage');
    await expect(manageLinks).toHaveCount(2);

    // Verify: quick launch preset appears
    await expect(menu.getByRole('menuitem', { name: 'echo-agent' })).toBeVisible();

    // Verify: emerged section shows empty state (no actions pinned yet)
    await expect(menu.getByText('No emerged actions yet')).toBeVisible();
  });

  test('quick launch manage link navigates to config page', async ({ page }) => {
    const workspaces = await getSessions();
    const ws = workspaces.find((w) => w.id === workspaceId);
    const sessionId = ws!.sessions[0].id;
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    await page.waitForSelector('[data-tour="session-tabs"]', { timeout: 15000 });

    // Open the dropdown
    await page.locator('[data-tour="session-tab-add"]').click();
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible({ timeout: 5000 });

    // Click the first "manage" link (Quick Launch)
    const manageLinks = menu.getByText('manage');
    await manageLinks.first().click();

    // Should navigate to config page with quicklaunch tab
    await page.waitForURL(/\/config\?tab=quicklaunch/, { timeout: 5000 });
    expect(page.url()).toContain('/config?tab=quicklaunch');
  });

  test('quick launch preset spawns a session', async ({ page }) => {
    const workspaces = await getSessions();
    const ws = workspaces.find((w) => w.id === workspaceId);
    const initialSessionCount = ws!.sessions.length;
    const sessionId = ws!.sessions[0].id;
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    await page.waitForSelector('[data-tour="session-tabs"]', { timeout: 15000 });

    // Open the dropdown
    await page.locator('[data-tour="session-tab-add"]').click();
    const menu = page.getByRole('menu');
    await expect(menu).toBeVisible({ timeout: 5000 });

    // Click the echo-agent quick launch item
    await menu.getByRole('menuitem', { name: 'echo-agent' }).click();

    // Wait for a new session to appear in the API (poll until count increases)
    const deadline = Date.now() + 15000;
    let newCount = initialSessionCount;
    while (Date.now() < deadline) {
      const updated = await getSessions();
      const totalSessions = updated.flatMap((w) => w.sessions).length;
      if (totalSessions > initialSessionCount) {
        newCount = totalSessions;
        break;
      }
      await sleep(500);
    }
    expect(newCount).toBeGreaterThan(initialSessionCount);
  });
});
