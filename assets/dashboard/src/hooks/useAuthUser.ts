import { useEffect, useState } from 'react';
import { getAuthMe } from '../lib/api';

export type AuthUser = {
  login: string;
  name?: string;
  avatar_url?: string;
};

/**
 * Hook to fetch the authenticated user.
 * @param authEnabled - If false, skips the fetch entirely (returns null).
 */
export default function useAuthUser(authEnabled: boolean = true) {
  const [user, setUser] = useState<AuthUser | null>(null);

  useEffect(() => {
    if (!authEnabled) {
      setUser(null);
      return;
    }

    let mounted = true;
    const load = async () => {
      try {
        const data = await getAuthMe();
        if (mounted) {
          setUser(data);
        }
      } catch {
        if (mounted) {
          setUser(null);
        }
      }
    };

    load();

    return () => {
      mounted = false;
    };
  }, [authEnabled]);

  return user;
}
