import { Routes, Route } from 'react-router-dom';
import AppShell from './components/AppShell.jsx';
import ToastProvider from './components/ToastProvider.jsx';
import ModalProvider from './components/ModalProvider.jsx';
import { ConfigProvider } from './contexts/ConfigContext.jsx';
import { ViewedSessionsProvider } from './contexts/ViewedSessionsContext.jsx';
import SessionsPage from './routes/SessionsPage.jsx';
import WorkspacesPage from './routes/WorkspacesPage.jsx';
import SpawnPage from './routes/SpawnPage.jsx';
import TipsPage from './routes/TipsPage.jsx';
import SessionDetailPage from './routes/SessionDetailPage.jsx';
import DiffPage from './routes/DiffPage.jsx';
import LegacyTerminalPage from './routes/LegacyTerminalPage.jsx';
import NotFoundPage from './routes/NotFoundPage.jsx';

export default function App() {
  return (
    <ConfigProvider>
      <ToastProvider>
        <ModalProvider>
          <ViewedSessionsProvider>
            <Routes>
              <Route element={<AppShell />}>
                <Route path="/" element={<SessionsPage />} />
                <Route path="/sessions" element={<SessionsPage />} />
                <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
                <Route path="/workspaces" element={<WorkspacesPage />} />
                <Route path="/diff/:workspaceId" element={<DiffPage />} />
                <Route path="/spawn" element={<SpawnPage />} />
                <Route path="/tips" element={<TipsPage />} />
                <Route path="/terminal.html" element={<LegacyTerminalPage />} />
                <Route path="*" element={<NotFoundPage />} />
              </Route>
            </Routes>
          </ViewedSessionsProvider>
        </ModalProvider>
      </ToastProvider>
    </ConfigProvider>
  );
}
