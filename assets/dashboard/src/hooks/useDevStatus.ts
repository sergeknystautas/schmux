import { useState, useEffect } from 'react';
import useVersionInfo from './useVersionInfo';
import { getDevStatus, type DevStatus } from '../lib/api';

/**
 * Returns the dev mode status when running in dev mode, or null otherwise.
 * Caches the result for the component's lifetime and refetches when
 * connection state changes.
 */
export default function useDevStatus() {
  const { versionInfo } = useVersionInfo();
  const isDevMode = !!versionInfo?.dev_mode;
  const [devStatus, setDevStatus] = useState<DevStatus | null>(null);

  useEffect(() => {
    if (!isDevMode) return;
    getDevStatus()
      .then(setDevStatus)
      .catch(() => {});
  }, [isDevMode]);

  return { isDevMode, devStatus };
}
