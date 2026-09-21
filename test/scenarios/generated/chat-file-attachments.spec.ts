import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// The browser's real File picker, fetch body, draft storage, and chat socket
// run together; controlled daemon responses keep model services out of the test.
// The "drop" method exercises the pane-wide drag target via screen-position hit
// testing; the "picker" method covers the filechooser wiring. Both run in light
// and dark themes.
for (const theme of ['light', 'dark']) {
  for (const method of ['picker', 'drop']) {
    test(`Chat file attachments: ${method}, restore and send (${theme})`, async ({
      page,
    }, testInfo) => {
      await waitForHealthy();
      await seedConfig();
      await page.addInitScript((value) => localStorage.setItem('schmux-theme', value), theme);
      const ts = new Date().toISOString();
      const filePath = '/tmp/attachment-workspace/.schmux/attachments/upload-1/data.csv';
      // Seeded history so the drop target can be hit-tested against real DOM
      // positions: a user message bubble and a completed tool row.
      const records: Record<string, unknown>[] = [
        { type: 'user_message', ts, id: 'existing-message', text: 'Existing chat message' },
        {
          type: 'harness',
          ts,
          line: {
            type: 'assistant',
            message: {
              content: [
                {
                  type: 'tool_use',
                  id: 'existing-tool',
                  name: 'Read',
                  input: { file_path: '/workspace/file.txt' },
                },
              ],
            },
          },
        },
        {
          type: 'harness',
          ts,
          line: {
            type: 'user',
            message: {
              content: [{ type: 'tool_result', tool_use_id: 'existing-tool', content: 'contents' }],
            },
          },
        },
      ];
      // Sent-message assertions must read from a buffer that ignores the
      // seeded history, so seeded records do not change their meaning.
      const sentRecords: Record<string, unknown>[] = [];
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
          sentRecords.push(record);
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
      if (method === 'picker') {
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
      } else {
        const dataTransfer = await page.evaluateHandle(() => {
          const transfer = new DataTransfer();
          transfer.items.add(new File(['a,b\n1,2'], 'data.csv', { type: 'text/csv' }));
          const png = Uint8Array.from(
            atob(
              'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII='
            ),
            (char) => char.charCodeAt(0)
          );
          transfer.items.add(new File([png], 'pixel.png', { type: 'image/png' }));
          return transfer;
        });

        const pointFor = async (testId: string) => {
          const box = await page.getByTestId(testId).boundingBox();
          expect(box).not.toBeNull();
          return { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 };
        };

        const dispatchAtPoint = async (
          type: 'dragenter' | 'dragover' | 'dragleave' | 'drop',
          point: { x: number; y: number }
        ) => {
          await dataTransfer.evaluate(
            (transfer, event) => {
              const target = document.elementFromPoint(event.x, event.y);
              if (!target) throw new Error(`No drag target at ${event.x},${event.y}`);
              target.dispatchEvent(
                new DragEvent(event.type, {
                  bubbles: true,
                  cancelable: true,
                  dataTransfer: transfer,
                })
              );
            },
            { ...point, type }
          );
        };

        const messagePoint = await pointFor('chat-user-message');
        const toolPoint = await pointFor('chat-tool-row');
        const composerPoint = await pointFor('chat-composer');
        const transcriptBox = await page.getByTestId('chat-transcript').boundingBox();
        expect(transcriptBox).not.toBeNull();
        const emptyTranscriptPoint = {
          x: transcriptBox!.x + transcriptBox!.width / 2,
          y: transcriptBox!.y + transcriptBox!.height - 12,
        };
        const emptyPointIsInTranscript = await page.evaluate((point) => {
          const target = document.elementFromPoint(point.x, point.y);
          return Boolean(target?.closest('[data-testid="chat-transcript"]'));
        }, emptyTranscriptPoint);
        expect(emptyPointIsInTranscript).toBe(true);

        await dispatchAtPoint('dragenter', messagePoint);
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme);
        await expect(page.getByTestId('chat-file-drop-overlay')).toBeVisible();
        await expect(page.getByText('Drop files to attach')).toBeVisible();

        await dispatchAtPoint('dragenter', toolPoint);
        await dispatchAtPoint('dragleave', messagePoint);
        await expect(page.getByTestId('chat-file-drop-overlay')).toBeVisible();

        // Narrow viewport must keep the centered prompt inside the chat pane
        // without moving the transcript or composer.
        const originalViewport = page.viewportSize()!;
        await page.setViewportSize({ width: 720, height: 720 });
        await page
          .getByTestId('chat-view')
          .screenshot({ path: testInfo.outputPath(`chat-drop-hint-${theme}.png`) });
        const promptBox = await page.getByText('Drop files to attach').boundingBox();
        const chatBox = await page.getByTestId('chat-view').boundingBox();
        expect(promptBox).not.toBeNull();
        expect(chatBox).not.toBeNull();
        expect(promptBox!.x).toBeGreaterThanOrEqual(chatBox!.x);
        expect(promptBox!.x + promptBox!.width).toBeLessThanOrEqual(chatBox!.x + chatBox!.width);
        expect(promptBox!.y).toBeGreaterThanOrEqual(chatBox!.y);
        expect(promptBox!.y + promptBox!.height).toBeLessThanOrEqual(chatBox!.y + chatBox!.height);
        await page.setViewportSize(originalViewport);

        await dispatchAtPoint('dragenter', emptyTranscriptPoint);
        await dispatchAtPoint('dragleave', toolPoint);
        await expect(page.getByTestId('chat-file-drop-overlay')).toBeVisible();
        await dispatchAtPoint('dragenter', composerPoint);
        await dispatchAtPoint('dragleave', emptyTranscriptPoint);
        await expect(page.getByTestId('chat-file-drop-overlay')).toBeVisible();

        await dispatchAtPoint('dragover', toolPoint);
        await dispatchAtPoint('drop', toolPoint);
        await dataTransfer.dispose();
      }
      await expect(page.getByTestId('chat-file-chip')).toHaveText('data.csv×');
      await expect(page.getByTestId('chat-image-chip')).toBeVisible();
      await expect(page.getByRole('button', { name: 'Send', exact: true })).toBeEnabled();
      if (method === 'drop') {
        expect(await page.getByText('Drop files to attach').count()).toBe(0);
        await page.getByTestId('chat-composer').screenshot({
          path: testInfo.outputPath(`chat-drop-chips-${theme}.png`),
        });
      }
      expect(uploads).toBe(1);
      if (method === 'drop') {
        // The drop must not auto-send: no outbound chat record, only the upload.
        expect(sentRecords).toHaveLength(0);
      }
      await page.getByTestId('chat-input').fill('Please inspect these.');

      await page.reload();
      await expect(page.getByTestId('chat-file-chip')).toContainText('data.csv');
      await expect(page.getByTestId('chat-image-chip')).toBeVisible();
      await expect(page.getByTestId('chat-input')).toHaveValue('Please inspect these.');
      await page.getByRole('button', { name: 'Send', exact: true }).click();
      await expect(page.getByTestId('chat-user-message').last()).toContainText(filePath);
      expect(sentRecords).toHaveLength(1);
      expect(sentRecords[0].text).toBe(`Please inspect these.\n\nFile attachments:\n${filePath}`);
      expect(sentRecords[0].images).toEqual([
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
}
