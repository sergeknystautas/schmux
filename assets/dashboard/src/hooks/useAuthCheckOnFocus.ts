import { useEffect } from 'react';
import { authCheck } from '../lib/api';

/**
 * Signed-out recovery trigger: every activation of the page — load,
 * session-tab switch (remount), window refocus, visibility change — asks the
 * daemon to verify the login for the session's protocol. Callers may also
 * request visible-page polling, used only while a sign-in helper is waiting
 * for authentication to complete. Remote sessions never ask (their login
 * lives on another host). Failures are swallowed: the check is best-effort
 * and state arrives via the session broadcast.
 */
export function useAuthCheckOnFocus(
  sessionId: string | undefined,
  skip: boolean,
  pollIntervalMs = 0
): void {
  useEffect(() => {
    if (!sessionId || skip) return;
    const fire = () => {
      void authCheck(sessionId).catch(() => {});
    };
    const fireWhileVisible = () => {
      if (document.visibilityState === 'visible') fire();
    };
    fire();
    const onVisibility = () => {
      if (document.visibilityState === 'visible') fire();
    };
    const pollId = pollIntervalMs > 0 ? window.setInterval(fireWhileVisible, pollIntervalMs) : null;
    window.addEventListener('focus', fire);
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      if (pollId !== null) window.clearInterval(pollId);
      window.removeEventListener('focus', fire);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [sessionId, skip, pollIntervalMs]);
}
