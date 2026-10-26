import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy, waitForDashboardLive } from './helpers';
import type { WebSocketRoute } from '@playwright/test';

test.describe('Client performance recording', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
  });
  for (const theme of ['light', 'dark']) {
    test(`launch, record, stop from sidebar (${theme})`, async ({ page }, testInfo) => {
      await page.addInitScript((value) => localStorage.setItem('schmux-theme', value), theme);
      await seedConfig({
        repos: ['https://example.com/schmux.git'],
        extra: {
          chat_sessions: true,
          ui: { panels: { clientPerformance: true } },
          client_performance: { enabled: true, repo: 'schmux.git', target: 'claude' },
        },
      });
      await page.route('**/api/healthz', (route) =>
        route.fulfill({
          json: { version: 'test', dev_mode: true },
          headers: { Date: new Date().toUTCString() },
        })
      );
      await page.route('**/api/dev/status', (route) =>
        route.fulfill({
          json: {
            active: true,
            source_workspace: '/tmp/serving-workspace',
            schmux_workspaces: [],
          },
        })
      );
      let dashboard!: WebSocketRoute;
      let launched = false;
      const workspace = {
        id: 'perf-workspace',
        repo: 'https://example.com/schmux.git',
        branch: 'fix/client-performance-test',
        path: '/tmp/perf-workspace',
        sessions: [
          {
            id: 'perf-session',
            target: 'claude',
            kind: 'chat',
            running: true,
            branch: 'fix/client-performance-test',
            created_at: new Date().toISOString(),
            attach_cmd: '',
          },
        ],
      };
      const ids = { workspace_id: 'perf-workspace', session_id: 'perf-session' };
      const snapshot = () => ({
        type: 'sessions',
        workspaces: launched ? [workspace] : [],
        client_performance: launched ? ids : undefined,
      });
      await page.routeWebSocket('**/ws/dashboard', (ws) => {
        dashboard = ws;
        ws.send(JSON.stringify(snapshot()));
      });
      await page.routeWebSocket('**/ws/chat/perf-session', (ws) => {
        ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records: [] }));
      });
      const sessionRequests: string[] = [];
      await page.route('**/api/client-performance/session', async (route) => {
        sessionRequests.push(route.request().method());
        expect(route.request().method()).toBe('POST');
        launched = true;
        expect(route.request().postDataJSON()).toEqual({ workspace_id: '' });
        await route.fulfill({
          json: { workspace_id: 'perf-workspace', session_id: 'perf-session' },
        });
        dashboard.send(JSON.stringify(snapshot()));
      });
      let recording: Record<string, unknown> | undefined;
      await page.route('**/api/client-performance/recording?*', async (route) => {
        expect(new URL(route.request().url()).searchParams.get('session_id')).toBe('perf-session');
        recording = route.request().postDataJSON();
        await route.fulfill({ json: { status: 'sent' } });
      });
      await page.goto('/');
      await waitForDashboardLive(page);
      await expect(page.getByRole('button', { name: 'Record' })).toBeHidden();
      await page.getByRole('button', { name: 'Launch' }).click();
      await expect(page).toHaveURL(/\/sessions\/perf-session$/);
      await expect(
        page.getByTestId('client-perf-pane').getByRole('link', { name: 'View' })
      ).toHaveAttribute('href', '/sessions/perf-session');
      await page.getByRole('button', { name: 'Record' }).click();
      await expect(page.getByText('Client Performance · REC')).toBeVisible();
      await page.getByRole('button', { name: 'Stop' }).click();
      await expect(page.getByRole('status').filter({ hasText: 'Recording sent' })).toBeVisible();
      await expect(page).toHaveURL(/\/sessions\/perf-session$/);
      await expect(page.getByLabel(/Recording since/)).toHaveCount(0);
      await page.screenshot({
        path: testInfo.outputPath(`client-performance-${theme}.png`),
        fullPage: true,
      });
      expect(recording).toEqual(
        expect.objectContaining({
          build: expect.objectContaining({ sourceWorkspace: '/tmp/serving-workspace' }),
          environment: expect.any(Object),
          timeline: expect.any(Array),
        })
      );
      await page.reload();
      await waitForDashboardLive(page);
      await expect(
        page.getByTestId('client-perf-pane').getByRole('link', { name: 'View' })
      ).toHaveAttribute('href', '/sessions/perf-session');
      await expect(page.getByRole('button', { name: 'Launch' })).toHaveCount(0);
      expect(sessionRequests).toEqual(['POST']);
    });
  }
});
