import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
} from './helpers';

test.describe.serial('Lore page with repo tabs', () => {
  let repoPathA: string;
  let repoPathB: string;
  const repoNameA = 'test-lore-repo-a';
  const repoNameB = 'test-lore-repo-b';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPathA = await createTestRepo(repoNameA);
    repoPathB = await createTestRepo(repoNameB);
    await seedConfig({
      repos: [repoPathA, repoPathB],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
          promptable: true,
        },
      ],
    });
  });

  test('sidebar shows single Lore link', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Open the More menu to access Lore link
    await page.click('button.more-menu__toggle');
    // There should be exactly one "Lore" link, not one per repo
    const loreLinks = page.locator('.more-menu__dropdown a', { hasText: /^Lore/ });
    await expect(loreLinks).toHaveCount(1);
  });

  test('navigates to /lore via sidebar', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Open the More menu to access Lore link
    await page.click('button.more-menu__toggle');
    const loreLink = page.locator('.more-menu__dropdown a', { hasText: /^Lore/ });
    await loreLink.click();

    // URL should be /lore with no repo name parameter
    await page.waitForURL('/lore');
    expect(page.url()).toMatch(/\/lore$/);
  });

  test('page shows repo tab bar with both repos', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Page heading
    await expect(page.locator('h2', { hasText: 'Lore' })).toBeVisible();

    // Tab bar with both repos (uses session-tabs classes)
    const tabs = page.locator('.session-tabs .session-tab');
    await expect(tabs).toHaveCount(2);
    await expect(tabs.nth(0)).toContainText(repoNameA);
    await expect(tabs.nth(1)).toContainText(repoNameB);

    // First tab is active by default
    await expect(tabs.nth(0)).toHaveClass(/session-tab--active/);
    await expect(tabs.nth(1)).not.toHaveClass(/session-tab--active/);
  });

  test('page shows proposals and raw signals sections', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Empty state for proposals
    await expect(
      page.locator('.empty-state__description', { hasText: /No pending proposals/ })
    ).toBeVisible();

    // Raw Signals toggle
    await expect(page.locator('button', { hasText: /Raw Signals/ })).toBeVisible();
  });

  test('switching tabs changes active state and reloads data', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    const tabs = page.locator('.session-tabs .session-tab');

    // Click second tab
    await tabs.nth(1).click();

    // Second tab should now be active
    await expect(tabs.nth(1)).toHaveClass(/session-tab--active/);
    await expect(tabs.nth(0)).not.toHaveClass(/session-tab--active/);

    // Empty state should still be visible (data reloaded for new repo)
    await expect(
      page.locator('.empty-state__description', { hasText: /No pending proposals/ })
    ).toBeVisible();

    // Click first tab again
    await tabs.nth(0).click();
    await expect(tabs.nth(0)).toHaveClass(/session-tab--active/);
  });

  test('API returns proposals for each repo', async () => {
    interface ProposalsResponse {
      proposals: Array<{ id: string }>;
    }

    // Both endpoints should respond without error (even if empty)
    const dataA = await apiGet<ProposalsResponse>(
      `/api/lore/${encodeURIComponent(repoNameA)}/proposals`
    );
    expect(dataA).toHaveProperty('proposals');

    const dataB = await apiGet<ProposalsResponse>(
      `/api/lore/${encodeURIComponent(repoNameB)}/proposals`
    );
    expect(dataB).toHaveProperty('proposals');
  });
});
