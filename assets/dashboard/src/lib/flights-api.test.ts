import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  getFlights,
  getFlight,
  createFlight,
  updateFlight,
  deleteFlight,
  reorderFlights,
  takeoffFlight,
  enterFlightExecution,
  enterFlightCompletion,
  markFlightDone,
  cancelFlight,
  terminateFlight,
} from './api';

const mockFetch = vi.fn();

describe('flights api client', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    // Use window.fetch via the transport; stub it directly so tests don't
    // need to import the transport singleton.
    vi.stubGlobal('fetch', mockFetch);
    window.history.pushState({}, '', '/');
    // Reset CSRF cookie between tests.
    document.cookie = 'schmux_csrf=; Max-Age=0';
    document.cookie = 'csrf=; Max-Age=0';
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  function ok(body: unknown): Response {
    return {
      ok: true,
      json: () => Promise.resolve(body),
      text: () => Promise.resolve(JSON.stringify(body)),
      status: 200,
    } as Response;
  }

  function fail(status: number, message: string): Response {
    return {
      ok: false,
      status,
      text: () => Promise.resolve(message),
      json: () => Promise.resolve({ error: message }),
    } as Response;
  }

  it('getFlights returns the array payload', async () => {
    mockFetch.mockResolvedValueOnce(ok({ flights: [{ id: 'a' }] }));
    const out = await getFlights();
    expect(out).toEqual([{ id: 'a' }]);
    expect(mockFetch).toHaveBeenCalledWith('/api/flights', undefined);
  });

  it('getFlights tolerates a missing flights field', async () => {
    mockFetch.mockResolvedValueOnce(ok({}));
    const out = await getFlights();
    expect(out).toEqual([]);
  });

  it('getFlight requests a single flight', async () => {
    mockFetch.mockResolvedValueOnce(ok({ id: 'a' }));
    await getFlight('a');
    expect(mockFetch.mock.calls[0][0]).toBe('/api/flights/a');
  });

  it('createFlight sends a JSON POST', async () => {
    mockFetch.mockResolvedValueOnce(ok({ id: 'a' }));
    await createFlight({
      repo: 'r',
      prompt: 'p',
      planner_model: 'p1',
      executor_model: 'e1',
    });
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({
      repo: 'r',
      prompt: 'p',
      planner_model: 'p1',
      executor_model: 'e1',
    });
  });

  it('updateFlight sends PATCH', async () => {
    mockFetch.mockResolvedValueOnce(ok({ id: 'a' }));
    await updateFlight('a', { prompt: 'p2' });
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights/a');
    expect(init.method).toBe('PATCH');
    expect(JSON.parse(init.body)).toEqual({ prompt: 'p2' });
  });

  it('deleteFlight sends DELETE', async () => {
    mockFetch.mockResolvedValueOnce(ok({ status: 'ok' }));
    await deleteFlight('a');
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights/a');
    expect(init.method).toBe('DELETE');
  });

  it('reorderFlights sends the ordered list', async () => {
    mockFetch.mockResolvedValueOnce(ok({ status: 'ok' }));
    await reorderFlights(['b', 'a']);
    const [, init] = mockFetch.mock.calls[0];
    expect(JSON.parse(init.body)).toEqual({ flight_ids: ['b', 'a'] });
  });

  it('takeoffFlight posts to the takeoff endpoint', async () => {
    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await takeoffFlight('a');
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights/a/takeoff');
    expect(init.method).toBe('POST');
  });

  it('enterFlightExecution posts to the execution endpoint', async () => {
    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await enterFlightExecution('a');
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights/a/execution');
    expect(init.method).toBe('POST');
  });

  it('enterFlightCompletion posts with no body', async () => {
    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await enterFlightCompletion('a');
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/flights/a/completion');
    expect(init.method).toBe('POST');
    expect(init.body).toBeUndefined();
  });

  it('markFlightDone and cancelFlight and terminateFlight post correctly', async () => {
    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await markFlightDone('a');
    expect(mockFetch.mock.calls[0][0]).toBe('/api/flights/a/done');

    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await cancelFlight('a', 'reason');
    const [, init] = mockFetch.mock.calls[1];
    expect(JSON.parse(init.body)).toEqual({ reason: 'reason' });

    mockFetch.mockResolvedValueOnce(ok({ flight: { id: 'a' } }));
    await terminateFlight('a', { workspace_id: 'ws-1', confirm: true });
    const [url, init3] = mockFetch.mock.calls[2];
    expect(url).toBe('/api/flights/a/terminate');
    expect(JSON.parse(init3.body)).toEqual({ workspace_id: 'ws-1', confirm: true });
  });

  it('throws on error responses', async () => {
    mockFetch.mockResolvedValueOnce(fail(409, 'conflict'));
    await expect(getFlight('a')).rejects.toThrow();
  });
});
