import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import { useClientPerf } from '../hooks/useClientPerf';
import { clientPerf } from '../lib/clientPerf';
import {
  ensureClientPerformanceSession,
  sendClientPerformanceRecording,
  getErrorMessage,
} from '../lib/api';
import type { ClientPerformanceSessionResponse } from '../lib/types.generated';
import styles from './ClientPerformance.module.css';

export default function ClientPerformance() {
  const { config } = useConfig();
  const { workspaces, sessionsById, waitForSession, clientPerformance, clientPerformanceError } =
    useSessions();
  const { alert, confirm } = useModal();
  const { success } = useToast();
  const navigate = useNavigate();
  const perf = useClientPerf();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('client-perf-collapsed') === '1'
  );
  const [busy, setBusy] = useState(false);
  const [tracked, setTracked] = useState<ClientPerformanceSessionResponse | null>(null);
  useEffect(() => {
    setTracked(clientPerformance ?? null);
  }, [clientPerformance]);
  useEffect(() => {
    if (clientPerformanceError) void alert('Client Performance', clientPerformanceError);
  }, [clientPerformanceError, alert]);
  const workspace = workspaces.find((w) => w.id === tracked?.workspace_id);
  const session = tracked?.session_id ? sessionsById[tracked.session_id] : undefined;
  const agent = session?.running ? session : undefined;
  const configured = Boolean(config.client_performance?.repo && config.client_performance?.target);
  const stage =
    perf.startedAt !== null ? 'send' : !configured ? 'configure' : !agent ? 'launch' : 'start';
  const viewSessionId = perf.startedAt !== null ? perf.chat?.sessionId : session?.id;
  const seconds = Math.floor(perf.elapsedMs / 1000);
  const duration = `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;

  const launch = async () => {
    setBusy(true);
    try {
      const ids = await ensureClientPerformanceSession({
        workspace_id: workspace?.id ?? '',
      });
      clientPerf.setChat({ workspaceId: ids.workspace_id, sessionId: ids.session_id });
      setTracked(ids);
      await waitForSession(ids.session_id);
      navigate(`/sessions/${ids.session_id}`);
    } catch (err) {
      await alert('Launch Failed', getErrorMessage(err, 'Failed to launch workspace'));
    } finally {
      setBusy(false);
    }
  };

  const send = async () => {
    clientPerf.finish();
    setBusy(true);
    try {
      if (!perf.chat?.sessionId) throw new Error('The recording has no associated agent session.');
      const data = clientPerf.buildFile();
      await sendClientPerformanceRecording(
        perf.chat.sessionId,
        new File([JSON.stringify(data)], `client-perf-${data.builtAt}-${data.browserId}.json`, {
          type: 'application/json',
        })
      );
      clientPerf.stop();
      success('Recording sent');
      navigate(`/sessions/${perf.chat.sessionId}`);
    } catch (err) {
      await alert('Send Failed', getErrorMessage(err, 'Failed to send recording'));
    } finally {
      setBusy(false);
    }
  };

  const discard = async () => {
    if (await confirm('Discard this recording?', { danger: true })) clientPerf.stop();
  };

  return (
    <div className={`client-perf ${styles.panel}`} data-testid="client-perf-pane">
      <div className="typing-perf__header">
        <button
          className="btn btn--ghost btn--sm diag-pane__toggle"
          aria-expanded={!collapsed}
          onClick={() =>
            setCollapsed((value) => {
              localStorage.setItem('client-perf-collapsed', value ? '0' : '1');
              return !value;
            })
          }
        >
          <span
            className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}
            aria-hidden="true"
          >
            ▶
          </span>
          <span className="nav-section-title">
            Client Performance{perf.recording ? ' · REC' : ''}
          </span>
        </button>
      </div>
      {!collapsed && (
        <div className="flex-col gap-xs">
          {perf.startedAt !== null && (
            <span className="text-muted" role="status">
              {perf.recording ? `Recording ${duration}` : 'Recording ready to send'}
            </span>
          )}
          <div className="btn-group flex-wrap" aria-busy={busy}>
            {viewSessionId && (
              <Link to={`/sessions/${viewSessionId}`} className="btn btn--secondary btn--sm">
                View
              </Link>
            )}
            {stage === 'configure' && (
              <Link to="/config?tab=advanced" className="btn btn--secondary btn--sm">
                Configure
              </Link>
            )}
            {stage === 'launch' && (
              <button className="btn btn--secondary btn--sm" onClick={launch} disabled={busy}>
                {busy ? 'Launching…' : 'Launch'}
              </button>
            )}
            {stage === 'start' && agent && (
              <button
                className="btn btn--secondary btn--sm"
                onClick={() => {
                  clientPerf.setChat({ workspaceId: agent.workspace_id, sessionId: agent.id });
                  void clientPerf.start();
                }}
                disabled={busy}
              >
                Record
              </button>
            )}
            {stage === 'send' && (
              <button className="btn btn--secondary btn--sm" disabled={busy} onClick={send}>
                {busy ? 'Sending…' : perf.recording ? 'Stop' : 'Send recording'}
              </button>
            )}
            {perf.startedAt !== null && (
              <button
                className="btn btn--sm btn--ghost btn--danger"
                disabled={busy}
                onClick={discard}
              >
                Discard
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
