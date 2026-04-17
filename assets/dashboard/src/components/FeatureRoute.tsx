import React from 'react';
import { Navigate } from 'react-router-dom';
import { useFeatures } from '../contexts/FeaturesContext';
import type { Features } from '../lib/types.generated';

type FeatureRouteProps = {
  feature: keyof Features;
  children: React.ReactNode;
};

// FeatureRoute renders its children only if the named build feature is
// available in this build. When the feature is compiled out, it redirects
// to the home page so direct URL navigation does not surface a page that
// fetches against a disabled API handler. While features are loading the
// children render — the FeaturesContext seeds with all-true defaults so
// available features avoid a redirect flash.
export default function FeatureRoute({ feature, children }: FeatureRouteProps) {
  const { features, loading } = useFeatures();
  if (!loading && !features[feature]) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
}
