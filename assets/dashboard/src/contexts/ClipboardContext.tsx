import { createContext, useContext } from 'react';
import type { PendingClipboardRequest } from '../hooks/useSessionsWebSocket';

// ClipboardContext exposes the per-session pending OSC 52 clipboard
// requests broadcast over /ws/dashboard. The state lives in the shared
// useSessionsWebSocket hook (single dispatch site for all dashboard
// events) and is published here as a focused sub-context — same pattern
// as SyncContext, OverlayContext, MonitorContext.
//
// Why a peer sub-context rather than extending SessionsContext: keeps
// SessionsContext focused on workspace/session list state, lets
// components that only need clipboard data subscribe without re-rendering
// on unrelated session updates.
type ClipboardContextValue = {
  /** Per-session map keyed by sessionID. Undefined slot means no pending request. */
  pendingClipboard: Record<string, PendingClipboardRequest | undefined>;
  /**
   * Locally clear a pending request for a session. Called by the banner
   * after a successful approve/reject POST so the UI updates immediately
   * without waiting for the daemon's clipboardCleared broadcast.
   */
  clearPendingClipboard: (sessionId: string) => void;
};

export const ClipboardContext = createContext<ClipboardContextValue | null>(null);

export function useClipboard() {
  const ctx = useContext(ClipboardContext);
  if (!ctx) {
    throw new Error('useClipboard must be used within a SessionsProvider');
  }
  return ctx;
}

// Re-export the request shape for component prop types.
export type { PendingClipboardRequest };
