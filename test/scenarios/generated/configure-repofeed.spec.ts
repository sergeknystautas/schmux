import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

test.describe.serial('Configure repofeed settings', () => {
  let repoPath: string;
  const repoName = 'test-repofeed-config';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo(repoName);
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
  });

  test('repofeed config round-trips via API', async () => {
    interface ConfigResp {
      repofeed: {
        enabled: boolean;
        publish_interval_seconds: number;
        fetch_interval_seconds: number;
        completed_retention_hours: number;
        repos: Record<string, boolean>;
      };
    }

    // Enable repofeed via API
    await apiPost('/api/config', {
      repofeed: {
        enabled: true,
        publish_interval_seconds: 15,
        fetch_interval_seconds: 45,
        completed_retention_hours: 24,
        repos: { [repoName]: true },
      },
    });

    // GET config and verify round-trip
    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.repofeed.enabled).toBe(true);
    expect(config.repofeed.publish_interval_seconds).toBe(15);
    expect(config.repofeed.fetch_interval_seconds).toBe(45);
    expect(config.repofeed.completed_retention_hours).toBe(24);
  });

  test('repofeed API returns empty list', async () => {
    interface RepofeedResp {
      repos: Array<{ name: string; slug: string; active_intents: number }>;
    }

    const data = await apiGet<RepofeedResp>('/api/repofeed');
    expect(data.repos).toEqual([]);
  });

  test('Repofeed is accessible on the Experimental tab', async ({ page }) => {
    await page.goto('/config?tab=experimental');
    await waitForDashboardLive(page);

    // Verify Experimental tab is active
    const experimentalTab = page.locator('[data-testid="config-tab-experimental"]');
    await expect(experimentalTab).toHaveAttribute('aria-selected', 'true');

    // Verify Repofeed section is visible (use .first() because the inner
    // config panel also renders an h3 "Repofeed" heading when enabled)
    const repofeedSection = page.locator('h3', { hasText: 'Repofeed' }).first();
    await expect(repofeedSection).toBeVisible();

    // Enable toggle should be present
    const enableToggle = page.locator('[data-testid="experimental-toggle-repofeed"]');
    await expect(enableToggle).toBeVisible();
  });
});
