import { createContext, useContext } from 'react';
import type { OverlayChangeEvent } from '../lib/types';

export type OverlayContextValue = {
  overlayEvents: OverlayChangeEvent[];
  overlayUnreadCount: number;
  clearOverlayEvents: () => void;
  markOverlaysRead: () => void;
};

export const OverlayContext = createContext<OverlayContextValue | null>(null);

export function useOverlay() {
  const ctx = useContext(OverlayContext);
  if (!ctx) {
    throw new Error('useOverlay must be used within a SessionsProvider');
  }
  return ctx;
}
