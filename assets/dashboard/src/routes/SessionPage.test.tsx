import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import SessionPage from './SessionPage';
import { useSessions } from '../contexts/SessionsContext';

vi.mock('../contexts/SessionsContext', () => ({ useSessions: vi.fn() }));
vi.mock('./ChatSessionPage', () => ({
  default: () => <div data-testid="chat-session-page" />,
}));
vi.mock('./SessionDetailPage', () => ({
  default: () => <div data-testid="session-detail-page" />,
}));

const useSessionsMock = vi.mocked(useSessions);

function renderPage(id: string) {
  return render(
    <MemoryRouter initialEntries={[`/sessions/${id}`]}>
      <Routes>
        <Route path="/sessions/:sessionId" element={<SessionPage />} />
      </Routes>
    </MemoryRouter>
  );
}

describe('SessionPage', () => {
  beforeEach(() => {
    useSessionsMock.mockReturnValue({
      sessionsById: {
        'chat-1': { id: 'chat-1', kind: 'chat', workspace_id: 'ws-1' },
        'term-1': { id: 'term-1', workspace_id: 'ws-1' },
      },
      workspaces: [{ id: 'ws-1', sessions: [] }],
    } as unknown as ReturnType<typeof useSessions>);
  });

  it('renders the chat page for a chat-kind session', () => {
    renderPage('chat-1');
    expect(screen.getByTestId('chat-session-page')).toBeInTheDocument();
    expect(screen.queryByTestId('session-detail-page')).not.toBeInTheDocument();
  });

  it('renders the terminal page for a terminal session', () => {
    renderPage('term-1');
    expect(screen.getByTestId('session-detail-page')).toBeInTheDocument();
    expect(screen.queryByTestId('chat-session-page')).not.toBeInTheDocument();
  });

  it('renders the terminal page while the session is unknown', () => {
    renderPage('nope');
    expect(screen.getByTestId('session-detail-page')).toBeInTheDocument();
  });
});
