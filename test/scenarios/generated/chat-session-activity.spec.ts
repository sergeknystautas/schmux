import { test, expect } from './coverage-fixture';
import type { WebSocketRoute } from '@playwright/test';
import { seedConfig, waitForHealthy } from './helpers';

// Controlled delivery tests the production page/socket/reducer path without
// depending on a model service. The task and launch identities remain distinct.
for (const theme of ['light', 'dark']) {
  test(`Chat activity: workers, late completion and transcript navigation (${theme})`, async ({
    page,
  }, testInfo) => {
    await waitForHealthy();
    await seedConfig();
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.addInitScript((theme) => localStorage.setItem('schmux-theme', theme), theme);
    const agentId = 'a715ccc45390d7701';
    const toolId = 'call_4460f63af58e42e2a810e0f3';
    const ts = new Date(Date.now() - 30_000).toISOString();
    const harness = (line: Record<string, unknown>) => ({ type: 'harness', ts, line });
    const records: Record<string, unknown>[] = [
      { type: 'user_message', ts, id: 'activity-user', text: 'Run the workers' },
      // Same heartbeat identity pattern as activity-heartbeats.jsonl; delivery
      // is synthetic here, as with the other controlled socket records.
      harness({
        type: 'assistant',
        message: {
          content: [
            {
              type: 'tool_use',
              id: 'completed-command',
              name: 'Bash',
              input: { command: 'just test-editor', description: 'Re-run level-authoring gate' },
            },
          ],
        },
      }),
      harness({
        type: 'system',
        subtype: 'task_started',
        task_id: 'completed-task',
        tool_use_id: 'completed-command',
        task_type: 'local_bash',
        is_backgrounded: false,
      }),
      ...[0, 1, 2].map((i) =>
        harness({
          type: 'tool_progress',
          tool_use_id: `completed-command-heartbeat-${i}`,
          parent_tool_use_id: 'completed-command',
          heartbeat: true,
          tool_name: 'Bash',
          elapsed_time_seconds: (i + 1) * 30,
        })
      ),
      harness({
        type: 'system',
        subtype: 'task_notification',
        task_id: 'completed-task',
        tool_use_id: 'completed-command',
        status: 'completed',
      }),
      harness({
        type: 'assistant',
        message: {
          content: [
            {
              type: 'tool_use',
              id: toolId,
              name: 'Agent',
              input: { description: 'Echo hello world', prompt: 'hello world' },
            },
          ],
        },
      }),
      harness({
        type: 'system',
        subtype: 'background_tasks_changed',
        tasks: [{ task_id: agentId, task_type: 'local_agent', description: 'Echo hello world' }],
      }),
      harness({
        type: 'system',
        subtype: 'task_started',
        task_id: agentId,
        task_type: 'local_agent',
        tool_use_id: toolId,
        description: 'Echo hello world',
        is_backgrounded: true,
      }),
      harness({
        type: 'user',
        message: {
          content: [
            { type: 'tool_result', tool_use_id: toolId, content: 'Agent launched in background' },
          ],
        },
        tool_use_result: {
          isAsync: true,
          status: 'async_launched',
          agentId,
          description: 'Echo hello world',
          outputFile: '/tmp/activity/agent-output.txt',
        },
      }),
    ];
    const tasks = [
      'Run editor tests',
      'Build web client',
      'Check level assets',
      'Run integration tests',
    ];
    const commands = [
      'just test-editor',
      'just build-web',
      'just check-assets',
      'just test-integration',
    ];
    for (let i = 1; i <= 4; i++) {
      records.push(
        harness({
          type: 'assistant',
          message: {
            content: [
              {
                type: 'tool_use',
                id: `launch-${i}`,
                name: 'Bash',
                input: { command: commands[i - 1], description: tasks[i - 1] },
              },
            ],
          },
        }),
        harness({
          type: 'system',
          subtype: 'task_started',
          task_id: `job-${i}`,
          task_type: 'local_bash',
          tool_use_id: `launch-${i}`,
          description: tasks[i - 1],
          is_backgrounded: true,
        }),
        harness({
          type: 'user',
          message: {
            content: [
              {
                type: 'tool_result',
                tool_use_id: `launch-${i}`,
                content: 'Job launched in background',
              },
            ],
          },
        })
      );
    }
    records.push({
      type: 'harness',
      ts: new Date(Date.now() - 6_000).toISOString(),
      line: {
        type: 'tool_progress',
        tool_use_id: 'launch-1-heartbeat-0',
        parent_tool_use_id: 'launch-1',
        heartbeat: true,
        tool_name: 'Bash',
        elapsed_time_seconds: 24,
      },
    });
    records.push(
      harness({
        type: 'assistant',
        message: {
          content: [
            {
              type: 'text',
              text: Array.from(
                { length: 45 },
                (_, i) => `Progress paragraph ${i + 1}. Work continues in the background.`
              ).join('\n\n'),
            },
          ],
        },
      }),
      harness({ type: 'result', subtype: 'success' })
    );
    await page.routeWebSocket('**/ws/dashboard', (ws) => {
      ws.send(
        JSON.stringify({
          type: 'sessions',
          workspaces: [
            {
              id: 'activity-workspace',
              repo: 'activity',
              branch: 'main',
              path: '/tmp/activity',
              sessions: [
                {
                  id: 'activity-session',
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
    let chatSocket: WebSocketRoute | undefined;
    let interrupts = 0;
    await page.routeWebSocket('**/ws/chat/activity-session', (ws) => {
      chatSocket = ws;
      ws.onMessage((message) => {
        if (JSON.parse(String(message)).type === 'interrupt') interrupts++;
      });
      ws.send(JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records }));
    });
    await page.goto('/sessions/activity-session');
    await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
    const activity = page.getByTestId('chat-activity');
    const transcript = page.getByTestId('chat-transcript');
    await expect(page.getByTestId('chat-stop')).toHaveCount(0);
    chatSocket!.send(
      JSON.stringify({
        type: 'record',
        record: {
          type: 'user_message',
          ts: new Date().toISOString(),
          id: 'next-turn',
          text: 'Continue working',
        },
      })
    );
    const stop = activity.getByTestId('chat-stop');
    await expect(stop).toBeVisible();
    await expect(activity.getByText('Run editor tests', { exact: true })).toBeVisible();
    await expect(activity.getByText('just test-editor', { exact: true })).toBeVisible();
    await expect(activity.getByText('Last reported runtime: 24s', { exact: true })).toBeVisible();
    await expect(activity.getByText(/Updated [0-9]+s ago/).first()).toBeVisible();
    await expect(activity.locator('[data-activity-key*="heartbeat"]')).toHaveCount(0);
    await expect(activity.getByText('Re-run level-authoring gate', { exact: true })).toHaveCount(0);
    const assertLayoutAndFollowing = async () => {
      const transcriptBox = await transcript.boundingBox();
      const activityBox = await activity.boundingBox();
      const composerBox = await page.getByTestId('chat-composer').boundingBox();
      expect(transcriptBox!.height).toBeGreaterThan(100);
      expect(transcriptBox!.y + transcriptBox!.height).toBeLessThanOrEqual(activityBox!.y + 1);
      expect(activityBox!.y + activityBox!.height).toBeLessThanOrEqual(composerBox!.y + 1);
      await expect
        .poll(() => transcript.evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop))
        .toBeLessThan(2);
      const lastParagraph = await transcript
        .getByText('Progress paragraph 45. Work continues in the background.')
        .boundingBox();
      expect(lastParagraph!.y + lastParagraph!.height).toBeLessThanOrEqual(
        transcriptBox!.y + transcriptBox!.height
      );
    };
    await expect(activity.getByTestId('chat-activity-row')).toHaveCount(3);
    await assertLayoutAndFollowing();
    await activity.getByRole('button', { name: 'Show all (5)' }).click();
    await expect(activity.getByTestId('chat-activity-row')).toHaveCount(5);
    await assertLayoutAndFollowing();
    const agent = activity.locator(`[data-activity-key="claude-agent:${agentId}"]`);
    await expect(agent).toHaveCount(1);
    await agent.getByRole('button', { name: 'Details', exact: true }).click();
    await assertLayoutAndFollowing();
    await agent.getByRole('button', { name: 'Hide details', exact: true }).click();
    await activity.getByRole('button', { name: 'Show less', exact: true }).click();
    await assertLayoutAndFollowing();
    const originalViewport = page.viewportSize()!;
    await page.setViewportSize({ width: 1000, height: 700 });
    await assertLayoutAndFollowing();
    await page.getByTestId('chat-input').fill('A growing draft\n'.repeat(6));
    await assertLayoutAndFollowing();
    await page.setViewportSize(originalViewport);
    await activity.getByRole('button', { name: 'Show all (5)' }).click();
    await assertLayoutAndFollowing();
    const activityBody = activity.getByTestId('chat-activity-body');
    await activityBody.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await expect(stop).toBeInViewport();
    const stopBox = await stop.boundingBox();
    const rightEdge = await activity.evaluate(
      (el) => el.getBoundingClientRect().right - parseFloat(getComputedStyle(el).paddingRight)
    );
    expect(Math.abs(stopBox!.x + stopBox!.width - rightEdge)).toBeLessThan(2);
    await activityBody.evaluate((el) => {
      el.scrollTop = 0;
    });
    await page.screenshot({
      path: testInfo.outputPath(`chat-activity-bottom-${theme}.png`),
      fullPage: true,
    });
    await page.getByTestId('chat-input').fill('Keep my draft');
    await agent.getByRole('button', { name: 'Jump to transcript' }).click();
    const target = page.getByTestId('chat-transcript').locator(`[data-tool-id="${toolId}"]`);
    await expect(target).toBeFocused();
    await expect(target).toBeInViewport();
    await expect(page.getByTestId('chat-resume')).toBeVisible();
    // Changes in footer height must preserve reading position after navigation.
    const readingPosition = await transcript.evaluate((el) => el.scrollTop);
    await activity.getByRole('button', { name: 'Show less', exact: true }).click();
    await expect
      .poll(() => transcript.evaluate((el) => el.scrollTop))
      .toBeCloseTo(readingPosition, 0);
    await activity.getByRole('button', { name: 'Show all (5)' }).click();
    await expect
      .poll(() => transcript.evaluate((el) => el.scrollTop))
      .toBeCloseTo(readingPosition, 0);
    await agent.getByRole('button', { name: 'Jump to transcript' }).click();
    await expect(target).toBeFocused();
    const resumeBox = await page.getByTestId('chat-resume').boundingBox();
    const composerBox = await page.getByTestId('chat-composer').boundingBox();
    expect(resumeBox!.y + resumeBox!.height).toBeLessThanOrEqual(composerBox!.y);
    await expect(page.getByTestId('chat-input')).toHaveValue('Keep my draft');
    chatSocket!.send(
      JSON.stringify({
        type: 'record',
        record: {
          type: 'harness',
          ts: new Date().toISOString(),
          line: {
            type: 'system',
            subtype: 'task_notification',
            task_id: agentId,
            tool_use_id: toolId,
            status: 'completed',
            summary: 'hello world — completed',
          },
        },
      })
    );
    await expect(target.getByTestId('chat-tool-dot')).toHaveAttribute('data-state', 'done');
    await expect(target.getByTestId('chat-tool-result')).toHaveText('hello world — completed');
    await expect(target).toBeFocused();
    await expect(target).toBeInViewport();
    await target.getByTestId('chat-tool-row').click();
    await expect(target.getByTestId('chat-tool-details')).toContainText(
      'Agent launched in background'
    );
    await expect(page.getByTestId('chat-input')).toHaveValue('Keep my draft');
    await page.screenshot({
      path: testInfo.outputPath(`chat-activity-${theme}.png`),
      fullPage: true,
    });
    await expect(agent).toHaveCount(0, { timeout: 6000 });
    await expect(activity.getByTestId('chat-activity-row')).toHaveCount(4);
    await page.getByTestId('chat-resume').click();
    await assertLayoutAndFollowing();
    for (let i = 1; i <= 4; i++) {
      chatSocket!.send(
        JSON.stringify({
          type: 'record',
          record: {
            type: 'harness',
            ts: new Date().toISOString(),
            line: {
              type: 'system',
              subtype: 'task_notification',
              task_id: `job-${i}`,
              tool_use_id: `launch-${i}`,
              status: 'completed',
              summary: `Job ${i} done`,
            },
          },
        })
      );
    }
    await expect(activity.getByTestId('chat-activity-row')).toHaveCount(0);
    await expect(stop).toBeVisible();
    await stop.click();
    await expect.poll(() => interrupts).toBe(1);
    chatSocket!.send(
      JSON.stringify({ type: 'record', record: harness({ type: 'result', subtype: 'success' }) })
    );
    await expect(stop).toHaveCount(0);
    await expect(activity).toHaveCount(0, { timeout: 6000 });
    await expect
      .poll(() => transcript.evaluate((el) => el.scrollHeight - el.clientHeight - el.scrollTop))
      .toBeLessThan(2);
    await expect(page.getByTestId('chat-input')).toHaveValue('Keep my draft');
  });
}
