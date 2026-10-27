import { useState } from 'react';
import type {
  FlightMessage,
  FlightMonitor,
  FlightMonitoredTurn,
  FlightSessionAssignment,
} from '../../lib/types.generated';
import { retryFlightMonitor } from '../../lib/api';
import { roleLabel } from '../../lib/flights';
import { formatTimestamp } from '../../lib/utils';

interface Props {
  flightID: string;
  sessions: FlightSessionAssignment[];
  monitor: FlightMonitor;
}

const MESSAGE_KIND_LABEL: Record<string, string> = {
  notification: 'Notification',
  execution_notice: 'Execution notice',
  review_notice: 'Review notice',
  rejection: 'Rejected decision',
  pilot_message: 'Pilot message',
  next_reviewer: 'Next reviewer',
  pilot_stop: 'Pilot stop',
};

/** The role of the assignment that holds `sessionId`, now or before a restart. */
function recipientRole(sessions: FlightSessionAssignment[], sessionId: string): string | null {
  const assignment = sessions.find(
    (s) => s.session_id === sessionId || s.prior_session_ids?.includes(sessionId)
  );
  return assignment ? roleLabel(assignment.role) : null;
}

// FlightMonitorHistory renders the durable evidence the monitor retains for a
// flight: completed turns, classification attempts, notifications, and
// pilot messages. The view is intentionally compact: the dashboard is for
// triage, not a transcript browser.
export default function FlightMonitorHistory({ flightID, sessions, monitor }: Props) {
  const turns = monitor.turns ?? [];
  const messages = monitor.messages ?? [];
  const [retrying, setRetrying] = useState<string | null>(null);
  const [error, setError] = useState('');

  if (turns.length === 0 && messages.length === 0 && !monitor.error) {
    return (
      <div className="card__body">
        <p className="m-0 text-muted">No monitor activity yet.</p>
      </div>
    );
  }

  const handleRetry = async (itemID: string) => {
    setRetrying(itemID);
    setError('');
    try {
      await retryFlightMonitor(flightID, itemID);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'retry failed');
    } finally {
      setRetrying(null);
    }
  };

  return (
    <div className="card__body flex-col gap-md">
      {monitor.error && (
        <p className="form-group__error m-0" role="alert" data-testid="flight-monitor-error">
          {monitor.error}
        </p>
      )}
      {error && (
        <p className="form-group__error m-0" role="alert">
          {error}
        </p>
      )}
      {turns.length > 0 && (
        <div className="flex-col gap-sm">
          <h3 className="settings-section__title m-0">Completed turns</h3>
          {turns.map((turn) => (
            <TurnRow
              key={turn.id}
              turn={turn}
              onRetry={() => handleRetry(turn.id)}
              retrying={retrying === turn.id}
            />
          ))}
        </div>
      )}
      {messages.length > 0 && (
        <div className="flex-col gap-sm">
          <h3 className="settings-section__title m-0">Messages</h3>
          {messages.map((msg) => (
            <MessageRow
              key={msg.id}
              msg={msg}
              recipient={recipientRole(sessions, msg.recipient_session_id)}
              onRetry={() => handleRetry(msg.id)}
              retrying={retrying === msg.id}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function TurnRow({
  turn,
  onRetry,
  retrying,
}: {
  turn: FlightMonitoredTurn;
  onRetry: () => void;
  retrying: boolean;
}) {
  const [open, setOpen] = useState(false);
  const canRetry = turn.status === 'failed';
  return (
    <div className="card" data-testid="flight-monitor-turn">
      <div className="card__header">
        <div className="flex-row gap-sm flex-wrap">
          <button
            type="button"
            className="btn btn--ghost btn--bordered"
            onClick={() => setOpen((v) => !v)}
            data-testid="flight-monitor-turn-toggle"
          >
            {open ? 'Hide' : 'Show'} response
          </button>
          <span className="m-0 font-normal text-muted">
            turn {turn.id} · session {turn.session_id} · status {turn.status}
          </span>
          {turn.artifacts && (
            <span
              className={`status-pill status-pill--sm ${turn.artifacts.error ? 'status-pill--error' : 'status-pill--running'}`}
              data-testid="flight-monitor-turn-artifacts"
            >
              <span className="status-pill__dot" />
              {turn.artifacts.error ? 'spec/plan check failed' : 'spec/plan verified'}
            </span>
          )}
          {turn.execution && (
            <span
              className={`status-pill status-pill--sm ${turn.execution.report_verified ? 'status-pill--running' : 'status-pill--error'}`}
              data-testid="flight-monitor-turn-execution"
            >
              <span className="status-pill__dot" />
              {turn.execution.report_verified
                ? 'report verified'
                : turn.execution.report_error === 'not written yet'
                  ? 'report not written'
                  : 'report check failed'}
            </span>
          )}
          {turn.review && (
            <span
              className={`status-pill status-pill--sm ${turn.review.skill.includes('not found') ? 'status-pill--error' : 'status-pill--running'}`}
              data-testid="flight-monitor-turn-review"
            >
              <span className="status-pill__dot" />
              {turn.review.skill}
            </span>
          )}
        </div>
        {canRetry && (
          <button
            type="button"
            className="btn btn--ghost btn--bordered"
            onClick={onRetry}
            disabled={retrying}
            data-testid="flight-monitor-turn-retry"
          >
            {retrying ? 'Retrying…' : 'Retry'}
          </button>
        )}
      </div>
      {open && (
        <div className="card__body flex-col gap-sm">
          <p className="m-0 text-muted">
            Outcome: {turn.outcome}
            {turn.error ? ` — ${turn.error}` : ''}
          </p>
          {turn.artifacts && (
            <div className="flex-col gap-xs" data-testid="flight-monitor-turn-report">
              <p className="m-0 mono">Spec: {turn.artifacts.spec_path || '(not reported)'}</p>
              <p className="m-0 mono">Plan: {turn.artifacts.plan_path || '(not reported)'}</p>
              {turn.artifacts.error && (
                <p className="m-0 form-group__error">{turn.artifacts.error}</p>
              )}
            </div>
          )}
          {turn.execution && (
            <div className="flex-col gap-xs" data-testid="flight-monitor-turn-execution-detail">
              <p className="m-0 mono">Skill check: {turn.execution.skill}</p>
              <p className="m-0 mono">
                Report check:{' '}
                {turn.execution.report_verified
                  ? `verified: ${turn.execution.report_path}`
                  : turn.execution.report_error}
              </p>
            </div>
          )}
          {turn.text && (
            <pre className="mono text-pre-wrap m-0" data-testid="flight-monitor-turn-text">
              {turn.text}
            </pre>
          )}
          {(turn.attempts ?? []).map((a, i) => (
            <div key={i} className="flex-col gap-xs">
              <p className="m-0 text-muted">
                {a.error ? `attempt failed: ${a.error}` : `attempt ${i + 1}`} (
                {formatTimestamp(a.created_at)})
              </p>
              {(a.answers ?? []).map((ans) => (
                <p className="m-0 mono" key={ans.id}>
                  {ans.id}: {ans.choice}
                  {ans.confidence !== undefined && ans.confidence !== null
                    ? ` (confidence ${ans.confidence.toFixed(2)})`
                    : ''}
                </p>
              ))}
              {(a.questions ?? []).map((question) => (
                <div key={question.id}>
                  <p className="m-0">
                    {question.id}: {question.question}
                  </p>
                  <ul>
                    {question.choices.map((choice) => (
                      <li key={choice.id}>
                        {choice.id}: {choice.description}
                      </li>
                    ))}
                  </ul>
                  <p className="m-0 text-muted">
                    Notify on: {question.notify_on.join(', ')}; minimum confidence:{' '}
                    {question.min_confidence}
                  </p>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function MessageRow({
  msg,
  recipient,
  onRetry,
  retrying,
}: {
  msg: FlightMessage;
  recipient: string | null;
  onRetry: () => void;
  retrying: boolean;
}) {
  const canRetry = msg.status === 'failed';
  return (
    <div className="card" data-testid="flight-monitor-message">
      <div className="card__header">
        <div className="flex-row gap-sm flex-wrap">
          <span className="m-0 font-normal" data-testid="flight-monitor-message-recipient">
            {MESSAGE_KIND_LABEL[msg.kind] ?? msg.kind} to {recipient ?? msg.recipient_session_id}
          </span>
          <span className="m-0 text-muted">session {msg.recipient_session_id}</span>
          <span className="m-0 text-muted">status {msg.status}</span>
        </div>
        {canRetry && (
          <button
            type="button"
            className="btn btn--ghost btn--bordered"
            onClick={onRetry}
            disabled={retrying}
            data-testid="flight-monitor-message-retry"
          >
            {retrying ? 'Retrying…' : 'Retry'}
          </button>
        )}
      </div>
      <div className="card__body">
        <pre className="mono text-pre-wrap m-0">{msg.text}</pre>
        {msg.error && <p className="form-group__error m-0">{msg.error}</p>}
        {msg.chat_message_id && (
          <p className="m-0 text-muted">chat_message_id: {msg.chat_message_id}</p>
        )}
      </div>
    </div>
  );
}
