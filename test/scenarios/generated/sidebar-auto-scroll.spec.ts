import type { Locator, Page } from '@playwright/test';
import { test, expect } from './coverage-fixture';
import {
  apiPatch,
  seedConfig,
  createTestRepo,
  spawnSession,
  disposeSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

const BRANCHES = Array.from({ length: 8 }, (_, i) => `scroll-${String(i + 1).padStart(2, '0')}`);

// Playwright launches headless Chromium with --hide-scrollbars; the scrollbar
// case needs the real scrollbar rendered. hasTouch enables touch input.
test.use({ launchOptions: { ignoreDefaultArgs: ['--hide-scrollbars'] }, hasTouch: true });

// Scrolls the sidebar list until the active workspace row is out of view.
type ManualScroll = (page: Page, list: Locator, active: Locator) => Promise<void>;

// Each method is real browser input dispatched through Chromium's input
// pipeline (CDP Input.*), not synthetic DOM events.
const MANUAL_SCROLLS: Array<[string, ManualScroll]> = [
  [
    'mouse wheel',
    async (page, list) => {
      await list.hover();
      await page.mouse.wheel(0, 5000);
    },
  ],
  [
    'scrollbar press',
    async (page, list, active) => {
      const box = await list.boundingBox();
      if (!box) throw new Error('sidebar list has no bounding box');
      // Hold the mouse on the scrollbar track just above the bottom arrow
      // button (8px scrollbar): Chrome pages the list toward the pointer while
      // the button is held.
      await page.mouse.move(box.x + box.width - 4, box.y + box.height - 12);
      await page.mouse.down();
      await expect(active).not.toBeInViewport();
      await page.mouse.up();
    },
  ],
  [
    'touch drag',
    async (page, list) => {
      const box = await list.boundingBox();
      if (!box) throw new Error('sidebar list has no bounding box');
      const cdp = await page.context().newCDPSession(page);
      const x = box.x + box.width / 2;
      const touch = (type: 'touchStart' | 'touchMove' | 'touchEnd', y?: number) =>
        cdp.send('Input.dispatchTouchEvent', {
          type,
          touchPoints: y === undefined ? [] : [{ x, y }],
        });
      // One swipe travels at most the list's height (~76px at this viewport),
      // so swipe three times, as a user would.
      for (let swipe = 0; swipe < 3; swipe++) {
        await touch('touchStart', box.y + box.height - 5);
        for (let y = box.y + box.height - 15; y > box.y + 5; y -= 10) await touch('touchMove', y);
        await touch('touchEnd');
      }
    },
  ],
  [
    'End key on a focused sidebar row',
    async (page) => {
      await page
        .locator('.nav-workspace--active .nav-workspace__header')
        .evaluate((el: HTMLElement) => el.focus({ preventScroll: true }));
      await page.keyboard.press('End');
    },
  ],
];

async function openScroll01(page: Page, sessionId: string) {
  await page.setViewportSize({ width: 1280, height: 480 });
  await page.goto(`/sessions/${sessionId}`);
  await waitForDashboardLive(page);
  await page.getByRole('button', { name: 'abc' }).click();

  const list = page.locator('.nav-workspaces');
  const active = page.locator('.nav-workspace--active');
  await expect(active).toContainText('scroll-01');
  await expect(active).toBeInViewport();
  return { list, active };
}

async function scrollAway(page: Page, list: Locator, active: Locator, scroll: ManualScroll) {
  await scroll(page, list, active);
  await expect(active).not.toBeInViewport();
}

async function expectBroadcastLeavesSidebarAlone(
  page: Page,
  active: Locator,
  sessionId: string,
  nickname: string
) {
  // A rename broadcasts a workspace update and is visible in the sidebar.
  await apiPatch(`/api/sessions-nickname/${sessionId}`, { nickname });
  await expect(page.locator('.nav-session', { hasText: nickname })).toHaveCount(1);

  // Negative claim (rule 4): the broadcast must not scroll the active row
  // back into view. A smooth scrollIntoView begins on the next frame and
  // finishes within ~300-500ms, so after a 1000ms window a yank would have
  // landed. Residual motion from the manual scroll only moves further away.
  await page.waitForTimeout(1000);
  await expect(active).not.toBeInViewport();
}

test.describe.serial('Sidebar auto-scroll yields to manual scrolling', () => {
  const sessionIds: string[] = [];

  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-repo-sidebar-scroll');
    await seedConfig({
      repos: [repoPath],
      agents: [{ name: 'echo-agent', command: "sh -c 'echo hello from agent; sleep 600'" }],
    });
    for (const branch of BRANCHES) {
      const [result] = await spawnSession({
        repo: repoPath,
        branch,
        targets: { 'echo-agent': 1 },
      });
      sessionIds.push(result.session_id);
    }
    for (const id of sessionIds) await waitForSessionRunning(id);
  });

  test.afterAll(async () => {
    for (const id of sessionIds) await disposeSession(id);
  });

  for (const [name, scroll] of MANUAL_SCROLLS) {
    test(`${name} survives a broadcast`, async ({ page }) => {
      const { list, active } = await openScroll01(page, sessionIds[0]);
      await scrollAway(page, list, active, scroll);
      await expectBroadcastLeavesSidebarAlone(
        page,
        active,
        sessionIds[7],
        `renamed-${name.split(' ')[0].toLowerCase()}`
      );
    });
  }

  test('navigation after a manual scroll re-centres the sidebar', async ({ page }) => {
    const { list, active } = await openScroll01(page, sessionIds[0]);
    await scrollAway(page, list, active, MANUAL_SCROLLS[0][1]);

    // Ctrl+ArrowDown moves to scroll-02, which must scroll into view.
    await page.keyboard.press('Control+ArrowDown');
    await expect(active).toContainText('scroll-02');
    await expect(active).toBeInViewport();
  });
});
