import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

interface PanelsConfig {
  ui?: { panels?: Record<string, boolean> };
}

async function panelEnabled(panel: string): Promise<boolean> {
  const config = await apiGet<PanelsConfig>('/api/config');
  return config.ui?.panels?.[panel] === true;
}

test.describe.serial('Toggle feature diagnostics from settings', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-feature-diagnostics');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
    await apiPost('/api/config', {
      ui: { panels: { eventMonitor: false, tmuxDiagnostic: false } },
    });
  });

  test('diagnostic panels start unchecked', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    await expect(page.getByRole('checkbox', { name: 'Event Monitor' })).not.toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'Tmux Diagnostics' })).not.toBeChecked();
  });

  test('enabling each panel saves its own diagnostic config', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    await page.getByRole('checkbox', { name: 'Event Monitor' }).check();
    await expect.poll(() => panelEnabled('eventMonitor')).toBe(true);
    await page.getByRole('checkbox', { name: 'Tmux Diagnostics' }).check();
    await expect.poll(() => panelEnabled('tmuxDiagnostic')).toBe(true);
  });

  test('enabled diagnostics persist across navigation', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    await expect(page.getByRole('checkbox', { name: 'Event Monitor' })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'Tmux Diagnostics' })).toBeChecked();
  });

  test('disabling each panel saves its own diagnostic config', async ({ page }) => {
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    await page.getByRole('checkbox', { name: 'Event Monitor' }).uncheck();
    await expect.poll(() => panelEnabled('eventMonitor')).toBe(false);
    await page.getByRole('checkbox', { name: 'Tmux Diagnostics' }).uncheck();
    await expect.poll(() => panelEnabled('tmuxDiagnostic')).toBe(false);
  });

  test('disabled diagnostics persist across navigation', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);
    await page.goto('/config?tab=advanced');
    await waitForDashboardLive(page);

    await expect(page.getByRole('checkbox', { name: 'Event Monitor' })).not.toBeChecked();
    await expect(page.getByRole('checkbox', { name: 'Tmux Diagnostics' })).not.toBeChecked();
  });
});
