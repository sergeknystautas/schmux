import { test, expect } from '@playwright/test';
import { apiGet, apiPost, waitForDashboardLive, waitForHealthy } from './helpers';

test.describe.serial('Remote access PIN authentication', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
  });

  test('config shows pin_hash_set false initially', async () => {
    const config = await apiGet<{ remote_access: { pin_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.pin_hash_set).toBe(false);
  });

  test('dashboard shows PIN warning when no PIN is set', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const panel = page.locator('[data-testid="remote-access-panel"]');
    await expect(panel).toBeVisible({ timeout: 10_000 });

    const warning = panel.locator('.remote-access-panel__warning');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText('set-pin');
  });

  test('tunnel start rejected without PIN', async () => {
    const res = await fetch('http://localhost:7337/api/remote-access/on', {
      method: 'POST',
    });
    // May be 400 (no PIN) or 503 (no tunnel manager in test env) — either is valid
    expect(res.ok).toBe(false);
    if (res.status === 400) {
      const body = await res.text();
      expect(body.toLowerCase()).toContain('pin');
    }
  });

  test('set PIN via API succeeds', async () => {
    const result = await apiPost<{ ok: boolean }>('/api/remote-access/set-pin', {
      pin: 'test1234',
    });
    expect(result.ok).toBe(true);
  });

  test('config shows pin_hash_set true after setting PIN', async () => {
    const config = await apiGet<{ remote_access: { pin_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.pin_hash_set).toBe(true);
  });

  test('dashboard PIN warning gone after setting PIN', async ({ page }) => {
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
    // Should not show the PIN form
    await expect(page.locator('form')).not.toBeVisible();
  });

  test('remote auth page rejects bogus token', async ({ page }) => {
    const response = await page.goto('/remote-auth?token=bogus-token-12345');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Invalid or expired link');
    await expect(page.locator('form')).not.toBeVisible();
  });
});
