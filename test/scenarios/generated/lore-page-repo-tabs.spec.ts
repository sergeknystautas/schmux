import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
} from './helpers';

test.describe.serial('Lore page as flat card wall', () => {
  let repoPathA: string;
  const repoNameA = 'test-lore-repo-a';

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

  test('sidebar shows single Lore link', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const loreLinks = page.locator('.tools-section__list a', { hasText: /^Lore/ });
    await expect(loreLinks).toHaveCount(1);
  });

  test('navigates to /lore via sidebar', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const loreLink = page.locator('.tools-section__list a', { hasText: /^Lore/ });
    await loreLink.click();

    await page.waitForURL('/lore');
    expect(page.url()).toMatch(/\/lore$/);
  });

  test('page shows heading, subtitle, and no repo tabs', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Page heading
    await expect(page.locator('h2', { hasText: 'Lore' })).toBeVisible();

    // Subtitle
    await expect(page.locator('p', { hasText: 'Schmux continual learning system' })).toBeVisible();

    // No repo tab bar
    const tabs = page.locator('[data-testid="repo-tab"]');
    await expect(tabs).toHaveCount(0);
  });

  test('shows empty state when no pending proposals', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    await expect(
      page.locator('.empty-state__description', { hasText: /Nothing to review/ })
    ).toBeVisible();
  });

  test('API returns proposals for configured repo', async () => {
    interface ProposalsResponse {
      proposals: Array<{ id: string }>;
    }

    const data = await apiGet<ProposalsResponse>(
      `/api/lore/${encodeURIComponent(repoNameA)}/proposals`
    );
    expect(data).toHaveProperty('proposals');
  });
});
