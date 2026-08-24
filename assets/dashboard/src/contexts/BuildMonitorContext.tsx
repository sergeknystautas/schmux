import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { useSessions } from './SessionsContext';
import type { BuildMonitorResponse, BuildMonitorUnit } from '../lib/types.generated';

type BuildMonitorContextValue = {
  data: BuildMonitorResponse;
  error: string;
  checking: boolean;
  checkNow: () => Promise<void>;
};

// Defend against null/absent arrays in the wire shape (Go nil slices marshal
// to null; workflows and failed_jobs are omitempty).
function normalize(raw: unknown): BuildMonitorResponse {
  const d = (raw || {}) as Partial<BuildMonitorResponse> & {
    units?: Array<Partial<BuildMonitorUnit> & { workflows?: unknown[] }>;
  };
  return {
    enabled: !!d.enabled,
    launch_configured: !!d.launch_configured,
    units: (d.units || []).map((u) => ({
      ...u,
      workflows: ((u.workflows || []) as unknown as Array<Record<string, unknown>>).map((w) => ({
        ...w,
        failed_jobs: (w.failed_jobs as unknown[]) || [],
      })) as BuildMonitorUnit['workflows'],
    })),
  };
}

const EMPTY: BuildMonitorResponse = { enabled: false, launch_configured: false, units: [] };

const BuildMonitorContext = createContext<BuildMonitorContextValue | null>(null);

async function fetchBuildMonitor(signal: AbortSignal): Promise<BuildMonitorResponse> {
  const r = await fetch('/api/build-monitor', { signal });
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return normalize(await r.json());
}

export function BuildMonitorProvider({ children }: { children: React.ReactNode }) {
  const { buildMonitorUpdateCount } = useSessions();
  const [data, setData] = useState<BuildMonitorResponse>(EMPTY);
  const [error, setError] = useState('');
  const [checking, setChecking] = useState(false);

  useEffect(() => {
    const ctrl = new AbortController();
    fetchBuildMonitor(ctrl.signal)
      .then((d) => {
        setData(d);
        setError('');
      })
      .catch((e) => {
        if (e?.name !== 'AbortError') setError(String(e?.message || e));
      });
    return () => ctrl.abort();
  }, [buildMonitorUpdateCount]);

  const checkNow = useCallback(async () => {
    setChecking(true);
    setError('');
    try {
      const r = await fetch('/api/build-monitor/check', { method: 'POST' });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      setData(normalize(await r.json()));
    } catch (e) {
      setError(String((e as Error)?.message || e));
    } finally {
      setChecking(false);
    }
  }, []);

  const value = useMemo<BuildMonitorContextValue>(
    () => ({ data, error, checking, checkNow }),
    [data, error, checking, checkNow]
  );

  return <BuildMonitorContext.Provider value={value}>{children}</BuildMonitorContext.Provider>;
}

export function useBuildMonitor() {
  const ctx = useContext(BuildMonitorContext);
  if (!ctx) {
    throw new Error('useBuildMonitor must be used within a BuildMonitorProvider');
  }
  return ctx;
}
