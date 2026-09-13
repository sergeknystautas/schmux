import { useEffect } from 'react';
import { authCheck } from '../lib/api';

/**
 * Signed-out recovery focus trigger: every activation of the chat page —
 * load, session-tab switch (remount), window refocus, visibility change —
 * asks the daemon to verify the login for the session's protocol,
 * unconditionally (banner or not). Remote sessions never ask (their login
 * lives on another host). Failures are swallowed: the check is best-effort
 * and state arrives via the session broadcast.
 */
export function useAuthCheckOnFocus(sessionId: string | undefined, skip: boolean): void {
  useEffect(() => {
    if (!sessionId || skip) return;
    const fire = () => {
      void authCheck(sessionId).catch(() => {});
    };
    fire();
    const onVisibility = () => {
      if (document.visibilityState === 'visible') fire();
    };
    window.addEventListener('focus', fire);
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      window.removeEventListener('focus', fire);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [sessionId, skip]);
}
