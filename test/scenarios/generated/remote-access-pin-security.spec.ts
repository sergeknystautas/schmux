import { test, expect } from '@playwright/test';
import { apiGet, apiPost, waitForHealthy } from './helpers';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Remote access password security enforcement', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    // Reset remote access config to defaults (prior tests may have changed timeout)
    await apiPost('/api/config', {
      remote_access: { enabled: true, timeout_minutes: 120 },
    });
  });

  test('rejects empty password', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: '' }),
    });
    expect(res.status).toBe(400);
  });

  test('rejects 3-char password with descriptive error', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: 'abc' }),
    });
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('at least 8');
  });

  test('rejects 5-char password', async () => {
    const res = await fetch(`${BASE_URL}/api/remote-access/set-password`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ password: '12345' }),
    });
    expect(res.status).toBe(400);
  });

  test('accepts valid 9-char password', async () => {
    const result = await apiPost<{ ok: boolean }>('/api/remote-access/set-password', {
      password: 'secure123',
    });
    expect(result.ok).toBe(true);
  });

  test('config shows password_hash_set true after setting password', async () => {
    const config = await apiGet<{ remote_access: { password_hash_set: boolean } }>('/api/config');
    expect(config.remote_access.password_hash_set).toBe(true);
  });

  test('config shows default timeout of 120 minutes', async () => {
    const config = await apiGet<{ remote_access: { timeout_minutes: number } }>('/api/config');
    expect(config.remote_access.timeout_minutes).toBe(120);
  });
});
