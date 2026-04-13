import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
} from './helpers';

test.describe.serial('Autolearn page as flat card wall', () => {
  let repoPathA: string;
  const repoNameA = 'test-autolearn-repo-a';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPathA = await createTestRepo(repoNameA);
    await seedConfig({
      repos: [repoPathA],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
  });

  test('sidebar shows single Autolearn link', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const autolearnLinks = page.locator('.tools-section__list a', { hasText: /^Autolearn/ });
    await expect(autolearnLinks).toHaveCount(1);
  });

  test('navigates to /autolearn via sidebar', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const autolearnLink = page.locator('.tools-section__list a', { hasText: /^Autolearn/ });
    await autolearnLink.click();

    await page.waitForURL('/autolearn');
    expect(page.url()).toMatch(/\/autolearn$/);
  });

  test('page shows heading, subtitle, and no repo tabs', async ({ page }) => {
    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Page heading
    await expect(page.locator('h2', { hasText: 'Autolearn' })).toBeVisible();

    // Subtitle
    await expect(page.locator('p', { hasText: 'Schmux continual learning system' })).toBeVisible();

    // No repo tab bar
    const tabs = page.locator('[data-testid="repo-tab"]');
    await expect(tabs).toHaveCount(0);
  });

  test('shows empty state when no pending proposals', async ({ page }) => {
    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    await expect(
      page.locator('.empty-state__description', { hasText: /Nothing to review/ })
    ).toBeVisible();
  });

  test('API returns batches for configured repo', async () => {
    interface BatchesResponse {
      batches: Array<{ id: string }>;
    }

    const data = await apiGet<BatchesResponse>(
      `/api/autolearn/${encodeURIComponent(repoNameA)}/batches`
    );
    expect(data).toHaveProperty('batches');
  });
});
