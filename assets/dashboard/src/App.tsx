import { Routes, Route, useLocation } from 'react-router-dom';
import AppShell from './components/AppShell';
import ToastProvider from './components/ToastProvider';
import ModalProvider from './components/ModalProvider';
import HelpModalProvider from './components/KeyboardHelpModal';
import KeyboardProvider from './contexts/KeyboardContext';
import { ConfigProvider } from './contexts/ConfigContext';
import { SessionsProvider } from './contexts/SessionsContext';
import { ViewedSessionsProvider } from './contexts/ViewedSessionsContext';
import HomePage from './routes/HomePage';
import SpawnPage from './routes/SpawnPage';
import TipsPage from './routes/TipsPage';
import ConfigPage from './routes/ConfigPage';
import RemoteSettingsPage from './routes/RemoteSettingsPage';
import SessionDetailPage from './routes/SessionDetailPage';
import DiffPage from './routes/DiffPage';
import PreviewPage from './routes/PreviewPage';
import GitGraphPage from './routes/GitGraphPage';
import LinearSyncResolveConflictPage from './routes/LinearSyncResolveConflictPage';
import LegacyTerminalPage from './routes/LegacyTerminalPage';
import NotFoundPage from './routes/NotFoundPage';
import OverlayPage from './routes/OverlayPage';
import LorePage from './routes/LorePage';

export default function App() {
  const location = useLocation();
  return (
    <ConfigProvider>
      <ToastProvider>
        <ModalProvider>
          <HelpModalProvider>
            <SessionsProvider>
              <ViewedSessionsProvider>
                <KeyboardProvider>
                  <Routes>
                    <Route element={<AppShell />}>
                      <Route path="/" element={<HomePage />} />
                      <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
                      <Route path="/diff/:workspaceId" element={<DiffPage />} />
                      <Route path="/preview/:workspaceId/:previewId" element={<PreviewPage />} />
                      <Route path="/git/:workspaceId" element={<GitGraphPage />} />
                      <Route
                        path="/resolve-conflict/:workspaceId"
                        element={<LinearSyncResolveConflictPage />}
                      />
                      <Route path="/spawn" element={<SpawnPage key={location.key} />} />
                      <Route path="/tips" element={<TipsPage />} />
                      <Route path="/config" element={<ConfigPage />} />
                      <Route path="/settings/remote" element={<RemoteSettingsPage />} />
                      <Route path="/overlays" element={<OverlayPage />} />
                      <Route path="/lore" element={<LorePage />} />
                      <Route path="/terminal.html" element={<LegacyTerminalPage />} />
                      <Route path="*" element={<NotFoundPage />} />
                    </Route>
                  </Routes>
                </KeyboardProvider>
              </ViewedSessionsProvider>
            </SessionsProvider>
          </HelpModalProvider>
        </ModalProvider>
      </ToastProvider>
    </ConfigProvider>
  );
}
