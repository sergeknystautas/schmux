import { test, expect } from '@playwright/test';
import { apiGet, apiPost, waitForHealthy } from './helpers';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Remote access PIN security enforcement', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
  });

  test('rejects empty PIN', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-pin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin: '' }),
    });
    expect(res.status).toBe(400);
  });

  test('rejects 3-char PIN with descriptive error', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-pin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin: 'abc' }),
    });
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('at least 6');
  });

  test('rejects 5-char PIN', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-pin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin: '12345' }),
    });
    expect(res.status).toBe(400);
  });

  test('accepts valid 9-char PIN', async () => {
    const result = await apiPost<{ ok: boolean }>('/api/remote-access/set-pin', {
      pin: 'secure123',
    });
    expect(result.ok).toBe(true);
  });

  test('config shows pin_hash_set true after setting PIN', async () => {
    const config = await apiGet<{ remote_access: { pin_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.pin_hash_set).toBe(true);
  });

  test('config shows default timeout of 120 minutes', async () => {
    const config = await apiGet<{ remote_access: { timeout_minutes: number } }>('/api/config');
    expect(config.remote_access.timeout_minutes).toBe(120);
  });
});
