import { createContext, useContext } from 'react';
import type {
  LinearSyncResolveConflictStatePayload,
  WorkspaceLockState,
  WorkspaceSyncResultEvent,
} from '../lib/types';

type SyncContextValue = {
  linearSyncResolveConflictStates: Record<string, LinearSyncResolveConflictStatePayload>;
  clearLinearSyncResolveConflictState: (workspaceId: string) => void;
  workspaceLockStates: Record<string, WorkspaceLockState>;
  syncResultEvents: WorkspaceSyncResultEvent[];
  clearSyncResultEvents: () => void;
};

export const SyncContext = createContext<SyncContextValue | null>(null);

export function useSyncState() {
  const ctx = useContext(SyncContext);
  if (!ctx) {
    throw new Error('useSyncState must be used within a SessionsProvider');
  }
  return ctx;
}
