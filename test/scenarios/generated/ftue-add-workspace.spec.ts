import { test, expect } from './coverage-fixture';
import {
  apiGet,
  createTestRepo,
  seedConfig,
  waitForDashboardLive,
  waitForHealthy,
} from './helpers';

interface ConfigResponse {
  repos: Array<{ name: string; url: string }>;
  [key: string]: unknown;
}

test.describe('Add first workspace and navigate to spawn', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('ftue-add-ws');

    // Seed an empty config: no repos, no run_targets
    // Agents are auto-detected from PATH
    await seedConfig({
      repos: [],
      agents: [],
    });
  });

  test('add workspace via modal and navigate to spawn', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Verify: "+ Add Workspace" CTA is visible
    const addWorkspaceCta = page.locator('[data-testid="add-workspace-cta"]');
    await expect(addWorkspaceCta).toBeVisible();

    // Click the CTA to open the modal
    await addWorkspaceCta.click();

    // Verify: modal shows "Clone from" label
    await expect(page.getByText('Clone from')).toBeVisible();

    // Verify: modal shows subtext about isolated copies
    await expect(page.getByText('your original stays untouched')).toBeVisible();

    // Enter the test repo path and submit via Enter key
    // (using Enter avoids issues with the suggestion dropdown overlapping the Add button)
    const input = page.locator('#repo-input');
    await input.fill(repoPath);
    await input.press('Enter');

    // Verify: spinner appears during access validation
    await expect(page.getByText('Checking repository access')).toBeVisible({ timeout: 5_000 });

    // Wait for navigation to spawn page
    await page.waitForURL('**/spawn', { timeout: 15_000 });

    // Verify: landed on the spawn page
    expect(page.url()).toContain('/spawn');

    // Verify: the repo is configured now
    const config = await apiGet<ConfigResponse>('/api/config');
    const addedRepo = config.repos.find((r) => r.url === repoPath);
    expect(addedRepo).toBeDefined();

    // Verify: spawn page shows at least one agent
    // Auto-detected agents should be available via the model catalog
    const agentSelector = page.locator('select, [data-testid="agent-select"]').first();
    if (await agentSelector.isVisible()) {
      const options = await agentSelector.locator('option').count();
      expect(options).toBeGreaterThan(0);
    }
  });
});
