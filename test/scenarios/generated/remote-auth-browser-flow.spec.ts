import { test, expect } from '@playwright/test';
import { apiPost, simulateTunnel, stopSimulatedTunnel, waitForHealthy } from './helpers';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Remote auth browser flow', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    // Set password for this test suite
    await apiPost('/api/remote-access/set-password', { password: 'scenariotest123' });
  });

  test.afterAll(async () => {
    // Clean up any simulated tunnel
    try {
      await stopSimulatedTunnel();
    } catch {
      // Ignore if already stopped
    }
  });

  test('tunnel simulation returns token and URL', async () => {
    const tunnel = await simulateTunnel();
    expect(tunnel.token).toBeTruthy();
    expect(tunnel.url).toContain('trycloudflare.com');
  });

  test('token URL redirects to nonce and shows password form', async ({ page }) => {
    // Get a fresh tunnel + token
    await stopSimulatedTunnel();
    const tunnel = await simulateTunnel();

    // Visit auth URL with token — should redirect to nonce page
    await page.goto(`/remote-auth?token=${tunnel.token}`);
    // After redirect, URL should contain nonce=
    expect(page.url()).toContain('nonce=');

    // Password form should be visible
    const passwordInput = page.locator('input[name="password"]');
    await expect(passwordInput).toBeVisible({ timeout: 5_000 });

    const submitButton = page.locator('button[type="submit"]');
    await expect(submitButton).toBeVisible();
  });

  test('wrong password shows error but keeps form', async ({ page }) => {
    // Get a fresh tunnel + token
    await stopSimulatedTunnel();
    const tunnel = await simulateTunnel();

    await page.goto(`/remote-auth?token=${tunnel.token}`);
    expect(page.url()).toContain('nonce=');

    // Enter wrong password
    await page.fill('input[name="password"]', 'wrongpassword');
    await page.click('button[type="submit"]');

    // Should show error
    await expect(page.locator('.error')).toContainText('Incorrect password');

    // Form should still be visible for retry
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('correct password grants dashboard access', async ({ page }) => {
    // Get a fresh tunnel + token
    await stopSimulatedTunnel();
    const tunnel = await simulateTunnel();

    // Visit auth URL
    await page.goto(`/remote-auth?token=${tunnel.token}`);
    expect(page.url()).toContain('nonce=');

    // Enter correct password
    await page.fill('input[name="password"]', 'scenariotest123');
    await page.click('button[type="submit"]');

    // Should redirect to dashboard
    await page.waitForURL('**/');

    // Dashboard content should be visible (not the auth page)
    await expect(page.locator('body')).not.toContainText('Authenticate');
  });

  test('authenticated API access works with session cookie', async ({ page }) => {
    // Reuse the browser context from the previous test (has the cookie).
    // Navigate to healthz to verify the cookie grants API access.
    const response = await page.goto('/api/healthz');
    expect(response?.status()).toBe(200);
  });

  test('stopping tunnel makes API accessible without auth', async ({ browser }) => {
    await stopSimulatedTunnel();

    // With no tunnel active, auth is not required — any request should work
    const freshContext = await browser.newContext();
    const freshPage = await freshContext.newPage();
    const res = await freshPage.goto(`${BASE_URL}/api/healthz`);
    expect(res?.status()).toBe(200);
    await freshContext.close();
  });
});
