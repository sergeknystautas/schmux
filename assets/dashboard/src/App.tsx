import { lazy, Suspense } from 'react';
import { Routes, Route, useLocation } from 'react-router-dom';
import AppShell from './components/AppShell';
import ToastProvider from './components/ToastProvider';
import ModalProvider from './components/ModalProvider';
import HelpModalProvider from './components/KeyboardHelpModal';
import KeyboardProvider from './contexts/KeyboardContext';
import { ConfigProvider } from './contexts/ConfigContext';
import { FeaturesProvider } from './contexts/FeaturesContext';
import { SessionsProvider } from './contexts/SessionsContext';
import { ViewedSessionsProvider } from './contexts/ViewedSessionsContext';
import { CurationProvider } from './contexts/CurationContext';

// Eager: primary views that users hit immediately
import HomePage from './routes/HomePage';
import SessionDetailPage from './routes/SessionDetailPage';

// Lazy: secondary pages loaded on demand
const SpawnPage = lazy(() => import('./routes/SpawnPage'));
const TipsPage = lazy(() => import('./routes/TipsPage'));
const ConfigPage = lazy(() => import('./routes/ConfigPage'));
const DiffPage = lazy(() => import('./routes/DiffPage'));
const MarkdownPreviewPage = lazy(() => import('./routes/MarkdownPreviewPage'));
const ImagePreviewPage = lazy(() => import('./routes/ImagePreviewPage'));
const PreviewPage = lazy(() => import('./routes/PreviewPage'));
const CommitGraphPage = lazy(() => import('./routes/CommitGraphPage'));
const CommitDetailPage = lazy(() => import('./routes/CommitDetailPage'));
const LinearSyncResolveConflictPage = lazy(() => import('./routes/LinearSyncResolveConflictPage'));
const LegacyTerminalPage = lazy(() => import('./routes/LegacyTerminalPage'));
const NotFoundPage = lazy(() => import('./routes/NotFoundPage'));
const OverlayPage = lazy(() => import('./routes/OverlayPage'));
const AutolearnPage = lazy(() => import('./routes/AutolearnPage'));
const PersonasListPage = lazy(() => import('./routes/PersonasListPage'));
const PersonaCreatePage = lazy(() => import('./routes/PersonaCreatePage'));
const PersonaEditPage = lazy(() => import('./routes/PersonaEditPage'));
const StylesListPage = lazy(() => import('./routes/StylesListPage'));
const StyleCreatePage = lazy(() => import('./routes/StyleCreatePage'));
const StyleEditPage = lazy(() => import('./routes/StyleEditPage'));
const EventsPage = lazy(() => import('./routes/EventsPage'));
const RepofeedPage = lazy(() => import('./routes/RepofeedPage'));
const TimelapsePage = lazy(() => import('./routes/TimelapsePage'));
const EnvironmentPage = lazy(() => import('./routes/EnvironmentPage'));

export default function App() {
  const location = useLocation();
  return (
    <ConfigProvider>
      <FeaturesProvider>
        <ToastProvider>
          <ModalProvider>
            <HelpModalProvider>
              <SessionsProvider>
                <ViewedSessionsProvider>
                  <KeyboardProvider>
                    <CurationProvider>
                      <Suspense fallback={null}>
                        <Routes>
                          <Route element={<AppShell />}>
                            <Route path="/" element={<HomePage />} />
                            <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
                            <Route
                              path="/diff/:workspaceId/img/:filepath"
                              element={<ImagePreviewPage />}
                            />
                            <Route
                              path="/diff/:workspaceId/md/:filepath"
                              element={<MarkdownPreviewPage />}
                            />
                            <Route path="/diff/:workspaceId" element={<DiffPage />} />
                            <Route
                              path="/preview/:workspaceId/:previewId"
                              element={<PreviewPage />}
                            />
                            <Route path="/commits/:workspaceId" element={<CommitGraphPage />} />
                            <Route
                              path="/commits/:workspaceId/:commitHash"
                              element={<CommitDetailPage />}
                            />
                            <Route
                              path="/resolve-conflict/:workspaceId/:tabId"
                              element={<LinearSyncResolveConflictPage />}
                            />
                            <Route path="/spawn" element={<SpawnPage key={location.key} />} />
                            <Route path="/tips" element={<TipsPage />} />
                            <Route path="/config" element={<ConfigPage />} />
                            <Route path="/environment" element={<EnvironmentPage />} />
                            <Route path="/overlays" element={<OverlayPage />} />
                            <Route path="/personas" element={<PersonasListPage />} />
                            <Route path="/personas/create" element={<PersonaCreatePage />} />
                            <Route path="/personas/:personaId" element={<PersonaEditPage />} />
                            <Route path="/styles" element={<StylesListPage />} />
                            <Route path="/styles/create" element={<StyleCreatePage />} />
                            <Route path="/styles/:styleId" element={<StyleEditPage />} />
                            <Route path="/autolearn" element={<AutolearnPage />} />
                            <Route path="/events" element={<EventsPage />} />
                            <Route path="/timelapse" element={<TimelapsePage />} />
                            <Route path="/repofeed" element={<RepofeedPage />} />
                            <Route path="/terminal.html" element={<LegacyTerminalPage />} />
                            <Route path="*" element={<NotFoundPage />} />
                          </Route>
                        </Routes>
                      </Suspense>
                    </CurationProvider>
                  </KeyboardProvider>
                </ViewedSessionsProvider>
              </SessionsProvider>
            </HelpModalProvider>
          </ModalProvider>
        </ToastProvider>
      </FeaturesProvider>
    </ConfigProvider>
  );
}
