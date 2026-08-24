import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import React from 'react';
import { BuildMonitorProvider, useBuildMonitor } from './BuildMonitorContext';

let mockUpdateCount = 0;
vi.mock('./SessionsContext', () => ({
  useSessions: () => ({ buildMonitorUpdateCount: mockUpdateCount }),
}));

const realFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  mockUpdateCount = 0;
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});
afterEach(() => {
  globalThis.fetch = realFetch;
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: React.ReactNode }) {
  return <BuildMonitorProvider>{children}</BuildMonitorProvider>;
}

function mockGet(payload: unknown) {
  fetchMock.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(payload),
  });
}

function mockPost(payload: unknown) {
  fetchMock.mockResolvedValueOnce({
    ok: true,
    json: () => Promise.resolve(payload),
  });
}

describe('BuildMonitorContext', () => {
  it('fetches /api/build-monitor on mount', async () => {
    mockGet({ enabled: true, launch_configured: false, units: [] });
    const { result } = renderHook(() => useBuildMonitor(), { wrapper });
    await waitFor(() => expect(result.current.data.units).toBeDefined());
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/api/build-monitor'),
      expect.anything()
    );
  });

  it('exposes normalized units — null arrays become empty arrays', async () => {
    mockGet({
      enabled: true,
      launch_configured: false,
      units: [
        {
          slug: 'a',
          repo_name: 'a',
          repo: 'https://x/a',
          configured: true,
          // workflows intentionally missing
          // failed_jobs intentionally missing
        },
      ],
    });
    const { result } = renderHook(() => useBuildMonitor(), { wrapper });
    await waitFor(() => expect(result.current.data.units.length).toBe(1));
    const unit = result.current.data.units[0];
    expect(unit.workflows).toEqual([]);
  });

  it('refetches when buildMonitorUpdateCount changes', async () => {
    mockGet({ enabled: true, launch_configured: false, units: [] });
    const { result, rerender } = renderHook(() => useBuildMonitor(), { wrapper });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    mockGet({ enabled: true, launch_configured: false, units: [] });
    mockUpdateCount = 1;
    rerender();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it('checkNow POSTs /api/build-monitor/check and replaces data', async () => {
    mockGet({ enabled: true, launch_configured: false, units: [] });
    const { result } = renderHook(() => useBuildMonitor(), { wrapper });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    mockPost({
      enabled: true,
      launch_configured: false,
      units: [
        {
          slug: 'a',
          repo_name: 'a',
          repo: 'https://x/a',
          configured: true,
          workflows: [
            {
              name: 'ci',
              path: '.github/workflows/ci.yml',
              status: 'completed',
              conclusion: 'success',
            },
          ],
        },
      ],
    });

    await act(async () => {
      await result.current.checkNow();
    });

    expect(fetchMock).toHaveBeenLastCalledWith('/api/build-monitor/check', { method: 'POST' });
    expect(result.current.data.units[0].workflows?.[0]?.conclusion).toBe('success');
    expect(result.current.checking).toBe(false);
  });

  it('checkNow surfaces error and clears checking on failure', async () => {
    mockGet({ enabled: true, launch_configured: false, units: [] });
    const { result } = renderHook(() => useBuildMonitor(), { wrapper });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    fetchMock.mockResolvedValueOnce({ ok: false, status: 500, json: () => Promise.resolve({}) });

    await act(async () => {
      await result.current.checkNow();
    });

    expect(result.current.error).toMatch(/HTTP 500/);
    expect(result.current.checking).toBe(false);
  });
});
