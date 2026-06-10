import React from 'react';
import { useAuth } from '../contexts/AuthContext';
import AuthGate from './AuthGate';

// Decides what fills the screen based on auth state. Sits directly inside
// AuthProvider and outside the rest of the app so a newcomer's other providers
// never mount (and never fire a burst of 401s).
export default function AuthGateBoundary({ children }: { children: React.ReactNode }) {
  const { authenticated, loading, renewing } = useAuth();

  if (renewing) {
    return <div className="auth-boundary-status">Reconnecting…</div>;
  }
  if (loading) {
    return <div className="auth-boundary-status" aria-busy="true" />;
  }
  if (authenticated === false) {
    return <AuthGate />;
  }
  return <>{children}</>;
}
