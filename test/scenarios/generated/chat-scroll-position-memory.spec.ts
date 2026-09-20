import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// A user reading earlier in a long chat transcript switches to another tab in
// the workspace and comes back to the same place. A user following the tail
// comes back to the new bottom. Replays controlled chat WebSocket records.
test('Chat scroll position: return to where you left off', async ({ page }) => {
  await waitForHealthy();
  await seedConfig();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.addInitScript((theme) => localStorage.setItem('schmux-theme', theme), 'light');
  const historyTs = new Date(Date.now() - 30_000).toISOString();
  const liveTs = new Date(Date.now() - 5_000).toISOString();
  const harness = (ts: string, line: Record<string, unknown>) => ({ type: 'harness', ts, line });
  // Long enough history that the transcript scrolls in a 900px viewport.
  const records: Record<string, unknown>[] = [];
  for (let i = 1; i <= 20; i++) {
    records.push({
      type: 'user_message',
      ts: historyTs,
      id: `scroll-user-${i}`,
      text: `Question ${i}: what does part ${i} do?`,
    });
    records.push(
      harness(historyTs, {
        type: 'assistant',
        message: {
          content: [{ type: 'text', text: `Answer ${i}. `.repeat(8) }],
        },
      }),
      harness(historyTs, { type: 'result', subtype: 'success' })
    );
  }
  records.push({
    type: 'user_message',
    ts: historyTs,
    id: 'scroll-final-user',
    text: 'Summarize everything.',
  });
  records.push(
    harness(historyTs, {
      type: 'assistant',
      message: {
        content: [
          {
            type: 'text',
            text: Array.from(
              { length: 40 },
              (_, i) => `Summary paragraph ${i + 1}. Work continues in the background.`
            ).join('\n\n'),
          },
        ],
      },
    }),
    harness(historyTs, { type: 'result', subtype: 'success' })
  );

  await page.routeWebSocket('**/ws/dashboard', (ws) => {
    ws.send(
      JSON.stringify({
        type: 'sessions',
        workspaces: [
          {
            id: 'scroll-workspace',
            repo: 'scroll',
            branch: 'main',
            path: '/tmp/scroll',
            sessions: [
              {
                id: 'scroll-session',
                target: 'claude',
                kind: 'chat',
                running: true,
                branch: 'main',
                created_at: historyTs,
                attach_cmd: '',
              },
            ],
            tabs: [
              {
                id: 'diff-tab-1',
                kind: 'diff',
                label: 'Diff',
                route: '/diff/scroll-workspace',
                closable: true,
                created_at: historyTs,
              },
            ],
          },
        ],
      })
    );
  });

  // Every tab switch or reload remounts the chat and opens a new WebSocket,
  // which replays the full history. The first two connections (initial load,
  // and the return while detached) replay the history alone. From the third
  // connection on, the replay also carries the two exchanges that arrived
  // while the user was away, so the following-the-tail return sees a longer
  // history than the one it left. Connection counting is deterministic; no
  // timing is involved.
  const liveRecords: Record<string, unknown>[] = [
    { type: 'user_message', ts: liveTs, id: 'live-1', text: 'follow up one' },
    harness(liveTs, {
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'live reply one' }] },
    }),
    harness(liveTs, { type: 'result', subtype: 'success' }),
    { type: 'user_message', ts: liveTs, id: 'live-2', text: 'follow up two' },
    harness(liveTs, {
      type: 'assistant',
      message: {
        content: [{ type: 'text', text: 'live reply two '.repeat(20) }],
      },
    }),
    harness(liveTs, { type: 'result', subtype: 'success' }),
  ];

  let chatConnections = 0;
  await page.routeWebSocket('**/ws/chat/scroll-session', (ws) => {
    chatConnections += 1;
    const replay = chatConnections >= 3 ? [...records, ...liveRecords] : records;
    ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records: replay }));
  });

  await page.goto('/sessions/scroll-session');
  const transcript = page.getByTestId('chat-transcript');

  // Wait for history to render so the layout is settled.
  await expect(transcript).toBeVisible();
  await expect(page.getByText(/Summary paragraph 40\./)).toBeVisible();
  await expect
    .poll(() => transcript.evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop))
    .toBeLessThan(2);

  // Detach: scroll to a known earlier position and verify Resume shows.
  const detachedScrollTop = await transcript.evaluate((el) => {
    el.scrollTop = 200;
    el.dispatchEvent(new Event('scroll'));
    return el.scrollTop;
  });
  expect(detachedScrollTop).toBe(200);
  await expect(page.getByTestId('chat-resume')).toBeVisible();

  // Give the report-on-scroll effect a tick to persist.
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('chat-scroll-scroll-session')))
    .toBe(JSON.stringify({ mode: 'position', scrollTop: 200 }));

  // Switch to the diff tab (route switches, transcript unmounts).
  await page.locator('[data-tour="diff-tab"]').click();
  await expect(page).toHaveURL(/\/diff\/scroll-workspace$/);

  // Switch back to the chat tab by clicking the session tab.
  await page.locator('.session-tab').first().click();
  await expect(page).toHaveURL(/\/sessions\/scroll-session$/);
  // After re-mount the restore layout effect should land at the saved offset.
  await expect.poll(() => transcript.evaluate((el) => el.scrollTop)).toBe(200);
  await expect(page.getByTestId('chat-resume')).toBeVisible();

  // Press Resume: pins to the bottom, hides Resume, persists bottom record.
  await page.getByTestId('chat-resume').click();
  await expect(page.getByTestId('chat-resume')).toHaveCount(0);
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('chat-scroll-scroll-session')))
    .toBe(JSON.stringify({ mode: 'bottom' }));

  // Following the tail: switch away, two exchanges arrive while away, return
  // pinned to the new bottom with Resume hidden. The follow-ups are absent
  // before leaving, so their presence on return proves the longer history.
  await expect(page.getByText('follow up two')).toHaveCount(0);
  await page.locator('[data-tour="diff-tab"]').click();
  await expect(page).toHaveURL(/\/diff\/scroll-workspace$/);
  await page.locator('.session-tab').first().click();
  await expect(page).toHaveURL(/\/sessions\/scroll-session$/);
  await expect(page.getByText('follow up two')).toBeVisible();
  await expect
    .poll(() => transcript.evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop))
    .toBeLessThan(2);
  await expect(page.getByTestId('chat-resume')).toHaveCount(0);

  // Reload while detached: scrollTop preserved.
  const detachedAgain = await transcript.evaluate((el) => {
    el.scrollTop = 400;
    el.dispatchEvent(new Event('scroll'));
    return el.scrollTop;
  });
  expect(detachedAgain).toBe(400);
  await expect(page.getByTestId('chat-resume')).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('chat-scroll-scroll-session')))
    .toBe(JSON.stringify({ mode: 'position', scrollTop: 400 }));

  await page.reload();
  await expect(transcript).toBeVisible();
  await expect.poll(() => transcript.evaluate((el) => el.scrollTop)).toBe(400);
  await expect(page.getByTestId('chat-resume')).toBeVisible();
});
