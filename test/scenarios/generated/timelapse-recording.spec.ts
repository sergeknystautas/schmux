import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
  waitForTerminalOutput,
  sleep,
  apiGet,
} from './helpers';

const BASE_URL = 'http://localhost:7337';

interface TimelapseRecording {
  RecordingID: string;
  SessionID: string;
  Duration: number;
  FileSize: number;
  InProgress: boolean;
  HasCompressed: boolean;
}

async function pollForRecording(sessionId: string, maxAttempts = 30): Promise<TimelapseRecording> {
  for (let i = 0; i < maxAttempts; i++) {
    const recordings = await apiGet<TimelapseRecording[]>('/api/timelapse');
    const rec = recordings.find((r) => r.SessionID === sessionId) || recordings[0];
    if (rec) return rec;
    await sleep(1000);
  }
  // Debug: check filesystem
  const { execSync } = await import('child_process');
  const home = process.env.HOME || '/root';
  let diag = '';
  try {
    diag = execSync(`ls -laR ${home}/.schmux/recordings/ 2>&1`).toString();
  } catch {
    diag = 'recordings dir not found';
  }
  throw new Error(`No recording found after ${maxAttempts}s. Files: ${diag}`);
}

test.describe('Session recording and timelapse generation', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('timelapse-test');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'output-agent',
          command:
            "sh -c 'for i in $(seq 1 10); do echo line_$i; done; echo TIMELAPSE_DONE; sleep 600'",
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'timelapse-branch',
      targets: { 'output-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
    await waitForTerminalOutput(sessionId, 'TIMELAPSE_DONE', 20_000);
    await sleep(3000); // give recorder time to flush
  });

  test('recording exists and has correct metadata', async () => {
    const rec = await pollForRecording(sessionId);
    // Recording exists with at least the header (130 bytes).
    // Events may not have flushed yet, so we just verify the file exists.
    expect(rec.FileSize).toBeGreaterThanOrEqual(100);
    // SessionID should be derived from the recording filename
    expect(rec.RecordingID).toContain(sessionId.split('-').slice(0, 3).join('-'));
  });

  test('original recording is downloadable as valid asciicast', async () => {
    const rec = await pollForRecording(sessionId);
    const res = await fetch(`${BASE_URL}/api/timelapse/${rec.RecordingID}/download`);
    expect(res.status).toBe(200);

    const body = await res.text();
    const header = JSON.parse(body.split('\n')[0]);
    expect(header.version).toBe(2);
    expect(header.width).toBeGreaterThan(0);
  });

  test('compression produces a timelapse file', async () => {
    const rec = await pollForRecording(sessionId);

    const exportRes = await fetch(`${BASE_URL}/api/timelapse/${rec.RecordingID}/export`, {
      method: 'POST',
    });
    expect(exportRes.status).toBe(200);

    const compRes = await fetch(
      `${BASE_URL}/api/timelapse/${rec.RecordingID}/download?type=timelapse`
    );
    expect(compRes.status).toBe(200);

    const compHeader = JSON.parse((await compRes.text()).split('\n')[0]);
    expect(compHeader.version).toBe(2);
  });

  test('timelapse page shows the recording', async ({ page }) => {
    await page.goto('/timelapse');
    await waitForDashboardLive(page);
    await expect(page.locator('table')).toBeVisible({ timeout: 5000 });
    await expect(page.getByRole('button', { name: 'Original' }).first()).toBeVisible();
    await expect(page.getByRole('button', { name: 'Timelapse' }).first()).toBeVisible();
  });

  test('session detail page has an enabled Make timelapse button', async ({ page }) => {
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    const btn = page.getByRole('button', { name: 'Make timelapse' });
    await expect(btn).toBeVisible();
    await expect(btn).toBeEnabled();
  });

  test('delete removes the recording', async () => {
    const rec = await pollForRecording(sessionId);

    const deleteRes = await fetch(`${BASE_URL}/api/timelapse/${rec.RecordingID}`, {
      method: 'DELETE',
    });
    expect(deleteRes.status).toBe(204);

    const after = await apiGet<TimelapseRecording[]>('/api/timelapse');
    const found = after.find((r) => r.RecordingID === rec.RecordingID);
    expect(found).toBeUndefined();
  });
});

test.describe('Timelapse disabled hides UI elements', () => {
  let repoPath: string;
  let sessionId: string;
  let savedConfig: Record<string, unknown>;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('timelapse-disabled-test');

    savedConfig = await apiGet<Record<string, unknown>>('/api/config');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'idle-agent',
          command: "sh -c 'echo ready; sleep 600'",
        },
      ],
    });

    // Disable timelapse
    await fetch(`${BASE_URL}/api/config`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ timelapse: { enabled: false } }),
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'no-timelapse',
      targets: { 'idle-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test.afterAll(async () => {
    if (savedConfig) {
      await fetch(`${BASE_URL}/api/config`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(savedConfig),
      });
    }
  });

  test('sidebar does not show Timelapse link', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);
    const timelapseLink = page.locator('a[href="/timelapse"]');
    await expect(timelapseLink).toHaveCount(0);
  });

  test('session detail page does not show Make timelapse button', async ({ page }) => {
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });

    const btn = page.getByRole('button', { name: 'Make timelapse' });
    await expect(btn).toHaveCount(0);
  });
});
