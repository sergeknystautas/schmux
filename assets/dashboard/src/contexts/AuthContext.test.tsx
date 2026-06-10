import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import React from 'react';
import { AuthProvider, useAuth } from './AuthContext';

const mockGetAuthMe = vi.fn();
const mockLogoutAuth = vi.fn();
vi.mock('../lib/api', () => ({
  getAuthMe: (...a: unknown[]) => mockGetAuthMe(...a),
  logoutAuth: (...a: unknown[]) => mockLogoutAuth(...a),
}));

function wrapper({ children }: { children: React.ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}

const realLocation = window.location;
let assignMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  assignMock = vi.fn();
  // jsdom's window.location.assign throws "not implemented"; replace it.
  delete (window as unknown as { location?: Location }).location;
  (window as unknown as { location: unknown }).location = { ...realLocation, assign: assignMock };
});
afterEach(() => {
  (window as unknown as { location: Location }).location = realLocation;
  vi.restoreAllMocks();
});

describe('AuthContext', () => {
  it('exposes authenticated=true and the user on 200', async () => {
    mockGetAuthMe.mockResolvedValue({
      status: 'authenticated',
      user: { login: 'octocat', name: 'Mona', avatar_url: 'a.png' },
    });
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.authenticated).toBe(true);
    expect(result.current.user?.login).toBe('octocat');
  });

  it('exposes authenticated=false on 401 (gate)', async () => {
    mockGetAuthMe.mockResolvedValue({ status: 'unauthenticated' });
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.authenticated).toBe(false);
  });

  it('exposes authenticated=null when auth disabled (404)', async () => {
    mockGetAuthMe.mockResolvedValue({ status: 'disabled' });
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.authenticated).toBeNull();
  });

  it('redirects to /auth/login on schmux:auth-expired when it was authenticated', async () => {
    mockGetAuthMe.mockResolvedValue({
      status: 'authenticated',
      user: { login: 'octocat', name: 'Mona', avatar_url: 'a.png' },
    });
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.authenticated).toBe(true));
    act(() => {
      window.dispatchEvent(new Event('schmux:auth-expired'));
    });
    expect(assignMock).toHaveBeenCalledWith('/auth/login');
  });

  it('does NOT redirect on the initial unauthenticated 401', async () => {
    mockGetAuthMe.mockResolvedValue({ status: 'unauthenticated' });
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.loading).toBe(false));
    // getAuthMe's own 401 would have dispatched the event during mount.
    expect(assignMock).not.toHaveBeenCalled();
  });

  it('logout posts then navigates to /', async () => {
    mockGetAuthMe.mockResolvedValue({
      status: 'authenticated',
      user: { login: 'octocat', name: 'Mona', avatar_url: 'a.png' },
    });
    mockLogoutAuth.mockResolvedValue(undefined);
    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.authenticated).toBe(true));
    await act(async () => {
      await result.current.logout();
    });
    expect(mockLogoutAuth).toHaveBeenCalledOnce();
    expect(assignMock).toHaveBeenCalledWith('/');
  });
});
