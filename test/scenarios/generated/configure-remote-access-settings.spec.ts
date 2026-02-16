import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

test.describe.serial('Configure remote access settings', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-remote-access');
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
    // Reset remote access config to defaults so hasChanges() detects the test's fills
    await apiPost('/api/config', {
      remote_access: {
        disabled: false,
        timeout_minutes: 120,
        notify: { ntfy_topic: '', command: '' },
      },
    });
  });

  test('Access tab is accessible and contains expected sections', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Access tab should be active
    const accessTab = page.locator('[data-testid="config-tab-access"]');
    await expect(accessTab).toBeVisible();

    // Verify the three sections exist
    const networkSection = page.locator('h3', { hasText: 'Network' });
    await expect(networkSection).toBeVisible();

    const remoteAccessSection = page.locator('h3', { hasText: 'Remote Access' });
    await expect(remoteAccessSection).toBeVisible();

    const authSection = page.locator('h3', { hasText: 'Authentication' });
    await expect(authSection).toBeVisible();
  });

  test('Remote Access enable checkbox toggles fields', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Enable checkbox should be checked by default
    const enableCheckbox = page
      .locator('label', { hasText: 'Enable remote access' })
      .locator('input[type="checkbox"]');
    await expect(enableCheckbox).toBeChecked();

    // PIN, timeout, ntfy fields should be visible
    const pinField = page.locator('.form-group__label', { hasText: 'Access PIN' });
    await expect(pinField).toBeVisible();

    const timeoutField = page.locator('.form-group__label', { hasText: 'Timeout (minutes)' });
    await expect(timeoutField).toBeVisible();

    const ntfyField = page.locator('.form-group__label', { hasText: 'ntfy Topic' });
    await expect(ntfyField).toBeVisible();

    const commandField = page.locator('.form-group__label', { hasText: 'Notify Command' });
    await expect(commandField).toBeVisible();

    // Uncheck to disable
    await enableCheckbox.uncheck();

    // Fields should be hidden
    await expect(pinField).not.toBeVisible();
    await expect(timeoutField).not.toBeVisible();
    await expect(ntfyField).not.toBeVisible();
    await expect(commandField).not.toBeVisible();

    // Re-check to enable
    await enableCheckbox.check();

    // Fields should reappear
    await expect(pinField).toBeVisible();
    await expect(timeoutField).toBeVisible();
    await expect(ntfyField).toBeVisible();
    await expect(commandField).toBeVisible();
  });

  test('PIN validation shows error for mismatched PINs', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Type a PIN to reveal the confirm field
    const pinInput = page.locator('input[type="password"][placeholder*="PIN"]').first();
    await pinInput.fill('test1234');

    // Confirm field should now be visible
    const confirmInput = page.locator('input[type="password"][placeholder="Confirm PIN"]');
    await expect(confirmInput).toBeVisible();

    // Enter mismatched confirm
    await confirmInput.fill('wrong');

    // Click Set PIN button
    const setPinButton = page.locator('button', { hasText: /Set PIN|Update PIN/ });
    await expect(setPinButton).toBeVisible();
    await setPinButton.click();

    // Error message should appear
    const error = page.locator('.form-group__error', { hasText: 'PINs do not match' });
    await expect(error).toBeVisible();
  });

  test('setting PIN via the dashboard succeeds', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Type matching PINs
    const pinInput = page.locator('input[type="password"][placeholder*="PIN"]').first();
    await pinInput.fill('test1234');

    const confirmInput = page.locator('input[type="password"][placeholder="Confirm PIN"]');
    await confirmInput.fill('test1234');

    // Click Set PIN
    const setPinButton = page.locator('button', { hasText: /Set PIN|Update PIN/ });
    await setPinButton.click();

    // Success message should appear
    const success = page.locator('text=PIN updated');
    await expect(success).toBeVisible({ timeout: 5000 });

    // "PIN is configured" should appear
    const configured = page.locator('text=PIN is configured');
    await expect(configured).toBeVisible();
  });

  test('config API confirms PIN is set', async () => {
    interface ConfigResp {
      remote_access: {
        disabled: boolean;
        pin_hash_set: boolean;
        timeout_minutes: number;
        notify: {
          ntfy_topic: string;
          command: string;
        };
      };
    }

    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.remote_access.pin_hash_set).toBe(true);
  });

  test('saving remote access settings persists via API', async ({ page }) => {
    await page.goto('/config?tab=access');
    await waitForDashboardLive(page);

    // Fill in ntfy topic
    const ntfyInput = page
      .locator('.form-group', {
        has: page.locator('.form-group__label', { hasText: 'ntfy Topic' }),
      })
      .locator('input[type="text"]');
    await ntfyInput.fill('test-topic');

    // Fill in timeout
    const timeoutInput = page
      .locator('.form-group', {
        has: page.locator('.form-group__label', { hasText: 'Timeout (minutes)' }),
      })
      .locator('input[type="number"]');
    await timeoutInput.fill('30');

    // Fill in notify command
    const commandInput = page
      .locator('.form-group', {
        has: page.locator('.form-group__label', { hasText: 'Notify Command' }),
      })
      .locator('input[type="text"]');
    await commandInput.fill('echo test');

    // Save
    const saveButton = page.locator('[data-testid="config-save"]');
    await expect(saveButton).toBeEnabled({ timeout: 10000 });
    await saveButton.click();

    // Wait for save to complete
    await expect(saveButton).toBeDisabled({ timeout: 10000 });
  });

  test('GET /api/config reflects saved remote access values', async () => {
    interface ConfigResp {
      remote_access: {
        disabled: boolean;
        pin_hash_set: boolean;
        timeout_minutes: number;
        notify: {
          ntfy_topic: string;
          command: string;
        };
      };
    }

    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.remote_access.disabled).toBe(false);
    expect(config.remote_access.timeout_minutes).toBe(30);
    expect(config.remote_access.notify.ntfy_topic).toBe('test-topic');
    expect(config.remote_access.notify.command).toBe('echo test');
  });

  test('Advanced tab no longer contains Network or Authentication', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    // Advanced tab should NOT have Network or Authentication sections
    // (they moved to Access tab)
    const advancedContent = page.locator('.wizard-step-content[data-step="6"]');
    await expect(advancedContent).toBeVisible({ timeout: 10000 });

    const networkInAdvanced = advancedContent.locator('h3', { hasText: 'Network' });
    await expect(networkInAdvanced).toHaveCount(0);

    const authInAdvanced = advancedContent.locator('h3', { hasText: 'Authentication' });
    await expect(authInAdvanced).toHaveCount(0);
  });
});
