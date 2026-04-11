import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import React from 'react';
import { MemoryRouter } from 'react-router-dom';
import { ConfigProvider, useConfig } from './ConfigContext';
import { CONFIG_UPDATED_KEY } from '../lib/constants';

// --- Mocks ---

const mockGetConfig = vi.fn();
vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...args),
  getErrorMessage: (_err: unknown, fallback: string) => fallback,
}));

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <MemoryRouter>
      <ConfigProvider>{children}</ConfigProvider>
    </MemoryRouter>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ConfigContext', () => {
  it('fetches config on mount', async () => {
    mockGetConfig.mockResolvedValue({
      workspace_path: '/home/user/ws',
      repos: [{ name: 'my-repo', url: 'https://github.com/user/repo.git' }],
      run_targets: [],
      models: [],
    });

    const { result } = renderHook(() => useConfig(), { wrapper });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(mockGetConfig).toHaveBeenCalledOnce();
    expect(result.current.config.workspace_path).toBe('/home/user/ws');
    expect(result.current.error).toBeNull();
  });

  it('handles fetch error', async () => {
    mockGetConfig.mockRejectedValue(new Error('Network error'));

    const { result } = renderHook(() => useConfig(), { wrapper });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.error).toBe('Failed to load config');
  });

  it('cross-tab sync updates on storage event', async () => {
    mockGetConfig
      .mockResolvedValueOnce({
        workspace_path: '/home/user/ws',
        repos: [{ name: 'repo1', url: 'https://github.com/user/repo1.git' }],
        run_targets: [],
        models: [],
      })
      .mockResolvedValueOnce({
        workspace_path: '/home/user/ws',
        repos: [
          { name: 'repo1', url: 'https://github.com/user/repo1.git' },
          { name: 'repo2', url: 'https://github.com/user/repo2.git' },
        ],
        run_targets: [],
        models: [],
      });

    const { result } = renderHook(() => useConfig(), { wrapper });

    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });

    expect(result.current.config.repos).toHaveLength(1);

    // Simulate storage event from another tab
    act(() => {
      window.dispatchEvent(
        new StorageEvent('storage', {
          key: CONFIG_UPDATED_KEY,
          newValue: Date.now().toString(),
        })
      );
    });

    await waitFor(() => {
      expect(mockGetConfig).toHaveBeenCalledTimes(2);
    });

    await waitFor(() => {
      expect(result.current.config.repos).toHaveLength(2);
    });
  });
});
