import React, {
  createContext,
  useState,
  useContext,
  useEffect,
  useRef,
  useCallback,
  useMemo,
} from 'react';
import { getAuthMe, logoutAuth } from '../lib/api';
import type { AuthUser } from '../lib/types';

type AuthContextValue = {
  user: AuthUser | null;
  // true = signed in; false = show gate; null = auth disabled / not applicable.
  authenticated: boolean | null;
  loading: boolean;
  renewing: boolean;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [authenticated, setAuthenticated] = useState<boolean | null>(null);
  const [loading, setLoading] = useState(true);
  const [renewing, setRenewing] = useState(false);

  // authedRef mirrors `authenticated === true` for the event listener's closure.
  const authedRef = useRef(false);
  const redirectedRef = useRef(false);

  // Register the expiry listener FIRST so it is in place before the mount fetch
  // resolves (the mount /auth/me 401 dispatches this same event).
  useEffect(() => {
    const onExpired = () => {
      // Only refresh a session that was authenticated this run. On the initial
      // unauthenticated load authedRef is false, so we show the gate instead.
      if (!authedRef.current || redirectedRef.current) return;
      redirectedRef.current = true;
      setRenewing(true);
      window.location.assign('/auth/login');
    };
    window.addEventListener('schmux:auth-expired', onExpired);
    return () => window.removeEventListener('schmux:auth-expired', onExpired);
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const result = await getAuthMe();
      if (cancelled) return;
      if (result.status === 'authenticated') {
        setUser(result.user);
        setAuthenticated(true);
        authedRef.current = true;
      } else if (result.status === 'unauthenticated') {
        setUser(null);
        setAuthenticated(false);
        authedRef.current = false;
      } else {
        setUser(null);
        setAuthenticated(null);
        authedRef.current = false;
      }
      setLoading(false);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const logout = useCallback(async () => {
    try {
      await logoutAuth();
    } finally {
      window.location.assign('/');
    }
  }, []);

  const value = useMemo(
    () => ({ user, authenticated, loading, renewing, logout }),
    [user, authenticated, loading, renewing, logout]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
