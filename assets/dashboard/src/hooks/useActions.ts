import { useState, useCallback } from 'react';
import { getActions } from '../lib/api';
import type { Action } from '../lib/types.generated';

export function useActions(repo: string) {
  const [actions, setActions] = useState<Action[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refetch = useCallback(async () => {
    if (!repo) return;
    setLoading(true);
    setError(null);
    try {
      const data = await getActions(repo);
      setActions(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch actions');
    } finally {
      setLoading(false);
    }
  }, [repo]);

  return { actions, loading, error, refetch };
}
