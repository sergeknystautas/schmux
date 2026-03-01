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
    await expect(workspacesTab).toHaveClass(/wizard__step--active/);

    // Fill in the repo name
    await page.getByPlaceholder('Name').first().fill(repoName);

    // Fill in the repo URL (path to local repo)
    await page.getByPlaceholder('git@github.com:user/repo.git').fill(repoPath);

    // Click the Add button
    await page.locator('[data-testid="add-repo"]').click();

    // Verify the repo appears in the list (use exact match on the item name span)
    await expect(page.locator('.item-list__item-name', { hasText: repoName })).toBeVisible();

    // Click Save Changes
    const saveButton = page.locator('[data-testid="config-save"]');
    await expect(saveButton).toBeEnabled();
    await saveButton.click();

    // Verify save succeeds — the button becomes disabled after saving
    await expect(saveButton).toBeDisabled({ timeout: 10000 });
  });

  test('new repo appears in config API', async () => {
    // Verify the repo is present in the config via API
    // (The spawn page repo dropdown is only visible when models are available,
    // so we verify persistence via the config API instead)
    interface ConfigResp {
      repos: Array<{ name: string; url: string }>;
    }
    const config = await apiGet<ConfigResp>('/api/config');
    const match = config.repos.find((r) => r.name === repoName);
    expect(match).toBeDefined();
    expect(match!.url).toBe(repoPath);
  });
});
