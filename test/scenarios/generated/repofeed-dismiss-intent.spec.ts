import { test, expect } from './coverage-fixture';
import { waitForHealthy, seedConfig, createTestRepo, apiPost } from './helpers';

test.describe('Dismiss completed intent from repofeed', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-dismiss-intent');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    // Enable repofeed
    await apiPost('/api/config', {
      repofeed: { enabled: true },
    });
  });

  test('dismiss intent via API returns 200', async () => {
    const res = await fetch(
      `${process.env.SCHMUX_BASE_URL || 'http://localhost:7337'}/api/repofeed/dismiss`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          developer: 'alice@example.com',
          workspace_id: 'ws-test-001',
        }),
      }
    );
    expect(res.status).toBe(200);

    const body = await res.json();
    expect(body.status).toBe('ok');
  });

  test('dismiss intent with missing fields returns 400', async () => {
    const res = await fetch(
      `${process.env.SCHMUX_BASE_URL || 'http://localhost:7337'}/api/repofeed/dismiss`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          developer: 'alice@example.com',
          // workspace_id intentionally omitted
        }),
      }
    );
    expect(res.status).toBe(400);
  });
});
