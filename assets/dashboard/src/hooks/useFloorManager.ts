import { useState, useEffect, useCallback } from 'react';

export interface FloorManagerState {
  enabled: boolean;
  tmuxSession: string;
  running: boolean;
  injectionCount: number;
  rotationThreshold: number;
}

const defaultState: FloorManagerState = {
  enabled: false,
  tmuxSession: '',
  running: false,
  injectionCount: 0,
  rotationThreshold: 150,
};

/**
 * Hook to fetch and track floor manager state.
 * Polls the REST API since FM state changes are infrequent.
 */
export function useFloorManager(): FloorManagerState {
  const [state, setState] = useState<FloorManagerState>(defaultState);

  const fetchState = useCallback(() => {
    fetch('/api/floor-manager')
      .then((res) => res.json())
      .then((data: Record<string, unknown>) => {
        setState({
          enabled: data.enabled as boolean,
          tmuxSession: (data.tmux_session as string) || '',
          running: data.running as boolean,
          injectionCount: data.injection_count as number,
          rotationThreshold: data.rotation_threshold as number,
        });
      })
      .catch((err) => console.debug('Failed to fetch floor manager state:', err));
  }, []);

  useEffect(() => {
    fetchState();
    // Poll every 10s — FM state changes infrequently
    const interval = setInterval(fetchState, 10000);
    return () => clearInterval(interval);
  }, [fetchState]);

  return state;
}
