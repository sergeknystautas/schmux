import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// The recorder, the ensure-session endpoint, the attachment upload, and the
// chat socket run together; controlled daemon responses keep model services
// out of the test. Dev mode and the dev status come from fixtures.
test('Client performance recording: record, open chat, send with attachment', async ({ page }) => {
  await waitForHealthy();
  await seedConfig({
    repos: ['https://example.com/schmux.git'],
    extra: {
      chat_sessions: true,
      client_performance: { enabled: true, repo: 'schmux.git', target: 'claude' },
    },
  });
  const ts = new Date().toISOString();
  const uploadDir = '/tmp/perf-workspace/.schmux/attachments/u1';
  const records: Record<string, unknown>[] = [];
  const sentRecords: Record<string, unknown>[] = [];
  let ensureBody: unknown = null;
  let uploadRequestUrl = '';
  let uploadedBody: Record<string, unknown> | null = null;

  await page.route('**/api/healthz', async (route) => {
    await route.fulfill({
      json: { version: 'test', dev_mode: true },
      headers: { Date: new Date().toUTCString() },
    });
  });
  await page.route('**/api/dev/status', async (route) => {
    await route.fulfill({
      json: {
        active: true,
        source_workspace: '/tmp/perf-workspace',
        schmux_workspaces: ['perf-workspace'],
      },
    });
  });
  await page.route('**/api/client-performance/session', async (route) => {
    ensureBody = route.request().postDataJSON();
    await route.fulfill({
      json: { workspace_id: 'perf-workspace', session_id: 'perf-session' },
    });
  });
  await page.route('**/api/workspaces/perf-workspace/attachments?*', async (route) => {
    const request = route.request();
    expect(request.method()).toBe('POST');
    uploadRequestUrl = request.url();
    const name = new URL(uploadRequestUrl).searchParams.get('filename') ?? '';
    expect(name).toMatch(/^client-perf-\d+-[a-z0-9]+\.json$/);
    uploadedBody = JSON.parse(request.postData() ?? '{}');
    await route.fulfill({
      json: { name, path: `${uploadDir}/${name}` },
      status: 201,
    });
  });
  await page.routeWebSocket('**/ws/dashboard', (ws) => {
    ws.send(
      JSON.stringify({
        type: 'sessions',
        workspaces: [
          {
            id: 'perf-workspace',
            repo: 'https://example.com/schmux.git',
            branch: 'client-performance',
            path: '/tmp/perf-workspace',
            sessions: [
              {
                id: 'perf-session',
                target: 'claude',
                kind: 'chat',
                running: true,
                branch: 'client-performance',
                created_at: ts,
                attach_cmd: '',
              },
            ],
          },
        ],
      })
    );
  });
  await page.routeWebSocket('**/ws/chat/perf-session', (ws) => {
    ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records }));
    ws.onMessage((raw) => {
      const message = JSON.parse(String(raw));
      if (message.type !== 'send') return;
      const record = { ...message, type: 'user_message', ts, id: 'perf-message' };
      records.push(record);
      sentRecords.push(record);
      ws.send(JSON.stringify({ type: 'record', record }));
    });
  });

  await page.goto('/');
  await page.getByRole('button', { name: 'Start recording' }).click();
  await expect(page.getByText('Client Performance · REC')).toBeVisible();
  await expect(page.getByTestId('client-perf-pane')).toContainText(
    /Recording \d+ min · \d+ stalls/
  );
  await page.getByRole('button', { name: 'Open performance chat' }).click();
  await expect(page).toHaveURL(/\/sessions\/perf-session$/);
  expect(ensureBody).toEqual({ workspace_id: '', session_id: '' });
  await expect(page.getByLabel(/Recording since \d\d:\d\d · attach/)).toBeChecked();
  await page.getByTestId('chat-input').fill('typing lags in the terminal');
  await page.getByRole('button', { name: 'Send' }).click();
  await expect.poll(() => sentRecords.length).toBe(1);
  expect(uploadedBody).toEqual(
    expect.objectContaining({
      build: expect.objectContaining({ sourceWorkspace: '/tmp/perf-workspace' }),
      environment: expect.any(Object),
      timeline: expect.any(Array),
    })
  );
  const text = String(sentRecords[0].text);
  expect(
    text.endsWith(
      `File attachments:\n${uploadDir}/${new URL(uploadRequestUrl).searchParams.get('filename')}`
    )
  ).toBe(true);
});
