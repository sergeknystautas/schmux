import { useParams } from 'react-router';
import { useSessions } from '../contexts/SessionsContext';
import SessionDetailPage from './SessionDetailPage';
import ChatSessionPage from './ChatSessionPage';

// Route switch: a chat-kind session renders the conversation page, everything
// else renders the terminal page exactly as before.
export default function SessionPage() {
  const { sessionId } = useParams();
  const { sessionsById } = useSessions();
  const session = sessionId ? sessionsById[sessionId] : undefined;
  if (session?.kind === 'chat') {
    return <ChatSessionPage />;
  }
  return <SessionDetailPage />;
}
