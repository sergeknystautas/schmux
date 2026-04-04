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
import HomePage from './routes/HomePage';
import SpawnPage from './routes/SpawnPage';
import TipsPage from './routes/TipsPage';
import ConfigPage from './routes/ConfigPage';
import RemoteSettingsPage from './routes/RemoteSettingsPage';
import SessionDetailPage from './routes/SessionDetailPage';
import DiffPage from './routes/DiffPage';
import MarkdownPreviewPage from './routes/MarkdownPreviewPage';
import ImagePreviewPage from './routes/ImagePreviewPage';
import PreviewPage from './routes/PreviewPage';
import CommitGraphPage from './routes/CommitGraphPage';
import CommitDetailPage from './routes/CommitDetailPage';
import LinearSyncResolveConflictPage from './routes/LinearSyncResolveConflictPage';
import LegacyTerminalPage from './routes/LegacyTerminalPage';
import NotFoundPage from './routes/NotFoundPage';
import OverlayPage from './routes/OverlayPage';
import LorePage from './routes/LorePage';
import PersonasListPage from './routes/PersonasListPage';
import PersonaCreatePage from './routes/PersonaCreatePage';
import PersonaEditPage from './routes/PersonaEditPage';
import EventsPage from './routes/EventsPage';
import RepofeedPage from './routes/RepofeedPage';
import TimelapsePage from './routes/TimelapsePage';
import EnvironmentPage from './routes/EnvironmentPage';

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
                          <Route path="/settings/remote" element={<RemoteSettingsPage />} />
                          <Route path="/environment" element={<EnvironmentPage />} />
                          <Route path="/overlays" element={<OverlayPage />} />
                          <Route path="/personas" element={<PersonasListPage />} />
                          <Route path="/personas/create" element={<PersonaCreatePage />} />
                          <Route path="/personas/:personaId" element={<PersonaEditPage />} />
                          <Route path="/lore" element={<LorePage />} />
                          <Route path="/events" element={<EventsPage />} />
                          <Route path="/timelapse" element={<TimelapsePage />} />
                          <Route path="/repofeed" element={<RepofeedPage />} />
                          <Route path="/terminal.html" element={<LegacyTerminalPage />} />
                          <Route path="*" element={<NotFoundPage />} />
                        </Route>
                      </Routes>
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
