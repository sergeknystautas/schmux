import { useMemo } from 'react';
import { useConfig } from '../../contexts/ConfigContext';
import { isSessionInProgress, type SessionInProgressPredicate } from '../../lib/workspaceSort';

/**
 * The same in-progress predicate AppShell uses for the sidebar workspace
 * sort. Flight activity lines must use it so they and the sidebar agree on
 * what is busy.
 */
export default function useFlightInProgress(): SessionInProgressPredicate {
  const { config } = useConfig();
  return useMemo<SessionInProgressPredicate>(() => {
    const runTargets = config?.run_targets?.map((t: { name: string }) => t.name) ?? [];
    const nudgenikEnabled = (config?.nudgenik?.targets?.length ?? 0) > 0;
    return (session) =>
      isSessionInProgress(session, {
        nudgenikEnabled,
        isPromptable: !runTargets.includes(session.target),
      });
  }, [config]);
}
