import { test, expect } from './coverage-fixture';
import { apiPost, waitForHealthy } from './helpers';

test.describe.serial('Remote access password authentication', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    // Set password for this test suite (password-setting is verified in onboarding tests)
    await apiPost('/api/remote-access/set-password', {
      password: 'test1234',
    });
  });

  test('remote auth page without token shows instructions', async ({ page }) => {
    const response = await page.goto('/remote-auth');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Check your notification app');
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
