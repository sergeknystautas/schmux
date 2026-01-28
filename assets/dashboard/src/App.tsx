import { Routes, Route } from 'react-router-dom';
import AppShell from './components/AppShell'
import ToastProvider from './components/ToastProvider'
import ModalProvider from './components/ModalProvider'
import { ConfigProvider } from './contexts/ConfigContext'
import { SessionsProvider } from './contexts/SessionsContext'
import { ViewedSessionsProvider } from './contexts/ViewedSessionsContext'
import SpawnPage from './routes/SpawnPage'
import TipsPage from './routes/TipsPage'
import ConfigPage from './routes/ConfigPage'
import SessionDetailPage from './routes/SessionDetailPage'
import DiffPage from './routes/DiffPage'
import LegacyTerminalPage from './routes/LegacyTerminalPage'
import NotFoundPage from './routes/NotFoundPage'

export default function App() {
  return (
    <ConfigProvider>
      <ToastProvider>
        <ModalProvider>
          <SessionsProvider>
            <ViewedSessionsProvider>
              <Routes>
                <Route element={<AppShell />}>
                  <Route path="/" element={<TipsPage />} />
                  <Route path="/sessions/:sessionId" element={<SessionDetailPage />} />
                  <Route path="/diff/:workspaceId" element={<DiffPage />} />
                  <Route path="/spawn" element={<SpawnPage />} />
                  <Route path="/tips" element={<TipsPage />} />
                  <Route path="/config" element={<ConfigPage />} />
                  <Route path="/terminal.html" element={<LegacyTerminalPage />} />
                  <Route path="*" element={<NotFoundPage />} />
                </Route>
              </Routes>
            </ViewedSessionsProvider>
          </SessionsProvider>
        </ModalProvider>
      </ToastProvider>
    </ConfigProvider>
  );
}
