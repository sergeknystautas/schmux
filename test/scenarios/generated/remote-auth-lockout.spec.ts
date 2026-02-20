import { test, expect } from '@playwright/test';
import { apiPost, waitForHealthy } from './helpers';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Remote auth lockout after failed password attempts', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    // Ensure a password is set for this test suite
    await apiPost('/api/remote-access/set-password', { password: 'testpassword123' });
  });

  test('remote auth page without token shows instructions', async ({ page }) => {
    const response = await page.goto('/remote-auth');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Check your notification app');
    await expect(page.locator('form')).not.toBeVisible();
  });

  test('remote auth page with fake token shows invalid link error', async ({ page }) => {
    const response = await page.goto('/remote-auth?token=fake-token-abc123');
    expect(response?.status()).toBe(200);
    await expect(page.locator('body')).toContainText('Invalid or expired link');
    await expect(page.locator('form')).not.toBeVisible();
  });

  test('POST remote auth with invalid token returns invalid link HTML', async () => {
    const res = await fetch(`${BASE_URL}/remote-auth`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'token=nonexistent-token&password=wrong',
    });
    expect(res.status).toBe(200);
    const body = await res.text();
    expect(body).toContain('Invalid or expired link');
  });

  test('POST remote auth with wrong token and wrong password returns invalid link HTML', async () => {
    const res = await fetch(`${BASE_URL}/remote-auth`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: 'token=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&password=wrongpassword',
    });
    expect(res.status).toBe(200);
    const body = await res.text();
    expect(body).toContain('Invalid or expired link');
  });
});
