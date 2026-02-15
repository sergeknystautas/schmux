import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
} from './helpers';

test.describe.serial('Overlay page with repo tabs', () => {
  let repoPathA: string;
  let repoPathB: string;
  const repoNameA = 'test-overlay-repo-a';
  const repoNameB = 'test-overlay-repo-b';

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

  test('sidebar shows single Overlays link', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // There should be exactly one "Overlays" link, not one per repo
    const overlayLinks = page.locator('a.nav-link', { hasText: 'Overlays' });
    await expect(overlayLinks).toHaveCount(1);

    // The link should NOT contain a repo name suffix
    await expect(overlayLinks.first()).toHaveText('Overlays');
  });

  test('navigates to /overlays via sidebar', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const overlayLink = page.locator('a.nav-link', { hasText: 'Overlays' });
    await overlayLink.click();

    // URL should be /overlays with no repo name parameter
    await page.waitForURL('/overlays');
    expect(page.url()).toMatch(/\/overlays$/);
  });

  test('page shows repo tab bar with both repos', async ({ page }) => {
    await page.goto('/overlays');
    await waitForDashboardLive(page);

    // Page title
    await expect(page.locator('h1')).toHaveText('Overlay Files');

    // Tab bar with both repos
    const tabs = page.locator('.repo-tabs .repo-tab');
    await expect(tabs).toHaveCount(2);
    await expect(tabs.nth(0)).toHaveText(repoNameA);
    await expect(tabs.nth(1)).toHaveText(repoNameB);

    // First tab is active by default
    await expect(tabs.nth(0)).toHaveClass(/repo-tab--active/);
    await expect(tabs.nth(1)).not.toHaveClass(/repo-tab--active/);
  });

  test('switching tabs changes active state', async ({ page }) => {
    await page.goto('/overlays');
    await waitForDashboardLive(page);

    const tabs = page.locator('.repo-tabs .repo-tab');

    // Click second tab
    await tabs.nth(1).click();

    // Second tab should now be active, first should not
    await expect(tabs.nth(1)).toHaveClass(/repo-tab--active/);
    await expect(tabs.nth(0)).not.toHaveClass(/repo-tab--active/);

    // Click first tab again
    await tabs.nth(0).click();

    // First tab active again
    await expect(tabs.nth(0)).toHaveClass(/repo-tab--active/);
    await expect(tabs.nth(1)).not.toHaveClass(/repo-tab--active/);
  });

  test('API returns overlay info for both repos', async () => {
    interface OverlayInfo {
      repo_name: string;
      declared_paths: Array<{ path: string; source: string }>;
    }
    const data = await apiGet<{ overlays: OverlayInfo[] }>('/api/overlays');
    expect(data.overlays.length).toBeGreaterThanOrEqual(2);

    const repoNames = data.overlays.map((o) => o.repo_name);
    expect(repoNames).toContain(repoNameA);
    expect(repoNames).toContain(repoNameB);
  });
});
