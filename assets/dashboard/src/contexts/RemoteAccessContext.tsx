import { createContext, useContext } from 'react';
import type { RemoteAccessStatus } from '../lib/types';

type RemoteAccessContextValue = {
  remoteAccessStatus: RemoteAccessStatus;
  simulateRemote: boolean;
  setSimulateRemote: (value: boolean) => void;
};

export const RemoteAccessContext = createContext<RemoteAccessContextValue | null>(null);

export function useRemoteAccess() {
  const ctx = useContext(RemoteAccessContext);
  if (!ctx) {
    throw new Error('useRemoteAccess must be used within a SessionsProvider');
  }
  return ctx;
}
