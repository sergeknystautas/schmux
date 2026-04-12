import React, { createContext, useState, useContext, useEffect, useMemo } from 'react';
import { getFeatures } from '../lib/api';
import type { Features } from '../lib/types.generated';

type FeaturesContextValue = {
  features: Features;
  loading: boolean;
};

const DEFAULT_FEATURES: Features = {
  tunnel: true,
  github: true,
  telemetry: true,
  update: true,
  dashboardsx: true,
  model_registry: true,
  repofeed: true,
  subreddit: true,
  personas: true,
  comm_styles: true,
  autolearn: true,
  floor_manager: true,
  timelapse: true,
};

const FeaturesContext = createContext<FeaturesContextValue | null>(null);

export function FeaturesProvider({ children }: { children: React.ReactNode }) {
  const [features, setFeatures] = useState(DEFAULT_FEATURES);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    (async () => {
      try {
        const data = await getFeatures();
        setFeatures(data);
      } catch (err) {
        console.error('Failed to load features:', err);
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const value = useMemo(() => ({ features, loading }), [features, loading]);

  return <FeaturesContext.Provider value={value}>{children}</FeaturesContext.Provider>;
}

export function useFeatures() {
  const ctx = useContext(FeaturesContext);
  if (!ctx) {
    throw new Error('useFeatures must be used within a FeaturesProvider');
  }
  return ctx;
}
