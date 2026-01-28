import React, { useEffect } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

export default function LegacyTerminalPage() {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const sessionId = searchParams.get('id');

  useEffect(() => {
    if (sessionId) {
      navigate(`/sessions/${sessionId}`, { replace: true });
    }
  }, [navigate, sessionId]);

  if (sessionId) {
    return null;
  }

  return (
    <div className="empty-state">
      <div className="empty-state__icon">⚠️</div>
      <h3 className="empty-state__title">No session ID provided</h3>
      <p className="empty-state__description">Use the sessions list to open a terminal view.</p>
      <Link to="/" className="btn btn--primary">Back to Home</Link>
    </div>
  );
}
