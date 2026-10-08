import { useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useModal } from './ModalProvider';
import { useClientPerf } from '../hooks/useClientPerf';
import { clientPerf } from '../lib/clientPerf';
import { ensureClientPerformanceSession, getErrorMessage } from '../lib/api';

export default function ClientPerformance() {
  const { config } = useConfig();
  const { waitForSession } = useSessions();
  const { confirm } = useModal();
  const navigate = useNavigate();
  const perf = useClientPerf();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('client-perf-collapsed') === '1'
  );
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const toggleCollapsed = () =>
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('client-perf-collapsed', next ? '1' : '0');
      return next;
    });

  const configured = Boolean(config.client_performance?.repo && config.client_performance?.target);
  const minutes = Math.floor(perf.elapsedMs / 60000);

  const openChat = async () => {
    setOpening(true);
    setError(null);
    try {
      const kept = clientPerf.getChat();
      const ids = await ensureClientPerformanceSession({
        workspace_id: kept?.workspaceId ?? '',
        session_id: kept?.sessionId ?? '',
      });
      clientPerf.setChat({ workspaceId: ids.workspace_id, sessionId: ids.session_id });
      await waitForSession(ids.session_id);
      navigate(`/sessions/${ids.session_id}`);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to open performance chat'));
    } finally {
      setOpening(false);
    }
  };

  const stop = async () => {
    if (perf.unsent) {
      const ok = await confirm(
        'Stop recording? The current recording has not been sent and will be discarded.',
        { danger: true }
      );
      if (!ok) return;
    }
    clientPerf.stop();
  };

  return (
    <div className="client-perf" data-testid="client-perf-pane">
      <div className="client-perf__header">
        <button className="diag-pane__toggle" onClick={toggleCollapsed}>
          <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
            ▶
          </span>
          <span className="nav-section-title">
            {perf.recording ? 'Client Performance · REC' : 'Client Performance'}
          </span>
        </button>
      </div>
      {!collapsed && !perf.recording && (
        <div className="client-perf__body">
          <p className="client-perf__text">
            Records what this browser is doing while the dashboard feels slow. Recordings are sent
            to an agent in a chat.
          </p>
          <button className="btn btn--secondary btn--sm" onClick={() => clientPerf.start()}>
            Start recording
          </button>
        </div>
      )}
      {!collapsed && perf.recording && (
        <div className="client-perf__body">
          <div className="client-perf__status">{`Recording ${minutes} min · ${perf.stalls} stalls`}</div>
          {configured ? (
            <button className="btn btn--secondary btn--sm" onClick={openChat} disabled={opening}>
              Open performance chat
            </button>
          ) : (
            <Link className="client-perf__hint" to="/config?tab=advanced">
              Pick a repo and target on the Config page, Advanced tab.
            </Link>
          )}
          {error && (
            <div className="client-perf__error" role="alert">
              {error}
            </div>
          )}
          <button className="btn btn--ghost btn--sm" onClick={stop}>
            Stop recording
          </button>
        </div>
      )}
    </div>
  );
}
