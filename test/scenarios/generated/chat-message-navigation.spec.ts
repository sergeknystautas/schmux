import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// A user reading a long chat transcript scrolls back through their own
// messages with the keyboard while keeping their place in the composer, steps
// from wherever they scrolled by hand, returns to the latest output, and finds
// the same place after a reload. Replays controlled chat WebSocket records.
test('Chat message navigation: step through messages and restore after reload', async ({
  page,
}) => {
  await waitForHealthy();
  await seedConfig();
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.addInitScript((theme) => localStorage.setItem('schmux-theme', theme), 'light');
  const ts = new Date(Date.now() - 30_000).toISOString();
  const harness = (line: Record<string, unknown>) => ({ type: 'harness', ts, line });

  // Twenty user messages, each with an answer. The last answer is long enough
  // that the transcript scrolls in a 900px viewport.
  const records: Record<string, unknown>[] = [];
  for (let i = 1; i <= 20; i++) {
    records.push({
      type: 'user_message',
      ts,
      id: `scroll-user-${i}`,
      text: `Question ${i}: what does part ${i} do?`,
    });
    records.push(
      harness({
        type: 'assistant',
        message: {
          content: [
            {
              type: 'text',
              text:
                i === 20
                  ? Array.from(
                      { length: 40 },
                      (_, n) => `Summary paragraph ${n + 1}. Work continues in the background.`
                    ).join('\n\n')
                  : `Answer ${i}. `.repeat(8),
            },
          ],
        },
      }),
      harness({ type: 'result', subtype: 'success' })
    );
  }

  await page.routeWebSocket('**/ws/dashboard', (ws) => {
    ws.send(
      JSON.stringify({
        type: 'sessions',
        workspaces: [
          {
            id: 'nav-workspace',
            repo: 'nav',
            branch: 'main',
            path: '/tmp/nav',
            sessions: [
              {
                id: 'nav-session',
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
  await page.routeWebSocket('**/ws/chat/nav-session', (ws) => {
    ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records }));
  });

  await page.goto('/sessions/nav-session');
  const transcript = page.getByTestId('chat-transcript');
  await expect(transcript).toBeVisible();
  await expect(page.getByText(/Summary paragraph 40\./)).toBeVisible();

  const composer = page.getByTestId('chat-input');
  await composer.click();
  await composer.fill('draft kept across the jump');
  // Leave the caret mid-draft, after "draft". The shortcuts only scroll, so
  // the composer must keep both the focus and this caret throughout.
  await page.keyboard.press('Home');
  for (let i = 0; i < 5; i++) await page.keyboard.press('ArrowRight');
  await expect
    .poll(() => composer.evaluate((el: HTMLTextAreaElement) => el.selectionStart))
    .toBe(5);
  const expectComposerUntouched = async () => {
    await expect(composer).toBeFocused();
    await expect(composer).toHaveValue('draft kept across the jump');
    expect(await composer.evaluate((el: HTMLTextAreaElement) => el.selectionStart)).toBe(5);
  };

  // How far a row sits from the top of the transcript viewport.
  const topOffset = async (id: string) => {
    const row = await page.locator(`[data-user-message-id="${id}"]`).boundingBox();
    const box = await transcript.boundingBox();
    if (!row || !box) return Number.POSITIVE_INFINITY;
    return Math.abs(row.y - box.y);
  };
  // Scroll by hand so the row's top sits `below` px above the viewport top.
  const scrollJustPast = (id: string, below: number) =>
    transcript.evaluate(
      (el, [rowId, px]) => {
        const row = el.querySelector(`[data-user-message-id="${rowId}"]`)!;
        el.scrollTop += row.getBoundingClientRect().top - el.getBoundingClientRect().top + px;
      },
      [id, below] as const
    );

  // From the bottom, one press scrolls the most recent user message to the top.
  await page.keyboard.press('Control+BracketLeft');
  await expect.poll(() => topOffset('scroll-user-20')).toBeLessThan(4);
  await expect(page.getByTestId('chat-resume')).toBeVisible();
  await expectComposerUntouched();

  // Two more steps reach the third-most-recent user message.
  await page.keyboard.press('Control+BracketLeft');
  await page.keyboard.press('Control+BracketLeft');
  await expect.poll(() => topOffset('scroll-user-18')).toBeLessThan(4);

  // Scrolled up by hand: the steps measure from where the user is looking,
  // not from the last step.
  await scrollJustPast('scroll-user-10', 20);
  await page.keyboard.press('Control+BracketLeft');
  await expect.poll(() => topOffset('scroll-user-10')).toBeLessThan(4);
  await page.keyboard.press('Control+BracketRight');
  await expect.poll(() => topOffset('scroll-user-11')).toBeLessThan(4);

  // Forward past the most recent message resumes at the very bottom.
  await scrollJustPast('scroll-user-19', 20);
  await page.keyboard.press('Control+BracketRight');
  await expect.poll(() => topOffset('scroll-user-20')).toBeLessThan(4);
  await page.keyboard.press('Control+BracketRight');
  await expect(page.getByTestId('chat-resume')).toBeHidden();
  await expect
    .poll(() => transcript.evaluate((el) => el.scrollHeight - el.scrollTop - el.clientHeight))
    .toBeLessThan(8);
  await expectComposerUntouched();

  // Back to the third-most-recent message, then reload: the jump is saved
  // through the transcript's scroll path, so the view returns to it.
  await page.keyboard.press('Control+BracketLeft');
  await page.keyboard.press('Control+BracketLeft');
  await page.keyboard.press('Control+BracketLeft');
  await expect.poll(() => topOffset('scroll-user-18')).toBeLessThan(4);
  await expect
    .poll(() => page.evaluate(() => localStorage.getItem('chat-scroll-nav-session')))
    .toContain('"mode":"position"');
  const savedScrollTop = await page.evaluate(() => {
    const raw = localStorage.getItem('chat-scroll-nav-session');
    return raw ? (JSON.parse(raw) as { scrollTop: number }).scrollTop : -1;
  });

  await page.reload();
  await expect(transcript).toBeVisible();
  await expect.poll(() => transcript.evaluate((el) => el.scrollTop)).toBe(savedScrollTop);
  await expect.poll(() => topOffset('scroll-user-18')).toBeLessThan(4);
  await expect(page.getByTestId('chat-resume')).toBeVisible();
});
