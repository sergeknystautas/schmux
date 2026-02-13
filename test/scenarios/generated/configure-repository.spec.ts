import { test, expect } from '@playwright/test';
import { createTestRepo, waitForDashboardLive, waitForHealthy } from './helpers';

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

  test('new repo appears in spawn page', async ({ page }) => {
    // Navigate to the spawn page
    await page.goto('/spawn');
    await waitForDashboardLive(page);

    // Verify the repo dropdown contains the newly added repo
    const repoSelect = page.locator('[data-testid="spawn-repo-select"]');
    await expect(repoSelect).toBeVisible();
    await expect(repoSelect).toContainText(repoName);
  });
});
