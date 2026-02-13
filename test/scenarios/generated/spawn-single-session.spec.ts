import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  waitForDashboardLive,
  waitForHealthy,
} from './helpers';

test.describe('Spawn a single session', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo');

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
  });

  test('spawn a single session via the UI', async ({ page }) => {
    await page.goto('/spawn');
    await waitForDashboardLive(page);

    // Fill the prompt textarea
    await page.locator('[data-testid="spawn-prompt"]').fill('Add unit tests for the auth module');

    // Select the agent — in "Single Agent" mode, pick from the dropdown
    await page.getByRole('combobox').filter({ hasText: 'Select agent' }).selectOption('echo-agent');

    // Select the repository
    await page.locator('[data-testid="spawn-repo-select"]').selectOption(repoPath);

    // Fill in the branch name (required when branch_suggest is not configured)
    await page.locator('#branch').fill('test-branch');

    // Submit the form
    await page.locator('[data-testid="spawn-submit"]').click();

    // Wait for navigation to the session detail page
    await page.waitForURL(/\/sessions\//);

    // Verify: URL matches /sessions/
    expect(page.url()).toMatch(/\/sessions\//);

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
