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

    // There should be exactly one "Lore" link, not one per repo
    const loreLinks = page.locator('a.nav-link', { hasText: /^Lore/ });
    await expect(loreLinks).toHaveCount(1);
  });

  test('navigates to /lore via sidebar', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const loreLink = page.locator('a.nav-link', { hasText: /^Lore/ });
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

    // Tab bar with both repos
    const tabs = page.locator('.repo-tabs .repo-tab');
    await expect(tabs).toHaveCount(2);
    await expect(tabs.nth(0)).toHaveText(repoNameA);
    await expect(tabs.nth(1)).toHaveText(repoNameB);

    // First tab is active by default
    await expect(tabs.nth(0)).toHaveClass(/repo-tab--active/);
    await expect(tabs.nth(1)).not.toHaveClass(/repo-tab--active/);
  });

  test('page shows proposals and raw entries sections', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Proposals section
    await expect(page.locator('h3', { hasText: 'Proposals' })).toBeVisible();

    // Raw Entries toggle
    await expect(page.locator('button', { hasText: /Raw Entries/ })).toBeVisible();
  });

  test('switching tabs changes active state and reloads data', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    const tabs = page.locator('.repo-tabs .repo-tab');

    // Click second tab
    await tabs.nth(1).click();

    // Second tab should now be active
    await expect(tabs.nth(1)).toHaveClass(/repo-tab--active/);
    await expect(tabs.nth(0)).not.toHaveClass(/repo-tab--active/);

    // Proposals section should still be visible (data reloaded for new repo)
    await expect(page.locator('h3', { hasText: 'Proposals' })).toBeVisible();

    // Click first tab again
    await tabs.nth(0).click();
    await expect(tabs.nth(0)).toHaveClass(/repo-tab--active/);
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
