import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
} from './helpers';

test.describe.serial('Spawn multiple agents on the same task', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-multi');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'agent-alpha',
          command: "sh -c 'echo hello from alpha; sleep 600'",
          promptable: true,
        },
        {
          name: 'agent-beta',
          command: "sh -c 'echo hello from beta; sleep 600'",
          promptable: true,
        },
      ],
    });
  });

  test('spawn multiple agents via the UI', async ({ page }) => {
    await page.goto('/spawn');
    await waitForDashboardLive(page);

    // Fill the prompt textarea
    await page.locator('[data-testid="spawn-prompt"]').fill('Compare approaches for auth module');

    // Switch to "Multiple Agents" mode
    await page.locator('select').filter({ hasText: 'Single Agent' }).selectOption('multiple');

    // Click both agent toggle buttons
    await page.locator('[data-testid="agent-agent-alpha"]').click();
    await page.locator('[data-testid="agent-agent-beta"]').click();

    // Verify both buttons are selected (have btn--primary class)
    await expect(page.locator('[data-testid="agent-agent-alpha"]')).toHaveClass(/btn--primary/);
    await expect(page.locator('[data-testid="agent-agent-beta"]')).toHaveClass(/btn--primary/);

    // Select the repository
    await page.locator('[data-testid="spawn-repo-select"]').selectOption(repoPath);

    // Fill in the branch name
    await page.locator('#branch').fill('test-multi');

    // Submit the form
    await page.locator('[data-testid="spawn-submit"]').click();

    // Wait for navigation to a session detail page
    await page.waitForURL(/\/sessions\//, { timeout: 30000 });

    // Verify: URL matches /sessions/
    expect(page.url()).toMatch(/\/sessions\//);

    // Wait for the session detail page to fully render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    // Verify: terminal viewport is visible
    await expect(page.locator('[data-testid="terminal-viewport"]')).toBeVisible();
  });

  test('both sessions are navigable', async ({ page }) => {
    // Wait for sessions to be fully available
    await sleep(2000);

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
