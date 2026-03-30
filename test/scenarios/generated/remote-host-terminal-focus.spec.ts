import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  apiPost,
  waitForDashboardLive,
  waitForHealthy,
} from './helpers';

test.describe('Remote host provisioning terminal holds focus for YubiKey auth', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-remote-focus');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    // Add a remote flavor via the config API
    await apiPost('/api/config', {
      remote_flavors: [
        {
          id: 'focus-flavor',
          flavor: 'test:focus',
          display_name: 'Focus Test Host',
          workspace_path: '/tmp/workspace',
          vcs: 'git',
          connect_command: 'echo connecting',
        },
      ],
    });
  });

  test('terminal receives focus on modal open and refocuses on body click', async ({ page }) => {
    // Mock the flavor-statuses endpoint to show our flavor as disconnected
    await page.route('**/api/remote/flavor-statuses', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            flavor: {
              id: 'focus-flavor',
              flavor: 'test:focus',
              display_name: 'Focus Test Host',
              workspace_path: '/tmp/workspace',
              vcs: 'git',
            },
            connected: false,
            host_id: '',
            hostname: '',
            status: '',
          },
        ]),
      })
    );

    // Mock the connect endpoint to return a provisioning session ID
    await page.route('**/api/remote/hosts/connect', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'focus-host-id',
          flavor_id: 'focus-flavor',
          hostname: '',
          uuid: '',
          connected_at: '',
          expires_at: '',
          status: 'provisioning',
          provisioned: false,
          provisioning_session_id: 'focus-provision-session',
        }),
      })
    );

    // Mock the hosts endpoint (polled by the modal for status updates)
    await page.route('**/api/remote/hosts', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            id: 'focus-host-id',
            flavor_id: 'focus-flavor',
            hostname: '',
            uuid: '',
            connected_at: '',
            expires_at: '',
            status: 'provisioning',
            provisioned: false,
            provisioning_session_id: 'focus-provision-session',
          },
        ]),
      })
    );

    // Navigate to the spawn page and wait for dashboard WebSocket
    await page.goto('/spawn');
    await waitForDashboardLive(page);

    // Click the flavor card to open the connection modal
    const flavorCard = page.locator('text=Focus Test Host');
    await expect(flavorCard).toBeVisible({ timeout: 10_000 });
    await flavorCard.click();

    // Wait for the modal and the xterm terminal to render
    const modal = page.locator('.modal-overlay');
    await expect(modal).toBeVisible({ timeout: 10_000 });

    const xtermTextarea = modal.locator('.xterm-helper-textarea');
    await expect(xtermTextarea).toBeAttached({ timeout: 5_000 });

    // Verification 1: the xterm textarea should be the focused element.
    // Uses polling because focus is deferred via requestAnimationFrame.
    await expect(xtermTextarea).toBeFocused({ timeout: 3_000 });

    // Verification 2: clicking the modal header moves focus away
    const modalHeader = modal.locator('.modal__header');
    await modalHeader.click();
    await expect(xtermTextarea).not.toBeFocused();

    // Verification 3: clicking the modal body re-focuses the terminal
    const modalBody = modal.locator('.modal__body');
    await modalBody.click();
    await expect(xtermTextarea).toBeFocused({ timeout: 3_000 });
  });
});
