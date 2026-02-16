import { test, expect } from '@playwright/test';
import { apiGet, apiPost, waitForDashboardLive, waitForHealthy } from './helpers';

test.describe.serial('Remote access password authentication', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
  });

  test('set password via API succeeds', async () => {
    const result = await apiPost<{ ok: boolean }>('/api/remote-access/set-password', {
      password: 'test1234',
    });
    expect(result.ok).toBe(true);
  });

  test('config shows password_hash_set true after setting password', async () => {
    const config = await apiGet<{ remote_access: { password_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.password_hash_set).toBe(true);
  });

  test('dashboard password warning gone after setting password', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const panel = page.locator('[data-testid="remote-access-panel"]');
    await expect(panel).toBeVisible({ timeout: 10_000 });

    const warning = panel.locator('.remote-access-panel__warning');
    await expect(warning).not.toBeVisible();
  });

  test('remote auth page rejects missing token', async ({ page }) => {
    const response = await page.goto('/remote-auth');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Invalid or expired link');
    // Should not show the password form
    await expect(page.locator('form')).not.toBeVisible();
  });

  test('remote auth page rejects bogus token', async ({ page }) => {
    const response = await page.goto('/remote-auth?token=bogus-token-12345');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Invalid or expired link');
    await expect(page.locator('form')).not.toBeVisible();
  });
});
