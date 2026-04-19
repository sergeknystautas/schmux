import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Outlet } from 'react-router-dom';
import type { Features } from './lib/types.generated';

// Mutable mock for FeaturesContext so each test can flip individual flags.
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

vi.mock('./contexts/FeaturesContext', () => ({
  useFeatures: () => ({ features: mockFeatures, loading: false }),
  FeaturesProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// All other providers become passthroughs so App can mount without
// reaching real context machinery (API calls, websockets, etc.).
vi.mock('./contexts/ConfigContext', () => ({
  ConfigProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useConfig: () => ({ config: null, loading: false, error: null, reloadConfig: vi.fn() }),
}));
vi.mock('./contexts/SessionsContext', () => ({
  SessionsProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSessions: () => ({ workspaces: [], sessionsById: {}, loading: false, connected: true }),
}));
vi.mock('./contexts/ViewedSessionsContext', () => ({
  ViewedSessionsProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./contexts/CurationContext', () => ({
  CurationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./contexts/KeyboardContext', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./components/ToastProvider', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./components/ModalProvider', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('./components/KeyboardHelpModal', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

// AppShell becomes a thin Outlet so route content renders without
// pulling in the heavy sidebar.
vi.mock('./components/AppShell', () => ({
  default: () => <Outlet />,
}));

// Stub every lazy-loaded page with a deterministic testid. The
// home stub is what FeatureRoute redirects to.
function stubPage(name: string) {
  return { default: () => <div data-testid={`page-${name}`}>{name}</div> };
}
vi.mock('./routes/HomePage', () => stubPage('home'));
vi.mock('./routes/SessionDetailPage', () => stubPage('session-detail'));
vi.mock('./routes/SpawnPage', () => stubPage('spawn'));
vi.mock('./routes/TipsPage', () => stubPage('tips'));
vi.mock('./routes/ConfigPage', () => stubPage('config'));
vi.mock('./routes/DiffPage', () => stubPage('diff'));
vi.mock('./routes/MarkdownPreviewPage', () => stubPage('md-preview'));
vi.mock('./routes/ImagePreviewPage', () => stubPage('img-preview'));
vi.mock('./routes/PreviewPage', () => stubPage('preview'));
vi.mock('./routes/CommitGraphPage', () => stubPage('commit-graph'));
vi.mock('./routes/CommitDetailPage', () => stubPage('commit-detail'));
vi.mock('./routes/LinearSyncResolveConflictPage', () => stubPage('linear-resolve'));
vi.mock('./routes/LegacyTerminalPage', () => stubPage('legacy-terminal'));
vi.mock('./routes/NotFoundPage', () => stubPage('not-found'));
vi.mock('./routes/OverlayPage', () => stubPage('overlay'));
vi.mock('./routes/AutolearnPage', () => stubPage('autolearn'));
vi.mock('./routes/PersonasListPage', () => stubPage('personas-list'));
vi.mock('./routes/PersonaCreatePage', () => stubPage('persona-create'));
vi.mock('./routes/PersonaEditPage', () => stubPage('persona-edit'));
vi.mock('./routes/StylesListPage', () => stubPage('styles-list'));
vi.mock('./routes/StyleCreatePage', () => stubPage('style-create'));
vi.mock('./routes/StyleEditPage', () => stubPage('style-edit'));
vi.mock('./routes/EventsPage', () => stubPage('events'));
vi.mock('./routes/RepofeedPage', () => stubPage('repofeed'));
vi.mock('./routes/TimelapsePage', () => stubPage('timelapse-list'));
vi.mock('./routes/TimelapsePlayerPage', () => stubPage('timelapse-player'));
vi.mock('./routes/EnvironmentPage', () => stubPage('environment'));

import App from './App';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>
  );
}

beforeEach(() => {
  // Restore all features to true between tests
  for (const k of Object.keys(mockFeatures) as Array<keyof Features>) {
    mockFeatures[k] = true;
  }
});

describe('App route guards', () => {
  // Mapping of feature-tied routes — must mirror App.tsx wiring.
  const guarded: Array<{
    path: string;
    feature: keyof Features;
    pageStub: string;
  }> = [
    { path: '/personas', feature: 'personas', pageStub: 'personas-list' },
    { path: '/personas/create', feature: 'personas', pageStub: 'persona-create' },
    { path: '/personas/abc-123', feature: 'personas', pageStub: 'persona-edit' },
    { path: '/styles', feature: 'comm_styles', pageStub: 'styles-list' },
    { path: '/styles/create', feature: 'comm_styles', pageStub: 'style-create' },
    { path: '/styles/abc-123', feature: 'comm_styles', pageStub: 'style-edit' },
    { path: '/autolearn', feature: 'autolearn', pageStub: 'autolearn' },
    { path: '/timelapse', feature: 'timelapse', pageStub: 'timelapse-list' },
    { path: '/timelapse/abc-123', feature: 'timelapse', pageStub: 'timelapse-player' },
    { path: '/repofeed', feature: 'repofeed', pageStub: 'repofeed' },
  ];

  for (const { path, feature, pageStub } of guarded) {
    it(`renders ${path} when features.${feature} is true`, async () => {
      renderAt(path);
      await waitFor(() => {
        expect(screen.getByTestId(`page-${pageStub}`)).toBeInTheDocument();
      });
    });

    it(`redirects ${path} to home when features.${feature} is false`, async () => {
      mockFeatures[feature] = false;
      renderAt(path);
      await waitFor(() => {
        expect(screen.getByTestId('page-home')).toBeInTheDocument();
      });
      expect(screen.queryByTestId(`page-${pageStub}`)).not.toBeInTheDocument();
    });
  }
});
