import { test, expect } from './coverage-fixture';
import { createTestRepo, waitForDashboardLive, waitForHealthy, apiGet } from './helpers';

test.describe.serial('Configure a new repository', () => {
  let repoPath: string;
  const repoName = 'test-config-repo';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo(repoName);
  });

  test('add repository via config page', async ({ page }) => {
    // Navigate to the config page
    await page.goto('/config');
    await waitForDashboardLive(page);

    // Verify the Workspaces tab is active (it's the default/first tab)
    const workspacesTab = page.locator('[data-testid="config-tab-workspaces"]');
    await expect(workspacesTab).toBeVisible();
    await expect(workspacesTab).toHaveAttribute('aria-selected', 'true');

    // Open the manual add form (collapsed by default behind a disclosure)
    await page.locator('summary', { hasText: 'Or add manually' }).click();

    // Fill in the repo name
    await page.getByPlaceholder('Name').first().fill(repoName);

    // Fill in the repo URL (path to local repo)
    await page.getByPlaceholder('git@github.com:user/repo.git').fill(repoPath);

    // Click the Add button
    await page.locator('[data-testid="add-repo"]').click();

    // Verify the repo appears in the list (use exact match on the item name span)
    await expect(page.locator('.item-list__item-name', { hasText: repoName })).toBeVisible();

    // Wait briefly for auto-save to complete
    await page.waitForTimeout(500);
  });

  test('new repo appears in config API', async () => {
    // Verify the repo is present in the config via API
    interface ConfigResp {
      repos: Array<{ name: string; url: string }>;
    }
    const config = await apiGet<ConfigResp>('/api/config');
    const match = config.repos.find((r) => r.name === repoName);
    expect(match).toBeDefined();
    expect(match!.url).toBe(repoPath);
  });
});
