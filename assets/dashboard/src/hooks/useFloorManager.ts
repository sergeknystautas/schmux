import { useMemo } from 'react';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig } from '../contexts/ConfigContext';

type FloorManagerState = {
  enabled: boolean;
  sessionId: string | null;
  running: boolean;
  loading: boolean;
  escalation: string | undefined;
};

export function useFloorManager(): FloorManagerState {
  const { sessionsById, loading: sessionsLoading } = useSessions();
  const { config, loading: configLoading } = useConfig();

  return useMemo(() => {
    const loading = sessionsLoading || configLoading;
    const enabled = config?.floor_manager?.enabled ?? false;

    // Find the floor manager session from the WebSocket-driven sessions data
    const fmEntry = Object.entries(sessionsById).find(([, session]) => session.is_floor_manager);

    if (!fmEntry) {
      return { enabled, sessionId: null, running: false, loading, escalation: undefined };
    }

    const [sessionId, session] = fmEntry;
    return {
      enabled,
      sessionId,
      running: session.running,
      loading,
      escalation: session.escalation,
    };
  }, [sessionsById, sessionsLoading, configLoading, config?.floor_manager?.enabled]);
}
