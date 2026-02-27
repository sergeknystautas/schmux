import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

test.describe.serial('Lore status warning banner', () => {
  let repoPath: string;
  const repoName = 'test-lore-warning';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo(repoName);
    // Seed config with lore enabled but NO llm_target — curator cannot run
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
          promptable: true,
        },
      ],
    });
    // Explicitly reset lore: enabled with no llm_target (prior tests may have changed these)
    // Also clear compound/nudgenik targets so the fallback chain doesn't find a stale target.
    await apiPost('/api/config', {
      lore: { enabled: true, llm_target: '' },
      nudgenik: { target: '' },
      compound_target: '',
    });
  });

  test('lore status API reports unconfigured curator', async () => {
    interface LoreStatus {
      enabled: boolean;
      curator_configured: boolean;
      curate_on_dispose: string;
      llm_target: string;
      issues: string[];
    }

    const status = await apiGet<LoreStatus>('/api/lore/status');
    expect(status.enabled).toBe(true);
    expect(status.curator_configured).toBe(false);
    expect(status.issues.length).toBeGreaterThan(0);
    expect(status.issues[0]).toContain('LLM target');
  });

  test('lore page shows warning banner', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Page heading should be visible
    await expect(page.locator('h2', { hasText: 'Lore' })).toBeVisible();

    // Warning banner should be visible
    const banner = page.locator('[data-testid="lore-warning-banner"]');
    await expect(banner).toBeVisible();

    // Banner should mention LLM target
    await expect(banner).toContainText('LLM target');

    // Banner should have a link to config advanced tab
    const configLink = banner.locator('a[href="/config?tab=advanced"]');
    await expect(configLink).toBeVisible();
  });
});
