import { test, expect } from './coverage-fixture';
import { seedConfig, waitForHealthy } from './helpers';

// The browser's real File picker, fetch body, and draft storage run together;
// the spawn endpoint is a controlled fixture because the scenario image has no
// promptable agent. The real /api/spawn-attachments endpoint runs.
test.describe('Spawn file attachments: picker, drop, restore, submit', () => {
  let stagedId: string;
  let uploads = 0;
  let spawnPayload: Record<string, unknown> | undefined;

  test.beforeAll(async () => {
    await waitForHealthy();
    await seedConfig();
  });

  test('attach a CSV by picker, drop a JPEG, reload, and submit', async ({ page }) => {
    uploads = 0;
    spawnPayload = undefined;
    stagedId = '00000000-0000-0000-0000-000000000001';

    await page.route('**/api/spawn-attachments?*', async (route) => {
      const request = route.request();
      expect(request.method()).toBe('POST');
      expect(new URL(request.url()).searchParams.get('filename')).toBe('users.csv');
      expect(request.postDataBuffer()).toEqual(Buffer.from('id,name\n1,Alice'));
      uploads += 1;
      await route.fulfill({
        json: { id: stagedId, name: 'users.csv' },
        status: 201,
      });
    });

    await page.route('**/api/spawn', async (route) => {
      const request = route.request();
      spawnPayload = JSON.parse(request.postData() ?? '{}');
      await route.fulfill({
        json: [
          {
            session_id: 'spawn-attachment-session',
            workspace_id: 'spawn-attachment-workspace',
          },
        ],
        status: 200,
      });
    });

    await page.goto('/spawn');
    await expect(page.getByTestId('spawn-submit')).toBeVisible();

    // Picker: Attach opens and accepts a CSV.
    const picker = page.waitForEvent('filechooser');
    await page.getByTestId('spawn-attach').click();
    await (
      await picker
    ).setFiles([
      { name: 'users.csv', mimeType: 'text/csv', buffer: Buffer.from('id,name\n1,Alice') },
    ]);
    await expect(page.getByTestId('spawn-file-chip')).toHaveText('users.csv');
    expect(uploads).toBe(1);

    // Drop: drag a JPEG over the spawn form, see the overlay, drop.
    const dataTransfer = await page.evaluateHandle(() => {
      const transfer = new DataTransfer();
      transfer.items.add(new File(['jpeg-bytes'], 'photo.jpg', { type: 'image/jpeg' }));
      return transfer;
    });
    const dropZone = page.getByTestId('spawn-drop-zone');
    const box = await dropZone.boundingBox();
    expect(box).not.toBeNull();
    const center = { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 };

    await dataTransfer.evaluate((transfer, point) => {
      const target = document.elementFromPoint(point.x, point.y);
      if (!target) throw new Error(`No drag target at ${point.x},${point.y}`);
      target.dispatchEvent(
        new DragEvent('dragenter', { bubbles: true, cancelable: true, dataTransfer: transfer })
      );
      target.dispatchEvent(
        new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: transfer })
      );
    }, center);
    await dataTransfer.dispose();

    await expect(page.getByTestId('spawn-file-drop-overlay')).toHaveCount(0);
    await expect(page.getByTestId('spawn-image-chip')).toBeVisible();

    // Reload: chips survive from sessionStorage without uploading again.
    await page.reload();
    await expect(page.getByTestId('spawn-file-chip')).toHaveText('users.csv');
    await expect(page.getByTestId('spawn-image-chip')).toBeVisible();
    expect(uploads).toBe(1);

    // Submit: send file_attachments with the staged id and the JPEG with its
    // real media type. Pick a target so submit is enabled.
    await page.locator('[data-testid="agent-select"]').selectOption({ index: 1 });
    await expect(page.getByTestId('spawn-submit')).toBeEnabled();
    await page.getByTestId('spawn-submit').click();

    await expect.poll(() => spawnPayload).toBeDefined();
    expect(spawnPayload!.file_attachments).toEqual([stagedId]);
    expect(spawnPayload!.images).toEqual([
      { media_type: 'image/jpeg', data: Buffer.from('jpeg-bytes').toString('base64') },
    ]);

    // After a successful spawn the chips clear.
    await expect(page.getByTestId('spawn-file-chip')).toHaveCount(0);
    await expect(page.getByTestId('spawn-image-chip')).toHaveCount(0);
  });
});
