import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import FeatureRoute from './FeatureRoute';
import type { Features } from '../lib/types.generated';

const mockFeatures: Features = {
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
  vendor_locked: false,
};
let mockLoading = false;

vi.mock('../contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: mockFeatures, loading: mockLoading }),
}));

function renderAt(path: string, feature: keyof Features) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/" element={<div>home</div>} />
        <Route
          path="/feature"
          element={
            <FeatureRoute feature={feature}>
              <div>feature page</div>
            </FeatureRoute>
          }
        />
      </Routes>
    </MemoryRouter>
  );
}

describe('FeatureRoute', () => {
  beforeEach(() => {
    for (const k of Object.keys(mockFeatures) as Array<keyof Features>) {
      mockFeatures[k] = true;
    }
    mockLoading = false;
  });

  it('renders children when the feature is available', () => {
    renderAt('/feature', 'personas');
    expect(screen.getByText('feature page')).toBeInTheDocument();
    expect(screen.queryByText('home')).not.toBeInTheDocument();
  });

  it('redirects to home when the feature is compiled out', () => {
    mockFeatures.personas = false;
    renderAt('/feature', 'personas');
    expect(screen.getByText('home')).toBeInTheDocument();
    expect(screen.queryByText('feature page')).not.toBeInTheDocument();
  });

  it('renders children while features are still loading (avoids redirect flash)', () => {
    mockFeatures.personas = false;
    mockLoading = true;
    renderAt('/feature', 'personas');
    // While loading, we trust the optimistic default and render the page.
    // Once loaded, an effect would re-render and redirect — but in this snapshot
    // the page stays mounted.
    expect(screen.getByText('feature page')).toBeInTheDocument();
  });
});
