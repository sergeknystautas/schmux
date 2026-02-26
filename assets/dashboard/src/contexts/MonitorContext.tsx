import { createContext, useContext } from 'react';
import type { MonitorEvent } from '../lib/types';

export type MonitorContextValue = {
  monitorEvents: MonitorEvent[];
  clearMonitorEvents: () => void;
};

export const MonitorContext = createContext<MonitorContextValue | null>(null);

export function useMonitor() {
  const ctx = useContext(MonitorContext);
  if (!ctx) {
    throw new Error('useMonitor must be used within a SessionsProvider');
  }
  return ctx;
}
