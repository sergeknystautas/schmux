import { useState, useEffect, useCallback } from 'react';
import { getPromptHistory } from '../lib/api';
import type { PromptHistoryEntry } from '../lib/types.generated';

export default function usePromptHistory(repo: string) {
  const [prompts, setPrompts] = useState<PromptHistoryEntry[]>([]);
  const [loading, setLoading] = useState(false);

  const fetch = useCallback(async () => {
    if (!repo) return;
    setLoading(true);
    try {
      const data = await getPromptHistory(repo);
      setPrompts(data.prompts || []);
    } catch {
      // Non-critical
    } finally {
      setLoading(false);
    }
  }, [repo]);

  useEffect(() => {
    fetch();
  }, [fetch]);

  return { prompts, loading, refetch: fetch };
}
