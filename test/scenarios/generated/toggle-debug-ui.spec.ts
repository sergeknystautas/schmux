import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

test.describe.serial('Toggle debug UI from settings', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-debug-ui');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
    // Ensure debug_ui starts disabled
    await apiPost('/api/config', { debug_ui: false });
  });

  test('Advanced tab shows unchecked debug UI checkbox', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    const debugCheckbox = page
      .locator('label', { hasText: 'Enable debug UI' })
      .locator('input[type="checkbox"]');
    await expect(debugCheckbox).toBeVisible();
    await expect(debugCheckbox).not.toBeChecked();
  });

  test('enable debug UI via the UI — auto-saves', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    const debugCheckbox = page
      .locator('label', { hasText: 'Enable debug UI' })
      .locator('input[type="checkbox"]');
    await debugCheckbox.check();

    // Wait briefly for auto-save to complete
    await page.waitForTimeout(500);
  });

  test('API confirms debug_ui=true after enabling', async () => {
    const config = await apiGet<{ debug_ui?: boolean }>('/api/config');
    expect(config.debug_ui).toBe(true);
  });

  test('checkbox is still checked after navigating away and back', async ({ page }) => {
    // Navigate away
    await page.goto('/');
    await waitForDashboardLive(page);

    // Navigate back
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    const debugCheckbox = page
      .locator('label', { hasText: 'Enable debug UI' })
      .locator('input[type="checkbox"]');
    await expect(debugCheckbox).toBeChecked();
  });

  test('disable debug UI via the UI — auto-saves', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    const debugCheckbox = page
      .locator('label', { hasText: 'Enable debug UI' })
      .locator('input[type="checkbox"]');
    await debugCheckbox.uncheck();

    // Wait briefly for auto-save to complete
    await page.waitForTimeout(500);
  });

  test('API confirms debug_ui=false after disabling', async () => {
    const config = await apiGet<{ debug_ui?: boolean }>('/api/config');
    expect(config.debug_ui).toBeFalsy();
  });

  test('checkbox is unchecked after navigating away and back', async ({ page }) => {
    // Navigate away
    await page.goto('/');
    await waitForDashboardLive(page);

    // Navigate back
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    const debugCheckbox = page
      .locator('label', { hasText: 'Enable debug UI' })
      .locator('input[type="checkbox"]');
    await expect(debugCheckbox).not.toBeChecked();
  });
});
