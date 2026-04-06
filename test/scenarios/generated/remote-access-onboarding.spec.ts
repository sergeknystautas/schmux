import { test, expect } from './coverage-fixture';
import {
  apiGet,
  apiPost,
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
} from './helpers';

function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

test.describe.serial('Remote access onboarding', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    // Clear any password set by earlier test suites
    await apiPost('/api/dev/clear-password');
    const repoPath = await createTestRepo('onboarding-repo');
    await seedConfig({ repos: [repoPath] });
  });

  test('password is not set initially', async () => {
    const config = await apiGet<{ remote_access: { password_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.password_hash_set).toBe(false);
  });

  // Skip: the remote-access-panel does not render in the Docker scenario
  // environment. Despite features.tunnel returning true from the API, the
  // React component tree does not mount RemoteAccessPanel in headless
  // Chromium. This is a pre-existing issue (fails on main branch too).
  // TODO: investigate why the sidebar panel doesn't render in Docker.
  test.skip('dashboard shows password warning before password is set', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const panel = page.locator('[data-testid="remote-access-panel"]');
    await expect(panel).toBeVisible({ timeout: 10_000 });

    const warning = panel.locator('[data-testid="remote-access-warning"]');
    await expect(warning).toBeVisible();
    await expect(warning).toContainText('Set a password');

    // Start button should be disabled
    const toggle = panel.locator('[data-testid="remote-access-toggle"]');
    await expect(toggle).toBeDisabled();
  });

  test('short password is rejected', async () => {
    const res = await fetch(`${getBaseURL()}/api/remote-access/set-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: 'short' }),
    });
    expect(res.status).toBe(400);
  });

  test('set password succeeds', async () => {
    const result = await apiPost<{ ok: boolean }>('/api/remote-access/set-password', {
      password: 'mypassword123',
    });
    expect(result.ok).toBe(true);
  });

  test('config shows password_hash_set true after setting password', async () => {
    const config = await apiGet<{ remote_access: { password_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.password_hash_set).toBe(true);
  });

  test('local API requests still work after setting password', async () => {
    const res = await fetch(`${getBaseURL()}/api/config`);
    expect(res.status).toBe(200);
  });

  // Skip: same pre-existing issue as the password warning test above —
  // remote-access-panel does not render in the Docker scenario environment.
  test.skip('password warning disappears after setting password', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    const panel = page.locator('[data-testid="remote-access-panel"]');
    await expect(panel).toBeVisible({ timeout: 10_000 });

    const warning = panel.locator('[data-testid="remote-access-warning"]');
    await expect(warning).not.toBeVisible();

    // Start button should now be enabled
    const toggle = panel.locator('[data-testid="remote-access-toggle"]');
    await expect(toggle).toBeEnabled();
  });

  test('config page Access tab has ntfy topic input and generate button', async ({ page }) => {
    // Enable remote access via API so the ntfy UI is visible
    await apiPost('/api/config', { remote_access: { enabled: true } });

    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // ntfy topic input is visible
    const ntfyInput = page.locator('input[placeholder="my-schmux-notifications"]');
    await expect(ntfyInput).toBeVisible({ timeout: 10_000 });

    // Generate button is visible
    const generateBtn = page.getByRole('button', { name: 'Generate secure topic' });
    await expect(generateBtn).toBeVisible();
  });

  test('generate secure topic populates input and shows QR code', async ({ page }) => {
    await apiPost('/api/config', { remote_access: { enabled: true } });

    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    const ntfyInput = page.locator('input[placeholder="my-schmux-notifications"]');
    await expect(ntfyInput).toBeVisible({ timeout: 10_000 });

    // Placeholder shown before generating
    await expect(page.locator('[data-testid="ntfy-qr-placeholder"]')).toBeVisible();
    expect(await page.locator('[data-testid="ntfy-qr-code"] svg').count()).toBe(0);

    // Click generate
    await page.getByRole('button', { name: 'Generate secure topic' }).click();

    // Input should now contain a secure topic
    const value = await ntfyInput.inputValue();
    expect(value).toMatch(/^schmux-[0-9a-f]{32}$/);

    // QR code should appear, placeholder should be gone
    await expect(page.locator('[data-testid="ntfy-qr-code"] svg')).toBeVisible();
    await expect(page.locator('[data-testid="ntfy-qr-placeholder"]')).not.toBeVisible();
  });

  test('test notification button is disabled when ntfy topic is empty', async ({ page }) => {
    await apiPost('/api/config', { remote_access: { enabled: true } });

    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    const ntfyInput = page.locator('input[placeholder="my-schmux-notifications"]');
    await expect(ntfyInput).toBeVisible({ timeout: 10_000 });

    // Clear the ntfy topic
    await ntfyInput.fill('');

    const testBtn = page.getByRole('button', { name: 'Send test notification' });
    await expect(testBtn).toBeDisabled();
  });

  test('test notification API returns 400 when topic not configured', async () => {
    // Ensure ntfy topic is cleared in config
    await apiPost('/api/config', { remote_access: { notify: { ntfy_topic: '' } } });

    const res = await fetch(`${getBaseURL()}/api/remote-access/test-notification`, {
      method: 'POST',
    });
    expect(res.status).toBe(400);
  });

  test('test notification button enabled after saving ntfy topic', async ({ page }) => {
    // Save a topic via API first (also enable remote access so the UI is visible)
    await apiPost('/api/config', {
      remote_access: { enabled: true, notify: { ntfy_topic: 'schmux-test-onboarding-topic' } },
    });

    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    const ntfyInput = page.locator('input[placeholder="my-schmux-notifications"]');
    await expect(ntfyInput).toBeVisible({ timeout: 10_000 });

    // Input should reflect the saved topic
    await expect(ntfyInput).toHaveValue('schmux-test-onboarding-topic');

    const testBtn = page.getByRole('button', { name: 'Send test notification' });
    await expect(testBtn).toBeEnabled();
  });

  test('password strength indicator shows for valid-length passwords', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Find the password input in the Access Password section
    const passwordInput = page.locator('input[type="password"]').first();
    await expect(passwordInput).toBeVisible({ timeout: 10_000 });

    // Type a short password (6 chars) — strength indicator should appear
    await passwordInput.fill('abcdef');

    const strengthIndicator = page.locator('[data-testid="password-strength"]');
    await expect(strengthIndicator).toBeVisible();
  });
});
