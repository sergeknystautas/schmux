import { afterEach, expect, it, vi } from 'vitest';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.resetModules();
  sessionStorage.clear();
  document.cookie = 'schmux_csrf=; Max-Age=0';
});

it('sends each new load directly without storing it in the tab', async () => {
  document.cookie = 'schmux_csrf=test-token';
  const fetchMock = vi.fn(() => new Promise<Response>(() => {}));
  vi.stubGlobal('fetch', fetchMock);
  const setItem = vi.spyOn(Storage.prototype, 'setItem');
  const { captureChatLoad } = await import('./loadTelemetry');

  captureChatLoad({
    sessionId: 'chat-1',
    at: '2026-09-24T14:00:00Z',
    start: 'click',
    frameChars: 15_000_000,
    records: 5700,
    routeToSocketMs: 20,
    socketOpenMs: 5,
    historyWaitMs: 200,
    parseMs: 16,
    reduceMs: 47,
    commitMs: 60,
    afterPaintMs: 40,
    totalMs: 388,
  });

  expect(fetchMock).toHaveBeenCalledOnce();
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/chat/telemetry',
    expect.objectContaining({
      method: 'POST',
      keepalive: true,
      headers: expect.objectContaining({ 'X-CSRF-Token': 'test-token' }),
      body: expect.stringContaining('"sessionId":"chat-1"'),
    })
  );
  expect(setItem).not.toHaveBeenCalled();
});

it('uploads resume fields unchanged', async () => {
  const fetchMock = vi.fn(
    (_input: RequestInfo | URL, _init?: RequestInit) => new Promise<Response>(() => {})
  );
  vi.stubGlobal('fetch', fetchMock);
  const { captureChatLoad } = await import('./loadTelemetry');

  captureChatLoad({
    sessionId: 'chat-1',
    at: '2026-09-25T14:00:00Z',
    start: 'reconnect',
    frameChars: 120,
    records: 1,
    cacheHit: true,
    since: 4,
    lastSeq: 5,
    durableRecords: 1,
    routeToSocketMs: 0,
    socketOpenMs: 5,
    historyWaitMs: 2,
    parseMs: 0,
    reduceMs: 1,
    commitMs: 3,
    afterPaintMs: 4,
    totalMs: 15,
  });

  const sample = JSON.parse(String(fetchMock.mock.calls[0][1]?.body)).loads[0];
  expect(sample).toMatchObject({
    cacheHit: true,
    since: 4,
    lastSeq: 5,
    durableRecords: 1,
  });
});

it('uploads measurements left by the old tab-storage code', async () => {
  sessionStorage.setItem(
    'schmux:chat-load-samples',
    JSON.stringify([{ sessionId: 'chat-1', at: '2026-09-24T14:00:00Z', totalMs: 900 }])
  );
  document.cookie = 'schmux_csrf=test-token';
  const fetchMock = vi.fn(() => new Promise<Response>(() => {}));
  vi.stubGlobal('fetch', fetchMock);

  await import('./loadTelemetry');

  expect(fetchMock).toHaveBeenCalledOnce();
  expect(fetchMock).toHaveBeenCalledWith(
    '/api/chat/telemetry',
    expect.objectContaining({
      method: 'POST',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'test-token' }),
      body: JSON.stringify({
        loads: [{ sessionId: 'chat-1', at: '2026-09-24T14:00:00Z', totalMs: 900 }],
        images: [],
      }),
    })
  );
});
