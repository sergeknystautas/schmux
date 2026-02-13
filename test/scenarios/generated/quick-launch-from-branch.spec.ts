import { test, expect } from '@playwright/test';
import { execSync } from 'child_process';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
  apiGet,
} from './helpers';

test.describe.serial('Quick launch a session from a recent branch', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();

    // Create a test repo with extra branches
    repoPath = await createTestRepo('test-repo-branches');

    // Create feature branches with commits so they appear in recent branches
    execSync(`git -C ${repoPath} checkout -b feature-alpha`);
    execSync(`echo "alpha work" > ${repoPath}/alpha.txt`);
    execSync(`git -C ${repoPath} add . && git -C ${repoPath} commit -m "add alpha feature"`);
    execSync(`git -C ${repoPath} checkout main`);

    execSync(`git -C ${repoPath} checkout -b feature-beta`);
    execSync(`echo "beta work" > ${repoPath}/beta.txt`);
    execSync(`git -C ${repoPath} add . && git -C ${repoPath} commit -m "add beta feature"`);
    execSync(`git -C ${repoPath} checkout main`);

    // Seed config with the repo and a promptable agent
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

    // Spawn a session on main to trigger bare clone creation
    await spawnSession({
      repo: repoPath,
      branch: 'main',
      prompt: 'init',
      targets: { 'echo-agent': 1 },
    });

    // Wait for bare clone to be ready and branches to be queryable
    await sleep(3000);

    // Poll recent-branches API until branches appear (bare clone may take a moment)
    for (let attempt = 0; attempt < 10; attempt++) {
      try {
        const branches = await apiGet<Array<{ branch: string }>>('/api/recent-branches?limit=10');
        if (branches && branches.length > 0) {
          break;
        }
      } catch {
        // not ready yet
      }
      await sleep(1000);
    }
  });

  test('home page shows recent branches', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Wait for the recent branches card to be visible
    const recentBranchesCard = page.locator('[data-testid="recent-branches"]');
    await expect(recentBranchesCard).toBeVisible({ timeout: 15000 });

    // Wait for at least one branch button to appear inside the card
    const branchButtons = recentBranchesCard.locator('button');
    await branchButtons.first().waitFor({ state: 'visible', timeout: 15000 });

    const count = await branchButtons.count();
    expect(count).toBeGreaterThanOrEqual(1);

    // Verify branch entries show branch name and repo info
    const firstBranch = branchButtons.first();
    const branchText = await firstBranch.textContent();
    expect(branchText).toBeTruthy();
    // The branch name should contain one of our created branches
    expect(branchText).toMatch(/feature-alpha|feature-beta/);
  });

  test('clicking branch navigates to spawn page', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Wait for the recent branches card and branch buttons to appear
    const recentBranchesCard = page.locator('[data-testid="recent-branches"]');
    await expect(recentBranchesCard).toBeVisible({ timeout: 15000 });

    const branchButtons = recentBranchesCard.locator('button');
    await branchButtons.first().waitFor({ state: 'visible', timeout: 15000 });

    // Click the first branch button
    await branchButtons.first().click();

    // Wait for navigation to /spawn (the click triggers an API call then navigates)
    await page.waitForURL(/\/spawn/, { timeout: 30000 });
    expect(page.url()).toMatch(/\/spawn/);

    // Verify the spawn page loaded — the submit button should be visible
    const submitButton = page.locator('[data-testid="spawn-submit"]');
    await expect(submitButton).toBeVisible({ timeout: 15000 });

    // In prefilled mode the repo/branch selects are hidden (already set via location state).
    // Verify the prompt textarea has auto-generated content from prepare-branch-spawn.
    const promptTextarea = page.locator('[data-testid="spawn-prompt"]');
    await expect(promptTextarea).toBeVisible({ timeout: 10000 });
    const promptValue = await promptTextarea.inputValue();
    expect(promptValue.length).toBeGreaterThan(0);
  });
});
