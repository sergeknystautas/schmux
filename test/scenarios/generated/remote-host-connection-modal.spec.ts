import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  apiPost,
  waitForDashboardLive,
  waitForHealthy,
} from './helpers';

test.describe('Remote host connection modal renders without errors', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-remote-modal');

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
          id: 'test-flavor',
          flavor: 'test:basic',
          display_name: 'Test Remote Host',
          workspace_path: '/tmp/workspace',
          vcs: 'git',
          connect_command: 'echo connecting',
        },
      ],
    });
  });

  test('clicking a remote flavor card opens the connection modal without errors', async ({
    page,
  }) => {
    // Collect any uncaught page errors (this catches xterm addon init failures)
    const pageErrors: Error[] = [];
    page.on('pageerror', (err) => pageErrors.push(err));

    // Mock the flavor-statuses endpoint to show our flavor as disconnected
    await page.route('**/api/remote/flavor-statuses', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            flavor: {
              id: 'test-flavor',
              flavor: 'test:basic',
              display_name: 'Test Remote Host',
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

    // Mock the connect endpoint to return a fake provisioning session ID
    // (avoids actually trying to SSH somewhere)
    await page.route('**/api/remote/hosts/connect', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'fake-host-id',
          flavor_id: 'test-flavor',
          hostname: '',
          uuid: '',
          connected_at: '',
          expires_at: '',
          status: 'provisioning',
          provisioned: false,
          provisioning_session_id: 'fake-provision-session',
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
            id: 'fake-host-id',
            flavor_id: 'test-flavor',
            hostname: '',
            uuid: '',
            connected_at: '',
            expires_at: '',
            status: 'provisioning',
            provisioned: false,
            provisioning_session_id: 'fake-provision-session',
          },
        ]),
      })
    );

    // Navigate to the spawn page
    await page.goto('/spawn');
    await waitForDashboardLive(page);

    // Verify: the remote flavor card shows "Click to connect"
    const flavorCard = page.locator('text=Test Remote Host');
    await expect(flavorCard).toBeVisible({ timeout: 10_000 });
    const clickToConnect = page.locator('text=Click to connect');
    await expect(clickToConnect).toBeVisible();

    // Click the flavor card to trigger connection
    await flavorCard.click();

    // Verify: the connection modal opens
    const modal = page.locator('.modal-overlay');
    await expect(modal).toBeVisible({ timeout: 10_000 });

    // Verify: the modal header shows the flavor display name
    await expect(modal.locator('text=Test Remote Host')).toBeVisible();

    // Verify: the modal shows a status message
    await expect(modal.locator('text=Provisioning remote host')).toBeVisible();

    // Verify: the terminal container is present (dark background div for xterm)
    const terminalContainer = modal.locator('div[style*="background-color"]').first();
    await expect(terminalContainer).toBeVisible();

    // Wait a moment for the xterm useEffect to run and any errors to fire
    await page.waitForTimeout(1000);

    // THE KEY ASSERTION: no uncaught JavaScript errors occurred
    // This catches the allowProposedApi error from xterm Unicode11Addon
    expect(pageErrors).toEqual([]);

    // Verify: the modal can be closed via the close button
    const closeButton = modal.locator('button[aria-label="Close"]');
    await expect(closeButton).toBeVisible();
    await closeButton.click();
    await expect(modal).not.toBeVisible();
  });
});
