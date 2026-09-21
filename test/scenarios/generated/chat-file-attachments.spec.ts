import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// The browser's real File picker, fetch body, draft storage, and chat socket
// run together; controlled daemon responses keep model services out of the test.
for (const theme of ['light', 'dark']) {
  test(`Chat file attachments: upload, restore and send (${theme})`, async ({ page }) => {
    await waitForHealthy();
    await seedConfig();
    await page.addInitScript((value) => localStorage.setItem('schmux-theme', value), theme);
    const ts = new Date().toISOString();
    const filePath = '/tmp/attachment-workspace/.schmux/attachments/upload-1/data.csv';
    const records: Record<string, unknown>[] = [];
    let uploads = 0;

    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      ws.send(
        JSON.stringify({
          type: 'sessions',
          workspaces: [
            {
              id: 'attachment-workspace',
              repo: 'attachment-fixture',
              branch: 'main',
              path: '/tmp/attachment-workspace',
              sessions: [
                {
                  id: 'attachment-session',
                  target: 'claude',
                  kind: 'chat',
                  running: true,
                  branch: 'main',
                  created_at: ts,
                  attach_cmd: '',
                },
              ],
            },
          ],
        })
      );
    });
    await page.routeWebSocket('**/ws/chat/attachment-session', (ws) => {
      ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records }));
      ws.onMessage((raw) => {
        const message = JSON.parse(String(raw));
        if (message.type !== 'send') return;
        const record = { ...message, type: 'user_message', ts, id: 'attachment-message' };
        records.push(record);
        ws.send(JSON.stringify({ type: 'record', record }));
      });
    });
    await page.route('**/api/workspaces/attachment-workspace/attachments?*', async (route) => {
      const request = route.request();
      expect(request.method()).toBe('POST');
      expect(new URL(request.url()).searchParams.get('filename')).toBe('data.csv');
      expect(request.postDataBuffer()).toEqual(Buffer.from('a,b\n1,2'));
      uploads += 1;
      await route.fulfill({ json: { name: 'data.csv', path: filePath }, status: 201 });
    });

    await page.goto('/sessions/attachment-session');
    const attach = page.getByRole('button', { name: 'Attach', exact: true });
    await expect(attach).toBeEnabled();
    const picker = page.waitForEvent('filechooser');
    await attach.click();
    await (
      await picker
    ).setFiles([
      { name: 'data.csv', mimeType: 'text/csv', buffer: Buffer.from('a,b\n1,2') },
      {
        name: 'pixel.png',
        mimeType: 'image/png',
        buffer: Buffer.from(
          'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
          'base64'
        ),
      },
    ]);
    await expect(page.getByTestId('chat-file-chip')).toHaveText('data.csv×');
    await expect(page.getByTestId('chat-image-chip')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeEnabled();
    expect(uploads).toBe(1);
    await page.getByTestId('chat-input').fill('Please inspect these.');

    await page.reload();
    await expect(page.getByTestId('chat-file-chip')).toContainText('data.csv');
    await expect(page.getByTestId('chat-image-chip')).toBeVisible();
    await expect(page.getByTestId('chat-input')).toHaveValue('Please inspect these.');
    await page.getByRole('button', { name: 'Send', exact: true }).click();
    await expect(page.getByTestId('chat-user-message')).toContainText(filePath);
    expect(records).toHaveLength(1);
    expect(records[0].text).toBe(`Please inspect these.\n\nFile attachments:\n${filePath}`);
    expect(records[0].images).toEqual([
      {
        media_type: 'image/png',
        data: 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
      },
    ]);
    expect(uploads).toBe(1);
    // The echoed message is the completion boundary; inspect cleared state once.
    expect(await page.getByTestId('chat-file-chip').count()).toBe(0);
    expect(await page.getByTestId('chat-image-chip').count()).toBe(0);
  });
}
